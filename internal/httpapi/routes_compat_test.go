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

// TestCompatPlaneE26: near-miss paths under the new route families answer
// the envelope 404 with the not-implemented wording — never a 500, never a
// silent 200, never a panic.
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
