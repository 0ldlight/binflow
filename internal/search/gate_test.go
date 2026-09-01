package search

import (
	"sync"
	"sync/atomic"
	"testing"
)

// T-413: the K63 admission gate in isolation — fixed slots, non-blocking
// acquire, no queueing (ADR-0043 pt 5).

func TestGateTryAcquireUntilFull(t *testing.T) {
	g := newGate(maxConcurrent)
	for i := 0; i < maxConcurrent; i++ {
		if !g.tryAcquire() {
			t.Fatalf("acquire %d of %d refused on an empty gate", i+1, maxConcurrent)
		}
	}
	if g.tryAcquire() {
		t.Fatalf("acquire %d succeeded on a full gate (must reject without queueing)", maxConcurrent+1)
	}
	g.release()
	if !g.tryAcquire() {
		t.Fatal("acquire after release refused")
	}
}

// TestGateConcurrentHammer pins the pairing under race: many goroutines
// acquire and release concurrently, and once they all drain the gate must
// admit the full width again — a leaked slot would refuse the last acquire.
func TestGateConcurrentHammer(t *testing.T) {
	g := newGate(maxConcurrent)
	var acquired atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 64; j++ {
				if g.tryAcquire() {
					acquired.Add(1)
					g.release()
				}
			}
		}()
	}
	wg.Wait()
	if acquired.Load() == 0 {
		t.Fatal("the hammer never acquired a slot")
	}
	// Drained gate admits the full width again.
	for i := 0; i < maxConcurrent; i++ {
		if !g.tryAcquire() {
			t.Fatalf("drained gate refused acquire %d — a slot leaked", i+1)
		}
	}
}
