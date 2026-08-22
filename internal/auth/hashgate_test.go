// T-192 acceptance surface, gate internals: the concurrency bound, the
// queue-abandonment on context cancellation, and the error taxonomy of a
// gate rejection (transport abandonment, never a credential verdict).
// These tests live in package auth because they exercise the unexported
// hashGate and the hashVerify seam directly (same precedent as
// pathmatch_test.go).

package auth

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// acquireOrFail fails the test when acquire returns an error, so worker
// goroutines can share one verdict channel instead of t.Fatalf races.
func acquireOrFail(ctx context.Context, t *testing.T, g *hashGate) func() {
	t.Helper()
	release, err := g.acquire(ctx)
	if err != nil {
		t.Errorf("acquire: %v", err)
		return func() {}
	}
	return release
}

// Table: whatever the limit, no more than limit derivations ever run at
// once, and every waiter is eventually admitted (no starvation, no lost
// slots).
func TestHashGateBoundsConcurrency(t *testing.T) {
	tests := []struct {
		name    string
		limit   int
		workers int
	}{
		{"limit 1 serializes", 1, 8},
		{"limit 2", 2, 12},
		{"limit 4", 4, 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newHashGate(tt.limit)
			var (
				inflight  atomic.Int32
				maxSeen   atomic.Int32
				releaseWG sync.WaitGroup
			)
			releaseWG.Add(tt.workers)
			for i := 0; i < tt.workers; i++ {
				go func() {
					defer releaseWG.Done()
					release := acquireOrFail(context.Background(), t, g)
					defer release()
					cur := inflight.Add(1)
					for {
						old := maxSeen.Load()
						if cur <= old || maxSeen.CompareAndSwap(old, cur) {
							break
						}
					}
					time.Sleep(2 * time.Millisecond) // hold the slot like a derivation would
					inflight.Add(-1)
				}()
			}
			releaseWG.Wait()
			if got := int(maxSeen.Load()); got > tt.limit {
				t.Fatalf("observed %d concurrent derivations, limit is %d", got, tt.limit)
			}
			if got := int(maxSeen.Load()); got < 1 {
				t.Fatalf("no derivation ever ran (maxSeen=%d)", got)
			}
		})
	}
}

// A canceled waiter abandons the queue immediately (its slot request is
// gone), the holder is unaffected, and the next live waiter can still be
// admitted — the T-172 D-1 disconnect-storm behavior.
func TestHashGateCancelAbandonsQueueNotHolder(t *testing.T) {
	g := newHashGate(1)
	holderRelease, err := g.acquire(context.Background())
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	const waiters = 4
	ctxs := make([]context.CancelFunc, 0, waiters)
	errs := make([]error, waiters)
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ctxs = append(ctxs, cancel)
		go func(slot int) {
			defer wg.Done()
			release, aerr := g.acquire(ctx)
			if aerr == nil {
				release()
			}
			errs[slot] = aerr
		}(i)
	}

	time.Sleep(5 * time.Millisecond) // let the waiters park on the full gate
	for _, cancel := range ctxs {
		cancel()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled waiters did not abandon the queue within 2s")
	}
	for i, err := range errs {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter %d error = %v, want context.Canceled in chain", i, err)
		}
	}

	// The gate is still held by the original holder: a probe with a
	// deadline proves no canceled waiter leaked a slot.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer probeCancel()
	if _, err := g.acquire(probeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe acquire err = %v, want DeadlineExceeded (holder must still own the gate)", err)
	}

	// Releasing the holder admits a live waiter again.
	holderRelease()
	liveCtx, liveCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer liveCancel()
	release, err := g.acquire(liveCtx)
	if err != nil {
		t.Fatalf("live acquire after holder release: %v", err)
	}
	release()
}

// A pre-canceled context fails fast and deterministically — it must never
// consume a slot (the acquire pre-check exists exactly to remove the
// select race).
func TestHashGatePreCanceledContextFailsFast(t *testing.T) {
	g := newHashGate(4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 8; i++ {
		if _, err := g.acquire(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("acquire #%d err = %v, want context.Canceled", i, err)
		}
	}
	// All four slots must still be free.
	var releases []func()
	for i := 0; i < 4; i++ {
		release, err := g.acquire(context.Background())
		if err != nil {
			t.Fatalf("clean acquire #%d: %v", i, err)
		}
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}

// A gate rejection on the verify path is a transport-level abandonment:
// it must not satisfy ErrInvalidCredentials (the HTTP plane's uniform-401
// family and the T-187 audit plane both key off that sentinel), and it
// must carry the context error in its chain.
func TestVerifyPasswordGateErrorIsNotCredentialRejection(t *testing.T) {
	s := New(nil, nil, nil, false)
	calls := 0
	s.hashVerify = func(_ context.Context, _, _ string) bool {
		calls++
		return true
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok, err := s.VerifyPassword(ctx, "pw", "$argon2id$phc")
	if err == nil {
		t.Fatal("expected gate error for canceled context")
	}
	if errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("gate error must not satisfy ErrInvalidCredentials (it is not a login failure)")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("gate error = %v, want context.Canceled in chain", err)
	}
	if ok {
		t.Fatal("canceled verify must not report a match")
	}
	if calls != 0 {
		t.Fatalf("hashVerify ran %d times, want 0 (canceled before the gate)", calls)
	}

	// Happy path: the seam runs exactly once, inside the slot.
	got, err := s.VerifyPassword(context.Background(), "pw", "$argon2id$phc")
	if err != nil || !got {
		t.Fatalf("verifyPassword = (%v, %v), want (true, nil)", got, err)
	}
	if calls != 1 {
		t.Fatalf("hashVerify calls = %d, want 1", calls)
	}
}

// WithHashConcurrency installs the operator's bound; a non-positive bound
// is an assembly bug and fails fast at build time of the service graph.
func TestWithHashConcurrency(t *testing.T) {
	s := New(nil, nil, nil, false)
	if got := s.hashGate.limit(); got < 1 || got > maxHashConcurrency {
		t.Fatalf("default limit = %d, want in [1, %d]", got, maxHashConcurrency)
	}
	for _, limit := range []int{1, 3, 64} {
		got := s.WithHashConcurrency(limit).hashGate.limit()
		if got != limit {
			t.Fatalf("WithHashConcurrency(%d).limit = %d", limit, got)
		}
	}
	for _, bad := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("WithHashConcurrency(%d) must panic", bad)
				}
			}()
			_ = s.WithHashConcurrency(bad)
		}()
	}
}

// The default follows GOMAXPROCS up to the clamp that bounds the
// worst-case transient heap on very wide hosts.
func TestDefaultHashConcurrencyClamp(t *testing.T) {
	old := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(old)

	tests := []struct {
		procs, want int
	}{
		{1, 1},
		{4, 4},
		{maxHashConcurrency, maxHashConcurrency},
		{maxHashConcurrency * 8, maxHashConcurrency},
	}
	for _, tt := range tests {
		runtime.GOMAXPROCS(tt.procs)
		if got := defaultHashConcurrency(); got != tt.want {
			t.Fatalf("GOMAXPROCS=%d: defaultHashConcurrency = %d, want %d", tt.procs, got, tt.want)
		}
	}
}
