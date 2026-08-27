package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// mkStaleSession fabricates a crashed session: an uploads/<id>/data file plus
// an upload_sessions row with the given absolute expires_at. The sweep's
// ListExpired boundary is expires_at <= now, so expiresAt determines whether
// the session survives.
func mkStaleSession(t *testing.T, root string, store *memUploadSessions, id string, expiresAt time.Time) {
	t.Helper()
	dir := filepath.Join(root, "uploads", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := expiresAt.Add(-DefaultSessionTTL)
	st := sessionState{Version: 1, ID: id, CreatedAt: created}
	if err := store.Create(context.Background(), &metadata.UploadSession{
		ID:        id,
		State:     marshalSessionState(st),
		CreatedAt: created.UTC().Format(time.RFC3339),
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed stale row %s: %v", id, err)
	}
}

func TestStartupSweepSessions(t *testing.T) {
	tests := []struct {
		name    string
		expires map[string]time.Duration // session id -> expires_at offset from now (negative = expired)
		wantLen int                      // sessions left after OpenEngine
	}{
		{
			name: "expired removed, fresh kept",
			expires: map[string]time.Duration{
				"old-1": -2 * time.Hour,
				"old-2": -25 * time.Hour,
				"new-1": 10 * time.Minute,
			},
			wantLen: 1,
		},
		{
			name: "default ttl keeps 23h sessions",
			expires: map[string]time.Duration{
				"edge-keeps": time.Hour, // expires 1h in the future
				"edge-drops": -time.Hour,
			},
			wantLen: 1,
		},
		{
			name:    "no sessions",
			expires: map[string]time.Duration{},
			wantLen: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			store := newMemUploadSessions()
			now := time.Now()
			for id, off := range tt.expires {
				mkStaleSession(t, root, store, id, now.Add(off))
			}
			eng, err := OpenEngine(root, Options{Sessions: store})
			if err != nil {
				t.Fatal(err)
			}
			defer eng.Close() //nolint:errcheck // test
			if got := countSessionDirs(t, root); got != tt.wantLen {
				t.Fatalf("sessions after sweep = %d, want %d", got, tt.wantLen)
			}
		})
	}
}

// TestSweepKeepsLiveSessions pins the guard that the sweep consults the
// engine's live-session registry: a session that is open in this process is
// never a deletion target, no matter its age. This exercises the same guard
// the second engine's sweep would use, via the owning engine directly.
func TestSweepKeepsLiveSessions(t *testing.T) {
	root := t.TempDir()
	store := newMemUploadSessions()
	eng, err := OpenEngine(root, Options{SessionTTL: time.Nanosecond, Sessions: store})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond) // the session is now older than the ttl

	// The sweep must skip it because it is registered as live.
	e := eng.(*engine)
	if _, err := e.sweepSessions(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := countSessionDirs(t, root); got != 1 {
		t.Fatalf("sessions after own-engine sweep = %d, want 1 (live session kept)", got)
	}
	if got := store.countRows(); got != 1 {
		t.Fatalf("rows after own-engine sweep = %d, want 1 (live row kept)", got)
	}

	// Once aborted (unregistered), the same sweep removes it.
	_ = s.Abort(context.Background())
	if _, err := e.sweepSessions(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := countSessionDirs(t, root); got != 0 {
		t.Fatalf("sessions after abort+sweep = %d, want 0", got)
	}
}

// TestSweepFallsBackToMtime: an orphan uploads/ dir with NO surviving row is
// reclaimed by the orphan scan (no row to age it out).
func TestSweepFallsBackToMtime(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "uploads", "no-state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	eng, err := OpenEngine(root, Options{Sessions: newMemUploadSessions()})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test
	if got := countSessionDirs(t, root); got != 0 {
		t.Fatalf("sessions after sweep = %d, want 0 (orphan dir reclaimed)", got)
	}
}

// busyUploadSessions wraps the in-memory store with a ListExpired that always
// fails, simulating the transient DB fault (SQLITE_BUSY under load, see T-54)
// a startup sweep can hit.
type busyUploadSessions struct {
	*memUploadSessions
	cause error
}

func (b *busyUploadSessions) ListExpired(context.Context, string, int) ([]*metadata.UploadSession, error) {
	return nil, b.cause
}

// TestOpenSweepFailureKeepsUploads pins review2 B1: when the startup sweep
// fails, OpenEngine must fail the open WITHOUT deleting anything under
// uploads/. runGC/runExport open an engine over a LIVE server's data dir, so
// one transient DB error must not destroy resumable upload dirs the sweep
// contract (unexpired rows protect their dirs) requires us to keep.
func TestOpenSweepFailureKeepsUploads(t *testing.T) {
	root := t.TempDir()
	store := newMemUploadSessions()
	// One resumable dir (fresh row), one expired dir the failing sweep never
	// reached, and one crash orphan: all must survive the failed open.
	mkStaleSession(t, root, store, "resumable", time.Now().Add(time.Hour))
	mkStaleSession(t, root, store, "expired-unswept", time.Now().Add(-time.Minute))
	orphan := filepath.Join(root, "uploads", "crash-orphan")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "data"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := OpenEngine(root, Options{Sessions: &busyUploadSessions{
		memUploadSessions: store,
		cause:             errors.New("db is locked (simulated SQLITE_BUSY)"),
	}})
	if err == nil {
		t.Fatal("OpenEngine = nil error, want the sweep failure to fail the open")
	}
	if !strings.Contains(err.Error(), "sweep sessions") {
		t.Fatalf("err = %v, want it to carry the sweep context", err)
	}

	// The failure path must not touch disk: every pre-existing dir is still
	// there with its bytes intact, and no row was deleted.
	for _, id := range []string{"resumable", "expired-unswept", "crash-orphan"} {
		b, rerr := os.ReadFile(filepath.Join(root, "uploads", id, "data"))
		if rerr != nil {
			t.Fatalf("uploads/%s/data after failed open: %v (dir was deleted)", id, rerr)
		}
		if string(b) != "partial" {
			t.Fatalf("uploads/%s/data = %q, want %q", id, b, "partial")
		}
	}
	if got := store.countRows(); got != 2 {
		t.Fatalf("rows after failed open = %d, want 2 (untouched)", got)
	}
}

func refsSet(sums ...string) func() (map[string]struct{}, error) {
	return func() (map[string]struct{}, error) {
		m := make(map[string]struct{}, len(sums))
		for _, s := range sums {
			m[s] = struct{}{}
		}
		return m, nil
	}
}

func TestGCDryRunAndApply(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()

	kept := put(t, eng, []byte("referenced blob"))             // in nodes
	orphanFresh := put(t, eng, []byte("orphan, inside grace")) // no reference, young
	orphanOld := put(t, eng, []byte("orphan, past grace"))     // no reference, old

	// Age only the last blob past the grace period.
	backdateBlob(t, root, orphanOld.Sha256, 2*DefaultGCGrace)

	// Dry-run: lists the old orphan only, deletes nothing.
	got, err := eng.GC(ctx, refsSet(kept.Sha256), DefaultGCGrace, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != orphanOld.Sha256 {
		t.Fatalf("dry-run candidates = %v, want [%s]", got, orphanOld.Sha256)
	}
	if n := countBlobs(t, root); n != 3 {
		t.Fatalf("blobs after dry-run = %d, want 3 (nothing deleted)", n)
	}

	// Apply: removes exactly the old orphan.
	deleted, err := eng.GC(ctx, refsSet(kept.Sha256), DefaultGCGrace, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != orphanOld.Sha256 {
		t.Fatalf("apply deletions = %v, want [%s]", deleted, orphanOld.Sha256)
	}
	if n := countBlobs(t, root); n != 2 {
		t.Fatalf("blobs after apply = %d, want 2", n)
	}
	if _, err := eng.Stat(ctx, orphanOld.Sha256); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Stat deleted blob err = %v, want ErrBlobNotFound", err)
	}
	// Referenced and in-grace orphans survive.
	if _, err := eng.Stat(ctx, kept.Sha256); err != nil {
		t.Fatalf("Stat referenced blob: %v", err)
	}
	if _, err := eng.Stat(ctx, orphanFresh.Sha256); err != nil {
		t.Fatalf("Stat in-grace orphan: %v", err)
	}
}

func backdateBlob(t *testing.T, root, sha256 string, age time.Duration) {
	t.Helper()
	p := wantBlobPath(t, root, sha256)
	past := time.Now().Add(-age)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatalf("chtimes blob %s: %v", p, err)
	}
}

func TestGCZeroGraceDefaultsAndNilCallback(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()
	ref := put(t, eng, []byte("will be referenced"))

	// grace=0 falls back to DefaultGCGrace: a young orphan survives.
	put(t, eng, []byte("young orphan"))
	if _, err := eng.GC(ctx, refsSet(ref.Sha256), 0, true); err != nil {
		t.Fatalf("gc with zero grace: %v", err)
	}
	if n := countBlobs(t, root); n != 2 {
		t.Fatalf("blobs after zero-grace gc = %d, want 2 (default grace applied)", n)
	}

	if _, err := eng.GC(ctx, nil, time.Hour, false); err == nil {
		t.Fatal("gc with nil referenced callback should fail")
	}
}

// engineRoot digs the root path back out of the Engine for path-based test
// assertions (test-only helper; production code never needs this).
func engineRoot(t *testing.T, eng Engine) string {
	t.Helper()
	e, ok := eng.(*engine)
	if !ok {
		t.Fatalf("engine is %T, want *engine", eng)
	}
	return e.root
}

// TestGCDoesNotCollectInFlightReference reproduces the grace-period race the
// design guards against: a blob committed but whose metadata row has not
// landed yet must survive a GC pass within the grace window.
func TestGCDoesNotCollectInFlightReference(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()
	// Metadata "transaction" has not happened: the referenced set is stale
	// and does not know about this blob yet.
	inFlight := put(t, eng, []byte("blob-first, metadata second"))
	backdateBlob(t, root, inFlight.Sha256, time.Minute) // young

	if _, err := eng.GC(ctx, refsSet( /* nothing references it yet */ ), DefaultGCGrace, true); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Stat(ctx, inFlight.Sha256); err != nil {
		t.Fatalf("in-flight blob collected inside grace window: %v", err)
	}

	// Once past grace and still unreferenced, it is collected.
	backdateBlob(t, root, inFlight.Sha256, 2*DefaultGCGrace)
	deleted, err := eng.GC(ctx, refsSet(), DefaultGCGrace, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != inFlight.Sha256 {
		t.Fatalf("deleted = %v, want [%s]", deleted, inFlight.Sha256)
	}
}

func TestGCIgnoresForeignEntries(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()
	ref := put(t, eng, []byte("real blob"))

	// Foreign junk that must never be touched or crash the scan.
	for _, junk := range []struct{ dir, name string }{
		{"blobs/zz", "not-a-checksum"},          // invalid shard name
		{"blobs/ab", "short"},                   // non-digest filename
		{"blobs/" + ref.Sha256[:2], "deadbeef"}, // right shard, wrong shape
		{"blobs/tmp", "x"},                      // non-hex shard
	} {
		if err := os.MkdirAll(filepath.Join(root, junk.dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, junk.dir, junk.name), []byte("junk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A blob whose name does not match its shard directory.
	p := filepath.Join(root, "blobs", "00", ref.Sha256)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("misplaced"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := eng.GC(ctx, refsSet(ref.Sha256), DefaultGCGrace, true); err != nil {
		t.Fatalf("gc over foreign entries: %v", err)
	}
	// The referenced blob is untouched; junk files are not blobs of ours and
	// are left alone (they are invisible to validSha256/shard matching).
	if _, err := eng.Stat(ctx, ref.Sha256); err != nil {
		t.Fatalf("referenced blob damaged: %v", err)
	}
}

func TestDeleteAndCorruption(t *testing.T) {
	eng := newEngine(t, Options{})
	ctx := context.Background()
	ref := put(t, eng, []byte("to be deleted"))

	if err := eng.Delete(ctx, ref.Sha256); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := eng.Delete(ctx, ref.Sha256); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("second Delete err = %v, want ErrBlobNotFound", err)
	}

	// Corruption detection: content that no longer hashes to its path.
	ref2 := put(t, eng, []byte("integrity check target"))
	root := engineRoot(t, eng)
	p := wantBlobPath(t, root, ref2.Sha256)
	if err := os.WriteFile(p, []byte("tampered!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Stat(ctx, ref2.Sha256); !errors.Is(err, ErrBlobCorrupt) {
		t.Fatalf("Stat tampered blob err = %v, want ErrBlobCorrupt", err)
	}
}

// TestCloseRefusesNewSessionsAndPreserves pins the ADR-0028 Close contract:
// a clean shutdown detaches live sessions (their fds close, the handles go
// final) but PRESERVES their uploads/<id>/ directories — the restart-resume
// state. Reclaiming them is the startup sweep + TTL's job, never Close's.
func TestCloseRefusesNewSessionsAndPreserves(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := s.ID()
	if _, err := s.Append(context.Background(), strings.NewReader("in-flight when shutdown hits")); err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close with live session: %v", err)
	}
	// ADR-0028: the session dir (and its data file) survive the Close.
	if n := countSessionDirs(t, root); n != 1 {
		t.Fatalf("session dirs after Close = %d, want 1 (preserved, ADR-0028)", n)
	}
	got, err := os.ReadFile(filepath.Join(root, uploadsDirName, id, dataFileName))
	if err != nil {
		t.Fatalf("preserved data file unreadable: %v", err)
	}
	if string(got) != "in-flight when shutdown hits" {
		t.Fatalf("preserved data = %q, want the appended bytes intact", got)
	}
	// The handle is detached, not usable — and must not panic on reuse.
	if _, err := s.Append(context.Background(), strings.NewReader("x")); err == nil {
		t.Fatal("Append on a Close-detached session should fail")
	}
	if _, err := s.Commit(context.Background(), BlobRef{}); err == nil {
		t.Fatal("Commit on a Close-detached session should fail")
	}
	if err := s.Abort(context.Background()); err != nil {
		t.Fatalf("Abort on a Close-detached session: %v", err)
	}
	// Close finalizes the engine; new mutations are refused.
	if _, err := eng.BeginSession(context.Background()); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("BeginSession after Close err = %v, want ErrEngineClosed", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	// Reopen: with no Sessions store the dir has no protecting row, so the
	// startup sweep's orphan scan reclaims it (the only reclamation path).
	eng2, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng2.Close() //nolint:errcheck // test
	if n := countSessionDirs(t, root); n != 0 {
		t.Fatalf("session dirs after reopen sweep = %d, want 0 (orphan, no row)", n)
	}
}
