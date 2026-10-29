// Recurrent-failure breaker, precise count on the wire (T-634): with the
// measured threshold — 2 counted failures, the block binding the NEXT
// attempt (T-632's converged constant) — a FRESH ledger must produce the
// reference-exact shape 401, 401, 403 on three consecutive bad Basic
// credentials for the same username, every pre-block response
// byte-for-byte the historical uniform 401. This pins the per-request
// count at EXACTLY one failure per rejected request: any double count
// (or a seeded counter, the T-634 root cause — the boot-time
// default-password self-check riding Authenticate instead of
// CheckLocalPassword) moves the 403 one attempt earlier.

package httpapi_test

import (
	"net/http"
	"testing"
)

func TestRecurrentBlockExactShape(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{blockedUser, blockedUserPW}})

	// Attempts 1 and 2: the uniform 401, byte-for-byte, challenge
	// included — the block is not armed yet, so NOTHING may differ from
	// the historical rejection here.
	for i := 1; i <= 2; i++ {
		r := h.do(http.MethodGet, "/binflow/api/system/ping", blockedUser, "wrong", nil, nil)
		body := bodyOf(t, r)
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d (%s), want the uniform 401", i, r.StatusCode, body)
		}
		if want := blockedEnvelope(http.StatusUnauthorized, "invalid credentials"); body != want {
			t.Fatalf("attempt %d body drift:\n got: %q\nwant: %q", i, body, want)
		}
		if ch := r.Header.Get("WWW-Authenticate"); ch != `Basic realm="BinFlow Realm"` {
			t.Fatalf("attempt %d WWW-Authenticate = %q, want the Basic challenge", i, ch)
		}
	}

	// Attempt 3: the FIRST blocked one — exactly two counted failures,
	// the block binds from here. A 403 on attempt 2 (the live-wire
	// divergence) means something counted twice or carried a stale
	// counter into a fresh ledger.
	blocked := h.do(http.MethodGet, "/binflow/api/system/ping", blockedUser, "wrong", nil, nil)
	body := bodyOf(t, blocked)
	if blocked.StatusCode != http.StatusForbidden {
		t.Fatalf("attempt 3: status = %d (%s), want the first blocked 403", blocked.StatusCode, body)
	}
	if want := blockedEnvelope(http.StatusForbidden, blockedWireText4); body != want {
		t.Fatalf("attempt 3 body drift:\n got: %q\nwant: %q", body, want)
	}
	assertBlockedWireShape(t, blocked, true)
}
