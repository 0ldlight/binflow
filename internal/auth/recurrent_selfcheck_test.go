// Recurrent-failure breaker vs server-internal self-checks (T-634). The
// live wire showed 401,403 where the reference-precise shape is
// 401,401,403 — NOT a per-request double count (the middleware →
// Authenticate → authenticateBasic chain counts exactly one failure per
// rejected request; pinned on the wire by internal/httpapi's exact-shape
// test) but a SEEDED counter: the boot-time default-password self-check
// walked Authenticate with admin:<default>, and on every secured
// instance that check is itself one counted failure before any request
// arrives. These tests pin both halves: the seeding mechanism (why
// Authenticate must not carry internal checks) and the ledger-bypassing
// seam (CheckLocalPassword) the self-check is required to ride.

package auth

import (
	"context"
	"errors"
	"testing"
)

// assertRecurrentFailures reads the ledger's failure count for username.
func assertRecurrentFailures(t *testing.T, s *Service, username string, want int) {
	t.Helper()
	s.recurrent.mu.Lock()
	defer s.recurrent.mu.Unlock()
	if got := s.recurrent.entries[username].failures; got != want {
		t.Fatalf("ledger failures for %q = %d, want %d", username, got, want)
	}
}

// TestRecurrentSelfCheckSeedsTheCounter reproduces the live-wire
// divergence end to end: ONE internal self-check with a rejected
// credential leaves failures=1 on a fresh ledger, so the FIRST real bad
// wire attempt is failure #2 — it arms the block, and the observable
// shape reads 401 (self-check's own class), 401, 403: one attempt ahead
// of the reference-precise 401,401,403. This is the mechanism that made
// the boot-time default-password check poisonous on secured instances,
// and the regression guard for why that check must ride
// CheckLocalPassword instead of Authenticate.
func TestRecurrentSelfCheckSeedsTheCounter(t *testing.T) {
	f := newRecurrentFixture(t)
	// The boot-check shape: one internal authentication presenting the
	// documented default credential to a user whose real password is not
	// it (the secured-instance posture).
	if err := f.attempt(t, recurrentUser, "boot-default-pw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("self-check: want ErrInvalidCredentials, got %v", err)
	}
	assertRecurrentFailures(t, f.svc, recurrentUser, 1)

	// The first REAL bad wire attempt is now failure #2: it arms the
	// block where the reference-precise shape needs a second attempt.
	if err := f.attempt(t, recurrentUser, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("first wire attempt: want ErrInvalidCredentials, got %v", err)
	}
	if wait := f.svc.recurrent.blockedRemaining(recurrentUser); wait <= 0 {
		t.Fatal("block not armed after the seeded counter plus one wire failure")
	}
	wantBlocked(t, f.attempt(t, recurrentUser, "wrong"),
		"This request is blocked due to recurrent request failures, please try again in 4 second(s)")
}

// TestCheckLocalPasswordNeverTouchesLedger pins the seam's contract:
// identical verdicts to the Basic arm, zero ledger interaction in every
// direction — no failure count on a rejection, no counter row (not even
// an empty one) on a success, no BlockedError behind an armed block, and
// no implicit clear of a block that earned itself.
func TestCheckLocalPasswordNeverTouchesLedger(t *testing.T) {
	f := newRecurrentFixture(t)
	ctx := context.Background()

	// A rejected credential: the plain invalid-credentials verdict, no
	// failure counted.
	if _, err := f.svc.CheckLocalPassword(ctx, recurrentUser, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: want ErrInvalidCredentials, got %v", err)
	}
	assertRecurrentFailures(t, f.svc, recurrentUser, 0)

	// The right credential: success, and still no ledger row.
	p, err := f.svc.CheckLocalPassword(ctx, recurrentUser, recurrentUserPW)
	if err != nil {
		t.Fatalf("right password: unexpected error %v", err)
	}
	if p == nil || p.Name != recurrentUser {
		t.Fatalf("right password: want the %q principal, got %+v", recurrentUser, p)
	}
	assertRecurrentFailures(t, f.svc, recurrentUser, 0)
	if wait := f.svc.recurrent.blockedRemaining(recurrentUser); wait != 0 {
		t.Fatalf("CheckLocalPassword armed the ledger: %v", wait)
	}

	// An unknown username: the same credential verdict, nothing tracked
	// (the empty username and unknown principals get no ledger entries).
	if _, err := f.svc.CheckLocalPassword(ctx, "no-such-user", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: want ErrInvalidCredentials, got %v", err)
	}
	f.svc.recurrent.mu.Lock()
	_, tracked := f.svc.recurrent.entries["no-such-user"]
	f.svc.recurrent.mu.Unlock()
	if tracked {
		t.Fatal("unknown user left a ledger entry behind")
	}

	// While the block is ARMED the self-check is neither refused by it
	// nor allowed to lift it: the verdict is the credential's own, and
	// the armed window with its untouched count survives.
	for i := 0; i < recurrentFailureThreshold; i++ {
		_ = f.attempt(t, recurrentUser, "wrong")
	}
	wantBlocked(t, f.attempt(t, recurrentUser, "wrong"),
		"This request is blocked due to recurrent request failures, please try again in 4 second(s)")
	if p, err := f.svc.CheckLocalPassword(ctx, recurrentUser, recurrentUserPW); err != nil || p == nil {
		t.Fatalf("armed block leaked into the self-check: p=%v err=%v", p, err)
	}
	if wait := f.svc.recurrent.blockedRemaining(recurrentUser); wait <= 0 {
		t.Fatal("CheckLocalPassword lifted an armed block")
	}
	// A WRONG credential behind the armed block: the plain rejection —
	// never the 403 verdict — and the ledger still untouched (the arming
	// failure() reset the counter; nothing since may have bumped it).
	if _, err := f.svc.CheckLocalPassword(ctx, recurrentUser, "wrong"); !errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrRecurrentFailureBlocked) {
		t.Fatalf("armed-block wrong check: want plain ErrInvalidCredentials, got %v", err)
	}
	assertRecurrentFailures(t, f.svc, recurrentUser, 0)
}
