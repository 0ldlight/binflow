package storage

// The prune walk's behavior tests (LOOP 003 / prune-gc-admin.md E2-E4):
// estimate vs apply polarity, the safety gates shared with GCSweep (mark,
// hold, grace, Live recheck), the stop marker, the startFrom resume and
// the full 00..ff enumeration. Seeding rides writeBlobFile (backup_test).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fixedMarker pins an explicit live set; Live is the set membership.
type fixedMarker struct {
	set map[string]struct{}
}

func (m fixedMarker) Mark() (map[string]struct{}, error) { return m.set, nil }
func (m fixedMarker) Live(sha string) (bool, error) {
	_, ok := m.set[sha]
	return ok, nil
}

// errMarker fails every Live answer — the prune error arm: no deletion on
// an uncertain answer (the GCSweep conservative posture).
type errMarker struct{}

func (errMarker) Mark() (map[string]struct{}, error) { return map[string]struct{}{}, nil }
func (errMarker) Live(string) (bool, error)          { return false, errors.New("marker unavailable") }

func TestPruneEstimateModeCountsNeverDeletes(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	old := time.Now().Add(-48 * time.Hour)
	sha := writeBlobFile(t, root, "orphan-body", 0o600, old)
	out, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker: fixedMarker{set: map[string]struct{}{}},
		Grace:  time.Nanosecond,
		Apply:  false, // Artifactory dryRun:true — the estimate mode
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if out.Totals.BinariesProcessed != 1 {
		t.Fatalf("processed = %d, want 1", out.Totals.BinariesProcessed)
	}
	if out.Totals.BinariesCleaned != 0 || out.Totals.BytesCleaned != 0 {
		t.Fatalf("estimate mode cleaned = %d/%d, want 0/0", out.Totals.BinariesCleaned, out.Totals.BytesCleaned)
	}
	if _, serr := os.Stat(filepath.Join(root, "blobs", sha[:2], sha)); serr != nil {
		t.Fatalf("estimate mode deleted the blob: %v", serr)
	}
}

func TestPruneApplyDeletesUnreferencedKeepsReferenced(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	old := time.Now().Add(-48 * time.Hour)
	orphan := writeBlobFile(t, root, "orphan", 0o600, old)
	live := writeBlobFile(t, root, "live", 0o600, old)
	out, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker: fixedMarker{set: map[string]struct{}{live: {}}},
		Grace:  time.Nanosecond,
		Apply:  true,
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if out.Totals.BinariesProcessed != 2 || out.Totals.BinariesCleaned != 1 {
		t.Fatalf("processed/cleaned = %d/%d, want 2/1", out.Totals.BinariesProcessed, out.Totals.BinariesCleaned)
	}
	if len(out.Deleted) != 1 || out.Deleted[0] != orphan {
		t.Fatalf("deleted = %v, want [%s]", out.Deleted, orphan)
	}
	if _, serr := os.Stat(filepath.Join(root, "blobs", live[:2], live)); serr != nil {
		t.Fatalf("referenced blob was deleted: %v", serr)
	}
	if _, serr := os.Stat(filepath.Join(root, "blobs", orphan[:2], orphan)); !os.IsNotExist(serr) {
		t.Fatalf("orphan blob still present: %v", serr)
	}
}

func TestPruneGraceWindowProtectsFreshBlobs(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	sha := writeBlobFile(t, root, "fresh-orphan", 0o600, time.Now().Add(-time.Minute))
	_, err = pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker: fixedMarker{set: map[string]struct{}{}},
		Grace:  time.Hour, // the standing configured window
		Apply:  true,
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "blobs", sha[:2], sha)); serr != nil {
		t.Fatalf("fresh blob inside grace was deleted: %v", serr)
	}
}

func TestPruneHoldGateProtectsInFlightUpload(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	sha := writeBlobFile(t, root, "in-flight", 0o600, time.Now().Add(-48*time.Hour))
	e := eng.(*engine)
	e.holds.acquire(sha) // a publisher between acquire and release
	defer e.holds.release(sha)

	out, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker: fixedMarker{set: map[string]struct{}{}},
		Grace:  time.Nanosecond, // grace aside: the hold alone must protect
		Apply:  true,
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if out.Totals.BinariesCleaned != 0 {
		t.Fatalf("held blob was cleaned: %d", out.Totals.BinariesCleaned)
	}
	if _, serr := os.Stat(filepath.Join(root, "blobs", sha[:2], sha)); serr != nil {
		t.Fatalf("held blob was deleted: %v", serr)
	}
}

func TestPruneUncertainLiveAnswerSkipsDeletionAndErrors(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	sha := writeBlobFile(t, root, "uncertain", 0o600, time.Now().Add(-48*time.Hour))
	out, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker: errMarker{},
		Grace:  time.Nanosecond,
		Apply:  true,
	})
	if err == nil {
		t.Fatal("an uncertain Live answer must surface as the pass's error")
	}
	if out == nil || out.Totals.BinariesCleaned != 0 {
		t.Fatalf("uncertain answer deleted a blob: %+v", out)
	}
	if _, serr := os.Stat(filepath.Join(root, "blobs", sha[:2], sha)); serr != nil {
		t.Fatalf("uncertain candidate was deleted: %v", serr)
	}
}

// deleteOnLiveMarker simulates the competing sweeper of review N3: at the
// walk's per-candidate Live recheck, another gc/prune has JUST collected
// the candidate — the file vanishes between this walk's ReadDir and its
// Delete, the loser-of-two-sweepers shape.
type deleteOnLiveMarker struct {
	path  string
	fired bool
}

func (m *deleteOnLiveMarker) Mark() (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

func (m *deleteOnLiveMarker) Live(string) (bool, error) {
	if !m.fired {
		m.fired = true
		_ = os.Remove(m.path) // the competing sweeper's deletion lands now
	}
	return false, nil
}

func TestPruneLostDeleteRaceIsBenignSkip(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	sha := writeBlobFile(t, root, "lost-race", 0o600, time.Now().Add(-48*time.Hour))
	out, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker: &deleteOnLiveMarker{path: filepath.Join(root, "blobs", sha[:2], sha)},
		Grace:  time.Nanosecond,
		Apply:  true,
	})
	if err != nil {
		t.Fatalf("a delete lost to a concurrent sweeper must be a benign skip, got the pass's error: %v", err)
	}
	if out.Totals.BinariesCleaned != 0 || len(out.Deleted) != 0 {
		t.Fatalf("the losing walk must not claim the deletion: %+v %v", out.Totals, out.Deleted)
	}
	if out.Totals.BinariesProcessed != 1 {
		t.Fatalf("processed = %d, want 1 (the candidate was examined)", out.Totals.BinariesProcessed)
	}
}

func TestPruneStopMarkerLandsWithinOneDirectory(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	var observed []PruneDirStats
	out, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker:  fixedMarker{set: map[string]struct{}{}},
		Grace:   time.Nanosecond,
		Stop:    func() bool { return len(observed) >= 3 }, // stop after three directories
		Observe: func(s PruneDirStats) { observed = append(observed, s) },
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !out.Stopped {
		t.Fatal("stopped pass must report Stopped")
	}
	if len(observed) != 3 {
		t.Fatalf("observed %d directories after stop, want 3", len(observed))
	}
	if out.LastDir.Name != observed[len(observed)-1].Name {
		t.Fatalf("last dir = %q, want %q", out.LastDir.Name, observed[len(observed)-1].Name)
	}
}

func TestPruneEnumeratesAll256WithAbsentShards(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	writeBlobFile(t, root, "only-blob", 0o600, time.Now().Add(-48*time.Hour))
	var names []string
	_, err = pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker:  fixedMarker{set: map[string]struct{}{}},
		Observe: func(s PruneDirStats) { names = append(names, s.Name) },
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(names) != PruneShardCount {
		t.Fatalf("enumerated %d directories, want %d", len(names), PruneShardCount)
	}
	if names[0] != "00" || names[len(names)-1] != "ff" {
		t.Fatalf("enumeration bounds = %q..%q, want 00..ff", names[0], names[len(names)-1])
	}
}

func TestPruneStartFromSkipsButCounts(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	old := time.Now().Add(-48 * time.Hour)
	// Bodies chosen by digest: "zzzz" hashes to shard 2d, "hi" to 8f —
	// one on each side of the 80 resume point.
	low := writeBlobFile(t, root, "zzzz", 0o600, old)
	high := writeBlobFile(t, root, "hi", 0o600, old)

	var skipped, processed int
	var lastIndex int
	out, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker:    fixedMarker{set: map[string]struct{}{}},
		Grace:     time.Nanosecond,
		Apply:     true,
		StartFrom: "80", // resume point: everything below 80 is skipped
		Observe: func(s PruneDirStats) {
			lastIndex = s.Index
			if s.Skipped {
				skipped++
				return
			}
			processed++
		},
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if skipped != 0x80 {
		t.Fatalf("skipped = %d, want %d", skipped, 0x80)
	}
	if lastIndex != PruneShardCount {
		t.Fatalf("final index = %d, want %d (the denominator stays 256)", lastIndex, PruneShardCount)
	}
	if out.Totals.BinariesProcessed != 1 {
		t.Fatalf("processed blobs = %d, want 1 (only the >=80 shard)", out.Totals.BinariesProcessed)
	}
	if !fileExists(t, root, low) {
		t.Fatal("the below-80 blob was cleaned despite the resume point")
	}
	if fileExists(t, root, high) {
		t.Fatal("the at/above-80 blob was not cleaned")
	}
	if processed != PruneShardCount-0x80 {
		t.Fatalf("processed dirs = %d, want %d", processed, PruneShardCount-0x80)
	}
}

func TestPruneInvalidStartFromRejected(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test
	if _, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker:    fixedMarker{},
		StartFrom: "zz",
	}); err == nil {
		t.Fatal("invalid startFromDirectory must be rejected")
	}
	if _, err := pruneOf(t, eng).Prune(context.Background(), PruneOptions{
		Marker:    fixedMarker{},
		StartFrom: "0",
	}); err == nil {
		t.Fatal("non-two-hex startFromDirectory must be rejected")
	}
}

func TestPruneConcurrentWithPublishNeverDeletesLiveBlob(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test
	// Resolved on the test thread: pruneOf's t.Fatal must never fire from
	// inside a racer goroutine (FailNow off the test goroutine is
	// undefined — the review N1 companion fix).
	pr := pruneOf(t, eng)

	// The publisher's timeline: acquire(hold) -> publish(rename) ->
	// metadata commit -> release. The test parks at "committed": the hold
	// is still up AND the mark set carries the reference, while four
	// prunes race through the same shards deleting the orphan.
	old := time.Now().Add(-48 * time.Hour)
	orphan := writeBlobFile(t, root, "race-orphan", 0o600, old)
	live := writeBlobFile(t, root, "race-live", 0o600, old)
	e := eng.(*engine)
	e.holds.acquire(live)
	defer e.holds.release(live)
	marked := fixedMarker{set: map[string]struct{}{live: {}}}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = pr.Prune(context.Background(), PruneOptions{
				Marker: marked,
				Grace:  time.Nanosecond,
				Apply:  true,
			})
		}()
	}
	wg.Wait()

	// The name's promise: the held+referenced live blob survives every
	// racer. The orphan is collected by the first racer through its shard
	// or survives whole — never half — and the losers' ErrBlobNotFound
	// answers ride the benign-skip arm, surfacing no pass error.
	if !fileExists(t, root, live) {
		t.Fatal("concurrent prunes deleted the held+referenced live blob")
	}
	out, err := pr.Prune(context.Background(), PruneOptions{Marker: marked})
	if err != nil {
		t.Fatalf("post-race estimate: %v", err)
	}
	want := int64(1) // the live blob
	if fileExists(t, root, orphan) {
		want = 2
	}
	if out.Totals.BinariesProcessed != want {
		t.Fatalf("post-race estimate processed = %d, want %d (on-disk truth)", out.Totals.BinariesProcessed, want)
	}
}

func TestPruneContextCancelStopsWalk(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test

	ctx, cancel := context.WithCancel(context.Background())
	var observed int
	cancel() // already dead: the first boundary check must stop the walk
	_, err = pruneOf(t, eng).Prune(ctx, PruneOptions{
		Marker:  fixedMarker{},
		Observe: func(PruneDirStats) { observed++ },
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context must surface, got %v", err)
	}
	if observed != 0 {
		t.Fatalf("observed %d directories after cancellation, want 0", observed)
	}
}

func fileExists(t *testing.T, root, sha string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, "blobs", sha[:2], sha))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat blob %s: %v", sha, err)
	return false
}

// pruneOf resolves the prune capability the way the REST face does — the
// optional interface asserted on the opened engine.
func pruneOf(t *testing.T, eng Engine) Pruner {
	t.Helper()
	pr, ok := eng.(Pruner)
	if !ok {
		t.Fatal("engine does not carry the prune capability")
	}
	return pr
}
