// Recurrent-failure block wire tests (T-633): the 403 arm the httpapi
// routing plane renders for the auth breaker's BlockedError verdict.
// The breaker itself is pinned by internal/auth (T-632, recurrent_test.go
// with the injected clock); here the block is armed through the PUBLIC
// surface — real requests with bad Basic credentials until the breaker
// trips — because the ledger and its clock are auth-internal. Every
// pre-block response is asserted byte-form identical to the historical
// uniform 401 (the zero-regression window), and the first blocked response
// is asserted against the reference capture's wire form (reports/
// compatibility/l0203-wire/A-c3-v1auth-badpw: 403, the charset media type,
// the errors[] envelope carrying the verbatim countdown sentence).

package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// blockedUser / blockedUserPW are the hammered principal and its real
// password (the block must precede verification: even the RIGHT password
// gets the countdown while armed); blockedBystander proves the block is
// scoped to the probed username on the wire too.
const (
	blockedUser        = "breaker-wire-user"
	blockedUserPW      = "right-password" //nolint:gosec // throwaway test credential
	blockedBystander   = "breaker-bystander"
	blockedBystanderPW = "bystander-password" //nolint:gosec // throwaway test credential
)

// blockedWireText4 is the verbatim countdown sentence a freshly armed
// block renders (window 4s, "(s)" spelling part of the text).
const blockedWireText4 = "This request is blocked due to recurrent request failures, please try again in 4 second(s)"

// blockedEnvelope lays out the byte-exact errors[] body the envelope seam
// renders (errface.Render's Jackson pretty form, no trailing newline).
func blockedEnvelope(status int, message string) string {
	return fmt.Sprintf("{\n  \"errors\" : [ {\n    \"status\" : %d,\n    \"message\" : %q\n  } ]\n}", status, message)
}

// bodyOf reads and closes resp's body.
func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

// armRecurrentBlock hammers bad Basic credentials for the blocked user
// until the breaker answers the 403 countdown, asserting EVERY pre-block
// response is the historical uniform 401 (nothing may change below the
// threshold). It returns an asserter for the tripped response itself
// (status plus byte-exact body; the response is returned for header
// assertions). The loop is threshold-agnostic by design: the count is a
// declared inference in the auth layer, not a wire-pinned fact.
func armRecurrentBlock(t *testing.T, h *harness) func(*http.Response) *http.Response {
	t.Helper()
	for i := 1; i <= 24; i++ {
		r := h.do(http.MethodGet, "/binflow/api/system/ping", blockedUser, "wrong", nil, nil)
		body := bodyOf(t, r)
		switch r.StatusCode {
		case http.StatusUnauthorized:
			// The uniform rejection, byte-for-byte the historical form.
			want := blockedEnvelope(http.StatusUnauthorized, "invalid credentials")
			if body != want {
				t.Fatalf("pre-block attempt %d body drift:\n got: %q\nwant: %q", i, body, want)
			}
		case http.StatusForbidden:
			// The breaker tripped; the caller asserts this response.
			return func(blocked *http.Response) *http.Response {
				body := bodyOf(t, blocked)
				want := blockedEnvelope(http.StatusForbidden, blockedWireText4)
				if blocked.StatusCode != http.StatusForbidden {
					t.Fatalf("blocked verdict status = %d, want 403", blocked.StatusCode)
				}
				if body != want {
					t.Fatalf("blocked body drift:\n got: %q\nwant: %q", body, want)
				}
				return blocked
			}
		default:
			t.Fatalf("pre-block attempt %d: unexpected status %d (%s)", i, r.StatusCode, body)
		}
	}
	t.Fatal("breaker never armed within 24 bad attempts")
	return nil
}

// assertBlockedWireShape checks the 403 family form: the charset media
// type the 401 family answers, and (when the caller's 401 twin carries
// the Basic challenge) the challenge header itself.
func assertBlockedWireShape(t *testing.T, resp *http.Response, wantChallenge bool) {
	t.Helper()
	if got := resp.Header.Get("Content-Type"); got != "application/json;charset=ISO-8859-1" {
		t.Errorf("blocked Content-Type = %q, want the auth family's charset form", got)
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	if wantChallenge && challenge != `Basic realm="BinFlow Realm"` {
		t.Errorf("blocked WWW-Authenticate = %q, want the Basic challenge (the 401 twin's header)", challenge)
	}
	if !wantChallenge && challenge != "" {
		t.Errorf("blocked WWW-Authenticate = %q, want none (the login face's 401 has no challenge)", challenge)
	}
}

// TestRecurrentBlockWire walks the wire matrix: the armed block answers
// 403 with the verbatim countdown on both Basic planes (the generic
// dispatch hard-401 point and the login face), every pre-block posture
// stays byte-for-byte the uniform 401, good credentials never notice the
// breaker, and the block stays scoped to the probed username.
func TestRecurrentBlockWire(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, h *harness)
	}{
		{
			name: "blocked basic credential answers 403 verbatim, unblocked ones keep the uniform 401",
			run: func(t *testing.T, h *harness) {
				// The hammer returns the asserter for the tripped response;
				// every pre-block attempt was already asserted uniform 401.
				assert := armRecurrentBlock(t, h)
				blocked := assert(h.do(http.MethodGet, "/binflow/api/system/ping", blockedUser, "wrong", nil, nil))
				assertBlockedWireShape(t, blocked, true)
				// A different username's bad credential is judged on its own
				// count: the uniform 401, never the breaker's 403.
				r := h.do(http.MethodGet, "/binflow/api/system/ping", blockedBystander, "wrong", nil, nil)
				body := bodyOf(t, r)
				if r.StatusCode != http.StatusUnauthorized {
					t.Fatalf("bystander status = %d, want 401", r.StatusCode)
				}
				if want := blockedEnvelope(http.StatusUnauthorized, "invalid credentials"); body != want {
					t.Fatalf("bystander body drift:\n got: %q\nwant: %q", body, want)
				}
			},
		},
		{
			name: "good credentials answer 200 and never arm the breaker",
			run: func(t *testing.T, h *harness) {
				// The correct credential, presented repeatedly: every request
				// a 200 — success clears the count, so the breaker can never
				// arm on it.
				for i := 0; i < 8; i++ {
					r := h.do(http.MethodGet, "/binflow/api/system/ping", blockedUser, blockedUserPW, nil, nil)
					body := bodyOf(t, r)
					if r.StatusCode != http.StatusOK {
						t.Fatalf("good attempt %d: status = %d (%s), want 200", i+1, r.StatusCode, body)
					}
				}
			},
		},
		{
			name: "the login face answers the blocked verdict 403 without a challenge",
			run: func(t *testing.T, h *harness) {
				// Arm through the login body itself: each bad POST is the
				// uniform 401 until the breaker trips, then the handler's
				// blocked arm takes over.
				loginBody := []byte(`{"username":"` + blockedUser + `","password":"wrong"}`)
				ct := map[string]string{"Content-Type": "application/json"}
				for i := 1; i <= 24; i++ {
					r := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", loginBody, ct)
					body := bodyOf(t, r)
					switch r.StatusCode {
					case http.StatusUnauthorized:
						if want := blockedEnvelope(http.StatusUnauthorized, "invalid credentials"); body != want {
							t.Fatalf("pre-block login %d body drift:\n got: %q\nwant: %q", i, body, want)
						}
					case http.StatusForbidden:
						assertBlockedWireShape(t, r, false)
						if want := blockedEnvelope(http.StatusForbidden, blockedWireText4); body != want {
							t.Fatalf("blocked login body drift:\n got: %q\nwant: %q", body, want)
						}
						return
					default:
						t.Fatalf("pre-block login %d: unexpected status %d (%s)", i, r.StatusCode, body)
					}
				}
				t.Fatal("breaker never armed within 24 bad logins")
			},
		},
		{
			name: "the login face keeps the uniform 401 below the threshold",
			run: func(t *testing.T, h *harness) {
				// A single bad login on a fresh ledger: the historical 401,
				// byte-for-byte, challenge-free.
				loginBody := []byte(`{"username":"` + blockedUser + `","password":"wrong"}`)
				ct := map[string]string{"Content-Type": "application/json"}
				r := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", loginBody, ct)
				body := bodyOf(t, r)
				if r.StatusCode != http.StatusUnauthorized {
					t.Fatalf("login status = %d, want 401", r.StatusCode)
				}
				if want := blockedEnvelope(http.StatusUnauthorized, "invalid credentials"); body != want {
					t.Fatalf("login body drift:\n got: %q\nwant: %q", body, want)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarnessCfg(t, nil, [][2]string{
				{blockedUser, blockedUserPW},
				{blockedBystander, blockedBystanderPW},
			})
			tc.run(t, h)
		})
	}
}
