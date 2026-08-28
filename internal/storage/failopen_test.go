package storage

// Fail-open legs for the dual-write MigrationEngine (ADR-0040 / T-338). The
// mock S3's injected downtime (mockS3Server.down) stands in for a stopped
// MinIO; the live-MinIO leg (failopen_minio_test.go) covers the real
// dial-refused posture end to end.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// foHarness is a migrationTestHarness with fail-open conveniences: fast drain
// parameters, an event recorder and queue-path helpers.
type foHarness struct {
	*migrationTestHarness
	events *foEventRecorder
}

// foEventRecorder captures ReplayEvents for assertions.
type foEventRecorder struct {
	mu  sync.Mutex
	evs []ReplayEvent
}

func (r *foEventRecorder) record(ev ReplayEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, ev)
}

func (r *foEventRecorder) snapshot() []ReplayEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ReplayEvent, len(r.evs))
	copy(out, r.evs)
	return out
}

// windowEvents returns the recorded window transitions.
func (r *foEventRecorder) windowEvents() []ReplayEvent {
	var out []ReplayEvent
	for _, ev := range r.snapshot() {
		if ev.Kind == ReplayEventWindow {
			out = append(out, ev)
		}
	}
	return out
}

func (r *foEventRecorder) drainedEvents() []ReplayEvent {
	var out []ReplayEvent
	for _, ev := range r.snapshot() {
		if ev.Kind == ReplayEventDrained {
			out = append(out, ev)
		}
	}
	return out
}

// newFOHarness builds a dual-write harness with fast drain parameters and an
// event recorder wired through SetReplayEvents.
func newFOHarness(t *testing.T) *foHarness {
	t.Helper()
	h := newHarness(t, true, false, 5)
	h.me.fo.drainLoop.backoffStart = 5 * time.Millisecond
	h.me.fo.drainLoop.backoffMax = 40 * time.Millisecond
	h.me.fo.drainLoop.permanentTries = 3
	h.me.fo.drainLoop.reconcileRounds = 3
	rec := &foEventRecorder{}
	h.me.SetReplayEvents(rec.record)
	return &foHarness{migrationTestHarness: h, events: rec}
}

// putContent PUTs content through the engine and returns the committed ref.
func (h *foHarness) putContent(t *testing.T, content string) BlobRef {
	t.Helper()
	ctx := context.Background()
	sess, err := h.me.BeginSession(ctx)
	if err != nil {
		t.Fatalf("begin session: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader(content)); err != nil {
		t.Fatalf("append: %v", err)
	}
	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := h.me.ReleaseGCHold(ref.Sha256); err != nil {
		t.Fatalf("release gc hold: %v", err)
	}
	return ref
}

// diskBlobPath returns the on-disk blob layout path for sha (blobs/<xx>/<sha>).
func (h *foHarness) diskBlobPath(sha string) string {
	return filepath.Join(h.diskRoot, "blobs", sha[:2], sha)
}

// queueEntryPaths lists the persisted queue entry files.
func (h *foHarness) queueEntryPaths(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(ReplayQueueDir(h.diskRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read replay queue: %v", err)
	}
	var out []string
	for _, de := range entries {
		if !de.IsDir() && !strings.HasPrefix(de.Name(), ".") {
			out = append(out, de.Name())
		}
	}
	return out
}

// waitFor polls cond until true or the deadline expires.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ---------------------------------------------------------------------------
// AC-A1: PUTs inside the failure window land on disk and enqueue replay debt
// ---------------------------------------------------------------------------

func TestFailOpenWindowPUTLandsDiskAndEnqueues(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	h.mock.setDown(true)

	var refs []BlobRef
	for i := 0; i < 3; i++ {
		refs = append(refs, h.putContent(t, fmt.Sprintf("window-put-%d", i)))
	}

	for _, ref := range refs {
		if _, err := os.Stat(h.diskBlobPath(ref.Sha256)); err != nil {
			t.Errorf("blob %s not on disk in the blob layout: %v", ref.Sha256[:12], err)
		}
		if h.s3Has(ref.Sha256) {
			t.Errorf("blob %s should not be on S3 while it is down", ref.Sha256[:12])
		}
	}

	st := h.me.ReplayStats()
	if st.QueueDepth != 3 {
		t.Errorf("queue depth = %d, want 3", st.QueueDepth)
	}
	if !st.WindowOpen {
		t.Error("failure window should be open after the S3 begin failure")
	}

	// Idempotent enqueue: re-PUT of the same content must not grow the queue
	// (the per-sha entry name is the same dedup source as the checksum).
	dup := h.putContent(t, "window-put-0")
	if dup.Sha256 != refs[0].Sha256 {
		t.Fatalf("same content committed to a different sha: %s vs %s", dup.Sha256, refs[0].Sha256)
	}
	if got := h.me.ReplayStats().QueueDepth; got != 3 {
		t.Errorf("queue depth after duplicate PUT = %d, want 3 (idempotent enqueue)", got)
	}

	// The persisted entries carry the ADR-0040 shape {version,sha256,enqueued_at}.
	for _, name := range h.queueEntryPaths(t) {
		raw, err := os.ReadFile(filepath.Join(ReplayQueueDir(h.diskRoot), name))
		if err != nil {
			t.Fatalf("read queue entry %s: %v", name, err)
		}
		var entry replayEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatalf("queue entry %s is not JSON: %v", name, err)
		}
		if entry.Version != replayEntryVersion || entry.Sha256 == "" || entry.EnqueuedAt.IsZero() {
			t.Errorf("queue entry %s = %+v, want version %d with sha256 and enqueued_at", name, entry, replayEntryVersion)
		}
	}

	// No temp residue: the atomic enqueue leaves only final names.
	entries, _ := os.ReadDir(ReplayQueueDir(h.diskRoot))
	for _, de := range entries {
		if strings.HasPrefix(de.Name(), ".") {
			t.Errorf("temp file %s left behind by the enqueue", de.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// AC-A2: GETs inside the window serve all three blob classes from disk
// ---------------------------------------------------------------------------

func TestFailOpenWindowGETThreeClasses(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	// Class 1: pre-migration disk-only blob (S3 misses it, window closed —
	// the classic fallback, plus the read_fallback metric).
	preRef := h.putOnDisk("pre-migration-disk-only")
	if _, _, err := h.me.Open(context.Background(), preRef.Sha256); err != nil {
		t.Fatalf("open pre-migration blob: %v", err)
	}

	// Class 2: dual-written blob (both sides) before the window.
	dualRef := h.putContent(t, "dual-before-window")

	fallbacksBefore := h.me.ReplayStats().ReadFallbackTotal
	if fallbacksBefore == 0 {
		t.Error("read_fallback_total did not count the S3-miss fallback")
	}

	// The outage begins.
	h.mock.setDown(true)

	// Class 3: new blob inside the window.
	windowRef := h.putContent(t, "in-window-new")

	classes := map[string]string{
		"pre-migration disk-only": preRef.Sha256,
		"dual-written":            dualRef.Sha256,
		"in-window new":           windowRef.Sha256,
	}
	for name, sha := range classes {
		rc, got, err := h.me.Open(context.Background(), sha)
		if err != nil {
			t.Errorf("GET %s blob in window failed (want 200-equivalent): %v", name, err)
			continue
		}
		_ = rc.Close()
		if got.Sha256 != sha {
			t.Errorf("GET %s blob returned sha %s", name, got.Sha256[:12])
		}
		// Stat mirrors Open (the X-Checksum-Deploy path rides Stat).
		if _, err := h.me.Stat(context.Background(), sha); err != nil {
			t.Errorf("Stat %s blob in window failed: %v", name, err)
		}
	}

	// A blob that exists nowhere still answers ErrBlobNotFound honestly.
	if _, _, err := h.me.Open(context.Background(), strings.Repeat("ab", 32)); !errors.Is(err, ErrBlobNotFound) {
		t.Errorf("GET of a missing blob in window = %v, want ErrBlobNotFound", err)
	}
}

// The window opens on the first errored read too (the breaker's only
// trigger is an S3-arm failure — reads included), and that first read still
// succeeds from disk.
func TestFailOpenWindowOpensOnRead(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	dualRef := h.putContent(t, "read-trigger")
	h.mock.setDown(true)

	if _, _, err := h.me.Open(context.Background(), dualRef.Sha256); err != nil {
		t.Fatalf("first GET after the outage should fall back to disk: %v", err)
	}
	if !h.me.ReplayStats().WindowOpen {
		t.Error("the window should have opened on the errored S3 read")
	}
	evs := h.events.windowEvents()
	if len(evs) == 0 || evs[0].Phase != "open" || evs[0].FirstError == "" {
		t.Errorf("window open event = %+v, want phase=open with the S3 error chain", evs)
	}

	// Subsequent writes take the disk-only fast path without dialing S3.
	start := time.Now()
	h.putContent(t, "after-read-trigger")
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("in-window PUT took %v — the fast path should not dial S3", d)
	}
}

// ---------------------------------------------------------------------------
// AC-A3: an S3 arm dying mid-session never destroys the committed disk blob
// ---------------------------------------------------------------------------

func TestFailOpenMidSessionDegradation(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	ctx := context.Background()
	sess, err := h.me.BeginSession(ctx)
	if err != nil {
		t.Fatalf("begin session: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("part-one ")); err != nil {
		t.Fatalf("first append (both arms): %v", err)
	}

	// MinIO dies mid-upload: the next Append's S3 arm fails.
	h.mock.setDown(true)
	if _, err := sess.Append(ctx, strings.NewReader("part-two")); err != nil {
		t.Fatalf("append after S3 death must succeed on the disk arm (fail-open): %v", err)
	}
	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatalf("commit after S3 death must succeed: %v", err)
	}
	if err := h.me.ReleaseGCHold(ref.Sha256); err != nil {
		t.Fatalf("release gc hold: %v", err)
	}

	// The direct site-3 assertion: the disk blob EXISTS (the old code
	// deleted it to "maintain consistency").
	if _, err := os.Stat(h.diskBlobPath(ref.Sha256)); err != nil {
		t.Fatal("committed disk blob was rolled back after the S3 commit failure (site-3 regression)")
	}
	if got := h.me.ReplayStats().QueueDepth; got != 1 {
		t.Errorf("queue depth = %d, want 1 (the S3 debt)", got)
	}
}

// The commit-arm variant: both appends succeed, S3 dies right before Commit.
func TestFailOpenCommitArmFailureKeepsDisk(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	ctx := context.Background()
	sess, err := h.me.BeginSession(ctx)
	if err != nil {
		t.Fatalf("begin session: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("whole-blob")); err != nil {
		t.Fatalf("append: %v", err)
	}
	h.mock.setDown(true)

	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatalf("commit with a dead S3 arm must succeed: %v", err)
	}
	_ = h.me.ReleaseGCHold(ref.Sha256)

	if _, err := os.Stat(h.diskBlobPath(ref.Sha256)); err != nil {
		t.Fatal("disk blob missing after S3 commit failure")
	}
	if !h.diskHas(ref.Sha256) {
		t.Error("disk engine no longer sees the blob")
	}
	if got := h.me.ReplayStats().QueueDepth; got != 1 {
		t.Errorf("queue depth = %d, want 1", got)
	}
}

// The floor: a DISK failure still fails the caller honestly (only one
// backend left means no room to degrade).
func TestFailOpenDiskFailureIsHonest(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	if err := h.me.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := h.me.BeginSession(context.Background()); !errors.Is(err, ErrEngineClosed) {
		t.Errorf("begin after close = %v, want ErrEngineClosed (the disk floor is honest)", err)
	}
}

// ---------------------------------------------------------------------------
// AC-B: the queue survives a restart and the drain resumes
// ---------------------------------------------------------------------------

func TestFailOpenQueueSurvivesRestart(t *testing.T) {
	h := newFOHarness(t)
	h.mock.setDown(true)

	var shas []string
	for i := 0; i < 2; i++ {
		shas = append(shas, h.putContent(t, fmt.Sprintf("survive-%d", i)).Sha256)
	}
	if got := len(h.queueEntryPaths(t)); got != 2 {
		t.Fatalf("queue entries on disk = %d, want 2", got)
	}
	// The "restart": close (worker canceled mid-episode — S3 still down) and
	// rebuild the whole stack over the same data dir with a FRESH S3 engine
	// (Close shut the old one down — a restart process would reconnect).
	if err := h.me.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	h.mock.setDown(false)

	disk2, err := OpenEngine(h.diskRoot, Options{SessionTTL: 24 * time.Hour})
	if err != nil {
		t.Fatalf("reopen disk engine: %v", err)
	}
	s3b, _, _ := newS3Engine(t)
	me2 := NewMigrationEngine(disk2, s3b, MigrationConfig{Enabled: true, Concurrency: 5}).(*MigrationEngine)
	defer func() { _ = me2.Close() }()

	st := me2.ReplayStats()
	if st.QueueDepth != 2 {
		t.Fatalf("restart watermark depth = %d, want 2 (the queue must survive)", st.QueueDepth)
	}

	// Recovery drain converges: every entry reaches S3, the queue empties.
	waitFor(t, "post-restart drain", func() bool { return me2.ReplayStats().QueueDepth == 0 })
	for _, sha := range shas {
		if _, err := s3b.Stat(context.Background(), sha); err != nil {
			t.Errorf("blob %s did not reach S3 after the restart drain: %v", sha[:12], err)
		}
	}
}

// The queue loader: stale temp files are swept, corrupt entries are salvaged
// by filename (derived state), foreign files are left alone.
func TestFailOpenQueueLoadSalvage(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()
	if err := h.me.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	qdir := ReplayQueueDir(h.diskRoot)
	good := strings.Repeat("aa", 32)
	if err := os.WriteFile(filepath.Join(qdir, good+".json"), []byte(`{"version":1,"sha256":"`+good+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Repeat("bb", 32)
	if err := os.WriteFile(filepath.Join(qdir, corrupt+".json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qdir, ".tmp-"+good), []byte("interrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qdir, "operator-note.txt"), []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	q, err := openReplayQueue(qdir)
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	if got := q.depth(); got != 2 {
		t.Errorf("salvaged depth = %d, want 2 (good + filename-salvaged corrupt)", got)
	}
	if _, err := os.Stat(filepath.Join(qdir, ".tmp-"+good)); !os.IsNotExist(err) {
		t.Error("stale temp file was not swept on load")
	}
	if _, err := os.Stat(filepath.Join(qdir, "operator-note.txt")); err != nil {
		t.Error("foreign file must be left alone")
	}
}

// ---------------------------------------------------------------------------
// AC-C: recovery drains the queue, closes the window and reconciles
// ---------------------------------------------------------------------------

func TestFailOpenRecoveryDrainAndReconcile(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	// A pre-migration blob the QUEUE never saw — only the reconcile diff
	// (diskList×s3Set) can catch it.
	orph := h.putOnDisk("reconcile-only-orphan")

	h.mock.setDown(true)
	win1 := h.putContent(t, "drain-me-1")
	win2 := h.putContent(t, "drain-me-2")
	if h.me.ReplayStats().QueueDepth != 2 {
		t.Fatalf("queue depth = %d, want 2", h.me.ReplayStats().QueueDepth)
	}

	// MinIO comes back.
	h.mock.setDown(false)

	waitFor(t, "queue drained", func() bool { return h.me.ReplayStats().QueueDepth == 0 })
	waitFor(t, "window closed", func() bool { return !h.me.ReplayStats().WindowOpen })

	for _, ref := range []BlobRef{win1, win2} {
		if !h.s3Has(ref.Sha256) {
			t.Errorf("blob %s did not reach S3", ref.Sha256[:12])
		}
	}
	// The reconcile diff caught the never-queued orphan.
	waitFor(t, "reconcile copied the orphan", func() bool { return h.s3Has(orph.Sha256) })
	// The convergence event is the divergence-closing point — wait for it
	// explicitly (window close can land before the reconcile rounds finish).
	waitFor(t, "drained event", func() bool { return len(h.events.drainedEvents()) > 0 })
	wins := h.events.windowEvents()
	if len(wins) < 2 {
		t.Fatalf("window events = %d, want >= 2 (open + close)", len(wins))
	}
	if wins[0].Phase != "open" || wins[0].FirstError == "" {
		t.Errorf("first window event = %+v, want open with first_error", wins[0])
	}
	if last := wins[len(wins)-1]; last.Phase != "close" {
		t.Errorf("last window event phase = %q, want close", last.Phase)
	}
	drained := h.events.drainedEvents()
	if len(drained) == 0 {
		t.Fatal("no storage.replay.drained event after convergence")
	}
	last := drained[len(drained)-1]
	if last.Drained < 2 {
		t.Errorf("drained event drained = %d, want >= 2", last.Drained)
	}
	if last.ReconcileMissing != 0 {
		t.Errorf("drained event reconcile_missing = %d, want 0 (converged)", last.ReconcileMissing)
	}

	// A fresh PUT is a steady-state dual write again (both sides, no queue).
	after := h.putContent(t, "post-recovery-steady")
	if !h.s3Has(after.Sha256) || !h.diskHas(after.Sha256) {
		t.Error("post-recovery PUT did not dual-write")
	}
	if got := h.me.ReplayStats().QueueDepth; got != 0 {
		t.Errorf("queue depth after recovery = %d, want 0", got)
	}
}

// Steady state is untouched: a healthy window-free PUT still lands on both
// sides synchronously (H13's truth) and enqueues nothing.
func TestFailOpenSteadyStateUnchanged(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	ref := h.putContent(t, "steady-dual")
	if !h.diskHas(ref.Sha256) || !h.s3Has(ref.Sha256) {
		t.Fatal("steady-state PUT must dual-write synchronously")
	}
	st := h.me.ReplayStats()
	if st.QueueDepth != 0 || st.WindowOpen {
		t.Errorf("steady state stats = %+v, want closed window and empty queue", st)
	}
	if len(h.events.windowEvents()) != 0 {
		t.Error("steady state must not emit window events")
	}
}

// ---------------------------------------------------------------------------
// AC-D: governance interplay — honest failures, source-gone drops
// ---------------------------------------------------------------------------

func TestFailOpenGovernanceInterplay(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	h.mock.setDown(true)
	ref := h.putContent(t, "gc-race-blob")

	// The S3 arm of Delete fails honestly during the window (fail-open never
	// covers the governance plane).
	if err := h.me.Delete(context.Background(), ref.Sha256); err == nil {
		t.Error("Delete during the window must fail honestly, not silently skip S3")
	}

	// GC: unreferenced blob, zero-ish grace, apply. The dual-write sweep
	// runs disk first (reclaims it) and then reports the S3 arm's failure.
	if _, err := h.me.GC(context.Background(), func() (map[string]struct{}, error) {
		return map[string]struct{}{}, nil
	}, time.Nanosecond, true); err == nil {
		t.Error("GC during the window must fail honestly")
	}
	if h.diskHas(ref.Sha256) {
		t.Fatal("disk sweep should still have reclaimed the unreferenced blob")
	}
	if got := h.me.ReplayStats().QueueDepth; got != 1 {
		t.Fatalf("queue depth = %d, want 1 (entry pending)", got)
	}

	// Recovery: the drain must not wedge on the entry whose source is gone —
	// it drops it as source_gone and converges (the drained event is the
	// convergence barrier; depth 0 alone can precede the reconcile).
	h.mock.setDown(false)
	waitFor(t, "source-gone drop", func() bool { return h.me.ReplayStats().QueueDepth == 0 })
	waitFor(t, "governance convergence", func() bool { return len(h.events.drainedEvents()) > 0 })

	st := h.me.ReplayStats()
	if st.SourceGone != 1 {
		t.Errorf("source_gone outcome = %d, want 1", st.SourceGone)
	}
	if h.s3Has(ref.Sha256) {
		t.Error("an unreferenced reclaimed blob must not be copied to S3")
	}
	drained := h.events.drainedEvents()
	if len(drained) == 0 || drained[len(drained)-1].SourceGone != 1 {
		t.Errorf("drained event source_gone = %+v, want 1", drained)
	}
}

// ---------------------------------------------------------------------------
// AC-E: mode boundaries — bypass and completed take none of this
// ---------------------------------------------------------------------------

func TestFailOpenBypassUntouched(t *testing.T) {
	h := newHarness(t, false, false, 5) // bypass
	defer func() { _ = h.me.Close() }()
	h.mock.setDown(true)

	// Bypass never dials S3: the PUT succeeds and nothing enqueues.
	ctx := context.Background()
	sess, err := h.me.BeginSession(ctx)
	if err != nil {
		t.Fatalf("bypass begin under S3 outage: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("bypass-blob")); err != nil {
		t.Fatalf("bypass append: %v", err)
	}
	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatalf("bypass commit: %v", err)
	}
	if !h.diskHas(ref.Sha256) || h.s3Has(ref.Sha256) {
		t.Error("bypass mode must remain disk-only")
	}
	st := h.me.ReplayStats()
	if st.QueueDepth != 0 || st.WindowOpen {
		t.Errorf("bypass fail-open stats = %+v, want all zero (no new code paths)", st)
	}
	if _, err := os.Stat(ReplayQueueDir(h.diskRoot)); !os.IsNotExist(err) {
		t.Error("bypass mode must not create the replay queue directory")
	}
}

func TestFailOpenCompletedUntouchedAndGate(t *testing.T) {
	// Completed mode: S3 is the source of truth; a dead S3 must surface
	// honestly (no disk fallback — disk may already be operator-deleted).
	h := newHarness(t, true, true, 5)
	defer func() { _ = h.me.Close() }()
	h.mock.setDown(true)

	if _, _, err := h.me.Open(context.Background(), strings.Repeat("cd", 32)); err == nil {
		t.Error("completed mode must fail honestly when S3 is down (no fallback floor)")
	}
	if h.me.ReplayStats().WindowOpen {
		t.Error("completed mode must not open fail-open windows")
	}

	// The boot gate: a completed assembly over a non-empty replay queue
	// must refuse to start.
	if err := CheckCompletedReplayQueue(h.diskRoot); err != nil {
		t.Fatalf("empty queue must pass the gate: %v", err)
	}
	qdir := ReplayQueueDir(h.diskRoot)
	if err := os.MkdirAll(qdir, 0o700); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("ee", 32)
	if err := os.WriteFile(filepath.Join(qdir, sha+".json"), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := CheckCompletedReplayQueue(h.diskRoot)
	if !errors.Is(err, ErrCompletedWithReplayQueue) {
		t.Fatalf("gate error = %v, want ErrCompletedWithReplayQueue", err)
	}
	if !strings.Contains(err.Error(), "dual-write") {
		t.Errorf("gate error must name both exits: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Permanent accounting: exhausted entries stay queued, are counted once, and
// still drain after recovery (no dead-letter deletion).
// ---------------------------------------------------------------------------

func TestFailOpenPermanentThenRecovery(t *testing.T) {
	h := newFOHarness(t) // permanentTries = 3
	defer func() { _ = h.me.Close() }()

	h.mock.setDown(true)
	h.putContent(t, "permanent-candidate")

	waitFor(t, "permanent accounting", func() bool { return h.me.ReplayStats().FailedPermanent >= 1 })
	if got := h.me.ReplayStats().QueueDepth; got != 1 {
		t.Errorf("queue depth after permanent accounting = %d, want 1 (entry retained)", got)
	}

	// Recovery still drains the permanently-marked entry.
	h.mock.setDown(false)
	waitFor(t, "post-permanent drain", func() bool { return h.me.ReplayStats().QueueDepth == 0 })
	if h.me.ReplayStats().FailedPermanent != 1 {
		t.Errorf("permanent counter must not double-count across rounds: %d", h.me.ReplayStats().FailedPermanent)
	}
}

// ---------------------------------------------------------------------------
// Concurrency: parallel PUTs into an open window all land, all enqueue
// exactly once, and the drain converges under -race.
// ---------------------------------------------------------------------------

func TestFailOpenConcurrentWindowPUTs(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	h.mock.setDown(true)

	const workers, per = 8, 4
	var wg sync.WaitGroup
	errs := make(chan error, workers*per)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < per; j++ {
				ctx := context.Background()
				sess, err := h.me.BeginSession(ctx)
				if err != nil {
					errs <- fmt.Errorf("w%d begin: %w", id, err)
					return
				}
				if _, err := sess.Append(ctx, strings.NewReader(fmt.Sprintf("race-%d-%d", id, j))); err != nil {
					_ = sess.Abort(ctx)
					errs <- fmt.Errorf("w%d append: %w", id, err)
					return
				}
				ref, err := sess.Commit(ctx, BlobRef{})
				if err != nil {
					errs <- fmt.Errorf("w%d commit: %w", id, err)
					return
				}
				if err := h.me.ReleaseGCHold(ref.Sha256); err != nil {
					errs <- fmt.Errorf("w%d hold: %w", id, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if got := h.me.ReplayStats().QueueDepth; got != workers*per {
		t.Errorf("queue depth = %d, want %d", got, workers*per)
	}

	h.mock.setDown(false)
	waitFor(t, "drain after concurrent window", func() bool { return h.me.ReplayStats().QueueDepth == 0 })
	if got := h.s3ObjectCount(); got != workers*per {
		t.Errorf("S3 objects = %d, want %d", got, workers*per)
	}
}

// ---------------------------------------------------------------------------
// AC-G: the migration status envelope keeps its exact shape through a
// fail-open episode (queue state rides metrics/logs, never this endpoint).
// ---------------------------------------------------------------------------

func TestFailOpenStatusViewShapeUnchanged(t *testing.T) {
	h := newFOHarness(t)
	defer func() { _ = h.me.Close() }()

	before, err := json.Marshal(h.me.StatusView())
	if err != nil {
		t.Fatal(err)
	}

	h.mock.setDown(true)
	h.putContent(t, "shape-check")
	h.mock.setDown(false)
	waitFor(t, "drain", func() bool { return h.me.ReplayStats().QueueDepth == 0 })

	after, err := json.Marshal(h.me.StatusView())
	if err != nil {
		t.Fatal(err)
	}

	// The key set must be byte-identical (93.6: the endpoint shape is frozen).
	var beforeMap, afterMap map[string]any
	if err := json.Unmarshal(before, &beforeMap); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &afterMap); err != nil {
		t.Fatal(err)
	}
	if len(beforeMap) != len(afterMap) {
		t.Fatalf("status view key set changed: %s vs %s", before, after)
	}
	for k := range beforeMap {
		if _, ok := afterMap[k]; !ok {
			t.Fatalf("status view lost key %q: %s vs %s", k, before, after)
		}
	}
	for _, k := range []string{"running", "done", "total", "migrated", "skipped", "failed"} {
		if _, ok := afterMap[k]; !ok {
			t.Errorf("status view missing frozen key %q", k)
		}
	}
}
