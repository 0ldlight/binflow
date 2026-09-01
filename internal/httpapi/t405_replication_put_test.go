package httpapi_test

// T-405 AC ①~③: the PUT /binflow/api/v1/replications/{id} enable/disable
// face. Three angles:
//
//   - the REST contract: the flip's response is exactly the GET projection,
//     the sealed credential and every throttle cap survive the
//     read-modify-write, the decode tolerates (and ignores) a round-tripped
//     full row, and the audit trail records replication.config.update;
//   - the error ladder: 401/403/404/400 in the family's errors[] envelope,
//     plus the 501 degradation on an instance without the replication store;
//   - the ENGINE semantics the flip rides (AC ①'s parenthetical): the bit is
//     read per event and per drain pass, so a disabled config neither
//     enqueues nor gets its backlog claimed (stop), the same backlog drains
//     through the next sweep once re-enabled (resume), and an attempt
//     already parked inside its push completes after the flip (in-flight
//     rows finish their current attempt). The engine here is the real one
//     over the harness's store — only the push target is scripted.
//
// The replication package itself is untouched by T-405 (its Store already
// had UpdateConfig and the engine already reads the bit); these tests pin
// that contract from the REST side so the coupling cannot drift silently.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---- the happy face: flip, echo shape, persistence, tolerance, audit ----

func TestReplicationsUpdateFace(t *testing.T) {
	h := newReplHarness(t, true) // cipher wired: the flip must carry the sealed credential through

	// A fully-populated row (credential + caps) so the read-modify-write has
	// something to lose if it ever rebuilds the row carelessly.
	resp, raw := h.admin(http.MethodPost, "/binflow/api/v1/replications",
		`{"name":"dr-flip","source_repo":"libs-release","target_url":"https://dr.example.com","target_repo":"libs-dr","target_username":"repl","target_password":"hunter2","max_bandwidth_bytes_per_sec":1234,"max_items_per_push":7}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d, body %s", resp.StatusCode, raw)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatalf("create decode: %v (%s)", err, raw)
	}
	id := int64(created["id"].(float64)) //nolint:gosec // fixture row id from the local test store

	before, err := h.replStore.GetConfig(context.Background(), id)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !strings.HasPrefix(before.TargetPasswordEnc, "enc:v1:") {
		t.Fatalf("stored credential = %q, want the enc:v1 at-rest form", before.TargetPasswordEnc)
	}

	// STOP: 200 + exactly the GET projection's keys, enabled flipped.
	resp, raw = h.admin(http.MethodPut, fmt.Sprintf("/binflow/api/v1/replications/%d", id), `{"enabled":false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put stop: status %d, body %s", resp.StatusCode, raw)
	}
	var stopped map[string]any
	if err := json.Unmarshal([]byte(raw), &stopped); err != nil {
		t.Fatalf("put stop decode: %v (%s)", err, raw)
	}
	want := map[string]any{
		"id": created["id"], "name": "dr-flip", "source_repo": "libs-release",
		"target_url": "https://dr.example.com", "target_repo": "libs-dr",
		"target_username": "repl", "enabled": false,
		"max_bandwidth_bytes_per_sec": float64(1234), "max_items_per_push": float64(7),
	}
	if len(stopped) != len(want)+2 { // + created_at, updated_at
		t.Fatalf("put stop keys = %v (%d), want exactly %v plus the two timestamps", stopped, len(stopped), want)
	}
	for k, v := range want {
		got, ok := stopped[k]
		if !ok || fmt.Sprint(got) != fmt.Sprint(v) {
			t.Errorf("put stop[%s] = %v (%T), want %v", k, got, got, v)
		}
	}
	if stopped["created_at"] != created["created_at"] {
		t.Errorf("put stop created_at = %v, want the row's %v", stopped["created_at"], created["created_at"])
	}
	if s, _ := stopped["updated_at"].(string); s == "" {
		t.Error("put stop updated_at is empty")
	}
	for _, leak := range []string{"target_password", "target_password_enc"} {
		if _, ok := stopped[leak]; ok {
			t.Errorf("put stop response carries %q", leak)
		}
	}

	// The store's truth: the bit moved, the sealed credential and the caps
	// are byte-identical (no rebuild, no re-seal).
	after, err := h.replStore.GetConfig(context.Background(), id)
	if err != nil {
		t.Fatalf("GetConfig after stop: %v", err)
	}
	if after.Enabled {
		t.Error("config still enabled after the stop flip")
	}
	if after.TargetPasswordEnc != before.TargetPasswordEnc {
		t.Fatalf("sealed credential changed across the flip: %q -> %q", before.TargetPasswordEnc, after.TargetPasswordEnc)
	}
	if secret, _, derr := h.cipher.Decrypt(after.TargetPasswordEnc); derr != nil || secret != "hunter2" {
		t.Fatalf("credential no longer decrypts to the original: %q, %v", secret, derr)
	}
	if after.MaxBandwidthBytesPerSec != 1234 || after.MaxItemsPerPush != 7 {
		t.Errorf("throttle caps changed across the flip: %d/%d", after.MaxBandwidthBytesPerSec, after.MaxItemsPerPush)
	}

	// RESUME: the same face flips back.
	resp, raw = h.admin(http.MethodPut, fmt.Sprintf("/binflow/api/v1/replications/%d", id), `{"enabled":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put resume: status %d, body %s", resp.StatusCode, raw)
	}
	var resumed map[string]any
	_ = json.Unmarshal([]byte(raw), &resumed)
	if resumed["enabled"] != true {
		t.Errorf("put resume echoed enabled = %v, want true", resumed["enabled"])
	}
	if cfg, gerr := h.replStore.GetConfig(context.Background(), id); gerr != nil || !cfg.Enabled {
		t.Errorf("config after resume = enabled:%v (%v), want true", cfg.Enabled, gerr)
	}

	// DECODE TOLERANCE: a client round-tripping the whole row it read from
	// the list is accepted; every field besides enabled is ignored (the
	// full-field edit face is a later ticket — the mini-PUT ruling).
	resp, raw = h.admin(http.MethodPut, fmt.Sprintf("/binflow/api/v1/replications/%d", id),
		`{"name":"renamed","source_repo":"other-repo","target_url":"https://elsewhere.example.com","target_repo":"x","target_username":"evil","target_password":"nope","enabled":false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put full-shape: status %d, body %s", resp.StatusCode, raw)
	}
	ignored, err := h.replStore.GetConfig(context.Background(), id)
	if err != nil {
		t.Fatalf("GetConfig after full-shape put: %v", err)
	}
	switch {
	case ignored.Name != "dr-flip":
		t.Errorf("name rewritten by the tolerance arm: %q", ignored.Name)
	case ignored.SourceRepo != "libs-release":
		t.Errorf("source_repo rewritten by the tolerance arm: %q", ignored.SourceRepo)
	case ignored.TargetURL != "https://dr.example.com":
		t.Errorf("target_url rewritten by the tolerance arm: %q", ignored.TargetURL)
	case ignored.TargetUsername != "repl":
		t.Errorf("target_username rewritten by the tolerance arm: %q", ignored.TargetUsername)
	case ignored.TargetPasswordEnc != before.TargetPasswordEnc:
		t.Errorf("sealed credential rewritten by the tolerance arm: %q", ignored.TargetPasswordEnc)
	case ignored.Enabled:
		t.Error("tolerance arm did not apply its enabled:false")
	}

	// The governance trail: one update event per flip, the admin's name on
	// each, the config named in the detail.
	events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "replication.config.update", Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("update audit rows = %d, want 3 (stop/resume/tolerance): %+v", len(events), events)
	}
	for _, ev := range events {
		if ev.Actor != "admin" || ev.RepoKey != "libs-release" {
			t.Errorf("update audit row = actor %q repo %q, want admin/libs-release", ev.Actor, ev.RepoKey)
		}
		if !strings.Contains(ev.Detail, `"dr-flip"`) || !strings.Contains(ev.Detail, `"enabled"`) {
			t.Errorf("update audit detail = %s, want the config name and the bit", ev.Detail)
		}
	}
	// Query order is (time DESC, id DESC): the tolerance flip (false) is
	// newest, the resume (true) sits between it and the stop (false).
	if got := events[0].Detail; !strings.Contains(got, `"enabled":"false"`) {
		t.Errorf("newest update audit detail = %s, want enabled false (the tolerance flip)", got)
	}
	if got := events[1].Detail; !strings.Contains(got, `"enabled":"true"`) {
		t.Errorf("middle update audit detail = %s, want enabled true (the resume flip)", got)
	}
}

// ---- the error ladder (table-driven: no-permission / 404 / 400 shapes) ----

func TestReplicationsUpdateValidation(t *testing.T) {
	h := newReplHarness(t, false)
	created := h.createConfig(t, "dr-flip")
	id := fmt.Sprint(created["id"])

	cases := []struct {
		name    string
		target  string // %s substituted with the config id where needed
		user    string
		pass    string
		body    string
		want    int
		wantMsg string
	}{
		{"anonymous update is a 401", "/binflow/api/v1/replications/%s", "", "", `{"enabled":false}`, http.StatusUnauthorized, ""},
		{"non-admin update is a 403", "/binflow/api/v1/replications/%s", "dev", "dev-pw", `{"enabled":false}`, http.StatusForbidden, "administrator"},
		{"unknown id is a 404", "/binflow/api/v1/replications/999", "admin", "password", `{"enabled":false}`, http.StatusNotFound, "replication config not found"},
		{"non-numeric id is a 400", "/binflow/api/v1/replications/abc", "admin", "password", `{"enabled":false}`, http.StatusBadRequest, "id must be a positive integer"},
		{"zero id is a 400", "/binflow/api/v1/replications/0", "admin", "password", `{"enabled":false}`, http.StatusBadRequest, "id must be a positive integer"},
		{"negative id is a 400", "/binflow/api/v1/replications/-1", "admin", "password", `{"enabled":false}`, http.StatusBadRequest, "id must be a positive integer"},
		{"absent enabled is a 400", "/binflow/api/v1/replications/%s", "admin", "password", `{"name":"x"}`, http.StatusBadRequest, "enabled is required"},
		{"null enabled is a 400", "/binflow/api/v1/replications/%s", "admin", "password", `{"enabled":null}`, http.StatusBadRequest, "enabled is required"},
		{"string enabled is a 400", "/binflow/api/v1/replications/%s", "admin", "password", `{"enabled":"yes"}`, http.StatusBadRequest, "not valid JSON"},
		{"empty body is a 400", "/binflow/api/v1/replications/%s", "admin", "password", "", http.StatusBadRequest, "not valid JSON"},
		{"malformed body is a 400", "/binflow/api/v1/replications/%s", "admin", "password", `{{{`, http.StatusBadRequest, "not valid JSON"},
		{"two-segment path stays the E-26 404", "/binflow/api/v1/replications/%s/x", "admin", "password", `{"enabled":false}`, http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := strings.ReplaceAll(tc.target, "%s", id)
			resp, raw := h.do(http.MethodPut, path, tc.user, tc.pass, tc.body)
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

	// Nothing above mutated the row: still enabled, exactly one audit-free
	// posture (no replication.config.update events from the ladder).
	cfg, err := h.replStore.GetConfig(context.Background(), int64(created["id"].(float64))) //nolint:gosec // fixture id
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Error("validation ladder disabled the config")
	}
	events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{Action: "replication.config.update", Limit: 10})
	if err != nil || len(events) != 0 {
		t.Errorf("update audit rows after rejects = %d (%v), want none", len(events), err)
	}
}

// ---- the engine semantics the flip rides (AC ①'s stop/resume/in-flight) ----

// t405Blobs is a minimal replication.BlobSource: one in-memory payload per
// sha256 (the engine only needs the task's bytes).
type t405Blobs struct {
	content map[string]string
}

// Open implements replication.BlobSource.
func (b *t405Blobs) Open(_ context.Context, sha string) (io.ReadCloser, storage.BlobRef, error) {
	body, ok := b.content[sha]
	if !ok {
		return nil, storage.BlobRef{}, fmt.Errorf("blob %s: %w", sha, storage.ErrBlobNotFound)
	}
	return io.NopCloser(strings.NewReader(body)), storage.BlobRef{Sha256: sha, Size: int64(len(body))}, nil
}

// t405Target is the scripted push target: HEAD answers 404 (nothing pushed
// yet), PUT answers 201 echoing the declared checksum. armPark switches the
// PUT arm into the in-flight leg's posture — the first parked PUT signals
// `arrived` and parks until the test closes `release`.
type t405Target struct {
	mu      sync.Mutex
	heads   int
	puts    int
	arrived chan struct{}
	release chan struct{}
}

func (t *t405Target) handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		t.mu.Lock()
		t.heads++
		t.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	case http.MethodPut:
		_, _ = io.Copy(io.Discard, r.Body)
		t.mu.Lock()
		t.puts++
		arrived, release := t.arrived, t.release
		t.arrived = nil
		t.mu.Unlock()
		if arrived != nil {
			close(arrived)
		}
		if release != nil {
			<-release
		}
		w.Header().Set("X-Checksum-Sha256", r.Header.Get("X-Checksum-Sha256"))
		w.WriteHeader(http.StatusCreated)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (t *t405Target) counts() (heads, puts int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.heads, t.puts
}

// armPark makes the next PUT park after arrival (single-shot: the engine's
// one worker serializes pushes, so at most one PUT is ever in flight).
func (t *t405Target) armPark() (arrived, release chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.arrived = make(chan struct{})
	t.release = make(chan struct{})
	return t.arrived, t.release
}

// newT405Engine runs the real push engine over the harness's replication
// store with a fast sweep (the 1m cron fallback compressed for the test).
func newT405Engine(t *testing.T, store replication.Store, blobs replication.BlobSource) *replication.Engine {
	t.Helper()
	eng, err := replication.NewEngine(store, blobs, replication.EngineOptions{
		SweepInterval: 40 * time.Millisecond,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = eng.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Errorf("engine Run did not return after cancel")
		}
		eng.CloseIdleConnections()
	})
	return eng
}

// t405SeedTask appends one pending ledger row straight through the store
// (the REST face under test has no trigger; the backlog is the sweep's).
func t405SeedTask(t *testing.T, store replication.Store, cfgID int64, sha, path, createdAt string) {
	t.Helper()
	if _, err := store.CreateTask(context.Background(), &replication.ReplicationTask{
		ReplicationID: cfgID, BlobSHA256: sha, NodePath: path,
		Status: replication.TaskStatusPending, CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("CreateTask(%s): %v", path, err)
	}
}

// t405Task returns the ledger row of one node path (nil when absent).
func t405Task(t *testing.T, store replication.Store, cfgID int64, path string) *replication.ReplicationTask {
	t.Helper()
	tasks, err := store.ListTasks(context.Background(), cfgID, 100)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, task := range tasks {
		if task.NodePath == path {
			return task
		}
	}
	return nil
}

// t405WaitTask polls until the path's row reaches status.
func t405WaitTask(t *testing.T, store replication.Store, cfgID int64, path, status, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if task := t405Task(t, store, cfgID, path); task != nil && task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to reach %s", path, what)
}

func TestReplicationsUpdateEngineSemantics(t *testing.T) {
	h := newReplHarness(t, false)
	created := h.createConfig(t, "dr-flip")
	id := int64(created["id"].(float64)) //nolint:gosec // fixture row id

	payload := "t405 replication payload"
	sum := sha256.Sum256([]byte(payload))
	sha := hex.EncodeToString(sum[:])
	blobs := &t405Blobs{content: map[string]string{sha: payload}}

	tgt := &t405Target{}
	srv := httptest.NewServer(http.HandlerFunc(tgt.handler))
	t.Cleanup(srv.Close)

	// Repoint the config at the scripted target through the SAME seam the
	// flip uses (the store row IS the engine's only view of the config).
	cfg, err := h.replStore.GetConfig(context.Background(), id)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	cfg.TargetURL = srv.URL
	if err := h.replStore.UpdateConfig(context.Background(), cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	eng := newT405Engine(t, h.replStore, blobs)

	flip := func(enabled bool) {
		t.Helper()
		resp, raw := h.admin(http.MethodPut, fmt.Sprintf("/binflow/api/v1/replications/%d", id),
			fmt.Sprintf(`{"enabled":%t}`, enabled))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT enabled=%t: status %d, body %s", enabled, resp.StatusCode, raw)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatalf("PUT enabled=%t decode: %v", enabled, err)
		}
		if body["enabled"] != enabled {
			t.Fatalf("PUT enabled=%t echoed %v", enabled, body["enabled"])
		}
	}

	// Leg A — STOP lands without a restart: flip first, then the backlog.
	// The sweep (>= 6 ticks in the window) must not claim the pending row,
	// and the repo-hook enqueue face must append nothing for the bit.
	flip(false)
	t405SeedTask(t, h.replStore, id, sha, "org/app/a.bin", "2026-09-01T00:00:01Z")
	time.Sleep(250 * time.Millisecond)
	if task := t405Task(t, h.replStore, id, "org/app/a.bin"); task == nil || task.Status != replication.TaskStatusPending {
		t.Fatalf("backlog row while disabled = %+v, want untouched pending", task)
	}
	eng.Enqueue(context.Background(), "libs-release", "org/app/new.bin", sha)
	time.Sleep(100 * time.Millisecond)
	if tasks, terr := h.replStore.ListTasks(context.Background(), id, 100); terr != nil || len(tasks) != 1 {
		t.Fatalf("tasks after disabled enqueue = %d (%v), want the single seeded row", len(tasks), terr)
	}
	if heads, puts := tgt.counts(); heads != 0 || puts != 0 {
		t.Fatalf("target calls while disabled = HEAD %d, PUT %d; want none", heads, puts)
	}

	// Leg B — RESUME: the same backlog drains through the next sweep (the
	// flip needs no trigger of its own).
	flip(true)
	t405WaitTask(t, h.replStore, id, "org/app/a.bin", replication.TaskStatusSuccess, "the resumed backlog's success")
	if heads, puts := tgt.counts(); heads != 1 || puts != 1 {
		t.Fatalf("target calls after resume = HEAD %d, PUT %d; want one push", heads, puts)
	}

	// Leg C — IN-FLIGHT: an attempt parked inside its push completes after
	// the flip to disabled (the current attempt is never aborted), and
	// nothing new is claimed once the bit is down.
	arrived, release := tgt.armPark()
	t405SeedTask(t, h.replStore, id, sha, "org/app/b.bin", "2026-09-01T00:00:02Z")
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the sweep never claimed the parked task")
	}
	flip(false) // the bit goes down while the attempt is inside its PUT
	close(release)
	t405WaitTask(t, h.replStore, id, "org/app/b.bin", replication.TaskStatusSuccess, "the in-flight attempt's completion")
	t405SeedTask(t, h.replStore, id, sha, "org/app/c.bin", "2026-09-01T00:00:03Z")
	time.Sleep(250 * time.Millisecond)
	if task := t405Task(t, h.replStore, id, "org/app/c.bin"); task == nil || task.Status != replication.TaskStatusPending {
		t.Fatalf("post-flip backlog row = %+v, want untouched pending (no new claims)", task)
	}
	if heads, puts := tgt.counts(); heads != 2 || puts != 2 {
		t.Fatalf("target calls at the end = HEAD %d, PUT %d; want exactly the two pushes", heads, puts)
	}
}

// ---- the unwired-instance degradation (family ruling 6: 501, not 404) ----

func TestReplicationsUpdateNotConfigured(t *testing.T) {
	h := newReplHarness(t, false)
	cfg := config.Defaults()
	authSvc := auth.NewFromStore(h.md, cfg.Security.AnonymousAccess)
	s := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: h.md, Repos: h.md.Repos(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodPut, ts.URL+"/binflow/api/v1/replications/1",
		strings.NewReader(`{"enabled":false}`))
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("admin", "password")
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("PUT /api/v1/replications/1 on a bare assembly = %d, want 501 (body %s)", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "replication is not configured") {
		t.Fatalf("501 body %q lacks the not-configured wording", raw)
	}
}
