package storage

import (
	"sync"
	"time"
)

// GC hold set — ADR-0031 mechanism B, the W-1 (pre-reference window) half of
// the M9 GC concurrency fix. The companion mechanism A (per-candidate delete
// recheck via GCMarker.Live) lives in gc.go; read GCSweep's godoc there for
// the combined happens-before argument.
//
// Lifecycle (architecture section 14.2 point 1):
//
//	acquire  — Session.Commit registers the sha256 BEFORE the blob becomes
//	           visible to a GC scan (before the disk rename / the S3 copy to
//	           the final key). Registration-before-visibility is the anchor
//	           the soundness proof depends on: a scan that can see the blob
//	           started after the rename, which started after the acquire.
//	release  — repo.Service calls Engine.ReleaseGCHold(sha256) after the
//	           metadata transaction referencing the blob has committed
//	           (wiring: T-256). Release is acceleration, never correctness:
//	           a missing release only delays collection.
//	TTL      — the backstop for owners that never release (crash between
//	           Commit and the metadata commit, a consumer without repo's
//	           release wiring). Past the TTL the hold stops protecting the
//	           sha, restoring collectability.
//
// Failure direction: every mishap (missed release, double acquire, crash)
// makes GC more conservative, never less. The one thing an owner MUST NOT do
// is release before its reference is durably committed — the set cannot tell
// a premature release from a finished one.
//
// The set is deliberately in-process state (ADR-0031: single-instance
// deployment is a section 9 precondition). Cross-process faces are handled
// outside the engine (serve.lock, T-256).
const (
	// DefaultGCHoldTTL is how long an unreleased hold protects a blob when
	// storage.gc_hold_ttl_seconds is not configured: long enough to dwarf any
	// Commit-to-metadata-commit latency (the release is expected within
	// milliseconds), short enough that crashed wiring cannot wedge
	// reclamation for a day.
	DefaultGCHoldTTL = 600 * time.Second
	// MinGCHoldTTL is the floor for a configured hold TTL. A TTL shorter
	// than the Commit-to-metadata window would silently re-open W-1, so
	// sub-floor values are clamped up instead of honored.
	MinGCHoldTTL = 60 * time.Second
)

// resolveGCHoldTTL normalizes a configured hold TTL: explicit non-positive
// values take the default (the same zero-value semantics grace uses — zero
// never means "off"), sub-floor values clamp up to MinGCHoldTTL.
func resolveGCHoldTTL(d time.Duration) time.Duration {
	switch {
	case d <= 0:
		return DefaultGCHoldTTL
	case d < MinGCHoldTTL:
		return MinGCHoldTTL
	default:
		return d
	}
}

// holdEntry is one sha's in-flight accounting.
type holdEntry struct {
	// count is the number of Commit calls that acquired this sha and have
	// not yet failed or been released. Refcounting matters for concurrent
	// uploads of identical content: with plain presence semantics, uploader
	// A's release would strip uploader B's protection while B's metadata
	// transaction is still in flight (B committed a dedup hit, A's reference
	// was deleted in between). Each acquire must be answered by exactly one
	// release — by repo's ReleaseGCHold on success, by the engine's failure
	// path on a failed Commit, or by TTL expiry if neither ever happens.
	count int
	// refreshed is the acquire time of the MOST RECENT registration and the
	// TTL basis. A held sha whose last acquire ages past the TTL stops
	// protecting: its owner crashed (or never had release wiring), and the
	// backstop must restore collectability rather than wedge the blob.
	refreshed time.Time
}

// holdSet is the engine-owned in-flight hold set. All methods are safe for
// concurrent use; none touch disk or network. held() lazily purges expired
// entries so the map cannot grow without bound under missing releases.
type holdSet struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]*holdEntry
}

// newHoldSet builds a hold set; ttl is normalized by resolveGCHoldTTL and a
// nil clock falls back to time.Now (tests inject Options.Now / Now).
func newHoldSet(ttl time.Duration, now func() time.Time) *holdSet {
	if now == nil {
		now = time.Now
	}
	return &holdSet{
		ttl:     resolveGCHoldTTL(ttl),
		now:     now,
		entries: make(map[string]*holdEntry),
	}
}

// acquire registers sha as in-flight (idempotent per Commit: a second acquire
// increments the refcount and refreshes the TTL basis).
func (h *holdSet) acquire(sha string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.entries[sha]
	if !ok {
		e = &holdEntry{}
		h.entries[sha] = e
	}
	e.count++
	e.refreshed = h.now()
}

// release decrements the in-flight count for sha, dropping the entry at zero.
// Unknown shas, double releases and post-TTL releases are no-ops: the set
// only ever needs to under-report protection, never over-report it.
func (h *holdSet) release(sha string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.entries[sha]
	if !ok || e.count <= 0 {
		return
	}
	e.count--
	if e.count == 0 {
		delete(h.entries, sha)
	}
}

// held reports whether sha is protected right now: at least one live
// registration whose most recent acquire is inside the TTL. Expired entries
// are purged on sight (the lazy TTL backstop).
func (h *holdSet) held(sha string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.entries[sha]
	if !ok {
		return false
	}
	if h.now().Sub(e.refreshed) >= h.ttl {
		delete(h.entries, sha)
		return false
	}
	return e.count > 0
}
