// T-204 acceptance surface: the EXPORTED VerifyPassword entry adapters call
// (T-192 leftover 2) carries the full gate semantics — the concurrency bound
// and the queue abandonment on cancellation — not just the argon2
// comparison. These tests live in package auth because they swap the
// hashVerify derivation seam to observe concurrency without timing games
// (same precedent as hashgate_test.go).

package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// instrumentedVerify swaps the derivation seam for a probe that records how
// many derivations run at once and holds each slot briefly, the way a real
// argon2 derivation would.
func instrumentedVerify() (s *Service, inflight, maxSeen *atomic.Int32) {
	s = New(nil, nil, nil, false)
	inflight, maxSeen = &atomic.Int32{}, &atomic.Int32{}
	s.hashVerify = func(_ context.Context, _, _ string) bool {
		cur := inflight.Add(1)
		for {
			old := maxSeen.Load()
			if cur <= old || maxSeen.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		inflight.Add(-1)
		return true
	}
	return s, inflight, maxSeen
}

// The exported entry is bounded by the service's gate: whatever the storm
// size, no more than WithHashConcurrency(limit) derivations ever run at
// once, and every caller is eventually admitted with a verdict.
func TestT204ServiceVerifyPasswordGateBounded(t *testing.T) {
	tests := []struct {
		name    string
		limit   int
		callers int
	}{
		{"limit 1 serializes", 1, 8},
		{"limit 2", 2, 10},
		{"limit 4", 4, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, _, maxSeen := instrumentedVerify()
			svc := base.WithHashConcurrency(tt.limit)

			var wg sync.WaitGroup
			verdicts := make([]bool, tt.callers)
			errs := make([]error, tt.callers)
			wg.Add(tt.callers)
			for i := 0; i < tt.callers; i++ {
				go func(slot int) {
					defer wg.Done()
					verdicts[slot], errs[slot] = svc.VerifyPassword(
						context.Background(), "pw", "$argon2id$phc")
				}(i)
			}
			wg.Wait()

			if got := int(maxSeen.Load()); got > tt.limit {
				t.Fatalf("observed %d concurrent derivations through the exported entry, limit is %d",
					got, tt.limit)
			}
			for i := range verdicts {
				if errs[i] != nil {
					t.Fatalf("caller %d err = %v, want nil (live callers must be admitted)", i, errs[i])
				}
				if !verdicts[i] {
					t.Fatalf("caller %d verdict = false, want the seam's true", i)
				}
			}
		})
	}
}

// A caller whose context dies while queued abandons the exported entry with
// the context error in its chain — and never a credential verdict, exactly
// like the internal Basic-arm path (T-192's taxonomy: a hung-up client is
// not a failed login).
func TestT204ServiceVerifyPasswordAbandonsCanceledCaller(t *testing.T) {
	base, inflight, _ := instrumentedVerify()
	// Swap the seam to a blocking derivation BEFORE cloning/launching: the
	// holder must occupy the only slot until the test releases it.
	block := make(chan struct{})
	base.hashVerify = func(_ context.Context, _, _ string) bool {
		inflight.Add(1)
		<-block
		inflight.Add(-1)
		return true
	}
	svc := base.WithHashConcurrency(1)

	holderDone := make(chan error, 1)
	go func() {
		_, err := svc.VerifyPassword(context.Background(), "holder", "$argon2id$phc")
		holderDone <- err
	}()
	deadline := time.After(2 * time.Second)
	for inflight.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("holder never entered the derivation")
		case <-time.After(time.Millisecond):
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := svc.VerifyPassword(ctx, "waiter", "$argon2id$phc")
		waiterDone <- err
	}()
	time.Sleep(5 * time.Millisecond) // let the waiter park on the full gate
	cancel()

	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter err = %v, want context.Canceled in chain", err)
		}
		if errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("abandonment must not satisfy ErrInvalidCredentials (not a login failure)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled waiter did not abandon the queue within 2s")
	}

	// The holder is unaffected; releasing it completes its derivation.
	close(block)
	if err := <-holderDone; err != nil {
		t.Fatalf("holder err = %v, want nil", err)
	}
}

// A pre-canceled context fails fast without consuming a slot — the acquire
// pre-check, seen through the exported surface adapters call.
func TestT204ServiceVerifyPasswordPreCanceledFailsFast(t *testing.T) {
	base, _, _ := instrumentedVerify()
	calls := &atomic.Int32{}
	base.hashVerify = func(_ context.Context, _, _ string) bool {
		calls.Add(1)
		return true
	}
	svc := base.WithHashConcurrency(2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 4; i++ {
		ok, err := svc.VerifyPassword(ctx, "pw", "$argon2id$phc")
		if err == nil || ok {
			t.Fatalf("pre-canceled call #%d = (%v, %v), want (false, error)", i, ok, err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-canceled call #%d err = %v, want context.Canceled", i, err)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("derivation ran %d times for canceled contexts, want 0", got)
	}
	// Both slots must still be free for live callers.
	if _, err := svc.VerifyPassword(context.Background(), "pw", "$argon2id$phc"); err != nil {
		t.Fatalf("live caller after pre-canceled storm: %v", err)
	}
}
