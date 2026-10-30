// Recurrent-failure breaker, endpoint-precise shape across the management
// plane (T-636): the deployment live-wire saw 401,403,403 on
// /binflow/api/system/ping (block armed on the FIRST bad attempt) and
// 401,403 on /binflow/api/repositories, read as a per-request double
// count. The local real-binary reproduction (deployed-equivalent env:
// non-default BINFLOW_ADMIN_PASSWORD + anonymous access) shows the
// reference-exact 401,401,403 on BOTH endpoints from a fresh ledger, so
// no double count exists in the served code; these tests pin that
// conclusion at the wire level. Any regression that counts a rejected
// request twice — or seeds the ledger before the first request — moves
// the 403 one attempt earlier and fails these tables.

package httpapi_test

import (
	"net/http"
	"testing"
)

// TestRecurrentBlockShapePerEndpoint walks the two management endpoints
// the deployment probed, each against its OWN fresh harness (a fresh
// ledger — the deployed probe read one endpoint's shape independently).
// The threshold semantics (2 counted failures, the block binding the
// NEXT attempt) demand the identical shape on every route: 401, 401,
// 403, pre-block responses byte-for-byte the historical uniform 401,
// the blocked one the reference countdown form.
func TestRecurrentBlockShapePerEndpoint(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "system/ping", path: "/binflow/api/system/ping"},
		{name: "repositories", path: "/binflow/api/repositories"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarnessCfg(t, nil, [][2]string{{blockedUser, blockedUserPW}})
			for i := 1; i <= 2; i++ {
				r := h.do(http.MethodGet, tc.path, blockedUser, "wrong", nil, nil)
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
			blocked := h.do(http.MethodGet, tc.path, blockedUser, "wrong", nil, nil)
			body := bodyOf(t, blocked)
			if blocked.StatusCode != http.StatusForbidden {
				t.Fatalf("attempt 3: status = %d (%s), want the first blocked 403", blocked.StatusCode, body)
			}
			if want := blockedEnvelope(http.StatusForbidden, blockedWireText4); body != want {
				t.Fatalf("attempt 3 body drift:\n got: %q\nwant: %q", body, want)
			}
			assertBlockedWireShape(t, blocked, true)
		})
	}
}

// TestRecurrentBlockCountCrossesRoutes pins the ONE-ledger semantics the
// deployed cross-endpoint observation rests on: the failure count rides
// the username, not the route, so two bad attempts on ping plus one on
// repositories is the third counted failure — the repositories request
// takes the blocked 403. A per-route ledger (or a per-request double
// count anywhere on either route) breaks this: two routes × isolated
// ledgers would keep the third shot at 401, and any double count would
// trip it on shot 2.
func TestRecurrentBlockCountCrossesRoutes(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{blockedUser, blockedUserPW}})

	for i := 1; i <= 2; i++ {
		r := h.do(http.MethodGet, "/binflow/api/system/ping", blockedUser, "wrong", nil, nil)
		body := bodyOf(t, r)
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("ping attempt %d: status = %d (%s), want the uniform 401", i, r.StatusCode, body)
		}
	}
	r := h.do(http.MethodGet, "/binflow/api/repositories", blockedUser, "wrong", nil, nil)
	body := bodyOf(t, r)
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("repositories shot 3: status = %d (%s), want the blocked 403 carried across routes", r.StatusCode, body)
	}
	if want := blockedEnvelope(http.StatusForbidden, blockedWireText4); body != want {
		t.Fatalf("repositories shot 3 body drift:\n got: %q\nwant: %q", body, want)
	}
	assertBlockedWireShape(t, r, true)
}
