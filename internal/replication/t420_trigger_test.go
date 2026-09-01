package replication_test

// T-420 (FR-138.1) — the manual full-sync trigger, two layers:
//
//	Unit (scripted target + fakeMeta): the seeding contract — one pending
//	row per FILE node of the source repository, the MaxItemsPerPush cap as
//	a cap+1 probe, the empty-repo run, the disabled and no-seam refusals,
//	and §9.2-A-7's no-dedup repeat (a second trigger while the first pass
//	is parked INSIDE its push still appends — the anchored in-flight leg).
//
//	L28 (two real instances): the REST wire POST /api/v1/replications/{id}/
//	run schedules the config's full reconciliation — artifacts that landed
//	BEFORE the config existed (so no event hook ever fired) converge, the
//	target's artifact count and checksums match the source exactly, the
//	repeat trigger converges again (idempotent sha256 hits), and a DOWN
//	enabled bit refuses the run. Observability rides the existing status
//	face (T-159) — no new query surface.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"

	_ "modernc.org/sqlite" // the replication store's own connection
)

// ---- unit: the seeding contract ----

// triggerFixture wires an engine with a faked MetaSource over the real 009
// store; cfgMutate adjusts the single config row (newEngineFixture creates
// it with the fixture defaults).
func triggerFixture(t *testing.T, target http.HandlerFunc, meta *fakeMeta, cfgMutate func(*replication.ReplicationConfig)) (*replication.Engine, replication.Store) {
	t.Helper()
	server := httptest.NewServer(target)
	t.Cleanup(server.Close)
	_, store := openStore(t)
	if meta == nil {
		meta = &fakeMeta{}
	}
	// The real seam answers the source repository's package type; without
	// this entry the fake fails plane selection and every push dies before
	// the wire (the 009 store did seed the repo row itself).
	if meta.pkg == nil {
		meta.pkg = map[string]string{}
	}
	if _, ok := meta.pkg["libs-local"]; !ok {
		meta.pkg["libs-local"] = "generic"
	}
	blobs := &fakeBlobs{content: map[string]string{testSHA256(testPayload): testPayload}}
	eng, err := replication.NewEngine(store, blobs, replication.EngineOptions{
		Sleep: func(context.Context, time.Duration) error { return nil },
		Meta:  meta,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	cfg := fixtureConfig("dr-trigger")
	cfg.TargetURL = server.URL
	cfg.SourceRepo = "libs-local"
	cfg.TargetRepo = "mirror"
	cfg.TargetPasswordEnc = ""
	if cfgMutate != nil {
		cfgMutate(cfg)
	}
	if _, err := store.CreateConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
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
	return eng, store
}

// filesOf builds a path-ordered file list with distinct payloads.
func filesOf(paths ...string) []replication.NodeFile {
	out := make([]replication.NodeFile, 0, len(paths))
	for _, p := range paths {
		body := "t420 payload " + p
		sum := sha256.Sum256([]byte(body))
		out = append(out, replication.NodeFile{Path: p, Sha256: hex.EncodeToString(sum[:])})
	}
	return out
}

// TestTriggerFullSyncSeeding walks the seeding contract table-style.
func TestTriggerFullSyncSeeding(t *testing.T) {
	meta := &fakeMeta{
		files: map[string][]replication.NodeFile{
			"libs-local": filesOf("a/one.bin", "b/two.bin", "c/three.bin"),
			"other-repo": filesOf("x.bin"),
		},
	}
	eng, store := triggerFixture(t, (&scriptTarget{}).handler, meta, nil)
	ctx := context.Background()

	cfgs, err := store.ListConfigs(ctx)
	if err != nil || len(cfgs) != 1 {
		t.Fatalf("ListConfigs = %d (%v), want the fixture config", len(cfgs), err)
	}
	cfg := cfgs[0]

	res, err := eng.TriggerFullSync(ctx, cfg)
	if err != nil {
		t.Fatalf("TriggerFullSync: %v", err)
	}
	if res.Scheduled != 3 || res.Capped {
		t.Fatalf("result = %+v, want {3 false}", res)
	}
	waitForTaskCount(t, store, cfg.ID, 3, "the seeded pass to drain")

	// The rows are the FILE nodes — one per path, sha256 carried verbatim.
	tasks, err := store.ListTasks(ctx, cfg.ID, 10)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	got := map[string]string{}
	for _, task := range tasks {
		got[task.NodePath] = task.BlobSHA256
		if task.Status != replication.TaskStatusSuccess {
			t.Errorf("task %s = %s, want success", task.NodePath, task.Status)
		}
	}
	for _, f := range meta.files["libs-local"] {
		if got[f.Path] != f.Sha256 {
			t.Errorf("task row %s = sha %s, want %s", f.Path, got[f.Path], f.Sha256)
		}
	}
}

// TestTriggerFullSyncCap pins the MaxItemsPerPush arm: the cap+1 probe keeps
// the first cap rows (path order) and reports Capped.
func TestTriggerFullSyncCap(t *testing.T) {
	files := filesOf("a/1.bin", "b/2.bin", "c/3.bin", "d/4.bin")
	eng, store := triggerFixture(t, (&scriptTarget{}).handler,
		&fakeMeta{files: map[string][]replication.NodeFile{"libs-local": files}},
		func(c *replication.ReplicationConfig) { c.MaxItemsPerPush = 2 })
	ctx := context.Background()
	cfgs, _ := store.ListConfigs(ctx)
	cfg := cfgs[0]

	res, err := eng.TriggerFullSync(ctx, cfg)
	if err != nil {
		t.Fatalf("TriggerFullSync: %v", err)
	}
	if res.Scheduled != 2 || !res.Capped {
		t.Fatalf("result = %+v, want {2 true}", res)
	}
	tasks, err := store.ListTasks(ctx, cfg.ID, 10)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want the capped 2", len(tasks))
	}
	// The kept slice is the head of the path-ordered enumeration.
	for i, want := range []string{"a/1.bin", "b/2.bin"} {
		if !taskPathPresent(tasks, want) {
			t.Errorf("capped pass kept %q somewhere unexpected (row %d)", want, i)
		}
	}
}

func taskPathPresent(tasks []*replication.ReplicationTask, path string) bool {
	for _, task := range tasks {
		if task.NodePath == path {
			return true
		}
	}
	return false
}

// TestTriggerFullSyncRefusals: the empty repository is a valid empty run;
// the down bit and the absent seam are refusals; a repeat run never dedups.
func TestTriggerFullSyncRefusals(t *testing.T) {
	ctx := context.Background()

	t.Run("empty repository schedules nothing", func(t *testing.T) {
		eng, store := triggerFixture(t, (&scriptTarget{}).handler, &fakeMeta{}, nil)
		cfgs, _ := store.ListConfigs(ctx)
		res, err := eng.TriggerFullSync(ctx, cfgs[0])
		if err != nil {
			t.Fatalf("TriggerFullSync: %v", err)
		}
		if res.Scheduled != 0 || res.Capped {
			t.Fatalf("result = %+v, want a clean empty run", res)
		}
	})

	t.Run("disabled config is refused", func(t *testing.T) {
		eng, store := triggerFixture(t, (&scriptTarget{}).handler, &fakeMeta{}, func(c *replication.ReplicationConfig) {
			c.Enabled = false
		})
		cfgs, _ := store.ListConfigs(ctx)
		_, err := eng.TriggerFullSync(ctx, cfgs[0])
		if !errors.Is(err, replication.ErrTriggerDisabled) {
			t.Fatalf("err = %v, want ErrTriggerDisabled", err)
		}
		if tasks, terr := store.ListTasks(ctx, cfgs[0].ID, 10); terr != nil || len(tasks) != 0 {
			t.Fatalf("tasks after refusal = %d (%v), want none", len(tasks), terr)
		}
	})

	t.Run("engine without the meta seam is refused", func(t *testing.T) {
		// newEngineFixture leaves Meta nil when opts says so — the trigger
		// must answer the honest seam error, never a fake empty run.
		f := newEngineFixture(t, &scriptTarget{}, &replication.EngineOptions{}, nil)
		cfgs, _ := f.store.ListConfigs(f.ctx)
		if _, err := f.engine.TriggerFullSync(f.ctx, cfgs[0]); !errors.Is(err, replication.ErrNoMetaSeam) {
			t.Fatalf("err = %v, want ErrNoMetaSeam", err)
		}
	})

	t.Run("repeat run does not dedup", func(t *testing.T) {
		eng, store := triggerFixture(t, (&scriptTarget{}).handler, &fakeMeta{
			files: map[string][]replication.NodeFile{"libs-local": filesOf("a/1.bin")},
		}, nil)
		cfgs, _ := store.ListConfigs(ctx)
		for i := 0; i < 2; i++ {
			res, err := eng.TriggerFullSync(ctx, cfgs[0])
			if err != nil {
				t.Fatalf("trigger %d: %v", i+1, err)
			}
			if res.Scheduled != 1 {
				t.Fatalf("trigger %d scheduled %d, want 1 (no merge)", i+1, res.Scheduled)
			}
		}
		waitForTaskCount(t, store, cfgs[0].ID, 2, "both passes' rows")
	})
}

// parkTarget parks every PUT after arrival until released — the deterministic
// in-flight posture for §9.2-A-7's repeat-trigger leg.
type parkTarget struct {
	mu      sync.Mutex
	arrived chan struct{}
	release chan struct{}
}

func (p *parkTarget) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	p.mu.Lock()
	arrived, release := p.arrived, p.release
	p.arrived = nil
	p.mu.Unlock()
	if arrived != nil {
		close(arrived)
	}
	if release != nil {
		<-release
	}
	w.Header().Set("X-Checksum-Sha256", r.Header.Get("X-Checksum-Sha256"))
	w.WriteHeader(http.StatusCreated)
}

func (p *parkTarget) arm() (arrived, release chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.arrived = make(chan struct{})
	p.release = make(chan struct{})
	return p.arrived, p.release
}

// TestTriggerRepeatWhileInFlight is the anchored in-flight leg: the worker
// parked INSIDE the first pass's PUT, the trigger face still answers (and
// appends) — no merge, no refusal (§9.2-A-7, medium confidence).
func TestTriggerRepeatWhileInFlight(t *testing.T) {
	tgt := &parkTarget{}
	eng, store := triggerFixture(t, tgt.handler, &fakeMeta{
		files: map[string][]replication.NodeFile{"libs-local": filesOf("a/1.bin")},
	}, nil)
	ctx := context.Background()
	cfgs, _ := store.ListConfigs(ctx)
	cfg := cfgs[0]

	arrived, release := tgt.arm()
	if _, err := eng.TriggerFullSync(ctx, cfg); err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never claimed the seeded task")
	}

	// The first pass is parked inside its push — the repeat must append.
	res, err := eng.TriggerFullSync(ctx, cfg)
	if err != nil {
		t.Fatalf("repeat trigger while in flight: %v", err)
	}
	if res.Scheduled != 1 {
		t.Fatalf("repeat scheduled %d, want 1", res.Scheduled)
	}
	close(release)
	waitForTaskCount(t, store, cfg.ID, 2, "both passes to finish")
}

// ---- L28: the two-instance REST wire ----

// t420Source is a source instance whose REST face carries the replication
// plane complete (store + cipher + trigger seam) — the production cmd wiring
// in miniature; the shared newBinFlow fixture leaves it unwired on purpose
// (its engines are per-test constructions over a later-opened store).
type t420Source struct {
	*binflow
	store  replication.Store
	engine *replication.Engine
	cipher *remote.Cipher
}

func newT420Source(t *testing.T, repos []*metadata.Repo) *t420Source {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", Path: filepath.Join(dataDir, "binflow.db"), AdminPassword: "pw-source",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	cfg.Audit.Enabled = true // the replication.run row must land
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	admin := &repo.Principal{Name: "admin", Admin: true}
	for _, r := range repos {
		r.CreatedAt = metadata.Now()
		r.UpdatedAt = metadata.Now()
		if _, err := svc.CreateRepo(ctx, admin, r); err != nil {
			t.Fatalf("CreateRepo %s: %v", r.RepoKey, err)
		}
	}

	dsn := "file:" + url.PathEscape(filepath.Join(dataDir, "binflow.db")) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := replication.NewSQLiteStore(db)

	cipher, err := remote.NewCipher(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	engine, err := replication.NewEngine(store, st, replication.EngineOptions{
		Cipher: cipher,
		Audit:  audit.New(md, true),
		Meta:   replication.NewStoreMetaSource(md),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	repo.AttachReplicator(svc, engine)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = engine.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("engine Run did not return after cancel")
		}
		engine.CloseIdleConnections()
	})

	srv := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: md,
		Repos: md.Repos(), ReposSvc: svc, Passwords: authSvc, Tokens: authSvc,
		GC: st, DataDir: dataDir,
		Adapters:          []adapter.Handler{generic.New(svc, md.Blobs())},
		Version:           "test",
		Replication:       store,
		ReplicationCipher: cipher,
		ReplicationRunner: engine,
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &t420Source{
		binflow: &binflow{t: t, url: ts.URL, dataDir: dataDir, svc: svc, st: st, md: md, adminPw: "pw-source"},
		store:   store, engine: engine, cipher: cipher,
	}
}

// repoFileSums returns the repo's FILE node path→sha256 map (folder marker
// rows excluded) — the parity oracle both instances are compared through.
func repoFileSums(t *testing.T, md metadata.Store, repoKey string) map[string]string {
	t.Helper()
	nodes, err := md.Nodes().ListByPrefix(context.Background(), repoKey, "")
	if err != nil {
		t.Fatalf("ListByPrefix %s: %v", repoKey, err)
	}
	out := map[string]string{}
	for _, n := range nodes {
		if n.Sha256 == "" || n.Sha256 == metadata.FolderMarkerSHA || strings.HasSuffix(n.Path, "/") {
			continue
		}
		out[n.Path] = n.Sha256
	}
	return out
}

// TestT420FullSyncTwoInstance is the L28 lane: artifacts that landed BEFORE
// the replication config existed converge through the REST trigger alone.
func TestT420FullSyncTwoInstance(t *testing.T) {
	ctx := context.Background()

	b := newBinFlow(t, "B420", "pw-target", []*metadata.Repo{
		{RepoKey: "replica-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	a := newT420Source(t, []*metadata.Repo{
		{RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})

	// 1. Land three artifacts on A through the real upload face — no config
	// exists yet, so the event hook appends nothing (the full-sync gap).
	artifacts := map[string][]byte{
		"org/app/1.0/app-1.0.bin": []byte("t420 artifact one"),
		"org/app/1.1/app-1.1.bin": []byte("t420 artifact two"),
		"org/lib/extra.bin":       []byte("t420 artifact three"),
	}
	sums := map[string]string{}
	for path, body := range artifacts {
		sum := sha256.Sum256(body)
		sums[path] = hex.EncodeToString(sum[:])
		resp, _ := a.do(http.MethodPut, "/binflow/libs/"+path, "admin", "pw-source", body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("upload %s: status %d, want 201", path, resp.StatusCode)
		}
	}
	if got := repoFileSums(t, a.md, "libs"); len(got) != 3 {
		t.Fatalf("source file nodes = %d, want 3", len(got))
	}

	// 2. Create the push config through the REST face (the password arrives
	// plaintext and is sealed server-side; the engine decrypts it).
	cfgBody, _ := json.Marshal(map[string]any{
		"name": "t420-fullsync", "source_repo": "libs", "target_url": b.url,
		"target_repo": "replica-local", "target_username": "admin",
		"target_password": "pw-target", "enabled": true,
	})
	resp, raw := a.do(http.MethodPost, "/binflow/api/v1/replications", "admin", "pw-source", cfgBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create config: status %d body %s, want 201", resp.StatusCode, raw)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("create config decode: %v (%s)", err, raw)
	}

	// No config existed at upload time: the ledger must be empty until the
	// trigger runs (the gap this ticket closes).
	if tasks, terr := a.store.ListTasks(ctx, created.ID, 100); terr != nil || len(tasks) != 0 {
		t.Fatalf("tasks before the trigger = %d (%v), want none", len(tasks), terr)
	}

	// 3. TRIGGER: POST /{id}/run answers the scheduled-async shape.
	run := func(wantStatus int) map[string]any {
		t.Helper()
		resp, raw := a.do(http.MethodPost, fmt.Sprintf("/binflow/api/v1/replications/%d/run", created.ID),
			"admin", "pw-source", nil)
		if resp.StatusCode != wantStatus {
			t.Fatalf("run: status %d body %s, want %d", resp.StatusCode, raw, wantStatus)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("run decode: %v (%s)", err, raw)
		}
		return body
	}
	body := run(http.StatusOK)
	if body["info"] != "The replication tasks was successfully scheduled to run" {
		t.Fatalf("run info = %v, want the anchored scheduling line", body["info"])
	}
	if body["scheduled"] != float64(3) || body["capped"] != false {
		t.Fatalf("run body = %v, want scheduled 3 capped false", body)
	}

	// 4. Convergence: every artifact lands on B — count AND checksums.
	waitForTaskCount(t, a.store, created.ID, 3, "the full pass")
	target := repoFileSums(t, b.md, "replica-local")
	if len(target) != 3 {
		t.Fatalf("target file nodes = %d (%v), want 3", len(target), target)
	}
	for path, sum := range sums {
		if target[path] != sum {
			t.Errorf("target %s = %s, want the source sha %s", path, target[path], sum)
		}
	}
	// Spot-check the wire face B serves: same checksum header, same bytes.
	gresp, gbody := b.do(http.MethodGet, "/binflow/replica-local/org/app/1.0/app-1.0.bin", "", "", nil)
	if gresp.StatusCode != http.StatusOK || gresp.Header.Get("X-Checksum-Sha256") != sums["org/app/1.0/app-1.0.bin"] ||
		!bytes.Equal(gbody, artifacts["org/app/1.0/app-1.0.bin"]) {
		t.Fatalf("GET on B = %d/%q, want the replicated artifact", gresp.StatusCode, gbody)
	}

	// 5. Observability rides the existing status face (AC ②): the config's
	// row shows the pass, no new query surface.
	sresp, sraw := a.do(http.MethodGet, "/binflow/api/v1/replication/status", "admin", "pw-source", nil)
	if sresp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d (%s)", sresp.StatusCode, sraw)
	}
	var status struct {
		Targets []struct {
			ID        int64 `json:"id"`
			Succeeded int64 `json:"succeeded"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(sraw, &status); err != nil || len(status.Targets) != 1 {
		t.Fatalf("status decode: %v (%s)", err, sraw)
	}
	if status.Targets[0].ID != created.ID || status.Targets[0].Succeeded != 3 {
		t.Fatalf("status target = %+v, want id %d succeeded 3", status.Targets[0], created.ID)
	}

	// 6. REPEAT trigger (§9.2-A-7): no merge, no refusal — the pass runs
	// again and converges through the target's sha256 idempotent hits.
	body = run(http.StatusOK)
	if body["scheduled"] != float64(3) {
		t.Fatalf("repeat run scheduled = %v, want 3 again (no dedup)", body["scheduled"])
	}
	waitForTaskCount(t, a.store, created.ID, 6, "the repeat pass")
	target = repoFileSums(t, b.md, "replica-local")
	if len(target) != 3 {
		t.Fatalf("target file nodes after the repeat = %d, want 3 (idempotent convergence)", len(target))
	}
	for path, sum := range sums {
		if target[path] != sum {
			t.Errorf("target %s after repeat = %s, want %s", path, target[path], sum)
		}
	}

	// 7. enabled:false refuses the run (409) — the drain never claims a
	// disabled config's rows, so scheduling one would only park dead rows.
	presp, praw := a.do(http.MethodPut, fmt.Sprintf("/binflow/api/v1/replications/%d", created.ID),
		"admin", "pw-source", []byte(`{"enabled":false}`))
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("PUT enabled=false: %d (%s)", presp.StatusCode, praw)
	}
	dresp, draw := a.do(http.MethodPost, fmt.Sprintf("/binflow/api/v1/replications/%d/run", created.ID),
		"admin", "pw-source", nil)
	if dresp.StatusCode != http.StatusConflict {
		t.Fatalf("run while disabled: status %d (%s), want 409", dresp.StatusCode, draw)
	}
	if !bytes.Contains(draw, []byte("disabled")) {
		t.Fatalf("409 body %q lacks the disabled wording", draw)
	}
	if tasks, terr := a.store.ListTasks(ctx, created.ID, 100); terr != nil || len(tasks) != 6 {
		t.Fatalf("tasks after the refused run = %d (%v), want the six drained rows", len(tasks), terr)
	}

	// 8. The governance trail: one replication.run row per trigger, the
	// admin's name on it, the config named in the detail.
	events, err := a.md.Audits().Query(ctx, metadata.AuditQuery{Action: "replication.run", Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("replication.run rows = %d, want 2 (the two accepted triggers)", len(events))
	}
	for _, ev := range events {
		if ev.Actor != "admin" || ev.RepoKey != "libs" {
			t.Errorf("replication.run row = actor %q repo %q, want admin/libs", ev.Actor, ev.RepoKey)
		}
		if !bytes.Contains([]byte(ev.Detail), []byte("t420-fullsync")) {
			t.Errorf("replication.run detail = %s, want the config named", ev.Detail)
		}
	}
}
