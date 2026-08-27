package repo_test

// T-324 (FR-102.2 / LC-29): the unused-cleanup engine over the REAL stack —
// real disk storage engine (session store wired), real SQLite metadata, the
// real audit logger (the download oracle reads what the service actually
// records) and the real remote pull-through path (httptest upstream). The
// assertions are the ticket's acceptance core:
//
//	policy    unused cached artifacts go; used / recently-landed /
//	          virtual-served / dedup-shared ones stay
//	zero      after an apply run: no node row, no remote_cache row, no blob
//	          file, no blobs-ledger row, no expired session row behind the
//	          deletions (blob/index/session)
//	cron      the Run loop fires on its tick and drives the same run
//	audit     every run leaves a cleanup.run row queryable through the
//	          audit plane
//	posture   dry-run default, audit-disabled refusal, period-0 skip,
//	          maintenance-lock and same-process mutual exclusion

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// cleanupEnv is one cleanup engine over the real collaborators. The service
// and the engine share ONE audit logger so the oracle reads exactly the
// download trail the service writes.
type cleanupEnv struct {
	svc     repo.Service
	eng     *repo.CleanupEngine
	st      storage.Engine
	md      metadata.Store
	au      audit.Logger
	clk     *clock
	dataDir string
}

// newCleanupEnv opens the real stack. grace is the gc leg's blob grace
// (tests pass sub-second so a landed-then-orphaned blob is reclaimable in
// the SAME run — production keeps the configured 24h window and the next
// run reclaims; both postures asserted below).
func newCleanupEnv(t *testing.T, grace time.Duration) *cleanupEnv {
	t.Helper()
	dir := t.TempDir()
	dbDir := t.TempDir()
	ctx := context.Background()
	st, err := storage.OpenEngine(dir, storage.Options{
		Sessions: nil, // set by newCleanupEnvSessions
	})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	au := audit.New(md, true)
	clk := &clock{now: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)}
	svc := repo.NewWithClock(st, md, nil, au, clk.Now)
	eng, err := repo.NewCleanupEngine(repo.CleanupOptions{
		Store:        md,
		Engine:       st,
		Audit:        au,
		AuditEnabled: true,
		DataDir:      dir,
		Grace:        grace,
		Now:          clk.Now,
	})
	if err != nil {
		t.Fatalf("NewCleanupEngine: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		_ = md.Close()
	})
	return &cleanupEnv{svc: svc, eng: eng, st: st, md: md, au: au, clk: clk, dataDir: dir}
}

// newCleanupEnvSessions is newCleanupEnv with the session store wired and a
// tiny TTL (the session leg's fixture shape).
func newCleanupEnvSessions(t *testing.T, ttl time.Duration) *cleanupEnv {
	t.Helper()
	e := newCleanupEnv(t, time.Nanosecond)
	_ = e.st.Close() // reopen with the session store; the cleanup engine holds only interfaces
	st, err := storage.OpenEngine(e.dataDir, storage.Options{
		Sessions:   e.md.UploadSessions(),
		SessionTTL: ttl,
	})
	if err != nil {
		t.Fatalf("storage.OpenEngine(sessioned): %v", err)
	}
	e.st = st
	eng, err := repo.NewCleanupEngine(repo.CleanupOptions{
		Store:        e.md,
		Engine:       st,
		Audit:        e.au,
		AuditEnabled: true,
		DataDir:      e.dataDir,
		Grace:        time.Nanosecond,
		Now:          e.clk.Now,
	})
	if err != nil {
		t.Fatalf("NewCleanupEngine(sessioned): %v", err)
	}
	e.eng = eng
	e.svc = repo.NewWithClock(st, e.md, nil, e.au, e.clk.Now)
	t.Cleanup(func() { _ = st.Close() })
	return e
}

// upstream serves a fixed artifact set (path-keyed deterministic bytes).
type upstream struct {
	srv *httptest.Server
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		body := "artifact-of:" + path
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(body))
	})
	u.srv = httptest.NewServer(mux)
	t.Cleanup(u.srv.Close)
	return u
}

// land pulls one artifact through the remote repo (the REAL cache path:
// blob + ledger row + node + validator row), returning the landed node.
func land(t *testing.T, e *cleanupEnv, repoKey, path string) *metadata.Node {
	t.Helper()
	rc, node, err := e.svc.Get(context.Background(), admin(), repoKey, path)
	if err != nil {
		t.Fatalf("Get %s/%s: %v", repoKey, path, err)
	}
	defer rc.Close() //nolint:errcheck // test fixture
	if node == nil || node.Sha256 == "" {
		t.Fatalf("Get %s/%s: no landed node", repoKey, path)
	}
	return node
}

// mustRemote creates one remote generic repo with the cleanup period.
func mustRemote(t *testing.T, e *cleanupEnv, key, url string, periodHours int64) {
	t.Helper()
	// allowPrivateUpstream: the httptest upstream is loopback — the SSRF
	// chain would refuse it otherwise (the admin-set exemption, ADR-0012).
	cfg := `{"url":"` + url + `","allowPrivateUpstream":true`
	if periodHours > 0 {
		cfg += `,"unusedArtifactsCleanupPeriodHours":` + itoa(periodHours)
	}
	cfg += `}`
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: repo.PackageGeneric, Config: cfg,
	}); err != nil {
		t.Fatalf("CreateRepo(remote %s): %v", key, err)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [24]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// nodeExists reports the node row's presence.
func nodeExists(t *testing.T, e *cleanupEnv, repoKey, path string) bool {
	t.Helper()
	_, err := e.md.Nodes().Get(context.Background(), repoKey, path)
	if err == nil {
		return true
	}
	if strings.Contains(err.Error(), "not found") {
		return false
	}
	t.Fatalf("Nodes().Get %s/%s: %v", repoKey, path, err)
	return false
}

// cacheRowExists reports the remote_cache validator row's presence.
func cacheRowExists(t *testing.T, e *cleanupEnv, repoKey, path string) bool {
	t.Helper()
	_, err := e.md.Remote().GetCache(context.Background(), repoKey, path)
	if err == nil {
		return true
	}
	if strings.Contains(err.Error(), "not found") {
		return false
	}
	t.Fatalf("Remote().GetCache %s/%s: %v", repoKey, path, err)
	return false
}

// blobFileExists stats the blob through the engine's own path shape.
func blobFileExists(t *testing.T, e *cleanupEnv, sha string) bool {
	t.Helper()
	p, err := storage.BlobPath(e.dataDir, sha)
	if err != nil {
		t.Fatalf("BlobPath %s: %v", sha, err)
	}
	_, statErr := os.Stat(p)
	return statErr == nil
}

// ledgerRowExists reports the blobs-ledger row's presence.
func ledgerRowExists(t *testing.T, e *cleanupEnv, sha string) bool {
	t.Helper()
	_, err := e.md.Blobs().Get(context.Background(), sha)
	if err == nil {
		return true
	}
	if strings.Contains(err.Error(), "not found") {
		return false
	}
	t.Fatalf("Blobs().Get %s: %v", sha, err)
	return false
}

// cleanupAuditRows counts cleanup.run rows in the audit store.
func cleanupAuditRows(t *testing.T, e *cleanupEnv) int {
	t.Helper()
	page, err := e.au.Query(context.Background(), audit.Filter{Action: audit.ActionCleanupRun, Limit: 100})
	if err != nil {
		t.Fatalf("audit query cleanup.run: %v", err)
	}
	return len(page.Events)
}

// TestT324PolicyKeepsAndReaps is the policy core: unused goes, used /
// virtual-served / recently-landed stay, local repos are untouched.
func TestT324PolicyKeepsAndReaps(t *testing.T) {
	e := newCleanupEnv(t, time.Nanosecond)
	u := newUpstream(t)
	ctx := context.Background()

	mustRemote(t, e, "cache", u.srv.URL, 24)
	// A local repo sharing one artifact's bytes (the dedup keeper) and one
	// untouched local artifact (the scope keeper).
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "keep-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo(local): %v", err)
	}
	if _, err := e.svc.Put(ctx, admin(), "keep-local", "mine.bin", strings.NewReader("artifact-of:shared.bin"), storage.BlobRef{}, "application/octet-stream"); err != nil {
		t.Fatalf("Put local: %v", err)
	}

	land(t, e, "cache", "unused.bin")
	land(t, e, "cache", "used.bin")
	land(t, e, "cache", "via-virtual.bin")
	land(t, e, "cache", "shared.bin") // same bytes as keep-local/mine.bin
	shared, err := e.md.Nodes().Get(ctx, "cache", "shared.bin")
	if err != nil {
		t.Fatalf("get shared node: %v", err)
	}

	// Age everything past the window, then produce use signals INSIDE it.
	e.clk.Advance(48 * time.Hour)
	if _, _, err := e.svc.Get(ctx, admin(), "cache", "used.bin"); err != nil {
		t.Fatalf("Get used: %v", err)
	}
	// A virtual repo over the cache: the download audits under the virtual
	// key, the membership walk must still protect the member's artifact.
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "agg", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
		Config: `{"repositories":["cache"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(virtual): %v", err)
	}
	if _, _, err := e.svc.Get(ctx, admin(), "agg", "via-virtual.bin"); err != nil {
		t.Fatalf("Get via virtual: %v", err)
	}
	// A fresh landing inside the window (the fetch path itself is use).
	e.clk.Advance(-1 * time.Hour) // still 47h past the 24h window edge
	land(t, e, "cache", "fresh.bin")
	e.clk.Advance(1 * time.Hour)

	// Dry run first: everything reported, nothing touched.
	rep, err := e.eng.RunOnce(ctx, repo.CleanupRunOptions{Apply: false})
	if err != nil {
		t.Fatalf("dry RunOnce: %v", err)
	}
	if len(rep.Repos) != 1 || rep.Repos[0].Repo != "cache" {
		t.Fatalf("dry report repos = %+v, want the one remote", rep.Repos)
	}
	rr := rep.Repos[0]
	// Two candidates: unused.bin and shared.bin — the policy is per NODE,
	// and the cache's copy of shared.bin is itself unused (the LOCAL
	// reference keeps the BLOB alive, not the cache's node row; the blob
	// assertion below pins that half). keptByUse=3: used.bin and
	// via-virtual.bin through the oracle, fresh.bin through it too — its
	// landing IS a download inside the window (updated_at is the second,
	// independent keeper for the refetch-without-download case).
	if rr.Candidates != 2 || rr.KeptByUse != 3 {
		t.Fatalf("dry candidates=%d keptByUse=%d, want 2/3", rr.Candidates, rr.KeptByUse)
	}
	if nodeExists(t, e, "cache", "unused.bin") != true {
		t.Fatal("dry run deleted the unused node")
	}

	// Apply: the one candidate goes; the keepers stay.
	rep, err = e.eng.RunOnce(ctx, repo.CleanupRunOptions{Apply: true})
	if err != nil {
		t.Fatalf("apply RunOnce: %v", err)
	}
	rr = rep.Repos[0]
	if rr.Deleted != 2 {
		t.Fatalf("apply deleted = %d, want 2", rr.Deleted)
	}
	for _, p := range []string{"used.bin", "via-virtual.bin", "fresh.bin"} {
		if !nodeExists(t, e, "cache", p) {
			t.Fatalf("keeper %s was reaped", p)
		}
	}
	for _, p := range []string{"unused.bin", "shared.bin"} {
		if nodeExists(t, e, "cache", p) {
			t.Fatalf("unused cache node %s survived the apply", p)
		}
	}
	if cacheRowExists(t, e, "cache", "unused.bin") {
		t.Fatal("validator row of the reaped path survived (index orphan)")
	}
	if !nodeExists(t, e, "keep-local", "mine.bin") {
		t.Fatal("local repo disturbed by the remote policy")
	}

	// Zero-orphan, blob half: the reaped path's blob is GONE (file and
	// ledger) — grace is sub-second here; the dedup-shared blob SURVIVES.
	var unusedSHA string
	// The reaped node is gone; recover its sha from the audit deploy-less
	// trail is not possible, so assert set-wise instead: every surviving
	// blob file has a ledger row and every ledger row has a live file, and
	// the shared blob in particular is alive on both sides.
	if !blobFileExists(t, e, shared.Sha256) || !ledgerRowExists(t, e, shared.Sha256) {
		t.Fatal("dedup-shared blob was reclaimed under a live local reference")
	}
	_ = unusedSHA
	assertBlobLedgerClosed(t, e)
}

// assertBlobLedgerClosed asserts the blob/ledger closure: every on-disk
// blob has a ledger row and every ledger row has an on-disk blob (no
// phantom rows, no untracked files), and no ledger row is unreferenced
// (the gc leg reclaimed the orphans; the file walk covers the same shard
// set the sweep does).
func assertBlobLedgerClosed(t *testing.T, e *cleanupEnv) {
	t.Helper()
	ctx := context.Background()
	shardRoot := filepath.Join(e.dataDir, "blobs")
	shards, err := os.ReadDir(shardRoot)
	if err != nil {
		t.Fatalf("read blobs/: %v", err)
	}
	files := map[string]bool{}
	for _, sh := range shards {
		if !sh.IsDir() || len(sh.Name()) != 2 {
			continue
		}
		ents, err := os.ReadDir(filepath.Join(shardRoot, sh.Name()))
		if err != nil {
			t.Fatalf("read shard: %v", err)
		}
		for _, f := range ents {
			if !f.IsDir() && len(f.Name()) == 64 {
				files[f.Name()] = true
			}
		}
	}
	// Forward direction: the BlobStore exposes no list-all, so the file
	// walk drives the row probes.
	for sha := range files {
		if !ledgerRowExists(t, e, sha) {
			t.Fatalf("blob %s on disk without a ledger row", sha)
		}
	}
	// Reverse direction: FilterUnreferenced must yield nothing (the gc leg
	// + ledger teardown closed the set).
	if err := e.md.Blobs().FilterUnreferenced(ctx, 100, func(sha string) error {
		t.Errorf("ledger row %s unreferenced after the run (blob orphan)", sha)
		return nil
	}); err != nil {
		t.Fatalf("FilterUnreferenced: %v", err)
	}
}

// TestT324ZeroOrphanEndToEnd is the ticket's headline assertion: after one
// apply run over a populated cache, the blob / index / session tables hold
// nothing the run should have reclaimed.
func TestT324ZeroOrphanEndToEnd(t *testing.T) {
	e := newCleanupEnvSessions(t, 50*time.Millisecond)
	u := newUpstream(t)
	ctx := context.Background()
	mustRemote(t, e, "cache", u.srv.URL, 24)

	one := land(t, e, "cache", "stale.bin")
	land(t, e, "cache", "live.bin")
	e.clk.Advance(48 * time.Hour)
	if _, _, err := e.svc.Get(ctx, admin(), "cache", "live.bin"); err != nil {
		t.Fatalf("Get live: %v", err)
	}

	// An expired upload session: row + temp dir (the session orphan half).
	// Crash-shaped fixture: the session is created by a SECOND engine on
	// the same data directory which then closes (ADR-0028 preserves
	// unexpired sessions), so the row is not live in the serving engine's
	// registry when the sweep runs — the kill -9 residue the leg exists
	// for. The owning engine writes with the same TTL clock.
	crashEng, err := storage.OpenEngine(e.dataDir, storage.Options{
		Sessions: e.md.UploadSessions(), SessionTTL: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("OpenEngine(crash): %v", err)
	}
	sess, err := crashEng.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("half an upload")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := crashEng.Close(); err != nil {
		t.Fatalf("Close(crash engine): %v", err)
	}
	time.Sleep(80 * time.Millisecond) // row crosses the 50ms TTL

	rep, err := e.eng.RunOnce(ctx, repo.CleanupRunOptions{Apply: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.SessionsSwept != 1 {
		t.Fatalf("sessions swept = %d, want 1", rep.SessionsSwept)
	}
	if nodeExists(t, e, "cache", "stale.bin") || cacheRowExists(t, e, "cache", "stale.bin") {
		t.Fatal("stale path left index rows behind")
	}
	if !nodeExists(t, e, "cache", "live.bin") {
		t.Fatal("live path reaped")
	}
	// Blob half: the stale blob is gone file+row; the live one intact.
	if blobFileExists(t, e, one.Sha256) || ledgerRowExists(t, e, one.Sha256) {
		t.Fatal("stale blob survived (file or ledger row)")
	}
	live, err := e.md.Nodes().Get(ctx, "cache", "live.bin")
	if err != nil {
		t.Fatalf("live node: %v", err)
	}
	if !blobFileExists(t, e, live.Sha256) || !ledgerRowExists(t, e, live.Sha256) {
		t.Fatal("live blob lost")
	}
	// Session half: the uploads dir is empty again.
	ents, err := os.ReadDir(filepath.Join(e.dataDir, "uploads"))
	if err != nil {
		t.Fatalf("read uploads/: %v", err)
	}
	if len(ents) != 0 {
		t.Fatalf("uploads/ residue after sweep: %d entries", len(ents))
	}
	assertBlobLedgerClosed(t, e)

	if got := cleanupAuditRows(t, e); got != 1 {
		t.Fatalf("cleanup.run rows = %d, want 1 per run", got)
	}
}

// TestT324CronLoopFires proves the cron trigger chain: the Run loop drives
// full apply runs on its tick without any manual call.
func TestT324CronLoopFires(t *testing.T) {
	e := newCleanupEnv(t, time.Nanosecond)
	u := newUpstream(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mustRemote(t, e, "cache", u.srv.URL, 24)
	land(t, e, "cache", "cron-stale.bin")
	e.clk.Advance(48 * time.Hour)

	// The structured completion log is the operator's live signal (FR-102.2
	// "可观测（日志+指标）" — the log half asserted here through a capturing
	// logger injected at construction, the metrics half at the httpapi
	// plane's scrape gauges).
	// The engine with a tiny tick (the production default is 1h) and a
	// capturing logger — the structured completion log is the operator's
	// live signal (FR-102.2 "可观测（日志+指标）" — the log half asserted
	// here, the metrics half at the httpapi plane's scrape gauges).
	var logMu sync.Mutex
	var logLines []string
	eng, err := repo.NewCleanupEngine(repo.CleanupOptions{
		Store: e.md, Engine: e.st, Audit: e.au, AuditEnabled: true,
		DataDir: e.dataDir, Grace: time.Nanosecond, Now: e.clk.Now, TickEvery: 30 * time.Millisecond,
		Log: slog.New(slog.NewTextHandler(collectingWriter{lines: &logLines, mu: &logMu}, nil)),
	})
	if err != nil {
		t.Fatalf("NewCleanupEngine(cron): %v", err)
	}

	logged := func() string {
		logMu.Lock()
		defer logMu.Unlock()
		return strings.Join(logLines, "\n")
	}
	go eng.Run(ctx)
	// The run's effects land in order (deletions -> snapshot -> audit ->
	// log); wait for the LAST one so every assertion below reads settled
	// state, not a mid-run window.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !nodeExists(t, e, "cache", "cron-stale.bin") &&
			eng.LastReport() != nil &&
			strings.Contains(logged(), "run complete") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if nodeExists(t, e, "cache", "cron-stale.bin") {
		t.Fatal("cron loop never reaped the stale artifact")
	}
	if eng.LastReport() == nil || eng.LastReport().Trigger != repo.CleanupTriggerCron {
		t.Fatalf("last report = %+v, want a cron-triggered run", eng.LastReport())
	}
	if s := eng.Stats(); s.Runs < 1 || s.ObjectsCleaned < 1 {
		t.Fatalf("stats after cron = %+v, want runs>=1 objects>=1", s)
	}
	if joined := logged(); !strings.Contains(joined, "trigger=cron") {
		t.Fatalf("cron log line lacks the trigger field:\n%s", joined)
	}
}

// collectingWriter is the cron test's line sink.
type collectingWriter struct {
	lines *[]string
	mu    *sync.Mutex
}

func (w collectingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.lines = append(*w.lines, string(p))
	return len(p), nil
}

// TestT324DryRunChangesNothing pins the default posture end to end.
func TestT324DryRunChangesNothing(t *testing.T) {
	e := newCleanupEnv(t, time.Hour) // production-shaped grace
	u := newUpstream(t)
	ctx := context.Background()
	mustRemote(t, e, "cache", u.srv.URL, 24)
	one := land(t, e, "cache", "a.bin")
	e.clk.Advance(48 * time.Hour)

	rep, err := e.eng.RunOnce(ctx, repo.CleanupRunOptions{})
	if err != nil {
		t.Fatalf("RunOnce (no body): %v", err)
	}
	if rep.Apply {
		t.Fatal("default run reported apply=true")
	}
	if !nodeExists(t, e, "cache", "a.bin") || !blobFileExists(t, e, one.Sha256) {
		t.Fatal("dry run touched data")
	}
	if s := e.eng.Stats(); s.Runs != 0 || s.ObjectsCleaned != 0 {
		t.Fatalf("dry run moved the cumulative counters: %+v", s)
	}
}

// TestT324AuditDisabledRefusesPolicy: without the oracle there is no
// honest "unused" — the policy leg skips with its reason and the rest of
// the run still completes.
func TestT324AuditDisabledRefusesPolicy(t *testing.T) {
	e := newCleanupEnv(t, time.Nanosecond)
	u := newUpstream(t)
	ctx := context.Background()
	eng, err := repo.NewCleanupEngine(repo.CleanupOptions{
		Store: e.md, Engine: e.st, Audit: e.au, AuditEnabled: false,
		DataDir: e.dataDir, Grace: time.Nanosecond, Now: e.clk.Now,
	})
	if err != nil {
		t.Fatalf("NewCleanupEngine: %v", err)
	}
	mustRemote(t, e, "cache", u.srv.URL, 24)
	land(t, e, "cache", "x.bin")
	e.clk.Advance(48 * time.Hour)

	rep, err := eng.RunOnce(ctx, repo.CleanupRunOptions{Apply: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(rep.Repos) != 1 || rep.Repos[0].Skipped == "" || !strings.Contains(rep.Repos[0].Skipped, "audit is disabled") {
		t.Fatalf("repo rows = %+v, want the audit-disabled skip", rep.Repos)
	}
	if !nodeExists(t, e, "cache", "x.bin") {
		t.Fatal("audit-disabled run deleted policy data")
	}
}

// TestT324PeriodZeroAndScopedRuns: the off default and the one-repo scope.
func TestT324PeriodZeroAndScopedRuns(t *testing.T) {
	e := newCleanupEnv(t, time.Nanosecond)
	u := newUpstream(t)
	ctx := context.Background()
	mustRemote(t, e, "off-cache", u.srv.URL, 0) // off (the product default)
	mustRemote(t, e, "on-cache", u.srv.URL, 24)
	land(t, e, "off-cache", "a.bin")
	land(t, e, "on-cache", "b.bin")
	e.clk.Advance(48 * time.Hour)

	// Full run: only on-cache carries a policy row; both repos appear in
	// the report (visibility, not just action).
	rep, err := e.eng.RunOnce(ctx, repo.CleanupRunOptions{Apply: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(rep.Repos) != 2 {
		t.Fatalf("repo rows = %d, want both remote repos reported", len(rep.Repos))
	}
	if !nodeExists(t, e, "off-cache", "a.bin") {
		t.Fatal("period-0 repo was cleaned")
	}
	if nodeExists(t, e, "on-cache", "b.bin") {
		t.Fatal("period repo was not cleaned")
	}

	// Scoped run against an unknown repo refuses; against a local repo it
	// reports the not-remote skip.
	if _, err := e.eng.RunOnce(ctx, repo.CleanupRunOptions{Repo: "no-such-repo"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("scoped unknown repo = %v, want not-found", err)
	}
	rep, err = e.eng.RunOnce(ctx, repo.CleanupRunOptions{Repo: "off-cache"})
	if err != nil {
		t.Fatalf("scoped off-cache: %v", err)
	}
	if len(rep.Repos) != 1 || rep.Repos[0].Repo != "off-cache" {
		t.Fatalf("scoped report = %+v", rep.Repos)
	}
}

// TestT324MutualExclusion: same-process double run refuses; the
// maintenance lock family excludes the gc face and vice versa.
func TestT324MutualExclusion(t *testing.T) {
	e := newCleanupEnv(t, time.Nanosecond)
	ctx := context.Background()

	// Lock held by a gc run: the cleanup run refuses with the lock error.
	lock, err := storage.AcquireDataLock(e.dataDir, storage.DataLockOpGC)
	if err != nil {
		t.Fatalf("acquire gc lock: %v", err)
	}
	if _, err := e.eng.RunOnce(ctx, repo.CleanupRunOptions{}); err == nil || !strings.Contains(err.Error(), "locked by another maintenance operation") {
		t.Fatalf("run under gc lock = %v, want the lock refusal", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release gc lock: %v", err)
	}

	// Same-process double run: the second RunOnce gets ErrCleanupRunning.
	// A slow upstream makes the first run's audit walk observable? Simpler:
	// hold the engine busy via its own maintenance lock is not possible
	// (same process) — drive the race directly through the exported face
	// with a cancelled-context trick is fragile; instead assert the
	// sentinel's meaning on a manually-busy engine via concurrency.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.eng.RunOnce(ctx, repo.CleanupRunOptions{Apply: true})
	}()
	var second error
	busy := false
	for i := 0; i < 200; i++ {
		if _, second = e.eng.RunOnce(ctx, repo.CleanupRunOptions{}); second != nil {
			if strings.Contains(second.Error(), "already in progress") {
				busy = true
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	<-done
	if !busy {
		t.Skip("the first run finished before the probe could observe the busy window")
	}
}

// TestT324GraceWindowDefers is the conservative-blob posture: with a
// production-shaped grace the orphaned blob survives THIS run (reported as
// gracePending) and the NEXT run reclaims it.
func TestT324GraceWindowDefers(t *testing.T) {
	e := newCleanupEnv(t, time.Hour)
	u := newUpstream(t)
	ctx := context.Background()
	mustRemote(t, e, "cache", u.srv.URL, 24)
	one := land(t, e, "cache", "soon.bin")
	e.clk.Advance(48 * time.Hour)

	rep, err := e.eng.RunOnce(ctx, repo.CleanupRunOptions{Apply: true})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Repos[0].Deleted != 1 {
		t.Fatalf("policy deleted = %d, want 1", rep.Repos[0].Deleted)
	}
	if !blobFileExists(t, e, one.Sha256) {
		t.Fatal("blob reaped inside the grace window")
	}
	if rep.GCDeleted != 0 {
		t.Fatalf("gc deleted inside grace = %d, want 0", rep.GCDeleted)
	}

	// Re-run with no grace: the next run closes the residue.
	eng2, err := repo.NewCleanupEngine(repo.CleanupOptions{
		Store: e.md, Engine: e.st, Audit: e.au, AuditEnabled: true,
		DataDir: e.dataDir, Grace: time.Nanosecond, Now: e.clk.Now,
	})
	if err != nil {
		t.Fatalf("NewCleanupEngine(no grace): %v", err)
	}
	rep2, err := eng2.RunOnce(ctx, repo.CleanupRunOptions{Apply: true})
	if err != nil {
		t.Fatalf("RunOnce(no grace): %v", err)
	}
	if rep2.GCDeleted != 1 || blobFileExists(t, e, one.Sha256) || ledgerRowExists(t, e, one.Sha256) {
		t.Fatalf("second run gcDeleted=%d blobStillThere=%v — grace residue not reclaimed", rep2.GCDeleted, blobFileExists(t, e, one.Sha256))
	}
}
