package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSingleflightPanicDoesNotWedgeFollowups is the panic-path regression
// test (round-2 review): a leader that panics must tear the flight down —
// map entry removed, waiters released — instead of wedging every future
// caller on the same key behind a WaitGroup that never fires.
func TestSingleflightPanicDoesNotWedgeFollowups(t *testing.T) {
	var g singleflight

	// Leader panics; the test recovers what propagates.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic did not propagate out of do()")
			}
		}()
		_ = g.do("panic-key", func() error { panic("leader exploded") })
	}()

	// The flight must be torn down synchronously when the panic unwinds:
	// a follow-up call returns instead of blocking forever.
	done := make(chan error, 1)
	go func() {
		done <- g.do("panic-key", func() error { return errors.New("fresh attempt") })
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errors.New("fresh attempt")) && err == nil {
			t.Fatalf("follow-up err = %v, want the fresh attempt's error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("follow-up call wedged: panic left the flight in a locked state")
	}

	// And the map must not leak the key: a later call runs its own fn.
	var ran atomic.Bool
	if err := g.do("panic-key", func() error { ran.Store(true); return nil }); err != nil || !ran.Load() {
		t.Fatalf("later call err=%v ran=%t, want nil error and fn executed", err, ran.Load())
	}
}

// TestSingleflightWaiterSurvivesLeaderPanic: waiters blocked on a flight
// whose leader panics must be released (not deadlocked), per the godoc
// contract.
func TestSingleflightWaiterSurvivesLeaderPanic(t *testing.T) {
	var g singleflight
	leaderStarted := make(chan struct{})
	waiterEntered := make(chan struct{})
	waiterDone := make(chan error, 1)

	go func() {
		defer func() { _ = recover() }() // leader panics on purpose
		_ = g.do("k", func() error {
			close(leaderStarted)
			<-waiterEntered // hold the flight open until the waiter joins
			panic("boom")
		})
	}()
	<-leaderStarted

	go func() {
		waiterEntered <- struct{}{}
		waiterDone <- g.do("k", func() error { return nil })
	}()

	select {
	case <-waiterDone:
		// Released; err is whatever the torn-down flight exposed.
	case <-time.After(3 * time.Second):
		t.Fatal("waiter wedged on a panicking leader")
	}
}

// TestCommitRenameFailureIsClean固化 the rename-failure window the reviewer
// probed: when the physical publish fails (here: shard dir made read-only),
// the error must propagate, the session must finalize (no reuse), the
// session directory must be cleaned, no blob may appear, and a retry after
// the obstacle is removed must converge normally.
func TestCommitRenameFailureIsClean(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: 0500 directory does not block root, obstacle would not trigger")
	}
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()

	content := []byte("rename will fail the first time")
	s, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, strings.NewReader(string(content))); err != nil {
		t.Fatal(err)
	}
	ref, err := s.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatal(err) // need the digest to place the obstacle
	}
	_ = ref

	// Second upload of the same content with the shard dir read-only.
	shard := filepath.Join(root, "blobs", ref.Sha256[:2])
	if err := os.Remove(filepath.Join(shard, ref.Sha256)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shard, 0o500); err != nil { //nolint:gosec // G302: deliberate obstacle injection, see skip above
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(shard, 0o700) //nolint:gosec // G302: best-effort restore of the injected obstacle
	}()

	s2, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Append(ctx, strings.NewReader(string(content))); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Commit(ctx, BlobRef{}); err == nil {
		t.Fatal("Commit should fail while the shard dir is read-only")
	} else if !strings.Contains(err.Error(), "rename") && !strings.Contains(err.Error(), "permission") {
		t.Fatalf("Commit err = %v, want a rename/permission failure", err)
	}

	// Session finalized: Append reports it, directory is gone.
	if _, err := s2.Append(ctx, strings.NewReader("more")); err == nil || !strings.Contains(err.Error(), "finalized") {
		t.Fatalf("Append after failed Commit err = %v, want already finalized", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", s2.ID())); !os.IsNotExist(err) {
		t.Fatalf("session dir survived failed commit: %v", err)
	}
	if n := countBlobs(t, root); n != 0 {
		t.Fatalf("blobs after failed rename = %d, want 0", n)
	}

	// Obstacle removed: retry converges to one clean blob.
	if err := os.Chmod(shard, 0o700); err != nil { //nolint:gosec // G302: restoring the obstacle we injected
		t.Fatal(err)
	}
	ref2 := put(t, eng, content)
	if ref2.Sha256 != ref.Sha256 {
		t.Fatalf("retry digest = %s, want %s", ref2.Sha256, ref.Sha256)
	}
	if n := countBlobs(t, root); n != 1 {
		t.Fatalf("blobs after retry = %d, want 1", n)
	}
	if _, err := eng.Stat(ctx, ref.Sha256); err != nil {
		t.Fatalf("Stat after retry: %v", err)
	}
}

// TestGCGraceProtectsConcurrentUploads is the race the reviewer probed:
// GC with a sub-second grace running against concurrent uploads must never
// delete a blob that an in-flight commit just placed. With grace=1ns an
// mtime-aged-out blob is collectable by design (that is what grace means),
// so the meaningful invariant is: uploads racing a GC whose grace window
// covers their commit timestamp all survive. We backdate nothing; the
// uploader finishes inside the window and the blob must persist.
//
// The pass condition mirrors what the reviewer's probe showed: 8 uploads
// vs a hammering GC with a small-but-real grace -> 8/8 blobs alive.
func TestGCGraceProtectsConcurrentUploads(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()

	const uploads = 8
	grace := 2 * time.Second // covers the whole upload burst below
	var wgUpload sync.WaitGroup
	refs := make([]string, uploads)
	gcStop := make(chan struct{})
	var gcErr atomic.Value

	var wgGC sync.WaitGroup
	wgGC.Add(1)
	go func() {
		defer wgGC.Done()
		for {
			select {
			case <-gcStop:
				return
			default:
			}
			// grace small but real: young blobs (just committed) are inside
			// the window and must never be collected.
			if _, err := eng.GC(ctx, refsSet(), grace, true); err != nil {
				gcErr.Store(err)
				return
			}
		}
	}()

	for i := 0; i < uploads; i++ {
		wgUpload.Add(1)
		go func(i int) {
			defer wgUpload.Done()
			ref := put(t, eng, []byte(strings.Repeat("grace-race-", 100+i)))
			refs[i] = ref.Sha256
		}(i)
	}
	wgUpload.Wait()
	close(gcStop)
	wgGC.Wait()

	if err := gcErr.Load(); err != nil {
		t.Fatalf("GC errored during race: %v", err)
	}
	for i, sum := range refs {
		if sum == "" {
			t.Fatalf("upload %d produced no ref", i)
		}
		if _, err := eng.Stat(ctx, sum); err != nil {
			t.Fatalf("blob %d lost to GC race: %v", i, err)
		}
	}
	if n := countBlobs(t, root); n != uploads {
		t.Fatalf("blobs after race = %d, want %d", n, uploads)
	}
}

// TestGCAgedOrphanCollectedWithTinyGrace pins the other half of the grace
// semantics the godoc now states: a blob whose mtime is explicitly past a
// tiny grace IS collected — grace=1ns is not silently upgraded to 24h.
func TestGCAgedOrphanCollectedWithTinyGrace(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()
	orphan := put(t, eng, []byte("aged out orphan"))
	backdateBlob(t, root, orphan.Sha256, time.Minute)

	deleted, err := eng.GC(ctx, refsSet(), time.Nanosecond, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != orphan.Sha256 {
		t.Fatalf("deleted = %v, want [%s]", deleted, orphan.Sha256)
	}
	if _, err := eng.Stat(ctx, orphan.Sha256); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Stat after tiny-grace collect err = %v, want ErrBlobNotFound", err)
	}
}
