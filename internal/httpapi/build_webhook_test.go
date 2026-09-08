// The build domain's full-stack webhook weaving (M17 T-510, FR-152.3 /
// ADR-0045 decision 7): the REST faces drive the real assembly (the bus
// injected as Deps.Webhooks the way cmd does; httpapi.New adapts it onto
// the build service's Emit facet), and every successful mutation tail
// lands its event in the outbox — upload fires uploaded, the status-only
// promote fires promoted (dry run stays silent), retention's inline window
// fires deleted per discarded run, and the build scope's criteria filter
// gates what fires. The envelope contract itself is pinned in
// internal/webhook's own tests; this file pins the WIRING.

package httpapi_test

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

	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// buildHookStack is the t362 seam stack's build twin: the full harness
// with the unified-event bus wired onto Deps.Webhooks — the same shape cmd
// assembles, so httpapi.New's build Emit-facet adapter is the code under
// test.
type buildHookStack struct {
	*harness
	bus   *webhook.Bus
	store *webhook.SQLiteStore
}

func newBuildHookStack(t *testing.T, gateOpen bool) *buildHookStack {
	t.Helper()
	var st buildHookStack
	h := newHarnessFull(t, nil, nil, nil, func(deps *httpapi.Deps) {
		dsn := "file:" + url.PathEscape(deps.DataDir+"/binflow.db") +
			"?_pragma=foreign_keys(1)&_pragma=busy_timeout(" + strconv.Itoa(metadata.BusyTimeoutMs) + ")"
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
		st.bus, st.store = bus, store
	}, nil)
	st.harness = h
	return &st
}

// subscribe registers one build-domain subscription.
func (st *buildHookStack) subscribe(t *testing.T, key, eventType, criteria string) {
	t.Helper()
	body := `{
		"key": "` + key + `",
		"enabled": true,
		"event_filter": {"domain": "build", "event_types": ["` + eventType + `"], "criteria": ` + criteria + `},
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

// deliveriesOf lists one subscription's outbox rows.
func (st *buildHookStack) deliveriesOf(t *testing.T, key string) []webhook.Delivery {
	t.Helper()
	sub, err := st.bus.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get subscription %s: %v", key, err)
	}
	rows, err := st.store.ListDeliveries(context.Background(), sub.ID, 100)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	out := make([]webhook.Delivery, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out
}

// do is the request helper ("" body means none).
func (st *buildHookStack) do(method, path, body string) (int, string) {
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

// buildEnvelope is the parsed delivery payload.
type buildEnvelope struct {
	Domain      string `json:"domain"`
	EventType   string `json:"event_type"`
	UserContext struct {
		ID      string `json:"id"`
		IsToken bool   `json:"isToken"`
		Realm   string `json:"realm"`
	} `json:"userContext"`
	Data struct {
		BuildName    string `json:"build_name"`
		BuildNumber  string `json:"build_number"`
		BuildStarted string `json:"build_started"`
		BuildRepo    string `json:"build_repo"`
	} `json:"data"`
}

func parseBuildEnvelope(t *testing.T, payload string) buildEnvelope {
	t.Helper()
	var env buildEnvelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		t.Fatalf("envelope parse: %v", err)
	}
	return env
}

// hookUploadDoc is a complete-enough build info document.
const hookUploadDoc = `{
  "name": "pub-app", "number": "51", "type": "GENERIC",
  "started": "2026-09-07T10:00:00.000+0000",
  "modules": []
}`

// TestBuildWebhookUploadPromoteRetentionWeaving: the three flipped types
// fire off the REAL REST faces through the assembly's adapter, each with
// the run's four facts and the request's principal on the envelope.
func TestBuildWebhookUploadPromoteRetentionWeaving(t *testing.T) {
	st := newBuildHookStack(t, true)
	st.subscribe(t, "build-up", "uploaded", `{"anyBuild": true}`)
	st.subscribe(t, "build-promo", "promoted", `{"anyBuild": true}`)
	st.subscribe(t, "build-del", "deleted", `{"anyBuild": true}`)

	if code, body := st.do(http.MethodPut, "/binflow/api/build", hookUploadDoc); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("upload = %d %s", code, body)
	}
	rows := st.deliveriesOf(t, "build-up")
	if len(rows) != 1 {
		t.Fatalf("uploaded deliveries = %d, want 1", len(rows))
	}
	env := parseBuildEnvelope(t, rows[0].Payload)
	if env.Domain != "build" || env.EventType != "uploaded" {
		t.Fatalf("uploaded identity: %+v", env)
	}
	if env.Data.BuildName != "pub-app" || env.Data.BuildNumber != "51" ||
		env.Data.BuildStarted != "2026-09-07T10:00:00.000+0000" ||
		env.Data.BuildRepo != metadata.DefaultBuildRepo {
		t.Fatalf("uploaded data: %+v", env.Data)
	}
	// The adapter projected the REQUEST's principal onto userContext.
	if env.UserContext.ID != adminUser || env.UserContext.Realm != "internal" {
		t.Fatalf("uploaded userContext: %+v", env.UserContext)
	}
	// No cross-domain bleed: the upload fires nothing on the other types.
	for _, key := range []string{"build-promo", "build-del"} {
		if n := len(st.deliveriesOf(t, key)); n != 0 {
			t.Fatalf("subscription %s saw %d rows after upload, want 0", key, n)
		}
	}

	// Dry run first: zero side effects is zero events.
	if code, body := st.do(http.MethodPost, "/binflow/api/build/promote/pub-app/51",
		`{"status":"staged","dryRun":true}`); code != http.StatusOK {
		t.Fatalf("dry-run promote = %d %s", code, body)
	}
	if n := len(st.deliveriesOf(t, "build-promo")); n != 0 {
		t.Fatalf("dry-run promote enqueued %d rows, want 0", n)
	}
	// The real promotion fires promoted (status-only arm — nothing
	// migrated, the event is about the RUN).
	if code, body := st.do(http.MethodPost, "/binflow/api/build/promote/pub-app/51",
		`{"status":"released"}`); code != http.StatusOK {
		t.Fatalf("promote = %d %s", code, body)
	}
	rows = st.deliveriesOf(t, "build-promo")
	if len(rows) != 1 {
		t.Fatalf("promoted deliveries = %d, want 1", len(rows))
	}
	if env := parseBuildEnvelope(t, rows[0].Payload); env.EventType != "promoted" ||
		env.Data.BuildName != "pub-app" || env.Data.BuildNumber != "51" {
		t.Fatalf("promoted envelope: %+v", env)
	}

	// A second run falls outside a count=1 window: retention (inline)
	// discards it and fires deleted for exactly that run.
	newer := strings.Replace(hookUploadDoc, `"number": "51"`, `"number": "52"`, 1)
	newer = strings.Replace(newer, "2026-09-07T10:00:00.000+0000", "2026-09-07T12:00:00.000+0000", 1)
	if code, body := st.do(http.MethodPut, "/binflow/api/build", newer); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("second upload = %d %s", code, body)
	}
	if code, body := st.do(http.MethodPost, "/binflow/api/build/retention/pub-app?async=false",
		`{"count":1}`); code != http.StatusOK {
		t.Fatalf("retention = %d %s", code, body)
	}
	rows = st.deliveriesOf(t, "build-del")
	if len(rows) != 1 {
		t.Fatalf("deleted deliveries = %d, want 1 (run 51 discarded, 52 kept)", len(rows))
	}
	if env := parseBuildEnvelope(t, rows[0].Payload); env.EventType != "deleted" ||
		env.Data.BuildNumber != "51" || env.Data.BuildStarted != "2026-09-07T10:00:00.000+0000" {
		t.Fatalf("deleted envelope: %+v", env)
	}

	// The uploads kept firing throughout (two runs published).
	if n := len(st.deliveriesOf(t, "build-up")); n != 2 {
		t.Fatalf("uploaded deliveries = %d, want 2", n)
	}
}

// TestBuildWebhookCriteriaAndGate: the build scope's filter arms on the
// wire — a selectedBuilds miss enqueues nothing while anyBuild fires, and
// a denied entitlement gate keeps the REST faces green with an empty
// outbox (the zero-change contract's breaker arm).
func TestBuildWebhookCriteriaAndGate(t *testing.T) {
	st := newBuildHookStack(t, true)
	st.subscribe(t, "selected", "uploaded", `{"selectedBuilds": ["other-app"]}`)
	st.subscribe(t, "patterned", "uploaded", `{"anyBuild": true, "includePatterns": ["rel-*"]}`)
	st.subscribe(t, "broad", "uploaded", `{"anyBuild": true}`)
	if code, body := st.do(http.MethodPut, "/binflow/api/build", hookUploadDoc); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("upload = %d %s", code, body)
	}
	for _, key := range []string{"selected", "patterned"} {
		if n := len(st.deliveriesOf(t, key)); n != 0 {
			t.Fatalf("subscription %s saw %d rows, want 0 (scope miss)", key, n)
		}
	}
	if n := len(st.deliveriesOf(t, "broad")); n != 1 {
		t.Fatalf("broad subscription saw %d rows, want 1", n)
	}

	// The gate's breaker arm: the same upload stays green, the outbox
	// stays empty, and the loss counter stays at zero (a denied gate is a
	// routing decision, not a loss).
	denied := newBuildHookStack(t, false)
	denied.subscribe(denied.t, "broad", "uploaded", `{"anyBuild": true}`)
	if code, body := denied.do(http.MethodPut, "/binflow/api/build", hookUploadDoc); code != http.StatusOK && code != http.StatusCreated {
		denied.t.Fatalf("upload under denied gate = %d %s (main path must stay green)", code, body)
	}
	if n := len(denied.deliveriesOf(denied.t, "broad")); n != 0 {
		denied.t.Fatalf("denied gate enqueued %d rows, want 0", n)
	}
	if denied.bus.EnqueueFailures() != 0 {
		denied.t.Fatalf("a denied gate is a routing decision, not a loss: failures = %d", denied.bus.EnqueueFailures())
	}
}
