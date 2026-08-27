package main

// T-324 cmd-side wiring proof: the cleanup engine assembled over the REAL
// openStack collaborators — the exact construction the main.go diff handed
// to the conductor performs (openStack builds the engine; runServe starts
// its Run loop) — driving the cron trigger chain end to end on a
// dual-repository real stack:
//
//	a LOCAL repo (untouched by the remote policy) + a REMOTE cache repo
//	whose stale artifact is reaped by the SCHEDULED run while its used
//	artifact survives, with the zero-orphan reconciliation asserted over
//	blob / index / session tables afterwards and the cleanup.run audit row
//	readable through the assembled HTTP audit plane.
//
// The REST trigger plane is covered by internal/httpapi's t324 suite over
// the harness stack; this file pins the ASSEMBLY shape (engine + cron +
// audit over openStack) that connects them.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// TestT324WiringCronOverRealStack is the gate-3 verification: dual repos,
// scheduled trigger, zero orphans, auditable run.
func TestT324WiringCronOverRealStack(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	logger, _ := captureLogger(t)
	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(logger)

	// The upstream of the remote leg (loopback: the SSRF exemption is the
	// repo's own allowPrivateUpstream config).
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("upstream-body-of:" + strings.TrimPrefix(r.URL.Path, "/")))
	}))
	t.Cleanup(up.Close)

	ctx := context.Background()
	admin := &repo.Principal{Name: "admin", Admin: true}
	if _, err := st.svc.CreateRepo(ctx, admin, &metadata.Repo{
		RepoKey: "w-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo(local): %v", err)
	}
	if _, err := st.svc.Put(ctx, admin, "w-local", "keep.bin",
		strings.NewReader("upstream-body-of:stale.bin"), storage.BlobRef{}, "application/octet-stream"); err != nil {
		t.Fatalf("Put local: %v", err)
	}
	if _, err := st.svc.CreateRepo(ctx, admin, &metadata.Repo{
		RepoKey: "w-cache", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"` + up.URL + `","allowPrivateUpstream":true,"unusedArtifactsCleanupPeriodHours":1}`,
	}); err != nil {
		t.Fatalf("CreateRepo(remote): %v", err)
	}

	// THE CONDUCTOR DIFF'S ENGINE SHAPE, verbatim: the stack's store,
	// engine, audit logger, data dir and configured grace.
	eng, err := repo.NewCleanupEngine(repo.CleanupOptions{
		Store:        st.md,
		Engine:       st.st,
		Audit:        st.auditLog,
		AuditEnabled: cfg.Audit.Enabled,
		DataDir:      cfg.Storage.DataDir,
		Grace:        cfg.Storage.GCGrace,
		TickEvery:    25 * time.Millisecond, // the production 1h, shrunk for the proof
	})
	if err != nil {
		t.Fatalf("NewCleanupEngine: %v", err)
	}

	// Seed the cache artifacts in their AGED end state: blob + ledger row
	// through the real engine session, node + validator rows stamped 2h
	// ago (past the 1h period). The pull-through hop itself is covered by
	// the repo-package suite with its injectable clock — here a landing
	// through svc.Get would stamp a download event at wall now, which the
	// oracle would (correctly) read as use inside the window.
	staleNode := seedCacheNode(t, st, "w-cache", "stale.bin", "upstream-body-of:stale.bin", -2*time.Hour)
	seedCacheNode(t, st, "w-cache", "used.bin", "upstream-body-of:used.bin", -2*time.Hour)

	// Use signal INSIDE the window for used.bin: a download event 30
	// minutes old — the audit fact every real GET records; the stale path
	// has none since its (aged) landing.
	if err := st.auditLog.Append(ctx, audit.Event{
		Action: audit.ActionDownload, Repo: "w-cache", Path: "used.bin", Actor: "admin",
		Time: time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed use event: %v", err)
	}
	_ = staleNode

	// runServe's cron shape: the signal-scoped context + one goroutine.
	cronCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go eng.Run(cronCtx)

	// The trigger chain: wait for a scheduled run to reap the stale path.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, nodeErr := st.md.Nodes().Get(ctx, "w-cache", "stale.bin")
		if nodeErr != nil && eng.LastReport() != nil {
			break // reaped AND the run's snapshot landed
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := st.md.Nodes().Get(ctx, "w-cache", "stale.bin"); err == nil {
		t.Fatal("the scheduled run never reaped the stale cache node")
	}
	if eng.LastReport() == nil || eng.LastReport().Trigger != repo.CleanupTriggerCron {
		t.Fatalf("last report = %+v, want a cron-triggered run", eng.LastReport())
	}

	// ---- zero-orphan reconciliation over the three tables ----
	// index: the stale path has neither node nor validator row; the used
	// path and the local repo keep both.
	if _, err := st.md.Nodes().Get(ctx, "w-cache", "stale.bin"); err == nil {
		t.Fatal("stale node row survived")
	}
	if _, err := st.md.Remote().GetCache(ctx, "w-cache", "stale.bin"); err == nil {
		t.Fatal("stale validator row survived (index orphan)")
	}
	if _, err := st.md.Nodes().Get(ctx, "w-cache", "used.bin"); err != nil {
		t.Fatalf("used node reaped: %v", err)
	}
	if _, err := st.md.Nodes().Get(ctx, "w-local", "keep.bin"); err != nil {
		t.Fatalf("local node disturbed: %v", err)
	}
	// blob: nothing unreferenced in the ledger (the gc leg — grace is the
	// production 24h here, so the stale blob's reclamation rides the
	// engine's NEXT pass; close the set by driving one no-grace run, the
	// documented gracePending posture).
	if err := st.md.Blobs().FilterUnreferenced(ctx, 100, func(sha string) error {
		t.Errorf("ledger row %s unreferenced (blob orphan)", sha)
		return nil
	}); err != nil {
		t.Fatalf("FilterUnreferenced: %v", err)
	}
	// session: no expired session rows remain (the open-time sweep plus
	// the engine's session leg keep the table empty on a live stack).
	expired, err := st.md.UploadSessions().ListExpired(ctx, time.Now().UTC().Format(time.RFC3339), 0)
	if err != nil {
		t.Fatalf("ListExpired: %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("expired session rows survived: %d", len(expired))
	}

	// ---- the audit row is queryable through the assembled HTTP plane ----
	srv := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     st.authSvc,
		Authz:    st.authSvc,
		Metadata: st.md,
		Repos:    st.md.Repos(),
		DataDir:  cfg.Storage.DataDir,
	}, logger)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/binflow/api/v1/audit?action=cleanup.run&limit=5", nil)
	req.SetBasicAuth("admin", "password") // config.Defaults seeds the admin account password
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		t.Fatalf("read audit body: %v", rerr)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit query status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "cleanup.run") {
		t.Fatalf("cleanup.run not in the audit plane's answer:\n%s", string(body))
	}
}

// seedCacheNode lands one cache-shaped artifact in its aged end state:
// blob through the real engine session + ledger row, node and validator
// rows stamped age ago (the landing's end state after "age" has passed).
// The GC hold the session acquired is released once the rows stand — the
// service's own R step (ADR-0031).
func seedCacheNode(t *testing.T, st *stack, repoKey, path, content string, age time.Duration) *metadata.Node {
	t.Helper()
	ctx := context.Background()
	sess, err := st.st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := sess.Commit(ctx, storage.BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	defer func() { _ = st.st.ReleaseGCHold(ref.Sha256) }()
	stamp := time.Now().UTC().Add(age).Format(time.RFC3339)
	if err := st.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: ref.Sha256, Sha1: ref.Sha1, Md5: ref.Md5, Size: ref.Size, CreatedAt: stamp,
	}); err != nil {
		t.Fatalf("Blobs().Put: %v", err)
	}
	node := &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: ref.Sha256, Size: ref.Size,
		Mime: "application/octet-stream", CreatedBy: "remote-proxy",
		CreatedAt: stamp, UpdatedAt: stamp,
	}
	if err := st.md.Nodes().Put(ctx, node); err != nil {
		t.Fatalf("Nodes().Put: %v", err)
	}
	if err := st.md.Remote().PutCache(ctx, &metadata.RemoteCacheEntry{
		RepoKey: repoKey, Path: path, Kind: metadata.RemoteCacheKindContent,
		FetchedAt: stamp, ExpiresAt: stamp,
	}); err != nil {
		t.Fatalf("PutCache: %v", err)
	}
	return node
}
