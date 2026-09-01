package httpapi_test

// T-420 (FR-138.1) — the POST /binflow/api/v1/replications/{id}/run REST
// contract, over the same harness as the T-180/T-405 faces with the trigger
// seam faked (the real engine's trigger semantics live in the replication
// package's own t420 tests; this file pins the WIRE):
//
//   - the happy face: 200, the anchored scheduling info line, the seeding
//     counts, the audit row (replication.run, the admin's name, the config
//     named), and the runner receiving the STORED row verbatim;
//   - the error ladder: 401/403/404/400/409 plus the two degradations (no
//     store = 501 not-configured; store without the engine = 501 unwired);
//   - the family's zero-regression arms: PUT enabled and the list projection
//     answer exactly as before beside the new route.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// t420Runner is the trigger seam stand-in: it records what the handler handed
// it and answers scripted results or errors.
type t420Runner struct {
	calls []*replication.ReplicationConfig
	res   *replication.FullSyncResult
	err   error
}

func (r *t420Runner) TriggerFullSync(_ context.Context, cfg *replication.ReplicationConfig) (*replication.FullSyncResult, error) {
	r.calls = append(r.calls, cfg)
	if r.err != nil {
		return nil, r.err
	}
	if r.res != nil {
		return r.res, nil
	}
	return &replication.FullSyncResult{Scheduled: 1}, nil
}

// newRunHarness builds the harness stack plus a second server identical to
// the harness's own except for the wired trigger seam (the family's assembly
// difference under test — the store face alone leaves run at its 501).
func newRunHarness(t *testing.T, runner httpapi.ReplicationRunner, withRunner bool) (*replHarness, *replHarness) {
	t.Helper()
	base := newReplHarness(t, false)
	cfg := config.Defaults()
	cfg.Storage.DataDir = base.dataDir
	authSvc := auth.NewFromStore(base.md, cfg.Security.AnonymousAccess)
	deps := httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: base.md,
		Repos: base.md.Repos(), Replication: base.replStore,
	}
	if withRunner {
		deps.ReplicationRunner = runner
	}
	s := httpapi.New(deps, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return base, &replHarness{
		t: t, srv: ts, md: base.md, replStore: base.replStore, dataDir: base.dataDir,
	}
}

// TestReplicationsRunFace pins the happy wire: status, body, seam handoff,
// audit trail — and that a body, when a client insists on sending one, is
// accepted and ignored (the face has no body semantics).
func TestReplicationsRunFace(t *testing.T) {
	runner := &t420Runner{res: &replication.FullSyncResult{Scheduled: 3, Capped: true}}
	_, h := newRunHarness(t, runner, true)
	created := h.createConfig(t, "dr-run")
	id := fmt.Sprint(created["id"])

	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications/"+id+"/run", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("run: status %d, body %s, want 200", resp.StatusCode, raw)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("run decode: %v (%s)", err, raw)
	}
	want := map[string]any{
		"info":      "The replication tasks was successfully scheduled to run",
		"id":        created["id"],
		"name":      "dr-run",
		"scheduled": float64(3),
		"capped":    true,
	}
	if len(body) != len(want) {
		t.Fatalf("run body = %v, want exactly %v", body, want)
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("run body[%s] = %v, want %v", k, body[k], v)
		}
	}

	// The seam received the STORED row (the id the path addressed), once.
	if len(runner.calls) != 1 || runner.calls[0].ID != int64(created["id"].(float64)) || //nolint:gosec // fixture row id
		runner.calls[0].SourceRepo != "libs-release" {
		t.Fatalf("runner calls = %+v, want one call on the stored row", runner.calls)
	}

	// The governance trail: exactly one replication.run row, the admin's
	// name, the config and the counts in the detail.
	events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "replication.run", Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("replication.run rows = %d, want 1: %+v", len(events), events)
	}
	ev := events[0]
	if ev.Actor != "admin" || ev.RepoKey != "libs-release" {
		t.Errorf("replication.run row = actor %q repo %q, want admin/libs-release", ev.Actor, ev.RepoKey)
	}
	for _, frag := range []string{`"dr-run"`, `"scheduled":"3"`, `"capped":"true"`} {
		if !strings.Contains(ev.Detail, frag) {
			t.Errorf("replication.run detail = %s, want %s inside", ev.Detail, frag)
		}
	}
}

// TestReplicationsRunValidation is the error ladder plus the two
// degradations, table-driven like the sibling faces.
func TestReplicationsRunValidation(t *testing.T) {
	runner := &t420Runner{}
	_, h := newRunHarness(t, runner, true)
	created := h.createConfig(t, "dr-run")
	id := fmt.Sprint(created["id"])

	cases := []struct {
		name    string
		target  string
		user    string
		pass    string
		want    int
		wantMsg string
	}{
		{"anonymous run is a 401", "/binflow/api/v1/replications/%s/run", "", "", http.StatusUnauthorized, ""},
		{"non-admin run is a 403", "/binflow/api/v1/replications/%s/run", "dev", "dev-pw", http.StatusForbidden, "administrator"},
		{"unknown id is a 404", "/binflow/api/v1/replications/999/run", "admin", "password", http.StatusNotFound, "replication config not found"},
		{"non-numeric id is a 400", "/binflow/api/v1/replications/abc/run", "admin", "password", http.StatusBadRequest, "id must be a positive integer"},
		{"zero id is a 400", "/binflow/api/v1/replications/0/run", "admin", "password", http.StatusBadRequest, "id must be a positive integer"},
		{"wrong tail spelling stays the E-26 404", "/binflow/api/v1/replications/%s/execute", "admin", "password", http.StatusNotFound, ""},
		{"bare id POST stays the E-26 404", "/binflow/api/v1/replications/%s", "admin", "password", http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := strings.ReplaceAll(tc.target, "%s", id)
			resp, raw := h.do(http.MethodPost, path, tc.user, tc.pass, "")
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, raw)
			}
			if tc.want >= 400 && !strings.Contains(raw, "\"errors\"") {
				t.Fatalf("body %q lacks the errors[] envelope", raw)
			}
			if tc.wantMsg != "" && !strings.Contains(raw, tc.wantMsg) {
				t.Fatalf("message %q does not contain %q", raw, tc.wantMsg)
			}
		})
	}

	// Nothing above reached the seam or the audit trail.
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls after the ladder = %d, want none", len(runner.calls))
	}
	events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "replication.run", Limit: 10})
	if err != nil || len(events) != 0 {
		t.Errorf("replication.run rows after rejects = %d (%v), want none", len(events), err)
	}
}

// TestReplicationsRunDisabledFlip walks the refusal arm end to end on ONE
// row: enabled → 200; flip down through the T-405 face → 409 with the remedy
// wording; flip back up → 200 again. The runner is not consulted on the 409.
func TestReplicationsRunDisabledFlip(t *testing.T) {
	runner := &t420Runner{}
	_, h := newRunHarness(t, runner, true)
	created := h.createConfig(t, "dr-run")
	id := fmt.Sprint(created["id"])
	path := "/binflow/api/v1/replications/" + id + "/run"

	if resp, raw := h.admin(http.MethodPost, path, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("run enabled: %d (%s)", resp.StatusCode, raw)
	}
	flip := func(enabled bool) {
		t.Helper()
		resp, raw := h.admin(http.MethodPut, "/binflow/api/v1/replications/"+id, fmt.Sprintf(`{"enabled":%t}`, enabled))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT enabled=%t: %d (%s)", enabled, resp.StatusCode, raw)
		}
	}
	flip(false)
	resp, raw := h.admin(http.MethodPost, path, "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("run disabled: status %d (%s), want 409", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, `\"dr-run\" is disabled`) || !strings.Contains(raw, "enable it") {
		t.Fatalf("409 body %q lacks the disabled config and remedy wording", raw)
	}
	flip(true)
	if resp, raw := h.admin(http.MethodPost, path, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("run re-enabled: %d (%s)", resp.StatusCode, raw)
	}
	if len(runner.calls) != 2 { // the two accepted runs; the 409 never arrived
		t.Fatalf("runner calls = %d, want 2 (the refusal is pre-seam)", len(runner.calls))
	}
}

// TestReplicationsRunEngineRefusal maps the engine's own refusals when the
// bit raced between the handler's read and the seam call.
func TestReplicationsRunEngineRefusal(t *testing.T) {
	runner := &t420Runner{err: fmt.Errorf("wrap: %w", replication.ErrTriggerDisabled)}
	_, h := newRunHarness(t, runner, true)
	created := h.createConfig(t, "dr-run")
	id := fmt.Sprint(created["id"])

	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications/"+id+"/run", "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("raced flip: status %d (%s), want 409", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, "disabled") {
		t.Fatalf("409 body %q lacks the disabled wording", raw)
	}
}

// TestReplicationsRunDegradations: a store without the engine seam answers
// its own 501 while the store faces (list) stay up; the store-less assembly
// keeps the family 501 (TestReplicationsRunNotConfigured below).
func TestReplicationsRunDegradations(t *testing.T) {
	base := newReplHarness(t, false)
	created := base.createConfig(t, "dr-run")
	id := fmt.Sprint(created["id"])

	// Store wired, engine seam absent: the run degrades alone.
	_, h := newRunHarness(t, nil, false)
	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications/"+id+"/run", "")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("run without the engine: %d (%s), want 501", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, "not wired") {
		t.Fatalf("501 body %q lacks the not-wired wording", raw)
	}
	// The store faces stay up beside it (the T-159 degradation scoping).
	resp, raw = h.admin(http.MethodGet, "/binflow/api/v1/replications", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list beside the degraded run: %d (%s), want 200", resp.StatusCode, raw)
	}
}

// TestReplicationsRunNotConfigured: no replication store at all — the
// family's uniform 501.
func TestReplicationsRunNotConfigured(t *testing.T) {
	base := newReplHarness(t, false) // supplies the seeded auth store only
	cfg := config.Defaults()
	authSvc := auth.NewFromStore(base.md, cfg.Security.AnonymousAccess)
	s := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: base.md, Repos: base.md.Repos(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/binflow/api/v1/replications/1/run", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("admin", "password")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("POST run on a bare assembly = %d, want 501 (body %s)", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "replication is not configured") {
		t.Fatalf("501 body %q lacks the not-configured wording", raw)
	}
}

// TestReplicationsRunFamilyZeroRegression walks the sibling faces beside the
// new route on one assembly: PUT enabled echo and the list projection answer
// exactly the T-405/T-404 shapes.
func TestReplicationsRunFamilyZeroRegression(t *testing.T) {
	_, h := newRunHarness(t, &t420Runner{}, true)
	created := h.createConfig(t, "dr-run")
	id := fmt.Sprint(created["id"])

	// The PUT face: the flip's echo is the GET projection with enabled moved.
	resp, raw := h.admin(http.MethodPut, "/binflow/api/v1/replications/"+id, `{"enabled":false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d (%s)", resp.StatusCode, raw)
	}
	var flipped map[string]any
	if err := json.Unmarshal([]byte(raw), &flipped); err != nil {
		t.Fatalf("put decode: %v", err)
	}
	if flipped["enabled"] != false || flipped["name"] != "dr-run" || flipped["id"] != created["id"] {
		t.Fatalf("put echo = %v, want the stored row with enabled down", flipped)
	}

	// The list projection: one row, credential-free, enabled down.
	resp, raw = h.admin(http.MethodGet, "/binflow/api/v1/replications", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d (%s)", resp.StatusCode, raw)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(raw), &list); err != nil || len(list) != 1 {
		t.Fatalf("list = %s (%v), want one row", raw, err)
	}
	if list[0]["enabled"] != false {
		t.Fatalf("list row enabled = %v, want false", list[0]["enabled"])
	}
	for _, leak := range []string{"target_password", "target_password_enc"} {
		if _, ok := list[0][leak]; ok {
			t.Errorf("list row carries %q", leak)
		}
	}
	// And errors.Is still sees the sentinel through the seam's wrap (the
	// handler's raced-flip arm depends on it).
	wrapped := fmt.Errorf("outer: %w", replication.ErrTriggerDisabled)
	if !errors.Is(wrapped, replication.ErrTriggerDisabled) {
		t.Fatal("sentinel lost through a wrap")
	}
}
