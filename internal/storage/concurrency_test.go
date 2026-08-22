package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrentSameBlobConverges drives N sessions with identical content
// through Commit at once. The per-checksum singleflight must collapse them
// into one physical write; every caller still gets a correct BlobRef.
func TestConcurrentSameBlobConverges(t *testing.T) {
	for _, n := range []int{2, 10, 32} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			root := t.TempDir()
			eng := newEngineAt(t, root, Options{})
			content := []byte(strings.Repeat("converge-", 1000))
			want := fmt.Sprintf("%x", sha256.Sum256(content))

			refs := make([]BlobRef, n)
			errs := make([]error, n)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					s, err := eng.BeginSession(context.Background())
					if err != nil {
						errs[i] = err
						return
					}
					if _, err := s.Append(context.Background(), bytes.NewReader(content)); err != nil {
						errs[i] = err
						return
					}
					<-start // maximize Commit overlap
					refs[i], errs[i] = s.Commit(context.Background(), BlobRef{})
				}(i)
			}
			close(start)
			wg.Wait()

			for i := 0; i < n; i++ {
				if errs[i] != nil {
					t.Fatalf("goroutine %d: %v", i, errs[i])
				}
				if refs[i].Sha256 != want {
					t.Fatalf("goroutine %d ref = %s, want %s", i, refs[i].Sha256, want)
				}
			}
			if got := countBlobs(t, root); got != 1 {
				t.Fatalf("physical blobs = %d, want 1 (singleflight convergence)", got)
			}
			if got := countSessionDirs(t, root); got != 0 {
				t.Fatalf("session dirs = %d, want 0", got)
			}
		})
	}
}

// TestConcurrentDistinctBlobs checks shard-level parallelism: different
// checksums must not serialize behind each other or corrupt the store.
func TestConcurrentDistinctBlobs(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	const n = 24
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			content := []byte(fmt.Sprintf("distinct blob #%d %s", i, strings.Repeat("x", i*13)))
			s, err := eng.BeginSession(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			if _, err := s.Append(context.Background(), bytes.NewReader(content)); err != nil {
				errCh <- err
				return
			}
			ref, err := s.Commit(context.Background(), BlobRef{})
			if err != nil {
				errCh <- err
				return
			}
			// Verify through Open right away: a visible blob must be complete.
			f, _, err := eng.Open(context.Background(), ref.Sha256)
			if err != nil {
				errCh <- err
				return
			}
			got, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil {
				errCh <- err
				return
			}
			if !bytes.Equal(got, content) {
				errCh <- fmt.Errorf("blob %d content mismatch (%d vs %d bytes)", i, len(got), len(content))
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if got := countBlobs(t, root); got != n {
		t.Fatalf("physical blobs = %d, want %d", got, n)
	}
}

// TestSingleflightErrorSharedToWaiters pins the waiter-path contract: when
// the leader's physical publish fails, every waiter observes the same error
// (so no caller mistakes a failed publish for success).
func TestSingleflightErrorSharedToWaiters(t *testing.T) {
	var g singleflight
	sentinel := errors.New("leader failure")
	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = g.do("k", func() error { return sentinel })
		}(i)
	}
	wg.Wait()
	leaders, failures := 0, 0
	for _, err := range errs {
		switch {
		case errors.Is(err, sentinel):
			failures++
		case err == nil:
			leaders++
		}
	}
	if failures != n || leaders != 0 {
		t.Fatalf("leaders=%d failures=%d, want all %d to observe the leader error", leaders, failures, n)
	}
}

// TestSingleflightExecutesOnce pins collapse-while-in-flight: when fn holds
// the key open until every caller has arrived, exactly one execution serves
// all of them.
//
// Deadlock history (T-54 run-2): the original form had the leader's fn DRAIN
// the arrival channel, relying on every caller joining the first flight. But
// singleflight only collapses callers that are already INSIDE do() — a
// goroutine descheduled between its arrival send and its do() call misses the
// flight, becomes a second leader, and its fn then blocked forever draining a
// channel nobody would ever send to again (40-minute package hang under
// -race load). The rewrite keeps the leader's flight open via an explicit
// release from the test body instead: arrivals are counted by the test (all n
// announce BEFORE any do() runs), the gate then opens simultaneously for
// everyone, a generous join window follows while the flight is open, and only
// then does the leader finish. No channel drain inside fn can ever block on
// missing senders, so the test can hang no more; the assertion keeps the
// original intent (one execution serves the contended flight).
func TestSingleflightExecutesOnce(t *testing.T) {
	var g singleflight
	var calls atomic.Int64
	const n = 64
	arrived := make(chan struct{}, n) // announcement happens before any do()
	gate := make(chan struct{})       // opens do() for everyone at once
	release := make(chan struct{})    // the leader's fn holds until told
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			arrived <- struct{}{}
			<-gate
			_ = g.do("same-key", func() error {
				calls.Add(1)
				<-release
				return nil
			})
		}()
	}
	// All n have announced (buffered), so none of them is still before its
	// announcement; open the gate and give the whole crowd the same instant
	// to enter do() while the flight is provably still open (release pending).
	for i := 0; i < n; i++ {
		<-arrived
	}
	close(gate)
	time.Sleep(100 * time.Millisecond) // join window: do() entry under the open flight
	close(release)
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("fn executed %d times under contention, want 1", got)
	}
}

// TestCrashWindowsSimulated walks every crash point of the commit protocol
// and asserts the two invariants that matter (ADR-0006):
//
//	a) no half-written blob is ever visible under blobs/
//	b) residue is confined to uploads/ + upload_sessions rows, which the next
//	   Open sweeps once expired
func TestCrashWindowsSimulated(t *testing.T) {
	content := []byte("payload that would have been committed")
	want := fmt.Sprintf("%x", sha256.Sum256(content))

	crashPoints := []struct {
		name string
		// leave simulates the process dying at a specific protocol step by
		// leaving on-disk + row state as it would be at that instant.
		leave func(t *testing.T, root string, store *memUploadSessions)
	}{
		{
			name: "mid-append",
			leave: func(t *testing.T, root string, store *memUploadSessions) {
				id := "00000000-0000-4000-8000-000000000001"
				dir := filepath.Join(root, "uploads", id)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "data"), content[:5], 0o644); err != nil {
					t.Fatal(err)
				}
				seedSessionRow(t, store, id, content[:5])
			},
		},
		{
			name: "after-fsync-before-rename",
			leave: func(t *testing.T, root string, store *memUploadSessions) {
				id := "00000000-0000-4000-8000-000000000002"
				dir := filepath.Join(root, "uploads", id)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "data"), content, 0o644); err != nil { // fully written, not yet renamed
					t.Fatal(err)
				}
				seedSessionRow(t, store, id, content)
			},
		},
		{
			name: "after-rename-before-metadata",
			leave: func(t *testing.T, root string, store *memUploadSessions) {
				p := filepath.Join(root, "blobs", want[:2], want)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, content, 0o644); err != nil {
					t.Fatal(err)
				}
				// A stray unrelated session the crash also leaves behind.
				id := "00000000-0000-4000-8000-000000000003"
				dir := filepath.Join(root, "uploads", id)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "data"), []byte("half"), 0o644); err != nil {
					t.Fatal(err)
				}
				seedSessionRow(t, store, id, []byte("half"))
			},
		},
	}

	for _, cp := range crashPoints {
		t.Run(cp.name, func(t *testing.T) {
			root := t.TempDir()
			store := newMemUploadSessions()
			cp.leave(t, root, store)
			// Simulate the crash residue having aged past the session TTL:
			// a fresh crash leaves young sessions (kept), time expires them.
			expireAllRows(t, store, DefaultSessionTTL+time.Hour)

			// Reopen (simulated restart): must succeed and sweep the residue.
			eng, err := OpenEngine(root, Options{Sessions: store})
			if err != nil {
				t.Fatalf("reopen after crash: %v", err)
			}
			defer eng.Close() //nolint:errcheck // test

			// Invariant b: residue only ever lived under uploads/, now swept.
			if n := countSessionDirs(t, root); n != 0 {
				t.Fatalf("session residue after restart sweep = %d, want 0", n)
			}
			if n := store.countRows(); n != 0 {
				t.Fatalf("session rows after restart sweep = %d, want 0", n)
			}

			// Invariant a: whatever exists under blobs/ is a complete blob.
			if n := countBlobs(t, root); n > 1 {
				t.Fatalf("blobs on disk = %d, want <= 1", n)
			}
			if n := countBlobs(t, root); n == 1 {
				st, err := eng.Stat(context.Background(), want)
				if err != nil {
					t.Fatalf("post-crash blob fails Stat: %v", err)
				}
				if st.Size != int64(len(content)) {
					t.Fatalf("post-crash blob size = %d, want %d", st.Size, len(content))
				}
			}
			// And a fresh identical upload still converges to one blob.
			ref := put(t, eng, content)
			if ref.Sha256 != want || countBlobs(t, root) != 1 {
				t.Fatalf("post-recovery upload: ref=%s blobs=%d", ref.Sha256, countBlobs(t, root))
			}
		})
	}
}
