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
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/httpapi"
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

// countingAuth wraps the stack's Authenticator and counts the
// Authenticate invocations the served chain makes. It exists to pin the
// T-636 closure invariant the response shapes alone cannot: the middleware
// is the ONLY Authenticate caller per wire request (router.go wires it
// once; /healthz, /readyz and /metrics ride disjoint chains), so N bad
// credentials must produce exactly N adjudications — the ledger's single
// mutation per adjudication (authenticateBasic) then yields 401,401,403.
// A regression that re-authenticates anywhere (a second middleware pass,
// a handler-side re-check) breaks the count BEFORE the shape can.
type countingAuth struct {
	auth.Authenticator
	calls atomic.Int64
}

func (c *countingAuth) Authenticate(ctx context.Context, r *http.Request) (*auth.Principal, error) {
	c.calls.Add(1)
	return c.Authenticator.Authenticate(ctx, r)
}

// TestRecurrentBlockSingleAuthenticationPerRequest pins the closure
// invariant end to end on /binflow/api/repositories — the deployed
// anomaly's second route (ping's shape is the table above): three
// sequential bad credentials answer the reference-exact 401,401,403 while
// the stack adjudicates EXACTLY once per request, and the armed block
// keeps answering 403 with the count still one-per-request (no implicit
// unblock, no shape decay). A per-request double count — the abandoned
// hypothesis — would move the 403 to shot 2; a ledger seeded before the
// first request would move it to shot 1; a re-authenticating chain would
// break the call count at whatever shot doubled.
func TestRecurrentBlockSingleAuthenticationPerRequest(t *testing.T) {
	var counter *countingAuth
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		counter = &countingAuth{Authenticator: d.Auth}
		d.Auth = counter
	}, [][2]string{{blockedUser, blockedUserPW}})

	const path = "/binflow/api/repositories"
	for shot, want := range []int{
		http.StatusUnauthorized, http.StatusUnauthorized, http.StatusForbidden,
	} {
		r := h.do(http.MethodGet, path, blockedUser, "wrong", nil, nil)
		body := bodyOf(t, r)
		if r.StatusCode != want {
			t.Fatalf("shot %d: status = %d (%s), want %d (the reference 401,401,403)",
				shot+1, r.StatusCode, body, want)
		}
		if got := counter.calls.Load(); got != int64(shot+1) {
			t.Fatalf("shot %d: Authenticate calls = %d, want exactly %d (one per wire request)",
				shot+1, got, shot+1)
		}
		if want == http.StatusUnauthorized {
			if w := blockedEnvelope(http.StatusUnauthorized, "invalid credentials"); body != w {
				t.Fatalf("shot %d body drift:\n got: %q\nwant: %q", shot+1, body, w)
			}
			if ch := r.Header.Get("WWW-Authenticate"); ch != `Basic realm="BinFlow Realm"` {
				t.Fatalf("shot %d WWW-Authenticate = %q, want the Basic challenge", shot+1, ch)
			}
		} else {
			if w := blockedEnvelope(http.StatusForbidden, blockedWireText4); body != w {
				t.Fatalf("shot 3 body drift:\n got: %q\nwant: %q", body, w)
			}
			assertBlockedWireShape(t, r, true)
		}
	}

	// Steady state: shots 4 and 5 stay the blocked 403 — the block is not
	// silently lifted or re-counted into a shape change. Only the status,
	// the blocked family form and the one-call invariant are asserted (the
	// countdown text decays with the wall clock, so byte-exactness ends at
	// the fresh-armed shot 3).
	for shot := 4; shot <= 5; shot++ {
		r := h.do(http.MethodGet, path, blockedUser, "wrong", nil, nil)
		body := bodyOf(t, r)
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("shot %d: status = %d (%s), want the steady blocked 403", shot, r.StatusCode, body)
		}
		if got := counter.calls.Load(); got != int64(shot) {
			t.Fatalf("shot %d: Authenticate calls = %d, want exactly %d (one per wire request)",
				shot, got, shot)
		}
		assertBlockedWireShape(t, r, true)
	}
}
