// Argon2 concurrency gate (T-192 / T-172 defect D-1).
//
// Every password check re-derives an argon2id tag at the M1 parameters of
// record (m=64 MiB, t=1, p=4), so each in-flight derivation costs ~64 MiB
// of transient heap plus p lanes of CPU. Before this gate a burst of
// Basic-auth requests started every derivation at once: T-172's D-1 repro
// (1000 parallel curls, most disconnecting mid-flight) drove the process
// to 337% CPU and 4.8 GB RSS with ~800 sockets parked on derivations no
// client was still waiting for, starving anonymous traffic for 10+ minutes
// — and the queued goroutines never noticed the disconnects because the
// computation did not observe the request context.
//
// The gate bounds how many derivations run at once. Queued waiters hold
// only a goroutine stack and abandon the queue the moment their context
// is canceled (net/http cancels the request context as soon as the client
// disconnects — T-41's primary disconnect leg), which is what lets a
// disconnect storm drain instead of piling up.
//
// The default limit is GOMAXPROCS clamped to [1, maxHashConcurrency]:
// argon2 is memory-bandwidth bound, so parallel derivations far beyond
// the clamp buy almost no throughput while multiplying the worst-case
// transient heap (limit x 64 MiB; the clamp keeps very wide hosts at
// ~1 GiB instead of GOMAXPROCS x 64 MiB). Operators override per service
// with WithHashConcurrency — since T-204 the auth.hash_concurrency config
// key feeds it at the cmd assembly point (0/unset keeps the derived
// default, so an unconfigured boot changes nothing).

package auth

import (
	"context"
	"fmt"
	"runtime"
)

// maxHashConcurrency caps the GOMAXPROCS-derived default gate limit. 16
// concurrent 64 MiB derivations peak at ~1 GiB of transient heap, which is
// the memory budget the default is tuned to hold on wide hosts.
const maxHashConcurrency = 16

// defaultHashConcurrency is the gate limit a service gets when the
// operator did not configure one: GOMAXPROCS clamped to [1,
// maxHashConcurrency]. GOMAXPROCS is always >= 1; the floor keeps
// hand-built runtimes honest.
func defaultHashConcurrency() int {
	if n := runtime.GOMAXPROCS(0); n > maxHashConcurrency {
		return maxHashConcurrency
	} else if n >= 1 {
		return n
	}
	return 1
}

// hashGate is a counting semaphore bounding concurrent argon2 derivations.
// A buffered channel of slots implements it: sending takes a slot, the
// release closure receives it back. Waiters park in the runtime's channel
// queue (FIFO), so storm traffic cannot starve individual requests.
type hashGate struct {
	slots chan struct{}
}

// newHashGate builds a gate admitting limit concurrent derivations.
// limit < 1 is a caller bug and is clamped to 1 rather than risking an
// unbuffered (deadlocking) channel.
func newHashGate(limit int) *hashGate {
	if limit < 1 {
		limit = 1
	}
	return &hashGate{slots: make(chan struct{}, limit)}
}

// limit reports the configured concurrency bound.
func (g *hashGate) limit() int { return cap(g.slots) }

// acquire reserves one derivation slot, waiting while the gate is full.
// The returned release func must be called exactly once. A context that
// is already canceled fails fast (the pre-check keeps that outcome
// deterministic instead of racing a free slot); a context canceled while
// queued abandons the wait, so a disconnected client frees its queue
// position immediately. The error wraps the context's own error and
// deliberately does NOT satisfy ErrInvalidCredentials: a client that
// hung up is not a failed login, and the audit plane (T-187) must not
// count it as one.
func (g *hashGate) acquire(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("auth: acquiring hash slot (concurrency limit %d): %w", g.limit(), err)
	}
	select {
	case g.slots <- struct{}{}:
		return func() { <-g.slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("auth: acquiring hash slot (concurrency limit %d): %w", g.limit(), ctx.Err())
	}
}

// VerifyPassword runs the argon2id comparison inside the gate. A non-nil
// error means the caller's context ended while queued — the derivation
// never ran, so there is no verdict to report, only the abandonment.
//
// This is the ctx-aware, gate-bounded entry the adapter login paths call
// (T-204 / T-192 leftover 2): docker's /v2/token form-credential exchange
// and npm's couch login previously called the pure package function
// auth.VerifyPassword, which starts a full ~64 MiB derivation with no
// concurrency bound and no cancellation — the same defect class T-172's
// D-1 fixed for the Basic arm. Callers that already hold a username (they
// resolved the user row themselves) pass the stored PHC string here and
// get the service's single shared gate: every password entrance in the
// process bounds one combined limit.
//
// A gate error deliberately does NOT satisfy ErrInvalidCredentials (see
// acquire); callers render their uniform rejection for it, which lands on
// a socket the canceled client already hung up.
func (s *Service) VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	release, err := s.hashGate.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return s.hashVerify(ctx, password, encoded), nil
}

// hashPassword derives a fresh PHC string inside the same gate: hashing
// costs the same ~64 MiB per call as verification, so an account-creation
// burst deserves the identical bound.
func (s *Service) hashPassword(ctx context.Context, password string) (string, error) {
	release, err := s.hashGate.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return HashPassword(password)
}
