package webhook_test

// T-362's persistence and bus legs: the 018 tables' CRUD round-trip (over
// a real migrated sqlite database, the replication-store posture), the
// Emit seam's matching/envelope/outbox chain, the entitlement gate's
// degradation (DENIED = nothing enqueued, AC-5), and the synchronous
// send path the test endpoint drives (envelope field-for-field, the
// secret dual-state header, SSRF default-refusal, no redirect following).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// openStore opens a migrated metadata database and the webhook store's own
// handle onto it (the openReplicationDB posture).
func openStore(t *testing.T) (*webhook.SQLiteStore, metadata.Store, string) {
	t.Helper()
	path := t.TempDir() + "/binflow.db"
	md, err := metadata.Open(context.Background(), metadata.Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(" + itoa(metadata.BusyTimeoutMs) + ")"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return webhook.NewSQLiteStore(db), md, path
}

func itoa(n int) string { return strconv.Itoa(n) }

func allowGate() func(context.Context) bool {
	return func(context.Context) bool { return true }
}

func denyGate() func(context.Context) bool {
	return func(context.Context) bool { return false }
}

func mustParse(t *testing.T, body string) *webhook.SubscriptionRequest {
	t.Helper()
	return mustParseCipher(t, body, nil)
}

func mustParseCipher(t *testing.T, body string, cipher webhook.Cipher) *webhook.SubscriptionRequest {
	t.Helper()
	req, err := webhook.ParseSubscriptionRequest([]byte(body), cipher)
	if err != nil {
		t.Fatalf("parse subscription: %v", err)
	}
	return req
}

const goodSubscription = `{
	"key": "ci-deploys",
	"description": "CI deploy notifications",
	"enabled": true,
	"event_filter": {"domain": "artifact", "event_types": ["deployed", "deleted"], "criteria": {"anyLocal": true}},
	"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
}`

func TestT362StoreCRUDRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openStore(t)
	bus, err := webhook.NewBus(webhook.BusOptions{Store: store, Gate: allowGate()})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	sub, err := bus.Create(ctx, mustParse(t, goodSubscription), "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sub.Enabled != true || sub.Domain != "artifact" || len(sub.EventTypes) != 2 {
		t.Fatalf("created shape mismatch: %+v", sub)
	}

	// Duplicate key refuses.
	if _, err := bus.Create(ctx, mustParse(t, goodSubscription), "admin"); !errors.Is(err, webhook.ErrDuplicateKey) {
		t.Fatalf("duplicate create = %v, want ErrDuplicateKey", err)
	}

	// Get echoes the stored row (secret absent).
	got, err := bus.Get(ctx, "ci-deploys")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	view := got.View()
	if view.Key != "ci-deploys" || view.Enabled != true || len(view.Handlers) != 1 {
		t.Fatalf("echo shape: %+v", view)
	}
	if view.Handlers[0].Secret != "" {
		t.Fatalf("absent secret must echo empty, got %q", view.Handlers[0].Secret)
	}

	// Update flips enabled, keeps key immutable refusal.
	updated := strings.Replace(goodSubscription, `"enabled": true`, `"enabled": false`, 1)
	if _, err := bus.Update(ctx, "ci-deploys", mustParse(t, updated), "admin"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = bus.Get(ctx, "ci-deploys")
	if got.Enabled {
		t.Fatal("update did not flip enabled")
	}
	renamed := strings.Replace(goodSubscription, `"ci-deploys"`, `"other-key"`, 1)
	if _, err := bus.Update(ctx, "ci-deploys", mustParse(t, renamed), "admin"); !errors.Is(err, webhook.ErrValidation) {
		t.Fatalf("key rename = %v, want validation refusal", err)
	}

	// Missing key answers the endpoint table's sentinel.
	if _, err := bus.Get(ctx, "nope"); !errors.Is(err, webhook.ErrNotFound) {
		t.Fatalf("get missing = %v, want ErrNotFound", err)
	}
	if err := bus.Delete(ctx, "nope"); !errors.Is(err, webhook.ErrNotFound) {
		t.Fatalf("delete missing = %v, want ErrNotFound", err)
	}

	// Delete drops the row and its event rows.
	if err := bus.Delete(ctx, "ci-deploys"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	subs, err := bus.List(ctx)
	if err != nil || len(subs) != 0 {
		t.Fatalf("list after delete = %v %v, want empty", subs, err)
	}
}

func TestT362EmitMatchAndEnvelope(t *testing.T) {
	ctx := context.Background()
	store, md, _ := openStore(t)
	// One local repo row so anyLocal's class arm resolves.
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "maven-local", Type: "local", PackageType: "maven",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	bus, err := webhook.NewBus(webhook.BusOptions{
		Store: store, Gate: allowGate(), Repos: md.Repos(),
		Origin: "https://binflow.example.com", NodeID: "01TESTNODE",
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	sub, err := bus.Create(ctx, mustParse(t, goodSubscription), "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	bus.Emit(ctx, webhook.Event{
		Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed,
		Repo: "maven-local", Path: "com/acme/app/1.0/app-1.0.jar",
		Sha256: "abc123", Size: 42,
		Actor: webhook.Actor{ID: "ci-bot", IsToken: true, Realm: "internal"},
	})
	if n, err := bus.PendingCount(ctx); err != nil || n != 1 {
		t.Fatalf("pending = %d (%v), want 1", n, err)
	}
	rows, err := store.ListDeliveries(ctx, sub.ID, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("deliveries = %v (%v)", rows, err)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(rows[0].Payload), &env); err != nil {
		t.Fatalf("payload: %v", err)
	}
	// The seven documented envelope fields (webhook.md section 4).
	if env["domain"] != "artifact" || env["event_type"] != "deployed" {
		t.Fatalf("domain/type: %v/%v", env["domain"], env["event_type"])
	}
	if env["subscription_key"] != "ci-deploys" {
		t.Fatalf("subscription_key: %v", env["subscription_key"])
	}
	if env["jpd_origin"] != "https://binflow.example.com" {
		t.Fatalf("jpd_origin: %v", env["jpd_origin"])
	}
	if src, _ := env["source"].(string); !strings.HasPrefix(src, "binflow/binflow@") {
		t.Fatalf("source: %v", env["source"])
	}
	uc, _ := env["userContext"].(map[string]any)
	if uc == nil || uc["id"] != "ci-bot" || uc["isToken"] != true || uc["realm"] != "internal" {
		t.Fatalf("userContext: %v", env["userContext"])
	}
	data, _ := env["data"].(map[string]any)
	if data == nil || data["repo_key"] != "maven-local" ||
		data["path"] != "com/acme/app/1.0/app-1.0.jar" ||
		data["name"] != "app-1.0.jar" || data["sha256"] != "abc123" || data["size"] != float64(42) {
		t.Fatalf("data: %v", env["data"])
	}
	if _, invented := data["timestamp"]; invented {
		t.Fatal("artifact domain must carry NO timestamp field (webhook.md section 4)")
	}

	// Criteria misses enqueue nothing (AC-4's dual arms).
	bus.Emit(ctx, webhook.Event{
		Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed,
		Repo: "npm-local", Path: "x/y.js", // not in anyLocal's class family (no row)
	})
	bus.Emit(ctx, webhook.Event{
		Domain: webhook.DomainDocker, Type: webhook.TypeDockerPushed, // type not subscribed
		Repo: "maven-local", Path: "x",
	})
	if n, _ := bus.PendingCount(ctx); n != 1 {
		t.Fatalf("pending after misses = %d, want 1", n)
	}
}

func TestT362EmitGateAndDormantDrops(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openStore(t)
	// DENIED gate: create still works (the REST plane's own gate governs
	// writes), but the weaving plane enqueues nothing (AC-5's breaker arm).
	bus, err := webhook.NewBus(webhook.BusOptions{Store: store, Gate: denyGate()})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	if _, err := bus.Create(ctx, mustParse(t, goodSubscription), "admin"); err != nil {
		t.Fatalf("create: %v", err)
	}
	bus.Emit(ctx, webhook.Event{
		Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed,
		Repo: "maven-local", Path: "a/b.jar",
	})
	if n, _ := bus.PendingCount(ctx); n != 0 {
		t.Fatalf("denied gate enqueued %d rows, want 0", n)
	}

	// Nil gate fails CLOSED the same way.
	noGate, err := webhook.NewBus(webhook.BusOptions{Store: store})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	noGate.Emit(ctx, webhook.Event{Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed})
	if n, _ := noGate.PendingCount(ctx); n != 0 {
		t.Fatalf("nil gate enqueued %d rows, want 0", n)
	}

	// Dormant types never enqueue even with the gate open.
	open, err := webhook.NewBus(webhook.BusOptions{Store: store, Gate: allowGate()})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	open.Emit(ctx, webhook.Event{Domain: webhook.DomainBuild, Type: "promoted"})
	open.Emit(ctx, webhook.Event{Domain: "no-such-domain", Type: "x"})
	if n, _ := open.PendingCount(ctx); n != 0 {
		t.Fatalf("dormant/unknown emits enqueued %d rows, want 0", n)
	}
}

func TestT362DeleteCascadesDeliveries(t *testing.T) {
	ctx := context.Background()
	store, md, _ := openStore(t)
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "maven-local", Type: "local", PackageType: "maven",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	bus, _ := webhook.NewBus(webhook.BusOptions{Store: store, Gate: allowGate(), Repos: md.Repos()})
	sub, err := bus.Create(ctx, mustParse(t, goodSubscription), "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	bus.Emit(ctx, webhook.Event{Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed, Repo: "maven-local", Path: "a"})
	if n, _ := bus.PendingCount(ctx); n != 1 {
		t.Fatalf("pending = %d, want 1", n)
	}
	if err := bus.Delete(ctx, sub.Key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n, _ := bus.PendingCount(ctx); n != 0 {
		t.Fatalf("deliveries survived their subscription's delete: %d rows", n)
	}
}

// ---- the synchronous send path ----

func TestT362SendEnvelopeAndSecretDualState(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openStore(t)

	// A loopback receiver: the bus must be explicitly opted in (the SSRF
	// default-refusal test below pins the other arm).
	var seen []capturedRequest
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, capturedRequest{
			method: r.Method,
			header: r.Header.Clone(),
			body:   string(body),
		})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	key := make([]byte, 32)
	cipher, err := remote.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	bus, err := webhook.NewBus(webhook.BusOptions{
		Store: store, Gate: allowGate(), Cipher: cipher,
		AllowPrivateTarget: true, Origin: "https://binflow.example.com",
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}

	// Passthrough mode (default): the secret itself rides the header.
	body := `{
		"key": "hook-passthrough",
		"enabled": true,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + receiver.URL + `", "secret": "s3cr3t"}]
	}`
	out, err := bus.Test(ctx, mustParseCipher(t, body, cipher), webhook.Actor{ID: "tester", Realm: "internal"})
	if err != nil {
		t.Fatalf("test send: %v", err)
	}
	if !out.OK || out.Attempt.StatusCode != 200 {
		t.Fatalf("outcome = %+v, want 200 OK", out)
	}
	if len(seen) != 1 {
		t.Fatalf("receiver saw %d requests, want 1", len(seen))
	}
	if got := seen[0].header.Get(webhook.EventAuthHeader); got != "s3cr3t" {
		t.Fatalf("passthrough auth header = %q, want the secret", got)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(seen[0].body), &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env["subscription_key"] != "hook-passthrough" || env["domain"] != "artifact" {
		t.Fatalf("test envelope: %v", env)
	}

	// Signing mode: the header carries the hex HMAC of the exact body.
	seen = nil
	signed := strings.Replace(body, `"key": "hook-passthrough"`, `"key": "hook-signed"`, 1)
	signed = strings.Replace(signed, `"secret": "s3cr3t"`, `"secret": "s3cr3t", "use_secret_for_signing": true`, 1)
	out, err = bus.Test(ctx, mustParseCipher(t, signed, cipher), webhook.Actor{ID: "tester", Realm: "internal"})
	if err != nil || !out.OK {
		t.Fatalf("signed test send: %v %+v", err, out)
	}
	if len(seen) != 1 {
		t.Fatalf("receiver saw %d requests, want 1", len(seen))
	}
	wantMac := webhook.Signature("s3cr3t", []byte(seen[0].body))
	if got := seen[0].header.Get(webhook.EventAuthHeader); got != wantMac {
		t.Fatalf("signed auth header = %q, want HMAC %q", got, wantMac)
	}

	// A receiver asserting the whole seven-field envelope again (the test
	// path's synthetic event): data carries the marker values.
	if data, _ := env["data"].(map[string]any); data == nil || data["repo_key"] != "example-repo" {
		t.Fatalf("synthetic data: %v", env["data"])
	}
}

func TestT362SendSSRFDefaultRefusal(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openStore(t)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)
	// Default posture: no AllowPrivateTarget — the loopback target is
	// refused before any connection (webhook.md 5.4's blacklist default).
	bus, _ := webhook.NewBus(webhook.BusOptions{Store: store, Gate: allowGate()})
	body := `{
		"key": "ssrf-probe",
		"enabled": true,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + receiver.URL + `"}]
	}`
	out, err := bus.Test(ctx, mustParse(t, body), webhook.Actor{ID: "tester"})
	if err != nil {
		t.Fatalf("test send: %v", err)
	}
	if out.OK || out.Attempt.StatusCode != 0 || !strings.Contains(out.Attempt.Error, "rejected") {
		t.Fatalf("loopback outcome = %+v, want SSRF refusal", out)
	}
}

func TestT362SendNoRedirectFollow(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openStore(t)
	var redirectHits, targetHits int
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hop":
			redirectHits++
			w.Header().Set("Location", "/landed")
			w.WriteHeader(http.StatusFound)
		default:
			targetHits++
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(receiver.Close)
	bus, _ := webhook.NewBus(webhook.BusOptions{
		Store: store, Gate: allowGate(), AllowPrivateTarget: true,
	})
	body := `{
		"key": "redirect-probe",
		"enabled": true,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + receiver.URL + `/hop"}]
	}`
	out, err := bus.Test(ctx, mustParse(t, body), webhook.Actor{ID: "tester"})
	if err != nil {
		t.Fatalf("test send: %v", err)
	}
	if out.OK || out.Attempt.StatusCode != http.StatusFound {
		t.Fatalf("redirect outcome = %+v, want the 302 itself", out)
	}
	if redirectHits != 1 || targetHits != 0 {
		t.Fatalf("redirect followed: hops=%d landed=%d, want 1/0", redirectHits, targetHits)
	}
}

func TestT362TroubleshootingRecords(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openStore(t)
	var ok bool
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if ok {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(receiver.Close)
	key := make([]byte, 32)
	cipher, _ := remote.NewCipher(key)
	bus, _ := webhook.NewBus(webhook.BusOptions{
		Store: store, Gate: allowGate(), Cipher: cipher, AllowPrivateTarget: true,
	})
	base := func(key string, debug bool) string {
		return `{
		"key": "` + key + `",
		"enabled": true,
		"debug": ` + strconv.FormatBool(debug) + `,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + receiver.URL + `"}]
	}`
	}

	// Failure: recorded always.
	out, err := bus.Test(ctx, mustParse(t, base("tr-1", false)), webhook.Actor{ID: "t"})
	if err != nil || out.OK {
		t.Fatalf("failing send: %v %+v", err, out)
	}
	// Success without debug: not recorded.
	ok = true
	if _, err := bus.Test(ctx, mustParse(t, base("tr-2", false)), webhook.Actor{ID: "t"}); err != nil {
		t.Fatalf("ok send: %v", err)
	}
	// Success with debug: recorded.
	if _, err := bus.Test(ctx, mustParse(t, base("tr-3", true)), webhook.Actor{ID: "t"}); err != nil {
		t.Fatalf("debug send: %v", err)
	}

	recs, err := bus.Troubleshooting(ctx, webhook.TroubleshootQuery{})
	if err != nil {
		t.Fatalf("troubleshooting: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2 (failure + debug-success)", len(recs))
	}
	var keys []string
	for _, r := range recs {
		keys = append(keys, r.Event.SubscriptionKey)
		if r.Event.ID == "" || len(r.Event.ID) != 26 {
			t.Fatalf("event id %q is not a 26-char ULID", r.Event.ID)
		}
		if r.Request.Method != "POST" || r.Request.URL == "" || r.Event.Domain != "artifact" {
			t.Fatalf("record shape: %+v", r)
		}
	}
	// Filtered read: only the named subscription.
	only, err := bus.Troubleshooting(ctx, webhook.TroubleshootQuery{Subscription: "tr-1"})
	if err != nil || len(only) != 1 || only[0].Event.SubscriptionKey != "tr-1" {
		t.Fatalf("filtered = %v (%v)", only, err)
	}
	// Window query: a start in the future matches nothing.
	future, _ := bus.Troubleshooting(ctx, webhook.TroubleshootQuery{
		Start: time.Now().Add(time.Hour).UnixMilli(),
	})
	if len(future) != 0 {
		t.Fatalf("future window = %d records, want 0", len(future))
	}
}

type capturedRequest struct {
	method string
	header http.Header
	body   string
}
