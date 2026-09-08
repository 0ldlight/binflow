package httpapi

// The outbox row-level REST legs (M17 T-496, FR-159.2 / LC-109), driven
// straight into dispatchAPI the t362 way (the router case is this
// ticket's own wiring, asserted here plus one real-chain anonymous 401
// leg through Handler()): the full dead-letter→replay chain over a
// running engine (500x4 retries exhausted → dead → replay → the consumer
// receives the stored envelope again → the row flips to delivered → the
// audit row lands), the filter/pagination contract, the role and feature
// gates, and the refusal arms (404/409/400/503, E-26 spellings).

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// outboxReceiver is the dogfood consumer: every hit logged, the status
// switch flips the fault class (the M13 t366 receiver's shape, in
// process).
type outboxReceiver struct {
	mu     sync.Mutex
	hits   int
	bodies []string
	ok     bool
	srv    *httptest.Server
}

func newOutboxReceiver() *outboxReceiver {
	rcv := &outboxReceiver{}
	rcv.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rcv.mu.Lock()
		rcv.hits++
		rcv.bodies = append(rcv.bodies, string(body))
		ok := rcv.ok
		rcv.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	return rcv
}

func (r *outboxReceiver) setOK(v bool) { r.mu.Lock(); r.ok = v; r.mu.Unlock() }
func (r *outboxReceiver) count() int   { r.mu.Lock(); defer r.mu.Unlock(); return r.hits }
func (r *outboxReceiver) lastBody() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		return ""
	}
	return r.bodies[len(r.bodies)-1]
}

// outboxStack is the full chain: migrated store, repo row, bus, RUNNING
// dispatcher (test-fast timings) and the Server with the plane wired.
type outboxStack struct {
	s      *Server
	store  *webhook.SQLiteStore
	bus    *webhook.Bus
	rcv    *outboxReceiver
	subKey string
}

func newOutboxStack(t *testing.T, eval fakeLicenseEval) *outboxStack {
	t.Helper()
	ctx := context.Background()
	path := t.TempDir() + "/binflow.db"
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "maven-local", Type: "local", PackageType: "maven",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	dsn := "file:" + url.PathEscape(path) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := webhook.NewSQLiteStore(db)
	unlocked := eval.AddonEnabled(ctx, "webhook", license.TierPro)
	bus, err := webhook.NewBus(webhook.BusOptions{
		Store:              store,
		Repos:              md.Repos(),
		Gate:               func(context.Context) bool { return unlocked },
		AllowPrivateTarget: true,
		Origin:             "https://binflow.example.com",
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	rcv := newOutboxReceiver()
	t.Cleanup(rcv.srv.Close)

	cfg := config.Defaults()
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	reg := addons.New(
		addons.Generic(), addons.Docker(), addons.Maven(), addons.Npm(), addons.Pypi(),
		addons.Properties(), addons.RepoOperations(), addons.Trashcan(),
		addons.HA(), addons.XrayIntegration(), addons.Webhook(),
	)
	s := New(Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		Webhooks: bus,
		Addons:   reg,
		License:  eval,
	}, nil)

	// The delivery engine, test-fast: five total attempts (the anchor's
	// retryCount, first counted) at a 30ms fixed wait.
	d, err := webhook.NewDispatcher(webhook.DispatcherOptions{
		Bus: bus, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RetryWait: 30 * time.Millisecond, PollInterval: 10 * time.Millisecond,
		FrequencyPerSec: 100000, BurstSize: 100000,
		TroubleshootingCleanupInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = d.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// The subscription the dead letter belongs to.
	body := `{
		"key": "dead-letter-hook",
		"enabled": true,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + rcv.srv.URL + `"}]
	}`
	req, err := webhook.ParseSubscriptionRequest([]byte(body), nil)
	if err != nil {
		t.Fatalf("parse subscription: %v", err)
	}
	if _, err := bus.Create(ctx, req, "admin"); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return &outboxStack{s: s, store: store, bus: bus, rcv: rcv, subKey: "dead-letter-hook"}
}

// outboxDo drives one request into the /api/v1 plane with a boxed
// principal (the dispatchEventAPI driver's shape).
func (st *outboxStack) outboxDo(t *testing.T, method, rest string, p *auth.Principal) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "/binflow/api/"+rest, nil)
	if p != nil {
		req = req.WithContext(withPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	// dispatchAPI receives the path only (EscapedPath excludes the query —
	// the production router's spelling).
	pathREST, _, _ := strings.Cut(rest, "?")
	st.s.dispatchAPI(rec, req, pathREST)
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func waitOutbox(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestOutboxReplayFullChain is AC-1: the dead letter (consumer 500, the
// retry budget exhausted), the REST replay, the consumer receiving the
// stored envelope again, the row's status flip, and the audit row.
func TestOutboxReplayFullChain(t *testing.T) {
	st := newOutboxStack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	ctx := context.Background()

	// Construct the dead letter: the consumer answers 500 to everything,
	// so the first try plus four retries exhaust the budget (5 attempts,
	// first counted — webhook.md 5.2).
	st.rcv.setOK(false)
	st.bus.Emit(ctx, webhook.Event{
		Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed,
		Repo: "maven-local", Path: "dead/letter.bin", Sha256: "deadbeef", Size: 7,
		Actor: webhook.Actor{ID: "ci-bot", Realm: "internal"},
	})
	var row *webhook.Delivery
	waitOutbox(t, 15*time.Second, "the retry budget to exhaust into a dead row", func() bool {
		rows, err := st.store.ListDeliveries(ctx, st.subIDOf(t), 10)
		if err != nil || len(rows) == 0 {
			return false
		}
		row = rows[0]
		return row.Status == webhook.StatusDead
	})
	if row.Attempts != 5 {
		t.Fatalf("dead row attempts = %d, want 5 (first try counted + four retries)", row.Attempts)
	}
	if n := st.rcv.count(); n != 5 {
		t.Fatalf("consumer saw %d attempts, want 5", n)
	}

	// The row-level query face: the dead row is filterable, the joined
	// key and observability columns ride the echo.
	code, body := st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?status=dead", t362Admin)
	if code != http.StatusOK {
		t.Fatalf("outbox GET = %d %s, want 200", code, body)
	}
	var page struct {
		Deliveries []struct {
			ID              string `json:"id"`
			SubscriptionKey string `json:"subscription_key"`
			Status          string `json:"status"`
			Attempts        int64  `json:"attempts"`
			LastError       string `json:"last_error"`
			LastStatusCode  *int64 `json:"last_status_code"`
			Payload         string `json:"payload"`
		} `json:"deliveries"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("page decode: %v (%s)", err, body)
	}
	if len(page.Deliveries) != 1 || page.Deliveries[0].ID != row.ID ||
		page.Deliveries[0].SubscriptionKey != "dead-letter-hook" ||
		page.Deliveries[0].Status != webhook.StatusDead ||
		page.Deliveries[0].Attempts != 5 || page.Deliveries[0].LastStatusCode == nil ||
		*page.Deliveries[0].LastStatusCode != 500 || page.Deliveries[0].Payload != "" {
		t.Fatalf("dead page shape: %s", body)
	}
	if page.Deliveries[0].LastError == "" {
		t.Fatalf("dead row must carry its last_error: %s", body)
	}

	// The subscription filter narrows to the same row; an unknown key is
	// an honest empty page.
	code, body = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?subscription=dead-letter-hook", t362Admin)
	if code != http.StatusOK || !strings.Contains(body, row.ID) {
		t.Fatalf("subscription filter = %d %s", code, body)
	}
	code, body = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?subscription=never-created", t362Admin)
	var empty struct {
		Deliveries []json.RawMessage `json:"deliveries"`
		NextCursor string            `json:"nextCursor"`
	}
	if err := json.Unmarshal([]byte(body), &empty); err != nil {
		t.Fatalf("empty page decode: %v (%s)", err, body)
	}
	if code != http.StatusOK || len(empty.Deliveries) != 0 || empty.NextCursor != "" {
		t.Fatalf("unknown subscription filter = %d %s, want an empty page", code, body)
	}

	// The consumer recovers; the replay resets the row and the engine
	// re-delivers the STORED envelope.
	st.rcv.setOK(true)
	code, body = st.outboxDo(t, http.MethodPost, "v1/webhooks/outbox/"+row.ID+"/replay", t362Admin)
	if code != http.StatusOK {
		t.Fatalf("replay = %d %s, want 200", code, body)
	}
	var echo struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Attempts int64  `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(body), &echo); err != nil {
		t.Fatalf("replay echo decode: %v (%s)", err, body)
	}
	if echo.ID != row.ID || echo.Status != webhook.StatusPending || echo.Attempts != 0 {
		t.Fatalf("replay echo = %s, want pending/0", body)
	}
	waitOutbox(t, 15*time.Second, "the replayed delivery to land", func() bool {
		got, err := st.store.GetDelivery(ctx, row.ID)
		return err == nil && got.Status == webhook.StatusDelivered && got.DeliveredAt != nil
	})
	if n := st.rcv.count(); n != 6 {
		t.Fatalf("consumer saw %d attempts after replay, want 6", n)
	}
	// The consumer received the SAME stored envelope: subscription_key
	// and the artifact identity, byte-stable across the replay.
	var env struct {
		Domain          string `json:"domain"`
		EventType       string `json:"event_type"`
		SubscriptionKey string `json:"subscription_key"`
	}
	if err := json.Unmarshal([]byte(st.rcv.lastBody()), &env); err != nil {
		t.Fatalf("replayed envelope decode: %v", err)
	}
	if env.Domain != "artifact" || env.EventType != "deployed" || env.SubscriptionKey != "dead-letter-hook" {
		t.Fatalf("replayed envelope = %+v", env)
	}

	// The audit row: one webhook.delivery.replay word, the admin actor,
	// the delivery id in the detail.
	events, err := st.s.auditLog.Query(ctx, audit.Filter{Action: auditWebhookReplay, Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events.Events) != 1 {
		t.Fatalf("replay audit rows = %d, want 1", len(events.Events))
	}
	if events.Events[0].Actor != "admin" || !strings.Contains(events.Events[0].Detail, row.ID) {
		t.Fatalf("replay audit row = %+v", events.Events[0])
	}
}

// subIDOf resolves the stack subscription's store id.
func (st *outboxStack) subIDOf(t *testing.T) string {
	t.Helper()
	sub, err := st.bus.Get(context.Background(), "dead-letter-hook")
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	return sub.ID
}

// TestOutboxGatesAndRefusals: the role matrix (401 anonymous, 403 plain
// user, readonly_admin reads but cannot replay), the refusal arms (404
// missing row, 409 not-dead, 400 malformed parameters) and the E-26
// spellings.
func TestOutboxGatesAndRefusals(t *testing.T) {
	st := newOutboxStack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	ctx := context.Background()

	// A dead row to replay and a live row to refuse.
	subID := st.subIDOf(t)
	at := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	rows := []*webhook.Delivery{
		{ID: "gate-dead-1", SubscriptionID: subID, EventType: "deployed",
			Payload: `{}`, NextAttemptAt: at, CreatedAt: at},
		{ID: "gate-live-1", SubscriptionID: subID, EventType: "deployed",
			Payload: `{}`, NextAttemptAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
			CreatedAt: at},
	}
	if err := st.store.EnqueueDeliveries(ctx, rows); err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	code500 := int64(500)
	if err := st.store.MarkDead(ctx, "gate-dead-1", 5, "receiver answered 500", &code500); err != nil {
		t.Fatalf("mark dead: %v", err)
	}

	// Role matrix.
	code, _ := st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %d, want 401", code)
	}
	code, _ = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox", t362User)
	if code != http.StatusForbidden {
		t.Fatalf("plain-user GET = %d, want 403", code)
	}
	code, _ = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?status=dead", t362Ro)
	if code != http.StatusOK {
		t.Fatalf("readonly-admin GET = %d, want 200", code)
	}
	code, _ = st.outboxDo(t, http.MethodPost, "v1/webhooks/outbox/gate-dead-1/replay", t362User)
	if code != http.StatusForbidden {
		t.Fatalf("plain-user replay = %d, want 403", code)
	}
	code, _ = st.outboxDo(t, http.MethodPost, "v1/webhooks/outbox/gate-dead-1/replay", t362Ro)
	if code != http.StatusForbidden {
		t.Fatalf("readonly-admin replay = %d, want 403 (system:write)", code)
	}

	// Refusal arms.
	code, body := st.outboxDo(t, http.MethodPost, "v1/webhooks/outbox/no-such-row/replay", t362Admin)
	if code != http.StatusNotFound || !strings.Contains(body, "delivery not found") {
		t.Fatalf("missing-row replay = %d %s, want 404", code, body)
	}
	// gate-live-1 is pending (a live row): replaying would double-deliver.
	code, body = st.outboxDo(t, http.MethodPost, "v1/webhooks/outbox/gate-live-1/replay", t362Admin)
	if code != http.StatusConflict || !strings.Contains(body, "not dead") {
		t.Fatalf("live-row replay = %d %s, want 409", code, body)
	}

	// Malformed parameters.
	code, _ = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?status=bogus", t362Admin)
	if code != http.StatusBadRequest {
		t.Fatalf("bogus status = %d, want 400", code)
	}
	code, _ = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?limit=0", t362Admin)
	if code != http.StatusBadRequest {
		t.Fatalf("zero limit = %d, want 400", code)
	}
	code, _ = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?limit=9999", t362Admin)
	if code != http.StatusBadRequest {
		t.Fatalf("over-max limit = %d, want 400", code)
	}
	code, body = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?cursor=garbage", t362Admin)
	if code != http.StatusBadRequest || !strings.Contains(body, "invalid cursor") {
		t.Fatalf("garbage cursor = %d %s, want 400 invalid cursor", code, body)
	}

	// The E-26 family: unknown spellings and verbs 404.
	for _, tc := range []struct {
		method, rest string
	}{
		{http.MethodPost, "v1/webhooks/outbox"},
		{http.MethodPost, "v1/webhooks/outbox/gate-dead-1/refresh"},
		{http.MethodDelete, "v1/webhooks/outbox/gate-dead-1"},
		{http.MethodGet, "v1/webhooks/outbox/gate-dead-1/replay"},
	} {
		code, _ = st.outboxDo(t, tc.method, tc.rest, t362Admin)
		if code != http.StatusNotFound && code != http.StatusNotImplemented {
			t.Fatalf("%s %s = %d, want the E-26 404", tc.method, tc.rest, code)
		}
	}
}

// TestOutboxFeatureGateAndNilPlane: community 403s the replay write (the
// webhook slot's feature gate) while the query stays open (D1); a stack
// without the plane answers the honest 503 on both endpoints.
func TestOutboxFeatureGateAndNilPlane(t *testing.T) {
	st := newOutboxStack(t, fakeLicenseEval{tier: license.TierCommunity})
	ctx := context.Background()
	subID := st.subIDOf(t)
	at := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if err := st.store.EnqueueDeliveries(ctx, []*webhook.Delivery{{
		ID: "gate-comm-1", SubscriptionID: subID, EventType: "deployed",
		Payload: `{}`, NextAttemptAt: at, CreatedAt: at,
	}}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	code500 := int64(500)
	if err := st.store.MarkDead(ctx, "gate-comm-1", 5, "x", &code500); err != nil {
		t.Fatalf("mark dead: %v", err)
	}
	// The query stays open on a locked instance (D1's extension).
	code, _ := st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox?status=dead", t362Admin)
	if code != http.StatusOK {
		t.Fatalf("community GET = %d, want 200 (D1)", code)
	}
	code, _ = st.outboxDo(t, http.MethodPost, "v1/webhooks/outbox/gate-comm-1/replay", t362Admin)
	if code != http.StatusForbidden {
		t.Fatalf("community replay = %d, want 403 (the webhook slot's feature gate)", code)
	}

	// The nil plane: both endpoints answer the honest 503.
	st.s.deps.Webhooks = nil
	code, _ = st.outboxDo(t, http.MethodGet, "v1/webhooks/outbox", t362Admin)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("nil-plane GET = %d, want 503", code)
	}
	code, _ = st.outboxDo(t, http.MethodPost, "v1/webhooks/outbox/gate-comm-1/replay", t362Admin)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("nil-plane replay = %d, want 503", code)
	}
}

// TestOutboxRouteReachableThroughRealChain: one leg through the assembled
// Handler() — the anonymous 401 proves the route case sits behind the
// real middleware chain, not only behind dispatchAPI.
func TestOutboxRouteReachableThroughRealChain(t *testing.T) {
	st := newOutboxStack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	req := httptest.NewRequest(http.MethodGet, "/binflow/api/v1/webhooks/outbox", nil)
	rec := httptest.NewRecorder()
	st.s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("real-chain anonymous GET = %d, want 401", rec.Code)
	}
	// An unknown verb through the real chain answers the E-26 404 too.
	req = httptest.NewRequest(http.MethodPut, "/binflow/api/v1/webhooks/outbox/some-id", nil)
	rec = httptest.NewRecorder()
	st.s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusNotImplemented {
		t.Fatalf("real-chain unknown verb = %d, want the E-26 404", rec.Code)
	}
}
