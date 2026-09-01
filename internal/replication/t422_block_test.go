package replication_test

// T-422 (FR-138.3) — the global blockPush/blockPull brake, three layers:
//
//	Unit (gate + engine): the K31 seed/persist contract of the gate, the
//	event-track drop (Enqueue appends nothing while blocked), the claim
//	track parking (drain claims nothing; the wake hook resumes on unblock),
//	the between-attempts halt (a task cycling its retry loop reverts to
//	pending at the next attempt boundary), and the trigger refusal
//	(§9.2-A-5 — 封锁与触发同门).
//
//	L28 (two real instances): the REST wire POST /api/v1/system/
//	replications/block stops a re-deploy of the source repository from
//	reaching the target (zero task rows, zero target nodes), the
//	configuration plane keeps flowing while the brake is on (the t226
//	UI-API-not-gated posture), the run trigger refuses with the anchored
//	skip wording, and an unblock + full sync converges the missed artifact.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
)

// ---- unit: the gate's seed/persist contract (K31 dual source) ----

func TestBlockGateSeedAndPersist(t *testing.T) {
	ctx, st := openStore(t)
	keeper, ok := st.(*replication.SQLiteStore)
	if !ok {
		t.Fatalf("store is %T, want *replication.SQLiteStore", st)
	}

	// Fresh database: Load seeds the row from the gate's initial value.
	gate := replication.NewBlockGate(keeper, replication.GlobalBlock{BlockPush: true}, nil, nil)
	if err := gate.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !gate.PushBlocked() || gate.PullBlocked() {
		t.Fatalf("flags after seed = push %t pull %t, want push true pull false", gate.PushBlocked(), gate.PullBlocked())
	}
	row, err := keeper.GetGlobalBlock(ctx)
	if err != nil || row == nil {
		t.Fatalf("seeded row: (%v, %v), want a row", row, err)
	}
	if !row.BlockPush || row.BlockPull || row.UpdatedBy != "" {
		t.Fatalf("seeded row = %+v, want push-only with empty actor", row)
	}

	// Restart with a DIFFERENT seed: the persisted row is authoritative.
	gate2 := replication.NewBlockGate(keeper, replication.GlobalBlock{BlockPull: true}, nil, nil)
	if err := gate2.Load(ctx); err != nil {
		t.Fatalf("Load(2): %v", err)
	}
	if !gate2.PushBlocked() || gate2.PullBlocked() {
		t.Fatalf("flags after reload = push %t pull %t, want the ROW (push true pull false)", gate2.PushBlocked(), gate2.PullBlocked())
	}

	// Update persists both directions and stamps the actor.
	next, err := gate2.Update(ctx, false, true, "admin")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if next.BlockPush || !next.BlockPull || next.UpdatedBy != "admin" || next.UpdatedAt == "" {
		t.Fatalf("update snapshot = %+v, want pull-only with actor", next)
	}
	row, err = keeper.GetGlobalBlock(ctx)
	if err != nil || row == nil || row.BlockPush || !row.BlockPull || row.UpdatedBy != "admin" {
		t.Fatalf("persisted row after update = (%+v, %v)", row, err)
	}
}

// blockedEngineFixture builds an engine with a gate over the real 009 store
// and one enabled config; the scripted target serves the generic push plane
// (HEAD 404 → PUT 201). sleep overrides the backoff sleeper (nil = no-op).
func blockedEngineFixture(t *testing.T, target http.HandlerFunc, sleep func(context.Context, time.Duration) error) (*replication.Engine, replication.Store, *replication.BlockGate) {
	t.Helper()
	server := httptest.NewServer(target)
	t.Cleanup(server.Close)
	_, store := openStore(t)
	keeper, ok := store.(*replication.SQLiteStore)
	if !ok {
		t.Fatalf("store is %T, want *replication.SQLiteStore", store)
	}
	gate := replication.NewBlockGate(keeper, replication.GlobalBlock{}, nil, nil)
	if err := gate.Load(context.Background()); err != nil {
		t.Fatalf("gate Load: %v", err)
	}
	if sleep == nil {
		sleep = func(context.Context, time.Duration) error { return nil }
	}
	blobs := &fakeBlobs{content: map[string]string{testSHA256(testPayload): testPayload}}
	eng, err := replication.NewEngine(store, blobs, replication.EngineOptions{
		Sleep:         sleep,
		SweepInterval: 40 * time.Millisecond,
		Meta:          &fakeMeta{pkg: map[string]string{"libs-local": "generic"}},
		Blocks:        gate,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	cfg := fixtureConfig("dr-blocked")
	cfg.TargetURL = server.URL
	cfg.SourceRepo = "libs-local"
	cfg.TargetRepo = "mirror"
	cfg.TargetPasswordEnc = "" // anonymous target (the T-420 fixture posture)
	if _, err := store.CreateConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	gate.SetWake(eng.Kick)
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = eng.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("engine Run did not return after cancel")
		}
		eng.CloseIdleConnections()
	})
	return eng, store, gate
}

// genericPushTarget scripts the generic plane's two hops: HEAD answers 404
// (not present), PUT answers 201.
func genericPushTarget(hits *atomic.Int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		switch r.Method {
		case http.MethodHead:
			http.NotFound(w, r)
		case http.MethodPut:
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}

// TestEnqueueBlockedDropsEvents: the event track — a blocked push appends
// NO task rows (a re-deploy of the source repository cannot reach the
// target), an unblocked one appends again.
func TestEnqueueBlockedDropsEvents(t *testing.T) {
	eng, store, gate := blockedEngineFixture(t, genericPushTarget(nil), nil)
	ctx := context.Background()
	if _, err := gate.Update(ctx, true, false, "admin"); err != nil {
		t.Fatalf("block: %v", err)
	}
	eng.Enqueue(ctx, "libs-local", "a/one.bin", testSHA256(testPayload))
	if tasks, err := store.ListTasks(ctx, 1, 100); err != nil || len(tasks) != 0 {
		t.Fatalf("tasks while blocked = %d (%v), want none", len(tasks), err)
	}
	if _, err := gate.Update(ctx, false, false, "admin"); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	eng.Enqueue(ctx, "libs-local", "a/one.bin", testSHA256(testPayload))
	waitForTaskCount(t, store, 1, 1, "the post-unblock event to drain")
}

// TestDrainParksWhileBlockedAndWakeResumes: the claim track — a pending row
// a blocked sweep never claims stays pending with zero attempts; the
// unblock's wake hook resumes the queue immediately (no sweep wait).
func TestDrainParksWhileBlockedAndWakeResumes(t *testing.T) {
	var hits atomic.Int64
	eng, store, gate := blockedEngineFixture(t, genericPushTarget(&hits), nil)
	ctx := context.Background()
	if _, err := store.CreateTask(ctx, &replication.ReplicationTask{
		ReplicationID: 1, BlobSHA256: testSHA256(testPayload), NodePath: "a/one.bin",
		Status: replication.TaskStatusPending, CreatedAt: "2026-09-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := gate.Update(ctx, true, false, "admin"); err != nil {
		t.Fatalf("block: %v", err)
	}
	// Several sweep ticks pass while blocked: nothing claimed, no contact.
	time.Sleep(300 * time.Millisecond)
	task, err := store.GetTask(ctx, 1)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != replication.TaskStatusPending || task.Attempts != 0 {
		t.Fatalf("task while blocked = (%s, attempts %d), want (pending, 0)", task.Status, task.Attempts)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("target hits while blocked = %d, want 0", got)
	}
	// Unblock fires the wake hook: the parked row drains within the test's
	// patience, long before the 40ms sweep would matter either way.
	if _, err := gate.Update(ctx, false, false, "admin"); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	waitForTaskCount(t, store, 1, 1, "the parked row to drain after the unblock wake")
	_ = eng
}

// TestProcessTaskHaltsBetweenAttemptsWhenBlocked: a brake that lands while a
// task cycles its retry loop stops the NEXT attempt — the row reverts to
// pending (never terminal), and the unblocked resume burns the remaining
// attempts to the exhausted failure.
func TestProcessTaskHaltsBetweenAttemptsWhenBlocked(t *testing.T) {
	failing := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("target boom"))
	}
	ctx := context.Background()
	var gate *replication.BlockGate
	var once sync.Once
	// The first backoff sleep (between attempts 1 and 2) flips the brake on:
	// the loop-top gate check must then revert the row instead of retrying.
	sleep := func(context.Context, time.Duration) error {
		once.Do(func() {
			if _, err := gate.Update(ctx, true, false, "test"); err != nil {
				t.Errorf("block during backoff: %v", err)
			}
		})
		return nil
	}
	eng, store, g := blockedEngineFixture(t, failing, sleep)
	gate = g

	if _, err := store.CreateTask(ctx, &replication.ReplicationTask{
		ReplicationID: 1, BlobSHA256: testSHA256(testPayload), NodePath: "a/one.bin",
		Status: replication.TaskStatusPending, CreatedAt: "2026-09-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	eng.Kick()
	waitFor := func(what string, cond func(*replication.ReplicationTask) bool) *replication.ReplicationTask {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			task, err := store.GetTask(ctx, 1)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if cond(task) {
				return task
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
	task := waitFor("the halted pending reversion", func(t *replication.ReplicationTask) bool {
		return t.Attempts >= 1 && t.Status == replication.TaskStatusPending &&
			strings.Contains(t.LastError, "blocked")
	})
	if task.Attempts != 1 {
		t.Fatalf("attempts after the halt = %d, want exactly 1 (no retry under the brake)", task.Attempts)
	}

	// Unblock: the sweep/wake resumes the cycle and the failing target burns
	// the remaining attempts to the exhausted terminal failure.
	if _, err := gate.Update(ctx, false, false, "test"); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	task = waitFor("the exhausted failure", func(t *replication.ReplicationTask) bool {
		return t.Status == replication.TaskStatusFailed
	})
	if task.Attempts != 6 { // the initial attempt + the five backoff retries
		t.Fatalf("terminal attempts = %d, want 6", task.Attempts)
	}
}

// TestTriggerBlockedRefusal: §9.2-A-5 — the manual trigger checks the brake
// before scheduling and fails immediately (never queue-behind).
func TestTriggerBlockedRefusal(t *testing.T) {
	eng, store, gate := blockedEngineFixture(t, genericPushTarget(nil), nil)
	ctx := context.Background()
	cfg, err := store.GetConfig(ctx, 1)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if _, err := gate.Update(ctx, true, false, "admin"); err != nil {
		t.Fatalf("block: %v", err)
	}
	_, err = eng.TriggerFullSync(ctx, cfg)
	if !errors.Is(err, replication.ErrPushBlocked) {
		t.Fatalf("blocked trigger err = %v, want ErrPushBlocked", err)
	}
	if tasks, terr := store.ListTasks(ctx, cfg.ID, 100); terr != nil || len(tasks) != 0 {
		t.Fatalf("tasks after the refused trigger = %d (%v), want none", len(tasks), terr)
	}
	if _, err := gate.Update(ctx, false, false, "admin"); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	res, err := eng.TriggerFullSync(ctx, cfg)
	if err != nil || res.Scheduled != 0 {
		t.Fatalf("unblocked trigger = (%+v, %v), want an empty successful run", res, err)
	}
}

// ---- L28: the two-instance REST wire ----

// TestT422BlockPushTwoInstance walks the AC's core lane: block → re-deploy
// reaches nothing (no rows, no target nodes), the config plane keeps
// flowing, the trigger refuses, unblock + full sync converges the miss.
func TestT422BlockPushTwoInstance(t *testing.T) {
	ctx := context.Background()
	b := newBinFlow(t, "B422", "pw-target", []*metadata.Repo{
		{RepoKey: "replica-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	a := newT420Source(t, []*metadata.Repo{
		{RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})

	// The brake reads false first (official camelCase pair, both keys).
	gresp, graw := a.do(http.MethodGet, "/binflow/api/v1/system/replications", "admin", "pw-source", nil)
	if gresp.StatusCode != http.StatusOK {
		t.Fatalf("block GET: %d (%s)", gresp.StatusCode, graw)
	}
	var blockState map[string]bool
	if err := json.Unmarshal(graw, &blockState); err != nil {
		t.Fatalf("block GET decode: %v (%s)", err, graw)
	}
	if len(blockState) != 2 || blockState["blockPushReplications"] || blockState["blockPullReplications"] {
		t.Fatalf("initial block state = %v, want exactly the two false keys", blockState)
	}

	// The push config (plaintext password sealed server-side).
	cfgBody, _ := json.Marshal(map[string]any{
		"name": "t422-dr", "source_repo": "libs", "target_url": b.url,
		"target_repo": "replica-local", "target_username": "admin",
		"target_password": "pw-target", "enabled": true,
	})
	cresp, craw := a.do(http.MethodPost, "/binflow/api/v1/replications", "admin", "pw-source", cfgBody)
	if cresp.StatusCode != http.StatusCreated {
		t.Fatalf("create config: %d (%s)", cresp.StatusCode, craw)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(craw, &created); err != nil {
		t.Fatalf("create decode: %v", err)
	}

	// Baseline: an artifact uploaded while unblocked converges.
	put := func(path string, body []byte) {
		t.Helper()
		resp, _ := a.do(http.MethodPut, "/binflow/libs/"+path, "admin", "pw-source", body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("upload %s: %d", path, resp.StatusCode)
		}
	}
	put("one.bin", []byte("t422 before the brake"))
	waitForTaskCount(t, a.store, created.ID, 1, "the baseline event to drain")
	if got := repoFileSums(t, b.md, "replica-local")["one.bin"]; got == "" {
		t.Fatalf("baseline artifact never landed on B")
	}

	// BLOCK the push direction alone (pull untouched — §9.2-B-1's selector).
	bresp, braw := a.do(http.MethodPost, "/binflow/api/v1/system/replications/block?push=true&pull=false",
		"admin", "pw-source", nil)
	if bresp.StatusCode != http.StatusOK {
		t.Fatalf("block: %d (%s)", bresp.StatusCode, braw)
	}
	if got := strings.TrimSpace(string(braw)); got != "Successfully blocked all push replications, no push replication will be triggered." {
		t.Fatalf("block message = %q, want the §9.2-B-3 push variant", got)
	}
	if ct := bresp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("block content-type = %q, want text/plain", ct)
	}
	_, graw = a.do(http.MethodGet, "/binflow/api/v1/system/replications", "admin", "pw-source", nil)
	if err := json.Unmarshal(graw, &blockState); err != nil {
		t.Fatalf("block GET decode: %v", err)
	}
	if !blockState["blockPushReplications"] || blockState["blockPullReplications"] {
		t.Fatalf("state after block = %v, want push-only", blockState)
	}

	// The AC's core: a re-deploy of the source reaches NOTHING — no task
	// rows, no target node (the event hook consults the gate).
	put("two.bin", []byte("t422 behind the brake"))
	time.Sleep(time.Second) // the enqueue hook runs detached; a second is ample
	if tasks, terr := a.store.ListTasks(ctx, created.ID, 100); terr != nil || len(tasks) != 1 {
		t.Fatalf("tasks while blocked = %d (%v), want still 1 (no new rows)", len(tasks), terr)
	}
	if got := repoFileSums(t, b.md, "replica-local")["two.bin"]; got != "" {
		t.Fatalf("two.bin reached B while push was blocked")
	}

	// The configuration plane keeps flowing while the brake is on (the t226
	// UI-API-not-gated posture — the REST family here).
	draft, _ := json.Marshal(map[string]any{
		"name": "t422-second", "source_repo": "libs", "target_url": b.url,
		"target_repo": "replica-local", "enabled": false,
	})
	dresp, draw := a.do(http.MethodPost, "/binflow/api/v1/replications", "admin", "pw-source", draft)
	if dresp.StatusCode != http.StatusCreated {
		t.Fatalf("create while blocked: %d (%s), want 201 (config plane ungated)", dresp.StatusCode, draw)
	}
	eres, eraw := a.do(http.MethodPut, "/binflow/api/v1/replications/2", "admin", "pw-source",
		[]byte(`{"enabled":true}`))
	if eres.StatusCode != http.StatusOK {
		t.Fatalf("PUT enabled while blocked: %d (%s), want 200", eres.StatusCode, eraw)
	}
	xres, _ := a.do(http.MethodDelete, "/binflow/api/v1/replications/t422-second", "admin", "pw-source", nil)
	if xres.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE while blocked: %d, want 204", xres.StatusCode)
	}

	// The manual trigger refuses with the anchored §9.2-A-5 skip wording.
	rres, rraw := a.do(http.MethodPost, fmt.Sprintf("/binflow/api/v1/replications/%d/run", created.ID),
		"admin", "pw-source", nil)
	if rres.StatusCode != http.StatusConflict {
		t.Fatalf("run while blocked: %d (%s), want 409", rres.StatusCode, rraw)
	}
	if !strings.Contains(string(rraw), "Push replication is blocked, skipping replication") {
		t.Fatalf("run refusal = %q, want the anchored skip wording", rraw)
	}

	// UNBLOCK the push direction alone (pull=false leaves it — §9.2-B-1's
	// selector); the full sync then converges the missed artifact.
	ures, uraw := a.do(http.MethodPost, "/binflow/api/v1/system/replications/unblock?push=true&pull=false",
		"admin", "pw-source", nil)
	if ures.StatusCode != http.StatusOK ||
		strings.TrimSpace(string(uraw)) != "Successfully unblocked all push replications." {
		t.Fatalf("unblock = %d (%s), want the §9.2-B-3 push variant", ures.StatusCode, uraw)
	}
	tr, traw := a.do(http.MethodPost, fmt.Sprintf("/binflow/api/v1/replications/%d/run", created.ID),
		"admin", "pw-source", nil)
	if tr.StatusCode != http.StatusOK {
		t.Fatalf("run after unblock: %d (%s)", tr.StatusCode, traw)
	}
	// The full sync re-pushes BOTH files (one.bin as an idempotent hit,
	// two.bin as the transfer the brake ate): three success rows total.
	waitForTaskCount(t, a.store, created.ID, 3, "the post-unblock full sync")
	if got := repoFileSums(t, b.md, "replica-local")["two.bin"]; got == "" {
		t.Fatalf("two.bin never converged after the unblock + full sync")
	}

	// The governance trail: one replication.block.update row per flip, the
	// final states in the detail.
	events, err := a.md.Audits().Query(ctx, metadata.AuditQuery{Action: "replication.block.update", Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("replication.block.update rows = %d, want 2 (block + unblock)", len(events))
	}
	for _, ev := range events {
		if ev.Actor != "admin" {
			t.Errorf("block row actor = %q, want admin", ev.Actor)
		}
	}
	// Query returns newest-first: the first row is the unblock (push back to
	// false, pull still false), the second the block (push true).
	if !strings.Contains(events[0].Detail, "\"blockPush\":\"false\"") {
		t.Fatalf("newest block row detail = %s, want blockPush false", events[0].Detail)
	}
	if !strings.Contains(events[1].Detail, "\"blockPush\":\"true\"") {
		t.Fatalf("oldest block row detail = %s, want blockPush true", events[1].Detail)
	}
}
