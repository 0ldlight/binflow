// T-192 acceptance surface, behavior side (T-172 defect D-1 repro
// translated to tests): an authentication storm must queue behind the
// argon2 gate, disconnected clients must abandon their queue slots, the
// anonymous path must never wait behind the gate, and the transient heap
// the storm allocated must be reclaimable (HeapInuse falls after GC, no
// goroutine leak). Real argon2 runs throughout — at a reduced m=16 MiB
// for the storm tests (the PHC string carries its parameters, so
// verification honestly re-derives at them) and at the full 64 MiB
// parameters of record for the memory-recovery test.

package auth_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"

	"golang.org/x/crypto/argon2"
)

// stormHashParams keeps storm tests fast: real argon2id, m=16 MiB (spec
// floor m >= 8*p holds), t=1, p=1. Verification reads the parameters from
// the PHC string, so this exercises the true verify path end to end.
const (
	stormHashKiB = 16 * 1024
	stormUser    = "storm-bot"
	stormPW      = "storm-pw" //nolint:gosec // throwaway test fixture password
	stormN       = 120
)

// stormHash derives a PHC string at the reduced storm parameters.
func stormHash(t *testing.T, password string) string {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("salt: %v", err)
	}
	tag := argon2.IDKey([]byte(password), salt, 1, stormHashKiB, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		stormHashKiB, 1, 1,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(tag))
}

// seedStormUser creates a local user whose stored hash carries the
// reduced storm parameters (the admin seed keeps the 64 MiB parameters).
func seedStormUser(t *testing.T, st metadata.Store, admin bool) {
	t.Helper()
	now := metadata.Now()
	err := st.Users().Create(context.Background(), &metadata.User{
		Username: stormUser, PasswordHash: stormHash(t, stormPW),
		IsAdmin: admin, Enabled: true, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed storm user: %v", err)
	}
}

// percentile returns the p-th percentile of an ascending-sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p/100*float64(len(sorted)-1) + 0.5)
	return sorted[idx]
}

// D-1 repro, auth layer: a Basic storm queues behind the gate, clients
// that disconnect while queued abandon their slots (context.Canceled —
// and never a credential rejection), and anonymous authentication never
// waits behind the gate.
func TestT192BasicStormQueuesAndDisconnectsAbandon(t *testing.T) {
	f := newFixture(t, true)
	seedStormUser(t, f.st, false)
	svc := f.svc.WithHashConcurrency(1)

	const disconnect = 60 // requests whose context dies 3ms in
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		canceled   int
		succeeded  int
		rejected   int
		watchdogCh = make(chan struct{})
	)
	wg.Add(stormN)
	for i := 0; i < stormN; i++ {
		go func(slot int) {
			defer wg.Done()
			ctx := context.Background()
			var cancel context.CancelFunc = func() {}
			if slot < disconnect {
				ctx, cancel = context.WithCancel(ctx)
				time.AfterFunc(3*time.Millisecond, cancel)
			}
			p, err := svc.Authenticate(ctx, req(basic(stormUser, stormPW), ""))
			cancel()
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && p != nil && p.Name == stormUser:
				succeeded++
			case errors.Is(err, context.Canceled):
				canceled++ // abandoned the queue: not a login failure
			default:
				rejected++
			}
		}(i)
	}
	go func() { wg.Wait(); close(watchdogCh) }()
	select {
	case <-watchdogCh:
	case <-time.After(30 * time.Second):
		t.Fatal("storm did not drain within 30s (queue slots are not released)")
	}

	mu.Lock()
	defer mu.Unlock()
	if rejected != 0 {
		t.Fatalf("%d storm requests were rejected as credential failures; disconnects must abandon, not reject", rejected)
	}
	if succeeded+canceled != stormN {
		t.Fatalf("succeeded=%d canceled=%d, want every request accounted for out of %d", succeeded, canceled, stormN)
	}
	if succeeded < stormN-disconnect {
		t.Fatalf("succeeded = %d, want >= %d (every live request must eventually authenticate)", succeeded, stormN-disconnect)
	}
	if canceled == 0 {
		t.Fatal("no request observed a queue abandonment; the disconnect window never overlapped the queue")
	}
	t.Logf("storm of %d (gate=1): %d completed, %d abandoned queue slots mid-wait", stormN, succeeded, canceled)
}

// The anonymous arm must stay fast while the gate is saturated: it never
// touches the argon2 path, so its latency is dispatch-only even under a
// full storm queue.
func TestT192AnonymousFastUnderSaturatedGate(t *testing.T) {
	f := newFixture(t, true)
	seedStormUser(t, f.st, false)
	svc := f.svc.WithHashConcurrency(1)

	stormDone := make(chan struct{})
	go func() {
		defer close(stormDone)
		var wg sync.WaitGroup
		for i := 0; i < stormN; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = svc.Authenticate(context.Background(), req(basic(stormUser, stormPW), ""))
			}()
		}
		wg.Wait()
	}()

	latencies := make([]time.Duration, 0, 60)
	deadline := time.After(10 * time.Second)
	for i := 0; i < 60; i++ {
		select {
		case <-stormDone:
			// The storm drained faster than the probe loop; remaining
			// probes still assert the anonymous path, just without load.
		default:
		}
		start := time.Now()
		p, err := svc.Authenticate(context.Background(), req("", ""))
		if err != nil {
			t.Fatalf("anonymous authenticate: %v", err)
		}
		if p != nil {
			t.Fatal("anonymous authenticate resolved a principal")
		}
		latencies = append(latencies, time.Since(start))
		select {
		case <-deadline:
			t.Fatal("anonymous probes starved for 10s behind the auth storm")
		case <-time.After(2 * time.Millisecond):
		}
	}
	<-stormDone

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p99 := percentile(latencies, 99)
	t.Logf("anonymous under storm: p50=%s p99=%s max=%s (n=%d)",
		percentile(latencies, 50), p99, latencies[len(latencies)-1], len(latencies))
	if p99 > 500*time.Millisecond {
		t.Fatalf("anonymous p99 = %s under a gate=1 storm, want < 500ms", p99)
	}
}

// AC 2: the storm's transient heap is reclaimable. Runs the REAL 64 MiB
// parameters of record (the fixture's admin seed) through a gate of 2,
// samples HeapInuse during the storm, and verifies the post-GC floor
// collapses back. Go cannot be forced to hand pages back to the OS, so
// the assertion is on HeapInuse (reclaimable heap) plus the goroutine
// count returning to baseline; RSS semantics are documented in the
// ticket report instead.
func TestT192HashStormHeapRecoversNoLeak(t *testing.T) {
	f := newFixture(t, false)
	svc := f.svc.WithHashConcurrency(2)

	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	goroutineBaseline := runtime.NumGoroutine()

	var (
		peakMu sync.Mutex
		peak   uint64
		stop   = make(chan struct{})
		sample = func() {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			peakMu.Lock()
			if ms.HeapInuse > peak {
				peak = ms.HeapInuse
			}
			peakMu.Unlock()
		}
	)
	sample()
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				sample()
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := svc.Authenticate(context.Background(), req(basic("admin", adminPW), ""))
			if err != nil || p == nil || p.Name != "admin" {
				t.Errorf("admin verify under gate: (%v, %v)", p, err)
			}
		}()
	}
	wg.Wait()
	close(stop)
	time.Sleep(10 * time.Millisecond) // let the sampler take a final look

	runtime.GC()
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	var post runtime.MemStats
	runtime.ReadMemStats(&post)

	peakMu.Lock()
	peakHeap := peak
	peakMu.Unlock()
	postHeap := post.HeapInuse
	t.Logf("argon2 storm (gate=2, m=64MiB x12): HeapInuse peak=%dMiB post-GC=%dMiB (returned %dMiB)",
		peakHeap>>20, postHeap>>20, (peakHeap-postHeap)>>20)

	if peakHeap < postHeap+48<<20 {
		t.Fatalf("storm peak HeapInuse (%dMiB) shows no reclaimable argon2 heap over post-GC (%dMiB)",
			peakHeap>>20, postHeap>>20)
	}
	if postHeap >= peakHeap/2 {
		t.Fatalf("post-GC HeapInuse = %dMiB, did not fall below half of peak %dMiB", postHeap>>20, peakHeap>>20)
	}

	// No leaked goroutines: the storm's waiters all returned.
	time.Sleep(100 * time.Millisecond)
	if got := runtime.NumGoroutine(); got > goroutineBaseline+2 {
		t.Fatalf("goroutines after storm = %d, baseline %d (leaked waiters)", got, goroutineBaseline)
	}
}

// The full-cost verify path still works through the gate at the default
// limit (a smoke of the production configuration, not a timing claim).
func TestT192DefaultGateVerifiesProductionParams(t *testing.T) {
	f := newFixture(t, true)
	p, err := f.svc.Authenticate(context.Background(), req(basic("admin", adminPW), ""))
	if err != nil || p == nil || p.Name != "admin" || p.Admin != true {
		t.Fatalf("default-gate admin verify = (%v, %v)", p, err)
	}
	if _, err := f.svc.Authenticate(context.Background(), req(basic("admin", "wrong"), "")); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password err = %v, want ErrInvalidCredentials", err)
	}
}
