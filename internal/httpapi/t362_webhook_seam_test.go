package httpapi_test

// T-362's domain-weaving legs over the REAL content plane (the harness
// stack with the Bus wired the way cmd will): a deploy/delete/copy/move/
// property chain each lands its event in the outbox with the official
// envelope, the criteria filter gates what fires, and the entitlement
// gate's degradation keeps the main path green while enqueueing NOTHING
// (AC-5's breaker arm: PUT 200 + queue_depth 0). The /event REST plane's
// own contract lives in webhooks_internal_test.go (the router case is the
// conductor's wiring).

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// t362SeamStack is the harness plus the webhook collaborators.
type t362SeamStack struct {
	*harness
	bus   *webhook.Bus
	store *webhook.SQLiteStore
}

func newT362SeamStack(t *testing.T, gateOpen bool) *t362SeamStack {
	t.Helper()
	var st t362SeamStack
	h := newHarnessFull(t, nil, nil, nil, func(deps *httpapi.Deps) {
		dbPath := deps.DataDir + "/binflow.db"
		dsn := "file:" + url.PathEscape(dbPath) +
			"?_pragma=foreign_keys(1)&_pragma=busy_timeout(" + t362Itoa(metadata.BusyTimeoutMs) + ")"
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatalf("open webhook db: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
		store := webhook.NewSQLiteStore(db)
		bus, err := webhook.NewBus(webhook.BusOptions{
			Store:  store,
			Repos:  deps.Metadata.Repos(),
			Gate:   func(context.Context) bool { return gateOpen },
			Origin: "https://binflow.example.com",
		})
		if err != nil {
			t.Fatalf("NewBus: %v", err)
		}
		deps.Webhooks = bus
		repo.AttachWebhookEmitter(deps.ReposSvc, bus)
		st.bus, st.store = bus, store
	}, nil)
	st.harness = h
	return &st
}

func t362Itoa(n int) string { return strconv.Itoa(n) }

// subscribe registers one subscription through the bus (the REST face is
// pinned separately; here the plane under test is the weaving).
func (st *t362SeamStack) subscribe(t *testing.T, key, domain, eventType, criteria string) {
	t.Helper()
	body := `{
		"key": "` + key + `",
		"enabled": true,
		"event_filter": {"domain": "` + domain + `", "event_types": ["` + eventType + `"], "criteria": ` + criteria + `},
		"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
	}`
	req, err := webhook.ParseSubscriptionRequest([]byte(body), nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := st.bus.Create(context.Background(), req, "admin"); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
}

func (st *t362SeamStack) deliveries(t *testing.T) []webhook.Delivery {
	t.Helper()
	subs, err := st.bus.List(context.Background())
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	var out []webhook.Delivery
	for _, sub := range subs {
		rows, err := st.store.ListDeliveries(context.Background(), sub.ID, 100)
		if err != nil {
			t.Fatalf("list deliveries: %v", err)
		}
		out = append(out, derefDeliveries(rows)...)
	}
	return out
}

func derefDeliveries(rows []*webhook.Delivery) []webhook.Delivery {
	out := make([]webhook.Delivery, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out
}

// do is the stack's request helper (body "" means none).
func (st *t362SeamStack) do(method, path, body string) (int, string) {
	st.t.Helper()
	var payload []byte
	if body != "" {
		payload = []byte(body)
	}
	resp := st.harness.do(method, path, adminUser, adminPass, payload, nil)
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestT362WeavingArtifactLifecycle(t *testing.T) {
	st := newT362SeamStack(t, true)
	st.subscribe(t, "all-artifact", "artifact", "deployed", `{"anyLocal": true}`)
	st.subscribe(t, "deletes-only", "artifact", "deleted", `{"repoKeys": ["src"]}`)

	if code, body := st.do(http.MethodPut, "/binflow/api/repositories/src", `{"rclass":"local","packageType":"generic"}`); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create repo = %d %s", code, body)
	}
	if code, body := st.do(http.MethodPut, "/binflow/src/com/acme/lib/1.0/acme.jar", "jar-bytes"); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("deploy = %d %s", code, body)
	}
	if code, body := st.do(http.MethodPut, "/binflow/api/repositories/dst", `{"rclass":"local","packageType":"generic"}`); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create target repo = %d %s", code, body)
	}

	rows := st.deliveries(t)
	if len(rows) != 1 {
		t.Fatalf("deliveries after deploy = %d, want 1 (deployed only)", len(rows))
	}
	var env struct {
		Domain          string `json:"domain"`
		EventType       string `json:"event_type"`
		SubscriptionKey string `json:"subscription_key"`
		Data            struct {
			RepoKey string  `json:"repo_key"`
			Path    string  `json:"path"`
			Name    string  `json:"name"`
			Sha256  string  `json:"sha256"`
			Size    float64 `json:"size"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(rows[0].Payload), &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env.Domain != "artifact" || env.EventType != "deployed" ||
		env.SubscriptionKey != "all-artifact" ||
		env.Data.RepoKey != "src" || env.Data.Path != "com/acme/lib/1.0/acme.jar" ||
		env.Data.Name != "acme.jar" || env.Data.Sha256 == "" || env.Data.Size != float64(len("jar-bytes")) {
		t.Fatalf("deployed envelope: %+v", env)
	}

	// A property write fires its own domain.
	st.subscribe(t, "props", "artifact_property", "added", `{"anyLocal": true}`)
	if code, body := st.do(http.MethodPut,
		"/binflow/api/storage/src/com/acme/lib/1.0/acme.jar?properties=license=apache-2.0", ""); code != http.StatusNoContent {
		t.Fatalf("properties put = %d %s", code, body)
	}
	rows = st.deliveries(t)
	if len(rows) != 2 {
		t.Fatalf("deliveries after property = %d, want 2", len(rows))
	}
	if !strings.Contains(rows[1].Payload, `"property_key":"license"`) ||
		!strings.Contains(rows[1].Payload, `"artifact_property"`) {
		t.Fatalf("property envelope: %s", rows[1].Payload)
	}

	st.subscribe(t, "ops", "artifact", "copied", `{"anyLocal": true}`)
	st.subscribe(t, "ops-moved", "artifact", "moved", `{"anyLocal": true}`)
	// Copy and move fire their own types with the source/target pair.
	// Driven through the service seam (the REST plane's repo-operations
	// license gate is T-339's contract, not this ticket's; the weaving
	// seam under test is the pipeline's transfer tail either way).
	cm, ok := st.svc.(repo.CopyMoveService)
	if !ok {
		t.Fatal("service does not carry the copy/move face")
	}
	admin := &repo.Principal{Name: adminUser, Admin: true, Source: auth.ProviderLocal}
	if _, err := cm.CopyOrMove(context.Background(), admin, repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "com/acme/lib/1.0/acme.jar",
		TargetRepo: "dst", TargetPath: "com/acme/lib/1.0/acme.jar",
	}); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if last := st.lastPayload(t, "copied"); !strings.Contains(last, `"source_repo_path":"src/com/acme/lib/1.0/acme.jar"`) ||
		!strings.Contains(last, `"target_repo_path":"dst/com/acme/lib/1.0/acme.jar"`) {
		t.Fatalf("copied envelope: %s", last)
	}
	if _, err := cm.CopyOrMove(context.Background(), admin, repo.CopyMoveRequest{
		Op: repo.OpMove, SrcRepo: "src", SrcPath: "com/acme/lib/1.0/acme.jar",
		TargetRepo: "dst", TargetPath: "moved/acme.jar",
	}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if last := st.lastPayload(t, "moved"); !strings.Contains(last, `"event_type":"moved"`) {
		t.Fatalf("moved envelope: %s", last)
	}

	// Delete closes the loop (deletes-only matches repoKeys src). The
	// move above took a.jar out of src, so a second artifact carries the
	// delete leg.
	if code, body := st.do(http.MethodPut, "/binflow/src/com/acme/lib/1.0/second.jar", "j2"); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("second deploy = %d %s", code, body)
	}
	if code, body := st.do(http.MethodDelete, "/binflow/src/com/acme/lib/1.0/second.jar", ""); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", code, body)
	}
	if last := st.lastPayload(t, "deleted"); !strings.Contains(last, `"event_type":"deleted"`) {
		t.Fatalf("deleted envelope: %s", last)
	}
}

// lastPayload returns the newest payload whose event_type matches.
func (st *t362SeamStack) lastPayload(t *testing.T, eventType string) string {
	t.Helper()
	for _, d := range st.deliveries(t) {
		if strings.Contains(d.Payload, `"event_type":"`+eventType+`"`) {
			return d.Payload
		}
	}
	t.Fatalf("no delivery of event_type %q among %d rows", eventType, len(st.deliveries(t)))
	return ""
}

func TestT362WeavingDockerPushDelete(t *testing.T) {
	st := newT362SeamStack(t, true)
	st.subscribe(t, "registry", "docker", "pushed", `{"anyLocal": true}`)
	st.subscribe(t, "registry-del", "docker", "deleted", `{"anyLocal": true}`)
	if code, body := st.do(http.MethodPut, "/binflow/api/repositories/docker-local", `{"rclass":"local","packageType":"docker"}`); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create docker repo = %d %s", code, body)
	}
	// The manifest publish seam (the /v2 push chain's index tail): a tag
	// publish fires docker/pushed with the image/tag/image_type triple.
	digest := strings.Repeat("ab", 32)
	admin := &repo.Principal{Name: adminUser, Admin: true, Source: auth.ProviderLocal}
	if _, err := st.svc.PutManifest(context.Background(), admin, "docker-local",
		"acme/app", digest, "1.0.0", "application/vnd.oci.image.manifest.v1+json",
		128, nil); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	last := st.lastPayload(t, "pushed")
	for _, want := range []string{
		`"image_name":"acme/app"`, `"tag":"1.0.0"`, `"image_type":"oci"`,
		`"sha256":"` + digest + `"`, `"repo_key":"docker-local"`, `"platforms":[]`,
	} {
		if !strings.Contains(last, want) {
			t.Fatalf("pushed envelope missing %s: %s", want, last)
		}
	}
	// The registry delete fires docker/deleted (tag empty — the digest-keyed
	// delete cascades tags without naming them).
	if err := st.svc.DeleteManifest(context.Background(), admin, "docker-local", "acme/app", digest); err != nil {
		t.Fatalf("DeleteManifest: %v", err)
	}
	if last := st.lastPayload(t, "deleted"); !strings.Contains(last, `"domain":"docker"`) ||
		!strings.Contains(last, `"tag":""`) {
		t.Fatalf("deleted envelope: %s", last)
	}
}

func TestT362WeavingCriteriaFilter(t *testing.T) {
	st := newT362SeamStack(t, true)
	// includePatterns that miss everything the test deploys.
	st.subscribe(t, "npm-only", "artifact", "deployed", `{"anyLocal": true, "includePatterns": ["**/*.tgz"]}`)
	if code, body := st.do(http.MethodPut, "/binflow/api/repositories/generic-local", `{"rclass":"local","packageType":"generic"}`); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create repo = %d %s", code, body)
	}
	if code, body := st.do(http.MethodPut, "/binflow/generic-local/pkg/app-1.0.jar", "j"); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("deploy = %d %s", code, body)
	}
	if rows := st.deliveries(t); len(rows) != 0 {
		t.Fatalf("pattern-missed deploy enqueued %d rows, want 0", len(rows))
	}
}

func TestT362WeavingGateDegradation(t *testing.T) {
	// AC-5's breaker arm: entitlement DENIED — the content plane stays
	// green and the outbox stays empty.
	st := newT362SeamStack(t, false)
	st.subscribe(t, "all", "artifact", "deployed", `{"anyLocal": true}`)
	if code, body := st.do(http.MethodPut, "/binflow/api/repositories/local-r", `{"rclass":"local","packageType":"generic"}`); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create repo = %d %s", code, body)
	}
	if code, body := st.do(http.MethodPut, "/binflow/local-r/a/b.bin", "bytes"); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("deploy under denied gate = %d %s (main path must stay green)", code, body)
	}
	if n, err := st.bus.PendingCount(context.Background()); err != nil || n != 0 {
		t.Fatalf("queue depth = %d (%v), want 0 under a denied gate", n, err)
	}
	if st.bus.EnqueueFailures() != 0 {
		t.Fatalf("a denied gate is a routing decision, not a loss: failures = %d", st.bus.EnqueueFailures())
	}
}
