package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestManagementPlane401Matrix: every T-15 route demands a credential —
// the management plane is never anonymous (ADR-0009), with the documented
// exception of the /api/storage read pair (content-plane semantics, PRD
// E-09) whose anonymous boundary is the anonymous_access switch.
func TestManagementPlane401Matrix(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	tests := []struct {
		path      string
		method    string
		body      string
		want      int
		challenge bool
	}{
		{"/binflow/api/repositories", http.MethodGet, "", http.StatusUnauthorized, true},
		{"/binflow/api/repositories/new-repo", http.MethodPut, `{"rclass":"local","packageType":"generic"}`, http.StatusUnauthorized, true},
		{"/binflow/api/repositories/generic-local", http.MethodDelete, "", http.StatusUnauthorized, true},
		{"/binflow/api/security/password", http.MethodPut, `{"oldPassword":"a","newPassword":"b"}`, http.StatusUnauthorized, true},
		{"/binflow/api/security/users/authorization/changePassword", http.MethodPost, `{}`, http.StatusUnauthorized, true},
		{"/binflow/api/security/token", http.MethodPost, "grant_type=client_credentials", http.StatusUnauthorized, true},
		{"/binflow/api/security/token/revoke", http.MethodPost, "token=x", http.StatusUnauthorized, true},
		{"/binflow/api/security/users", http.MethodGet, "", http.StatusUnauthorized, true},
		{"/binflow/api/security/users/someone", http.MethodPut, `{}`, http.StatusUnauthorized, true},
		{"/binflow/api/security/users", http.MethodPost, `{}`, http.StatusUnauthorized, true},
		{"/binflow/api/v1/permissions", http.MethodPost, `{}`, http.StatusUnauthorized, true},
		{"/binflow/api/v1/permissions", http.MethodGet, "", http.StatusUnauthorized, true},
		{"/binflow/api/v1/permissions/x", http.MethodDelete, "", http.StatusUnauthorized, true},
		// D2 endpoints in their anonymous posture: all management-plane.
		{"/binflow/api/v1/health", http.MethodGet, "", http.StatusUnauthorized, true},
		{"/binflow/api/v1/storage/stats", http.MethodGet, "", http.StatusUnauthorized, true},
		{"/binflow/api/repositories/generic-local", http.MethodGet, "", http.StatusUnauthorized, true},
		// The storage read pair follows the content plane instead: with the
		// default anonymous_access=true the anonymous GET passes.
		{"/binflow/api/storage/generic-local", http.MethodGet, "", http.StatusOK, false},
		{"/binflow/api/storage/generic-local?list", http.MethodGet, "", http.StatusForbidden, false},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body []byte
			if tc.body != "" {
				body = []byte(tc.body)
			}
			resp := h.do(tc.method, tc.path, "", "", body, nil)
			_ = mustGet(t, resp)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			chal := resp.Header.Get("WWW-Authenticate")
			if tc.challenge && !strings.HasPrefix(chal, `Basic realm="`) {
				t.Fatalf("WWW-Authenticate = %q, want the Basic challenge", chal)
			}
			if !tc.challenge && chal != "" {
				t.Fatalf("unexpected challenge %q", chal)
			}
		})
	}
}

// TestD2D3AdminReadPlaneMatrix is the D2/D3 regression matrix: the four
// surfaces QA flagged (repository reads, single-repo read, v1 stats, v1
// health, token minting) answer 403 for a non-admin principal, 200 for
// admin, 401 for anonymous — and the unauthenticated probes stay open.
func TestD2D3AdminReadPlaneMatrix(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"qa-bot", "qa-pw"}})
	seedRepo(t, h, "generic-local")

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"repo list", http.MethodGet, "/binflow/api/repositories"},
		{"single repo", http.MethodGet, "/binflow/api/repositories/generic-local"},
		{"v1 storage stats", http.MethodGet, "/binflow/api/v1/storage/stats"},
		{"v1 health", http.MethodGet, "/binflow/api/v1/health"},
		{"token mint", http.MethodPost, "/binflow/api/security/token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Non-admin: 403.
			var body []byte
			if tc.method == http.MethodPost {
				body = []byte("grant_type=client_credentials")
			}
			resp := h.do(tc.method, tc.path, "qa-bot", "qa-pw", body, nil)
			respBody := mustGet(t, resp)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("non-admin status = %d, want 403; body=%s", resp.StatusCode, respBody)
			}
			// Admin: 200.
			resp = h.do(tc.method, tc.path, adminUser, adminPass, body, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("admin status = %d, want 200; body=%s", resp.StatusCode, mustGet(t, resp))
			}
			_ = resp.Body.Close()
			// Anonymous: 401 + challenge.
			resp = h.do(tc.method, tc.path, "", "", body, nil)
			_ = mustGet(t, resp)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("anonymous status = %d, want 401", resp.StatusCode)
			}
			if chal := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(chal, `Basic realm="`) {
				t.Fatalf("WWW-Authenticate = %q", chal)
			}
		})
	}

	// The low-threshold endpoints must not be caught by the tightening.
	t.Run("ping and version stay anonymous", func(t *testing.T) {
		for _, path := range []string{"/binflow/api/system/ping", "/binflow/api/system/version"} {
			resp := h.do(http.MethodGet, path, "", "", nil, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s anonymous status = %d", path, resp.StatusCode)
			}
			_ = resp.Body.Close()
		}
	})

	// A minted token must inherit admin authority only from an admin
	// issuer: an admin-minted token still works, and nothing else can mint.
	t.Run("admin-minted token still authenticates", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
			[]byte("grant_type=client_credentials"),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mint status = %d; body=%s", resp.StatusCode, body)
		}
		token := jsonFieldString(t, body, "access_token")
		if token == "" {
			t.Fatalf("no token: %s", body)
		}
		resp = h.do(http.MethodGet, "/binflow/api/repositories", adminUser, token, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("admin token on repo list = %d, want 200", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})
}
func TestCompatPlaneE26(t *testing.T) {
	h := newHarness(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/binflow/api/repositories/generic-local/something"},
		{http.MethodPost, "/binflow/api/storage/generic-local/x"},
		{http.MethodDelete, "/binflow/api/storage/generic-local"},
		{http.MethodPost, "/binflow/api/security/users/someone"},
		{http.MethodDelete, "/binflow/api/security/users"},
		{http.MethodGet, "/binflow/api/security/token"},        // GET on a POST route
		{http.MethodGet, "/binflow/api/security/token/revoke"}, // GET on a POST route
		{http.MethodPut, "/binflow/api/v1/permissions"},        // PUT not in the M1 trio
		{http.MethodGet, "/binflow/api/v1/permissions/"},       // trailing slash, empty name
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, adminUser, adminPass, nil, nil)
			eb := decodeError(t, resp)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
			if !strings.Contains(eb.Errors[0].Message, "not implemented") {
				t.Fatalf("message = %q", eb.Errors[0].Message)
			}
		})
	}
}
