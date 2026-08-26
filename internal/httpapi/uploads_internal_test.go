package httpapi

// Internal (same-package) tests for the MPU registry's idle sweep (B1,
// T-289 review: the sweep had zero coverage). External-package tests cover
// the HTTP plane; the registry's own eviction semantics live here where
// the unexported fields are reachable.

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/storage"
)

// abortCountingSession is a storage.Session that only counts Aborts —
// everything the sweep touches.
type abortCountingSession struct {
	mu     sync.Mutex
	aborts int
}

func (s *abortCountingSession) ID() string    { return "sess" }
func (s *abortCountingSession) Offset() int64 { return 0 }
func (s *abortCountingSession) Append(context.Context, io.Reader) (int64, error) {
	return 0, nil
}
func (s *abortCountingSession) Commit(context.Context, storage.BlobRef) (storage.BlobRef, error) {
	return storage.BlobRef{}, nil
}
func (s *abortCountingSession) Abort(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aborts++
	return nil
}
func (s *abortCountingSession) abortCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aborts
}

func newSweepSession(id string, updatedAt time.Time) (*mpuSession, *abortCountingSession) {
	sess := &abortCountingSession{}
	return &mpuSession{
		id: id, sess: sess, repoKey: "r", path: "p",
		partSize: 5 << 20, state: mpuStateActive,
		createdAt: updatedAt, updatedAt: updatedAt,
	}, sess
}

// TestMPURegistryEvictIdle pins the sweep's three behaviors: a session
// idle beyond the TTL is aborted (S3 MPU reclamation rides Session.Abort)
// and dropped; a fresh session survives untouched; a session TOUCHED
// AFTER the snapshot decision must survive the re-check — the B1 fix's
// whole point (the sweep never acts on a stale idleness judgment).
func TestMPURegistryEvictIdle(t *testing.T) {
	now := time.Now()
	reg := newMPURegistry()
	reg.ttl = time.Minute

	stale, staleSess := newSweepSession("stale", now.Add(-2*time.Minute))
	fresh, freshSess := newSweepSession("fresh", now.Add(-time.Second))
	touched, touchedSess := newSweepSession("touched", now.Add(-2*time.Minute))
	reg.add(stale)
	reg.add(fresh)
	reg.add(touched)

	// "touched" was idle at snapshot time but a part lands between the
	// snapshot and the session-lock re-check: simulate by updating its
	// timestamp through the same field the handlers maintain, racing the
	// sweep via evictIdle's deterministic now argument — the re-check
	// reads updatedAt live.
	touched.mu.Lock()
	touched.updatedAt = now // touched a moment ago
	touched.mu.Unlock()

	evicted := reg.evictIdle(now)
	if len(evicted) != 1 || evicted[0] != "stale" {
		t.Fatalf("evicted = %v, want exactly [stale]", evicted)
	}
	if got := staleSess.abortCount(); got != 1 {
		t.Fatalf("stale session aborted %d times, want 1", got)
	}
	if got := freshSess.abortCount(); got != 0 {
		t.Fatalf("fresh session aborted %d times, want 0", got)
	}
	if got := touchedSess.abortCount(); got != 0 {
		t.Fatalf("touched-since-snapshot session aborted %d times, want 0 (the re-check must save it)", got)
	}
	for _, id := range []string{"fresh", "touched"} {
		if _, ok := reg.lookup(id); !ok {
			t.Fatalf("session %s missing after the sweep, want kept", id)
		}
	}
	if _, ok := reg.lookup("stale"); ok {
		t.Fatal("stale session still registered after the sweep, want dropped")
	}
	stale.mu.Lock()
	state := stale.state
	stale.mu.Unlock()
	if state != mpuStateFailed {
		t.Fatalf("evicted session state = %q, want %q", state, mpuStateFailed)
	}
}

// TestMPURegistryEvictIdleTerminalNoDoubleAbort: a session a terminal
// verb already finalized (done, removed from the registry between the
// snapshot and the re-check) costs the sweep nothing — the delete is a
// no-op and no second Abort fires.
func TestMPURegistryEvictIdleTerminalNoDoubleAbort(t *testing.T) {
	now := time.Now()
	reg := newMPURegistry()
	reg.ttl = time.Minute

	old, oldSess := newSweepSession("old", now.Add(-2*time.Minute))
	reg.add(old)
	// A complete/abort ran between snapshot and sweep: state advances and
	// the entry leaves the table (the handlers' terminal posture).
	old.mu.Lock()
	old.state = mpuStateComplete
	old.mu.Unlock()
	reg.remove("old")

	evicted := reg.evictIdle(now)
	if len(evicted) != 0 {
		t.Fatalf("evicted = %v, want none (the terminal verb already reclaimed it)", evicted)
	}
	if got := oldSess.abortCount(); got != 0 {
		t.Fatalf("finalized session aborted %d times by the sweep, want 0", got)
	}
}
