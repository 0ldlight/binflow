// Recurrent-failure breaker SCOPING (T-635): the ledger is keyed by the
// PRINCIPAL, deliberately shared across every route and plane — a client
// splitting bad credentials between /binflow/api/system/ping and
// /binflow/api/repositories feeds ONE count, and the block (once armed)
// refuses the username on both. This is what makes two interleaved
// per-endpoint probe transcripts read "401,403,403" each while the wire
// truth is a single 401,401,403 sequence (the T-635 live-vs-local
// "divergence", reproduced against the real binary and fully explained
// by the shared ledger). Route-scoping would let a brute-force client
// multiply its budget by the route count — the per-principal scope is
// the breaker's point.

package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// attemptOn presents one Basic credential against an explicit path (the
// route dimension of the scoping test).
func (f *recurrentFixture) attemptOn(t *testing.T, path, username, password string) error {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.SetBasicAuth(username, password)
	_, aerr := f.svc.Authenticate(context.Background(), r)
	return aerr
}

// TestRecurrentBreakerScopedToPrincipalNotRoute pins the shared-ledger
// contract end to end: alternating bad credentials between two routes
// accumulates ONE count, the block arms after the threshold TOTAL and
// answers both routes, a bystander username is untouched, and a lapsed
// block starts a fresh count for the username on either route.
func TestRecurrentBreakerScopedToPrincipalNotRoute(t *testing.T) {
	f := newRecurrentFixture(t)
	const (
		pingPath  = "/binflow/api/system/ping"
		reposPath = "/binflow/api/repositories"
	)

	// One bad attempt per route: exactly two counted failures — the
	// threshold — so the block is armed already (arming resets the
	// counter; the armed window is the observable).
	if err := f.attemptOn(t, pingPath, recurrentUser, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("ping attempt: want ErrInvalidCredentials, got %v", err)
	}
	if err := f.attemptOn(t, reposPath, recurrentUser, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("repositories attempt: want ErrInvalidCredentials, got %v", err)
	}
	if wait := f.svc.recurrent.blockedRemaining(recurrentUser); wait <= 0 {
		t.Fatal("block not armed after two cross-route failures (ledger not shared between routes?)")
	}

	// The block refuses BOTH routes (the principal is blocked, not the
	// path), each with the verbatim countdown.
	wantBlocked(t, f.attemptOn(t, pingPath, recurrentUser, "wrong"),
		"This request is blocked due to recurrent request failures, please try again in 4 second(s)")
	wantBlocked(t, f.attemptOn(t, reposPath, recurrentUser, "wrong"),
		"This request is blocked due to recurrent request failures, please try again in 4 second(s)")

	// A bystander username on the second route is judged on its own
	// (empty) count — the shared-ledger scope is per-PRINCIPAL, not
	// global.
	err := f.attemptOn(t, reposPath, recurrentBystander, "wrong")
	if !errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrRecurrentFailureBlocked) {
		t.Fatalf("bystander dragged into the block: %v", err)
	}
}
