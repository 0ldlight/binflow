package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The T-15 compatible-plane suite, part 2: security (E-16..E-19) and the
// /api/v1/permissions CRUD (E-24). These cover the PRD's C20/C21/C22
// command sequences in-process; the curl-level equivalents live in
// curl_compat_test.go.

// ---- E-16: change password (C20) ----

// TestChangePasswordRoutes exercises both spellings plus the error ladder.
func TestChangePasswordRoutes(t *testing.T) {
	t.Run("own route succeeds and invalidates the old password", func(t *testing.T) {
		h := newHarness(t)
		resp := h.do(http.MethodPut, "/binflow/api/security/password", adminUser, adminPass,
			[]byte(`{"oldPassword":"password","newPassword":"n3w-pw"}`),
			map[string]string{"Content-Type": "application/json"})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, "successfully changed") {
			t.Fatalf("body = %q", body)
		}
		// Old password now fails, new one works.
		resp = h.do(http.MethodGet, "/binflow/api/repositories", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("old password status = %d, want 401", resp.StatusCode)
		}
		_ = resp.Body.Close()
		resp = h.do(http.MethodGet, "/binflow/api/repositories", adminUser, "n3w-pw", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("new password status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("alias route with confirmation pair", func(t *testing.T) {
		h := newHarness(t)
		resp := h.do(http.MethodPost, "/binflow/api/security/users/authorization/changePassword", adminUser, adminPass,
			[]byte(`{"userName":"admin","oldPassword":"password","newPassword1":"n3w-pw","newPassword2":"n3w-pw"}`),
			map[string]string{"Content-Type": "application/json"})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, "successfully changed") {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
	})

	t.Run("wrong old password is 400 plain text, not 401", func(t *testing.T) {
		h := newHarness(t)
		resp := h.do(http.MethodPut, "/binflow/api/security/password", adminUser, adminPass,
			[]byte(`{"oldPassword":"wrong","newPassword":"n3w-pw"}`),
			map[string]string{"Content-Type": "application/json"})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (inside the authenticated context)", resp.StatusCode)
		}
		if body != "Incorrect username/password" {
			t.Fatalf("body = %q", body)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("content-type = %q", ct)
		}
	})

	t.Run("alias mismatch pair is 400", func(t *testing.T) {
		h := newHarness(t)
		resp := h.do(http.MethodPost, "/binflow/api/security/users/authorization/changePassword", adminUser, adminPass,
			[]byte(`{"userName":"admin","oldPassword":"password","newPassword1":"a","newPassword2":"b"}`),
			map[string]string{"Content-Type": "application/json"})
		if got := mustGet(t, resp); resp.StatusCode != http.StatusBadRequest || got != "New passwords do not match" {
			t.Fatalf("status = %d body = %q", resp.StatusCode, got)
		}
	})

	t.Run("empty and unchanged new passwords are 400", func(t *testing.T) {
		h := newHarness(t)
		for _, tc := range []struct{ body, want string }{
			{`{"oldPassword":"password","newPassword":""}`, "New passwords cannot be empty"},
			{`{"oldPassword":"password","newPassword":"password"}`, "New password has to be different from the old one"},
		} {
			resp := h.do(http.MethodPut, "/binflow/api/security/password", adminUser, adminPass,
				[]byte(tc.body), map[string]string{"Content-Type": "application/json"})
			if got := mustGet(t, resp); resp.StatusCode != http.StatusBadRequest || got != tc.want {
				t.Fatalf("body %s: status = %d got = %q want %q", tc.body, resp.StatusCode, got, tc.want)
			}
		}
	})

	t.Run("anonymous is challenged", func(t *testing.T) {
		h := newHarness(t)
		resp := h.do(http.MethodPut, "/binflow/api/security/password", "", "",
			[]byte(`{"oldPassword":"x","newPassword":"y"}`), nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})
}

// ---- E-17: token create (C21a) ----

// tokenResp decodes the create body.
type tokenResp struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   *int64 `json:"expires_in"`
	Scope       string `json:"scope"`
	TokenID     int64  `json:"token_id"`
}

// TestTokenCreate covers the calibrated field set, both request encodings,
// and the OAuth-style error ladder.
func TestTokenCreate(t *testing.T) {
	h := newHarness(t)

	issue := func(ct string, body string, hdr map[string]string) (*http.Response, string) {
		t.Helper()
		if hdr == nil {
			hdr = map[string]string{}
		}
		if ct != "" {
			hdr["Content-Type"] = ct
		}
		resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass, []byte(body), hdr)
		return resp, mustGet(t, resp)
	}

	t.Run("form request yields the full field set", func(t *testing.T) {
		resp, body := issue("application/x-www-form-urlencoded", "grant_type=client_credentials", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var tok tokenResp
		if err := json.Unmarshal([]byte(body), &tok); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if tok.AccessToken == "" {
			t.Fatal("access_token empty")
		}
		if tok.TokenType != "Bearer" {
			t.Fatalf("token_type = %q, want Bearer", tok.TokenType)
		}
		if tok.Scope == "" {
			t.Fatal("scope empty")
		}
		if tok.TokenID == 0 {
			t.Fatal("token_id (BinFlow superset) missing")
		}
		if tok.ExpiresIn == nil || *tok.ExpiresIn <= 0 {
			t.Fatalf("expires_in = %v, want the configured TTL in seconds", tok.ExpiresIn)
		}
	})

	t.Run("no content type defaults to form", func(t *testing.T) {
		resp, body := issue("", "grant_type=client_credentials", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, `"access_token"`) {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("JSON body is accepted as the BinFlow extension", func(t *testing.T) {
		resp, body := issue("application/json", `{"grant_type":"client_credentials","expires_in":3600}`, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var tok tokenResp
		if err := json.Unmarshal([]byte(body), &tok); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if tok.ExpiresIn == nil || *tok.ExpiresIn != 3600 {
			t.Fatalf("expires_in = %v, want 3600", tok.ExpiresIn)
		}
	})

	t.Run("expires_in=0 means never and omits the field", func(t *testing.T) {
		resp, body := issue("application/x-www-form-urlencoded", "grant_type=client_credentials&expires_in=0", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		if strings.Contains(body, "expires_in") {
			t.Fatalf("body %q should omit expires_in for a never-expiring token", body)
		}
	})

	t.Run("unknown grant type is unsupported_grant_type", func(t *testing.T) {
		resp, body := issue("application/x-www-form-urlencoded", "grant_type=password", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		assertOAuthError(t, body, "unsupported_grant_type")
	})

	t.Run("negative expires_in is invalid_request", func(t *testing.T) {
		resp, body := issue("application/x-www-form-urlencoded", "grant_type=client_credentials&expires_in=-5", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		assertOAuthError(t, body, "invalid_request")
	})

	t.Run("internal scope is invalid_scope", func(t *testing.T) {
		resp, body := issue("application/x-www-form-urlencoded", "grant_type=client_credentials&scope=internal:admin", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		assertOAuthError(t, body, "invalid_scope")
	})

	t.Run("anonymous is rejected", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/security/token", "", "",
			[]byte("grant_type=client_credentials"),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})

	t.Run("non-admin mints for self (T-190: Q11 ruling opens the endpoint)", func(t *testing.T) {
		h2 := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
		resp := h2.do(http.MethodPost, "/binflow/api/security/token", "ci-bot", "ci-pw",
			[]byte("grant_type=client_credentials"),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
		}
		var tok tokenResp
		if err := json.Unmarshal([]byte(body), &tok); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if tok.AccessToken == "" || tok.TokenID == 0 || tok.TokenType != "Bearer" {
			t.Fatalf("self-minted token incomplete: %+v", tok)
		}
		// The minted token authenticates its subject on the management read
		// plane (the default TTL is within the non-admin cap).
		use := h2.do(http.MethodGet, "/binflow/api/system/ping", "", "",
			nil, map[string]string{"X-JFrog-Art-Api": tok.AccessToken})
		if use.StatusCode != http.StatusOK {
			t.Fatalf("minted token use = %d, want 200", use.StatusCode)
		}

		// The negative branch: naming anyone else is the 403 OAuth form.
		resp = h2.do(http.MethodPost, "/binflow/api/security/token", "ci-bot", "ci-pw",
			[]byte("grant_type=client_credentials&username=admin"),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		body = mustGet(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("username=other status = %d, want 403; body=%s", resp.StatusCode, body)
		}
		assertOAuthError(t, body, "invalid_request")
	})
}

// assertOAuthError checks the {"error","error_description"} shape and code.
func assertOAuthError(t *testing.T, body, code string) {
	t.Helper()
	var oe struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal([]byte(body), &oe); err != nil {
		t.Fatalf("body %q is not the OAuth error shape: %v", body, err)
	}
	if oe.Error != code {
		t.Fatalf("error = %q, want %q (body %s)", oe.Error, code, body)
	}
}

// ---- E-18: token revoke (C21c) ----

// TestTokenRevoke walks C21c: form revoke by value and by id, the XOR rule,
// and the idempotent "Token not found".
func TestTokenRevoke(t *testing.T) {
	h := newHarness(t)

	mint := func(t *testing.T) (string, int64) {
		t.Helper()
		resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
			[]byte("grant_type=client_credentials"),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mint status = %d; body=%s", resp.StatusCode, body)
		}
		var tok tokenResp
		if err := json.Unmarshal([]byte(body), &tok); err != nil {
			t.Fatalf("mint body %q: %v", body, err)
		}
		return tok.AccessToken, tok.TokenID
	}
	revoke := func(t *testing.T, form string) (*http.Response, string) {
		t.Helper()
		resp := h.do(http.MethodPost, "/binflow/api/security/token/revoke", adminUser, adminPass,
			[]byte(form), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		return resp, mustGet(t, resp)
	}

	t.Run("revoke by value: 200 Token revoked, then the token is 401", func(t *testing.T) {
		token, _ := mint(t)
		resp, body := revoke(t, "token="+token)
		if resp.StatusCode != http.StatusOK || body != "Token revoked" {
			t.Fatalf("status = %d body = %q", resp.StatusCode, body)
		}
		resp = h.do(http.MethodGet, "/binflow/api/repositories", adminUser, token, nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("revoked token status = %d, want 401", resp.StatusCode)
		}
		_ = resp.Body.Close()

		// Repeat revoke: still 200, "Token not found" (idempotent).
		resp, body = revoke(t, "token="+token)
		if resp.StatusCode != http.StatusOK || body != "Token not found" {
			t.Fatalf("repeat status = %d body = %q", resp.StatusCode, body)
		}
	})

	t.Run("revoke by token_id", func(t *testing.T) {
		token, id := mint(t)
		resp, body := revoke(t, "token_id="+itoa(id))
		if resp.StatusCode != http.StatusOK || body != "Token revoked" {
			t.Fatalf("status = %d body = %q", resp.StatusCode, body)
		}
		resp = h.do(http.MethodGet, "/binflow/api/repositories", adminUser, token, nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("revoked token status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("both parameters is 400 mutually exclusive", func(t *testing.T) {
		resp, body := revoke(t, "token=abc&token_id=1")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		assertOAuthError(t, body, "invalid_request")
		if !strings.Contains(body, "mutually exclusive") {
			t.Fatalf("description = %q", body)
		}
	})

	t.Run("neither parameter is 400 required", func(t *testing.T) {
		resp, body := revoke(t, "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		assertOAuthError(t, body, "invalid_request")
		if !strings.Contains(body, "required") {
			t.Fatalf("description = %q", body)
		}
	})

	t.Run("non-admin is 403 in the OAuth error shape", func(t *testing.T) {
		h2 := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
		resp := h2.do(http.MethodPost, "/binflow/api/security/token/revoke", "ci-bot", "ci-pw",
			[]byte("token=whatever"), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		assertOAuthError(t, body, "invalid_request")
	})
}

// itoa avoids importing strconv for two call sites.
func itoa(v int64) string {
	return strings.TrimSpace(strings.TrimPrefix(jsonNumber(v), ""))
}

// jsonNumber renders v via encoding/json (no leading zeros, no fuss).
func jsonNumber(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---- E-19: users (C22a) ----

// TestUsersRoutes covers both create spellings, the GET shapes (no password
// ever), and the validation chain.
func TestUsersRoutes(t *testing.T) {
	h := newHarness(t)

	t.Run("PUT route creates with 201 and no body", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/api/security/users/ci-bot", adminUser, adminPass,
			[]byte(`{"name":"ci-bot","email":"ci@example.com","password":"ci-pw","admin":false}`),
			map[string]string{"Content-Type": "application/json"})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		if body != "" {
			t.Fatalf("body = %q, want empty", body)
		}
		// The new account authenticates (version is the unauthenticated
		// probe used here — the repository list went admin-only in D2, so a
		// 200 there is no longer proof of a working credential).
		resp = h.do(http.MethodGet, "/binflow/api/system/version", "ci-bot", "ci-pw", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("new user auth status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("PUT on an existing user replaces it with 201 (T-97 flip)", func(t *testing.T) {
		// M1 answered 409 "The user already exists" here. T-97 retired that
		// posture: auth-model.md section 1.3 item 11 (high confidence) makes
		// PUT create-or-replace — both outcomes 201 no body — and the M4
		// membership flow (W19b: PUT an existing user's groups) depends on
		// it. The collection POST route keeps the 409 (create-only).
		resp := h.do(http.MethodPut, "/binflow/api/security/users/ci-bot", adminUser, adminPass,
			[]byte(`{"name":"ci-bot","email":"ci@example.com","password":"other"}`),
			map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d body=%s", resp.StatusCode, mustGet(t, resp))
		}
		_ = resp.Body.Close()
		// The replacement password is the live credential; the old one died.
		resp = h.do(http.MethodGet, "/binflow/api/system/version", "ci-bot", "other", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("replaced password auth status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
		resp = h.do(http.MethodGet, "/binflow/api/system/version", "ci-bot", "ci-pw", nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("old password must be dead, status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("POST collection route with the same chain", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/security/users", adminUser, adminPass,
			[]byte(`{"name":"ci-two","email":"two@example.com","password":"pw2"}`),
			map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d body=%s", resp.StatusCode, mustGet(t, resp))
		}
	})

	t.Run("missing email is 400 with the spec wording", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/security/users", adminUser, adminPass,
			[]byte(`{"name":"ci-three","password":"pw3"}`),
			map[string]string{"Content-Type": "application/json"})
		if got := mustGet(t, resp); resp.StatusCode != http.StatusBadRequest || !strings.Contains(got, "email") {
			t.Fatalf("status = %d body = %q", resp.StatusCode, got)
		}
	})

	t.Run("missing password is 400", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/api/security/users/ci-four", adminUser, adminPass,
			[]byte(`{"name":"ci-four","email":"four@example.com"}`),
			map[string]string{"Content-Type": "application/json"})
		if got := mustGet(t, resp); resp.StatusCode != http.StatusBadRequest || !strings.Contains(got, "password") {
			t.Fatalf("status = %d body = %q", resp.StatusCode, got)
		}
	})

	t.Run("body/path name mismatch is 409", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/api/security/users/ci-five", adminUser, adminPass,
			[]byte(`{"name":"someone-else","email":"x@example.com","password":"pw"}`),
			map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})

	t.Run("reserved name _system_ is 400", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/api/security/users/_system_", adminUser, adminPass,
			[]byte(`{"name":"_system_","email":"s@example.com","password":"pw"}`),
			map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})

	t.Run("GET list entries are name/uri/realm and never a password", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/security/users", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var items []map[string]any
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if len(items) < 3 {
			t.Fatalf("len = %d (admin + ci-bot + ci-two at least)", len(items))
		}
		for _, it := range items {
			for k := range it {
				if strings.Contains(strings.ToLower(k), "pass") {
					t.Fatalf("entry carries a password-shaped field %q: %v", k, it)
				}
			}
			if it["realm"] != "internal" {
				t.Fatalf("entry realm = %v", it["realm"])
			}
			if u, _ := it["uri"].(string); !strings.Contains(u, "/api/security/users/") {
				t.Fatalf("entry uri = %v", it["uri"])
			}
		}
	})

	t.Run("GET single user has no password fields", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/security/users/ci-bot", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		// The Artifactory field set carries no password VALUE; the only
		// password-shaped names are the two boolean account flags
		// (internalPasswordDisabled is part of the real shape). Assert no
		// field holds a secret-like value.
		var fields map[string]any
		if err := json.Unmarshal([]byte(body), &fields); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		for k, v := range fields {
			if s, ok := v.(string); ok && (strings.Contains(strings.ToLower(k), "pass") || strings.Contains(strings.ToLower(k), "hash")) {
				t.Fatalf("field %q carries a string value %q", k, s)
			}
		}
	})

	t.Run("unknown user is 404", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/security/users/ghost", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})

	t.Run("non-admin is 403", func(t *testing.T) {
		h2 := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
		resp := h2.do(http.MethodGet, "/binflow/api/security/users", "ci-bot", "ci-pw", nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})

	t.Run("anonymous is 401", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/security/users", "", "", nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})
}

// ---- E-24: /api/v1/permissions (C22b/C22c) ----

// TestPermissionsCRUD covers create/replace, list, delete and the
// end-to-end grant effect on content operations.
func TestPermissionsCRUD(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
	seedRepo(t, h, "generic-local")

	// Baseline: ci-bot cannot write anywhere.
	resp := h.do(http.MethodPut, "/binflow/generic-local/other/z.bin", "ci-bot", "ci-pw", []byte("z"), nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("baseline write status = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()

	t.Run("C22b grant read+write on ci-out/**", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/v1/permissions", adminUser, adminPass,
			[]byte(`{"name":"ci-out-rw","repos":["generic-local"],"includePatterns":["ci-out/**"],`+
				`"principals":{"users":{"ci-bot":["read","write"]}}}`),
			map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
		}
		// The grant is effective immediately.
		resp = h.do(http.MethodPut, "/binflow/generic-local/ci-out/y.bin", "ci-bot", "ci-pw", []byte("y"), nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("granted write status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
		}
		// Outside the pattern still denied.
		resp = h.do(http.MethodPut, "/binflow/generic-local/other/z.bin", "ci-bot", "ci-pw", []byte("z"), nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("outside-pattern write status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
		// C22c: delete not granted.
		resp = h.do(http.MethodDelete, "/binflow/generic-local/ci-out/y.bin", "ci-bot", "ci-pw", nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("ungranted delete status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("list reflects the target and principals", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/v1/permissions", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var targets []struct {
			Name            string   `json:"name"`
			Repos           []string `json:"repos"`
			IncludePatterns []string `json:"includePatterns"`
			Principals      struct {
				Users map[string][]string `json:"users"`
			} `json:"principals"`
		}
		if err := json.Unmarshal([]byte(body), &targets); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if len(targets) != 1 || targets[0].Name != "ci-out-rw" {
			t.Fatalf("targets = %+v", targets)
		}
		tg := targets[0]
		if len(tg.Repos) != 1 || tg.Repos[0] != "generic-local" ||
			len(tg.IncludePatterns) != 1 || tg.IncludePatterns[0] != "ci-out/**" {
			t.Fatalf("target fields = %+v", tg)
		}
		actions := map[string]bool{}
		for _, a := range tg.Principals.Users["ci-bot"] {
			actions[a] = true
		}
		if !actions["read"] || !actions["write"] || actions["delete"] {
			t.Fatalf("actions = %v, want {read, write} only", tg.Principals.Users["ci-bot"])
		}
	})

	t.Run("replace adds delete", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/v1/permissions", adminUser, adminPass,
			[]byte(`{"name":"ci-out-rw","repos":["generic-local"],"includePatterns":["ci-out/**"],`+
				`"principals":{"users":{"ci-bot":["read","write","delete"]}}}`),
			map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
		resp = h.do(http.MethodDelete, "/binflow/generic-local/ci-out/y.bin", "ci-bot", "ci-pw", nil, nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("granted delete status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("FR-5-AC10 delete removes the grants", func(t *testing.T) {
		resp := h.do(http.MethodDelete, "/binflow/api/v1/permissions/ci-out-rw", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
		// Re-deposit then verify the grant is gone.
		resp = h.do(http.MethodPut, "/binflow/generic-local/ci-out/y.bin", "ci-bot", "ci-pw", []byte("y"), nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("post-delete write status = %d, want 403", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})

	t.Run("delete of an unknown name is 404", func(t *testing.T) {
		resp := h.do(http.MethodDelete, "/binflow/api/v1/permissions/nope", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})

	t.Run("validation ladder", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			body string
			want int
		}{
			{"missing name", `{"repos":["generic-local"]}`, http.StatusBadRequest},
			{"missing repos", `{"name":"x1"}`, http.StatusBadRequest},
			{"unknown repo", `{"name":"x2","repos":["ghost"]}`, http.StatusBadRequest},
			{"unknown user", `{"name":"x3","repos":["generic-local"],"principals":{"users":{"ghost":["read"]}}}`, http.StatusBadRequest},
			{"unknown action", `{"name":"x4","repos":["generic-local"],"principals":{"users":{"ci-bot":["manage"]}}}`, http.StatusBadRequest},
		} {
			resp := h.do(http.MethodPost, "/binflow/api/v1/permissions", adminUser, adminPass,
				[]byte(tc.body), map[string]string{"Content-Type": "application/json"})
			if resp.StatusCode != tc.want {
				t.Fatalf("%s: status = %d want %d body=%s", tc.name, resp.StatusCode, tc.want, mustGet(t, resp))
			}
			_ = resp.Body.Close()
		}
	})

	t.Run("non-admin is 403, anonymous 401", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/v1/permissions", "ci-bot", "ci-pw",
			[]byte(`{"name":"x5","repos":["generic-local"]}`), map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("non-admin status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
		resp = h.do(http.MethodGet, "/binflow/api/v1/permissions", "", "", nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous status = %d", resp.StatusCode)
		}
		_ = mustGet(t, resp)
	})
}
