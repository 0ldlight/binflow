package search

// The K63 concurrency gate (T-413, FR-133.4 / ADR-0043 pt 5): a fixed-slot
// semaphore acquired WITHOUT waiting. A full gate answers ErrResourceBusy
// immediately — BinFlow deliberately simplifies the official 10s pending
// wait away (aql.md §5: "BinFlow 直接 429（不排队）"; an unbounded queue is
// an unbounded wait surface, the AqlTooManyRequests semantics). The slot is
// the engine's whole admission state: one acquire/release pair per Run,
// release on every exit path, never recursive.

// gate is the admission semaphore. A buffered channel of struct{} is the
// whole mechanism: sending is try-acquire (non-blocking), receiving is
// release.
type gate struct {
	slots chan struct{}
}

// newGate builds a gate with n concurrent slots.
func newGate(n int) *gate {
	return &gate{slots: make(chan struct{}, n)}
}

// tryAcquire takes one slot when the gate is not full, reporting whether it
// did. It never blocks and never queues.
func (g *gate) tryAcquire() bool {
	select {
	case g.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// release returns one slot. The default arm makes a double release a no-op
// instead of a deadlock — defensive only; Run's defer is the sole caller.
func (g *gate) release() {
	select {
	case <-g.slots:
	default:
	}
}
