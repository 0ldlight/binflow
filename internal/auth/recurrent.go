// Recurrent-failure circuit breaker (T-632).
//
// Differential evidence: against the reference instance, repeated bad
// Basic credentials for the SAME username eventually stop answering the
// uniform 401 and start answering a 403 countdown block — verbatim
// "This request is blocked due to recurrent request failures, please
// try again in 4 second(s)" — which self-heals once the probe stops
// (live wire, L020-3 V5 + the T-632 first-wave probe; registered as
// known-divergence rest/login-failure-rate-limit / pending-rulings
// R-22c, ruling: align via our own shape).
//
// Confidence note (deliberate, do not delete): the message TEXT above is
// verbatim from the observed wire (including the "(s)" plural spelling).
// The THRESHOLD and the WINDOW, however, are single-episode inferences —
// the probes observed one block arm and one cooldown, not the parameter
// space — so the constants below are conservative choices annotated as
// such, pinned as code constants (no config surface until a second
// reference window confirms the parameters).
//
// State is in-process memory only (the stepUpLedger precedent, ADR-0027
// decision 4): single-instance deployment is the stated fact and a
// restart empties every counter — deliberately no persistence, no schema
// change. Concurrent access is guarded by one mutex.

package auth

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Recurrent-failure breaker parameters. BOTH are inferences from the
// observed reference behavior (see the file comment); the message text
// is the only verbatim-aligned part.
const (
	// recurrentFailureThreshold is the number of consecutive rejected
	// Basic credentials after which the username's NEXT attempt is
	// blocked. The observed reference episode armed no later than the
	// 3rd consecutive failure; 5 is the conservative choice — a user
	// L043-wave2 precise count (9/9 reproducible, 2026-10-27): the
	// reference arms on the 3rd consecutive bad attempt, i.e. 2 counted
	// failures — threshold converged from the conservative 5 to the
	// measured 2 (window semantics unchanged).
	recurrentFailureThreshold = 2

	// recurrentFailureWindow is how long a block stays armed. 4s makes
	// a freshly armed block answer the observed wire text byte-for-byte
	// ("... in 4 second(s)"); the message countdown decays with wall
	// clock (the observed arm's countdown read lower as time passed).
	recurrentFailureWindow = 4 * time.Second

	// recurrentSweepSize is the entry count that triggers an
	// opportunistic sweep of lapsed entries. Live footprint is one row
	// per username under active probe; the sweep exists so a client
	// spraying DISTINCT usernames cannot grow the map without bound
	// between the natural per-read deletions.
	recurrentSweepSize = 4096
)

// recurrentBlockedText is the verbatim wire text of the reference's
// blocked response; %d is the remaining whole seconds. The "(s)"
// spelling is part of the observed text and must not be "corrected".
const recurrentBlockedText = "This request is blocked due to recurrent request failures, please try again in %d second(s)"

// ErrRecurrentFailureBlocked marks the recurrent-failure block verdict
// (T-632). The HTTP plane, once wired, must test this sentinel BEFORE
// ErrInvalidCredentials and answer 403 with the error's Error() text
// verbatim — the countdown message is the reference-aligned wire body,
// not a log line. Until that wiring lands, the error also chains
// ErrInvalidCredentials (see BlockedError.Unwrap), so the plane keeps
// answering the historical uniform 401 instead of a 500.
var ErrRecurrentFailureBlocked = errors.New("auth: username blocked after recurrent request failures")

// BlockedError is the recurrent-failure verdict for one username: the
// block window is armed and the request was refused before any
// credential verification ran (a blocked username costs no argon2
// derivation — that cheap rejection is part of the breaker's point).
type BlockedError struct {
	// Username is the blocked principal the presented credential named.
	Username string
	// RetryIn is the remaining block window at verdict time.
	RetryIn time.Duration

	message string // precomputed verbatim text; empty renders from RetryIn
}

// Error renders the verbatim reference text with the remaining whole
// seconds (ceiling — never name a shorter wait than the window holds).
func (e *BlockedError) Error() string {
	if e.message != "" {
		return e.message
	}
	return fmt.Sprintf(recurrentBlockedText, blockedSeconds(e.RetryIn))
}

// Unwrap chains BOTH sentinels: ErrRecurrentFailureBlocked for the 403
// arm, and ErrInvalidCredentials so every consumer that only knows the
// uniform-401 posture (the pre-T-632 HTTP plane, the audit defaults)
// keeps its exact historical behavior. Wire consumers MUST match the
// blocked sentinel first — errors.Is alone cannot order the two.
func (e *BlockedError) Unwrap() []error {
	return []error{ErrRecurrentFailureBlocked, ErrInvalidCredentials}
}

// blockedSeconds renders a remaining window as the whole seconds the
// message names: the ceiling, floored at 1 so a sub-second remainder
// still reads as one second (a 0 would corrupt the sentence).
func blockedSeconds(d time.Duration) int {
	if d <= 0 {
		return 0 // unreachable through the armed path; callers gate on d > 0
	}
	return int((d + time.Second - 1) / time.Second)
}

// blockedMessage is the wire text for a remaining window.
func blockedMessage(remaining time.Duration) string {
	return fmt.Sprintf(recurrentBlockedText, blockedSeconds(remaining))
}

// recurrentEntry is one username's breaker state: the consecutive
// failure count and, once armed, the instant the block lapses. A zero
// blockedUntil means "never armed" — a pure failure counter.
type recurrentEntry struct {
	failures     int
	blockedUntil time.Time
}

// recurrentLimiter is the in-process breaker ledger. nil (hand-built
// services) renders the whole arm inert: no counting, no blocking —
// byte-for-byte the pre-T-632 posture. One instance is built by New and
// shared by every With* clone (struct pointer copy), so the ledger is
// process-wide like stepUpGrants.
type recurrentLimiter struct {
	mu      sync.Mutex
	entries map[string]recurrentEntry
	// now is the injectable clock (tests); nil = time.Now.
	now func() time.Time
}

func newRecurrentLimiter() *recurrentLimiter {
	return &recurrentLimiter{entries: make(map[string]recurrentEntry)}
}

// clock is the limiter's time source.
func (l *recurrentLimiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

// blockedRemaining reports the remaining block window for username
// (0 = not blocked). An entry whose window has lapsed is deleted on
// this read — the observed cooldown self-heal — so the next failure
// starts a FRESH count instead of resuming the stale one. The empty
// username is never tracked: there is no attributable principal.
func (l *recurrentLimiter) blockedRemaining(username string) time.Duration {
	if l == nil || username == "" {
		return 0
	}
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[username]
	if !ok || e.blockedUntil.IsZero() {
		return 0 // absent, or a pure failure counter: not blocked
	}
	if !now.Before(e.blockedUntil) {
		delete(l.entries, username)
		return 0
	}
	return e.blockedUntil.Sub(now)
}

// failure records one rejected Basic credential for username. Reaching
// the threshold arms the block window starting on THIS call — the
// tripping request itself already received its own uniform rejection,
// and the block binds from the next attempt on (the observed arm's
// shape). A failure arriving for an ALREADY-blocked username — a
// request that passed its block check before a concurrent arm — neither
// extends nor re-arms: the observed countdown decays with wall clock,
// not with continued probing.
func (l *recurrentLimiter) failure(username string) {
	if l == nil || username == "" {
		return
	}
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[username]
	if ok && now.Before(e.blockedUntil) {
		return // already armed: a racing late failure changes nothing
	}
	e.failures++
	if e.failures >= recurrentFailureThreshold {
		e.blockedUntil = now.Add(recurrentFailureWindow)
		e.failures = 0 // the block is armed; the counter restarts
	}
	l.entries[username] = e
	if len(l.entries) >= recurrentSweepSize {
		l.sweepLocked(now)
	}
}

// success clears username's failure count after a successful
// authentication through the same arm. Defensive posture (the reference
// arm was not observed for this case): a user who eventually presents
// the right credential must not stay one bad attempt away from a block
// they did not earn. An ACTIVE block is never lifted here — a success
// can only race past the block check (it verified before a concurrent
// arm), and the block was earned by the failures before it.
func (l *recurrentLimiter) success(username string) {
	if l == nil || username == "" {
		return
	}
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[username]
	if ok && (e.blockedUntil.IsZero() || !now.Before(e.blockedUntil)) {
		delete(l.entries, username) // a pure counter, or an already-lapsed block
	}
}

// sweepLocked bounds the map when it crosses recurrentSweepSize: lapsed
// blocks go first (they carry a deadline), then stale pure counters are
// evicted in map order. Evicting a counter FAILS OPEN — the evicted
// username simply needs a fresh full count to arm again — which is the
// safe direction for a memory bound: correctness for the username under
// active probe degrades to "one more threshold of evidence", never to a
// wrongful block. Caller holds mu.
func (l *recurrentLimiter) sweepLocked(now time.Time) {
	for u, e := range l.entries {
		if !e.blockedUntil.IsZero() && !now.Before(e.blockedUntil) {
			delete(l.entries, u)
		}
	}
	for u, e := range l.entries {
		if len(l.entries) < recurrentSweepSize {
			return
		}
		if e.blockedUntil.IsZero() {
			delete(l.entries, u)
		}
	}
}
