package httpapi_test

// T-422 (FR-138.2/FR-138.3) — the REST wire of the global-block family and
// the two test faces, over the same harness as the T-180/T-405/T-420 faces:
//
//   - the block family: the official camelCase GET pair, the §9.2-B-1 query
//     selector (absent = addressed, non-"true" = leave alone), the §9.2-B-3
//     message variants verbatim on text/plain, "No action taken." on the
//     double no-op, and the replication.block.update audit row;
//   - the run face's block pre-check (§9.2-A-5): 409 with the anchored skip
//     wording, the runner never consulted;
//   - the test faces: the verdict body (ok at 200, ok:false + the inline
//     reason at 400 — the auth.config.test posture), the 404/400 ladder,
//     the self-instance refusal, the draft body requirement, the two 501
//     degradations, and the replication.config.test audit row;
//   - the configuration plane keeps flowing while the brake is on (the t226
//     UI-API-not-gated posture — AC2's REST-channel arm).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
)

// t422Tester scripts the probe seam.
type t422Tester struct {
	calls int
	cfg   *replication.ReplicationConfig
	ov    replication.TargetOverride
	res   replication.TestResult
	err   error
}

func (tt *t422Tester) TestTarget(_ context.Context, cfg *replication.ReplicationConfig, ov replication.TargetOverride) (replication.TestResult, error) {
	tt.calls++
	tt.cfg, tt.ov = cfg, ov
	if tt.err != nil {
		return replication.TestResult{}, tt.err
	}
	return tt.res, nil
}

// newBlockHarness assembles the family stack with the gate, the optional
// tester and the optional runner (nil seams answer the family's 501s). The
// T-420 newRunHarness pattern: a second server over the base stack's
// collaborators, the assembly difference under test being the new seams.
func newBlockHarness(t *testing.T, tester httpapi.ReplicationTester, runner httpapi.ReplicationRunner) *replHarness {
	t.Helper()
	base := newReplHarness(t, false)
	cfg := config.Defaults()
	cfg.Storage.DataDir = base.dataDir
	authSvc := auth.NewFromStore(base.md, cfg.Security.AnonymousAccess)
	keeper, ok := base.replStore.(*replication.SQLiteStore)
	if !ok {
		t.Fatalf("store is %T, want *replication.SQLiteStore", base.replStore)
	}
	gate := replication.NewBlockGate(keeper, replication.GlobalBlock{}, nil, nil)
	if err := gate.Load(context.Background()); err != nil {
		t.Fatalf("gate load: %v", err)
	}
	deps := httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: base.md,
		Repos: base.md.Repos(), Replication: base.replStore,
		ReplicationBlocks: gate,
	}
	if tester != nil {
		deps.ReplicationTester = tester
	}
	if runner != nil {
		deps.ReplicationRunner = runner
	}
	s := httpapi.New(deps, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &replHarness{
		t: t, srv: ts, md: base.md, replStore: base.replStore, dataDir: base.dataDir,
	}
}

// TestReplicationGlobalBlockFace walks the official wire: the GET pair, the
// selector semantics, the message variants, the no-op, the audit trail and
// the auth ladder.
func TestReplicationGlobalBlockFace(t *testing.T) {
	h := newBlockHarness(t, nil, nil)

	// Initial state: exactly the two official keys, both false.
	resp, raw := h.admin(http.MethodGet, "/binflow/api/v1/system/replications", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET: %d (%s)", resp.StatusCode, raw)
	}
	var state map[string]bool
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("GET decode: %v (%s)", err, raw)
	}
	if len(state) != 2 || state["blockPushReplications"] || state["blockPullReplications"] {
		t.Fatalf("initial state = %v, want exactly the two false keys", state)
	}

	// The selector + variants table (§9.2-B-1/B-3).
	cases := []struct {
		name       string
		verb       string // "block" or "unblock"
		query      string
		wantMsg    string
		wantPush   bool
		wantPull   bool
		wantAudits int
	}{
		{"block both (no params)", "block", "",
			"Successfully blocked all replications, no replication will be triggered.", true, true, 1},
		{"unblock pull only", "unblock", "?push=false",
			"Successfully unblocked all pull replications.", true, false, 2},
		{"block push only", "block", "?push=true&pull=false",
			"Successfully blocked all push replications, no push replication will be triggered.", true, false, 3},
		{"block pull only via non-true push", "block", "?push=TRUE&pull=true",
			"Successfully blocked all pull replications, no pull replication will be triggered.", true, true, 4},
		{"double no-op", "block", "?push=false&pull=false",
			"No action taken.", true, true, 4},
		{"unblock both", "unblock", "",
			"Successfully unblocked all replications.", false, false, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/system/replications/"+tc.verb+tc.query, "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s%s: %d (%s)", tc.verb, tc.query, resp.StatusCode, raw)
			}
			if got := raw; got != tc.wantMsg+"\n" && got != tc.wantMsg {
				t.Fatalf("message = %q, want %q", got, tc.wantMsg)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
				t.Fatalf("content-type = %q, want text/plain", ct)
			}
			_, graw := h.admin(http.MethodGet, "/binflow/api/v1/system/replications", "")
			var state map[string]bool
			_ = json.Unmarshal([]byte(graw), &state)
			if state["blockPushReplications"] != tc.wantPush || state["blockPullReplications"] != tc.wantPull {
				t.Fatalf("state after %s%s = %v, want push %t pull %t", tc.verb, tc.query, state, tc.wantPush, tc.wantPull)
			}
			events, err := h.md.Audits().Query(context.Background(),
				metadata.AuditQuery{Action: "replication.block.update", Limit: 50})
			if err != nil {
				t.Fatalf("audit query: %v", err)
			}
			if len(events) != tc.wantAudits {
				t.Fatalf("block rows = %d, want %d (the no-op writes none)", len(events), tc.wantAudits)
			}
		})
	}

	// The auth ladder: anonymous 401, plain user 403 on both verbs and the
	// GET (the family's manage gates).
	if resp, _ := h.do(http.MethodGet, "/binflow/api/v1/system/replications", "", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET: %d, want 401", resp.StatusCode)
	}
	if resp, _ := h.do(http.MethodPost, "/binflow/api/v1/system/replications/block", "dev", "dev-pw", ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("dev POST block: %d, want 403", resp.StatusCode)
	}
	if resp, _ := h.do(http.MethodGet, "/binflow/api/v1/system/replications", "dev", "dev-pw", ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("dev GET: %d, want 403", resp.StatusCode)
	}

	// Foreign spellings keep the E-26 404.
	if resp, _ := h.admin(http.MethodPut, "/binflow/api/v1/system/replications", "{}"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PUT: %d, want 404", resp.StatusCode)
	}
	if resp, _ := h.admin(http.MethodPost, "/binflow/api/v1/system/replications/toggle", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST toggle: %d, want 404", resp.StatusCode)
	}

	// The audit rows carry the actor and the final flags.
	events, err := h.md.Audits().Query(context.Background(),
		metadata.AuditQuery{Action: "replication.block.update", Limit: 10})
	if err != nil || len(events) == 0 {
		t.Fatalf("audit rows: %d (%v)", len(events), err)
	}
	for _, ev := range events {
		if ev.Actor != "admin" {
			t.Errorf("block row actor = %q, want admin", ev.Actor)
		}
	}
}

// TestReplicationBlockThreeFaceConsistency pins AC3's chain in one observable
// thread: the binflow.yaml carrier seeds the runtime state (boot with a
// blocked-push section), the REST GET answers it in the official shape, a
// console-shaped unblock flips it (and persists — a reloaded gate adopts the
// ROW, not a re-read of the yaml seed).
func TestReplicationBlockThreeFaceConsistency(t *testing.T) {
	base := newReplHarness(t, false)
	cfg := config.Defaults()
	cfg.Storage.DataDir = base.dataDir
	// The binflow.yaml leg: a boot with replication.block_push=true (the
	// config package's own tests cover the key resolution; here the resolved
	// value is the cmd assembly's input).
	cfg.Replication.BlockPush = true
	authSvc := auth.NewFromStore(base.md, cfg.Security.AnonymousAccess)
	keeper, ok := base.replStore.(*replication.SQLiteStore)
	if !ok {
		t.Fatalf("store is %T, want *replication.SQLiteStore", base.replStore)
	}
	gate := replication.NewBlockGate(keeper, replication.GlobalBlock{BlockPush: cfg.Replication.BlockPush}, nil, nil)
	if err := gate.Load(context.Background()); err != nil {
		t.Fatalf("gate load: %v", err)
	}
	deps := httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: base.md,
		Repos: base.md.Repos(), Replication: base.replStore, ReplicationBlocks: gate,
	}
	s := httpapi.New(deps, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	h := &replHarness{t: t, srv: ts, md: base.md, replStore: base.replStore, dataDir: base.dataDir}

	// The REST leg answers the yaml-seeded state.
	_, raw := h.admin(http.MethodGet, "/binflow/api/v1/system/replications", "")
	var state map[string]bool
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("GET decode: %v (%s)", err, raw)
	}
	if !state["blockPushReplications"] || state["blockPullReplications"] {
		t.Fatalf("yaml-seeded state = %v, want push-only blocked", state)
	}

	// The console leg (the switch's exact call): one-direction unblock.
	if resp, msg := h.admin(http.MethodPost, "/binflow/api/v1/system/replications/unblock?push=true&pull=false", ""); resp.StatusCode != http.StatusOK ||
		strings.TrimSpace(msg) != "Successfully unblocked all push replications." {
		t.Fatalf("console-shaped unblock = %d (%q)", resp.StatusCode, msg)
	}

	// The persistence leg: a REBOOTED gate adopts the row, not the seed.
	gate2 := replication.NewBlockGate(keeper, replication.GlobalBlock{BlockPush: true}, nil, nil)
	if err := gate2.Load(context.Background()); err != nil {
		t.Fatalf("gate reload: %v", err)
	}
	if gate2.PushBlocked() {
		t.Fatalf("reloaded gate = push blocked, want the persisted unblock (row > yaml seed)")
	}
}

// TestReplicationConfigPlaneUnblockedByBrake: with push blocked, every
// configuration verb answers as before (the t226 UI-API-not-gated posture).
func TestReplicationConfigPlaneUnblockedByBrake(t *testing.T) {
	h := newBlockHarness(t, nil, nil)
	if resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/system/replications/block", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("block: %d (%s)", resp.StatusCode, raw)
	}
	created := h.createConfig(t, "dr-under-brake") // 201 or the test fails
	if resp, raw := h.admin(http.MethodPut, "/binflow/api/v1/replications/1", `{"enabled":false}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT enabled under brake: %d (%s)", resp.StatusCode, raw)
	}
	if resp, raw := h.admin(http.MethodGet, "/binflow/api/v1/replications", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET list under brake: %d (%s)", resp.StatusCode, raw)
	}
	if resp, _ := h.admin(http.MethodDelete, "/binflow/api/v1/replications/dr-under-brake", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE under brake: %d, want 204", resp.StatusCode)
	}
	_ = created
}

// TestReplicationsRunBlockedRefusal: the §9.2-A-5 pre-check — 409 with the
// anchored skip wording, the runner seam never consulted.
func TestReplicationsRunBlockedRefusal(t *testing.T) {
	runner := &t420Runner{}
	h := newBlockHarness(t, nil, runner)
	h.createConfig(t, "dr-blocked-run")
	if resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/system/replications/block", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("block: %d (%s)", resp.StatusCode, raw)
	}
	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications/1/run", "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("run under brake: %d (%s), want 409", resp.StatusCode, raw)
	}
	if want := "Push replication is blocked, skipping replication"; !contains(raw, want) {
		t.Fatalf("refusal = %q, want it to contain %q", raw, want)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner consulted %d times under the brake, want 0", len(runner.calls))
	}
}

func contains(raw, sub string) bool { return strings.Contains(raw, sub) }

// TestReplicationTestFaces pins the two probe faces' wire.
func TestReplicationTestFaces(t *testing.T) {
	tester := &t422Tester{res: replication.TestResult{OK: true, StatusCode: 200, Message: "Push replication target url 'x' tested successfully"}}
	h := newBlockHarness(t, tester, nil)
	created := h.createConfig(t, "dr-test")
	id := "1"

	// Pass: 200, the object body, the tester got the STORED row.
	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications/"+id+"/test", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("test: %d (%s), want 200", resp.StatusCode, raw)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("test decode: %v (%s)", err, raw)
	}
	if len(body) != 3 || body["ok"] != true || body["status_code"] != float64(200) {
		t.Fatalf("pass body = %v, want {ok,status_code,message}", body)
	}
	if tester.calls != 1 || tester.cfg.ID != int64(created["id"].(float64)) { //nolint:gosec // fixture row id
		t.Fatalf("tester saw (%d calls, cfg id %d), want the stored row once", tester.calls, tester.cfg.ID)
	}

	// Fail: 400 with the SAME body shape — the inline reason is the payload
	// (auth.config.test's posture, not the errors[] envelope).
	tester.res = replication.TestResult{StatusCode: 401, Message: "Connection failed: Target replication URL returned error 401: nope"}
	resp, raw = h.admin(http.MethodPost, "/binflow/api/v1/replications/"+id+"/test", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("failing test: %d (%s), want 400", resp.StatusCode, raw)
	}
	body = nil
	_ = json.Unmarshal([]byte(raw), &body)
	if body["ok"] != false || body["message"] == "" {
		t.Fatalf("fail body = %v, want ok:false with the inline reason", body)
	}

	// The override body reaches the seam verbatim (§9.3's draft arm); the
	// scripted verdict resets to a pass first.
	tester.res = replication.TestResult{OK: true, StatusCode: 200, Message: "ok"}
	resp, raw = h.admin(http.MethodPost, "/binflow/api/v1/replications/"+id+"/test",
		`{"target_url":"https://edited.example.com","target_repo":"other","target_username":"u","target_password":"p"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("override test: %d (%s)", resp.StatusCode, raw)
	}
	if tester.ov.TargetURL != "https://edited.example.com" || tester.ov.TargetRepo != "other" ||
		tester.ov.TargetUsername != "u" || tester.ov.TargetPassword != "p" {
		t.Fatalf("override handed = %+v", tester.ov)
	}

	// The draft face: body required, the candidate synthesized, the name
	// riding for the audit trail.
	resp, raw = h.admin(http.MethodPost, "/binflow/api/v1/replications/test", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty draft body: %d (%s), want 400", resp.StatusCode, raw)
	}
	resp, raw = h.admin(http.MethodPost, "/binflow/api/v1/replications/test",
		`{"name":"draft-1","target_url":"https://t.example.com","target_repo":"r","target_password":"pw"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("draft test: %d (%s), want 200", resp.StatusCode, raw)
	}
	if tester.cfg.TargetURL != "" || tester.cfg.Name != "draft-1" {
		t.Fatalf("draft candidate = %+v, want the synthesized shape", tester.cfg)
	}

	// The ladder: unknown id 404, malformed id 400, unparsable body 400.
	if resp, _ := h.admin(http.MethodPost, "/binflow/api/v1/replications/9999/test", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id: %d, want 404", resp.StatusCode)
	}
	if resp, _ := h.admin(http.MethodPost, "/binflow/api/v1/replications/zero/test", ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed id: %d, want 400", resp.StatusCode)
	}
	if resp, _ := h.admin(http.MethodPost, "/binflow/api/v1/replications/1/test", "not-json"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unparsable body: %d, want 400", resp.StatusCode)
	}

	// The self-instance refusal: the harness's own origin is the front door.
	selfBody := `{"target_url":"` + h.srv.URL + `","target_repo":"r"}`
	resp, raw = h.admin(http.MethodPost, "/binflow/api/v1/replications/test", selfBody)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("self draft: %d (%s), want 400", resp.StatusCode, raw)
	}
	if !contains(raw, "same instance") {
		t.Fatalf("self refusal = %q, want the same-instance wording", raw)
	}

	// Auth: anonymous 401, plain user 403.
	if resp, _ := h.do(http.MethodPost, "/binflow/api/v1/replications/1/test", "", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous test: %d, want 401", resp.StatusCode)
	}
	if resp, _ := h.do(http.MethodPost, "/binflow/api/v1/replications/1/test", "dev", "dev-pw", ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("dev test: %d, want 403", resp.StatusCode)
	}

	// The audit trail: one replication.config.test row per probe that ran
	// (pass, fail, override, draft — the refused ladder arms probe nothing).
	events, err := h.md.Audits().Query(context.Background(),
		metadata.AuditQuery{Action: "replication.config.test", Limit: 50})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) < 4 {
		t.Fatalf("replication.config.test rows = %d, want at least 4", len(events))
	}
	for _, ev := range events {
		if ev.Actor != "admin" {
			t.Errorf("test row actor = %q, want admin", ev.Actor)
		}
		if contains(ev.Detail, "\"pw\"") || contains(ev.Detail, "target_password") {
			t.Errorf("test row detail carries credential material: %s", ev.Detail)
		}
	}
}

// TestReplicationBlockFacesDegraded: the two 501s — no store at all keeps
// the block family at not-configured; the gate-less assembly (a unit stack)
// answers the same honest degradation.
func TestReplicationBlockFacesDegraded(t *testing.T) {
	base := newReplHarness(t, false)
	// The family stack WITHOUT the gate: the block family degrades, the
	// config plane (store-backed) stays up.
	cfg := config.Defaults()
	cfg.Storage.DataDir = base.dataDir
	authSvc := auth.NewFromStore(base.md, cfg.Security.AnonymousAccess)
	s := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: base.md,
		Repos: base.md.Repos(), Replication: base.replStore,
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	h := &replHarness{t: t, srv: ts, md: base.md, replStore: base.replStore, dataDir: base.dataDir}
	if resp, raw := h.admin(http.MethodGet, "/binflow/api/v1/system/replications", ""); resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("gate-less GET: %d (%s), want 501", resp.StatusCode, raw)
	}
	if resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/system/replications/block", ""); resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("gate-less block: %d (%s), want 501", resp.StatusCode, raw)
	}
	if resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications/1/test", ""); resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("tester-less test: %d (%s), want 501", resp.StatusCode, raw)
	}
	h.createConfig(t, "dr-degraded") // the store face stays up
}
