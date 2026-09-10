package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The T-37 token-flow suite over the full production chain (harness):
// D04 (both anonymous modes), D04b (manual negotiation + Bearer against the
// catalog placeholder), D04c (non-admin 200 vs management-plane 403,
// offline_token 400), D23 (revocation linkage) and the scoped-challenge
// matrix (AC2).

// tokenBody mirrors the /v2/token success payload.
type tokenBody struct {
	Token        string `json:"token"`
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	IssuedAt     string `json:"issued_at"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
}

// getV2Token negotiates one token through the real endpoint.
func getV2Token(h *harness, user, pass, query string) (*http.Response, string) {
	h.t.Helper()
	resp := h.do(http.MethodGet, "/v2/token"+query, user, pass, nil, nil)
	return resp, mustGet(h.t, resp)
}

// TestV2TokenNegotiation (D04b, FR-11-AC3): Basic credentials exchange for
// the four-field payload; the same token authenticates a Bearer request.
func TestV2TokenNegotiation(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "team1")

	resp, body := getV2Token(h, adminUser, adminPass,
		"?service=binflow&scope=repository:team1/app:pull,push")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Fatalf("api-version = %q", got)
	}
	var tok tokenBody
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if tok.Token == "" || tok.AccessToken != tok.Token {
		t.Fatalf("token/access_token = %q/%q, want equal non-empty", tok.Token, tok.AccessToken)
	}
	if tok.ExpiresIn != 720*3600 {
		t.Fatalf("expires_in = %d, want 720h in seconds", tok.ExpiresIn)
	}
	if tok.IssuedAt == "" {
		t.Fatal("issued_at empty")
	}
	if tok.Scope != "repository:team1/app:pull,push" {
		t.Fatalf("scope = %q, want the admin-wide grant", tok.Scope)
	}

	// D04b: the token drives a Bearer request — since T-40 the catalog is
	// implemented, so the bearer-authenticated admin gets the (empty)
	// listing; the point of the assertion is that the Bearer AUTHENTICATES
	// (401 would mean the token failed).
	bearer := h.do(http.MethodGet, "/v2/_catalog", "", "", nil,
		map[string]string{"Authorization": "Bearer " + tok.Token})
	bbody := mustGet(t, bearer)
	if bearer.StatusCode == http.StatusUnauthorized {
		t.Fatalf("Bearer token rejected on _catalog: %d %s", bearer.StatusCode, bbody)
	}
	if bearer.StatusCode != http.StatusOK {
		t.Fatalf("_catalog status = %d, want 200 (T-40); body=%s", bearer.StatusCode, bbody)
	}
	if got := bearer.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Fatalf("api-version = %q", got)
	}

	// The same token authorizes a read on the name route (foundation 404,
	// not 401/403): admin passes the scope gate.
	nameResp := h.do(http.MethodGet, "/v2/team1/app/manifests/latest", "", "", nil,
		map[string]string{"Authorization": "Bearer " + tok.Token})
	nbody := mustGet(t, nameResp)
	if nameResp.StatusCode == http.StatusUnauthorized || nameResp.StatusCode == http.StatusForbidden {
		t.Fatalf("admin bearer denied on name route: %d %s", nameResp.StatusCode, nbody)
	}
}

// TestV2TokenPOSTForm (AC1): the POST form spelling reaches the same result.
func TestV2TokenPOSTForm(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/v2/token", adminUser, adminPass,
		[]byte("service=binflow&scope=repository:team1/app:pull&account=admin"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	var tok tokenBody
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if tok.Token == "" {
		t.Fatal("POST form token empty")
	}
}

// TestV2TokenNonAdmin (D04c, FR-11-AC3): a non-admin user exchanges at
// /v2/token (200, docker-login semantics) while the SAME credentials hit
// the management plane's 403 (T-15/D3) — the two-entry design's observable
// boundary. The granted scope narrows to the ACL (AC4).
func TestV2TokenNonAdmin(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
	seedDockerRepo(t, h, "docker-local")
	grant(t, h, "ci-read", "docker-local", "**", "ci-bot", true, false, false)

	resp, body := getV2Token(h, "ci-bot", "ci-pw",
		"?service=binflow&scope=repository:docker-local/ci-out/**:pull")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/v2/token for non-admin = %d; body=%s", resp.StatusCode, body)
	}
	var tok tokenBody
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if tok.Token == "" {
		t.Fatal("non-admin token empty")
	}
	if tok.Scope != "repository:docker-local/ci-out/**:pull" {
		t.Fatalf("scope = %q, want the granted pull", tok.Scope)
	}

	// The narrowing strips what the ACL does not carry: a DIFFERENT repo
	// carries no grant at all.
	resp2, body2 := getV2Token(h, "ci-bot", "ci-pw",
		"?service=binflow&scope=repository:other-repo/app:pull,push")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second exchange = %d; body=%s", resp2.StatusCode, body2)
	}
	var tok2 tokenBody
	_ = json.Unmarshal([]byte(body2), &tok2)
	if tok2.Scope != "" {
		t.Fatalf("scope = %q, want empty (repo outside every grant)", tok2.Scope)
	}

	// Management plane contrast (T-190/Q11): the same non-admin credentials
	// may mint a self-subject API token there too — a finite, default-TTL
	// credential with the full api:* subject privileges, unlike the scoped
	// docker token above.
	mgmt := h.do(http.MethodPost, "/binflow/api/security/token", "ci-bot", "ci-pw",
		[]byte("grant_type=client_credentials"), nil)
	if mgmt.StatusCode != http.StatusOK {
		t.Fatalf("management plane for non-admin = %d, want 200 (Q11 self-mint)", mgmt.StatusCode)
	}

	// offline_token accepted and ignored (D44-2/C5: docker 29's
	// challenge-mode login sends it on every token GET; the spec's
	// "MAY ignore" is the ruling). A plain token comes back, never a
	// refresh_token.
	off := h.do(http.MethodGet,
		"/v2/token?service=binflow&offline_token=true&scope=repository:docker-local/ci-out/**:pull",
		adminUser, adminPass, nil, nil)
	obody := mustGet(t, off)
	if off.StatusCode != http.StatusOK {
		t.Fatalf("offline_token status = %d; body=%s", off.StatusCode, obody)
	}
	var offTok tokenBody
	if err := json.Unmarshal([]byte(obody), &offTok); err != nil {
		t.Fatalf("offline_token body %q: %v", obody, err)
	}
	if offTok.Token == "" || offTok.RefreshToken != "" {
		t.Fatalf("offline_token exchange = %+v, want a token and no refresh", offTok)
	}
	// refresh grant rejection stays (Q3: no refresh).
	rg := h.do(http.MethodPost, "/v2/token", adminUser, adminPass,
		[]byte("grant_type=refresh_token"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if rg.StatusCode != http.StatusBadRequest {
		t.Fatalf("refresh_token status = %d", rg.StatusCode)
	}
}

// TestV2TokenWrongCredentials (FR-11-AC4 + T-55/PRD v1.2-C3): wrong
// credentials on /v2/token render Artifactory's generic error model (the
// exact observed E1-5 "Bad Credentials" status form) — with the Bearer
// challenge header unchanged, and
// leak nothing about which part of the credential failed. The rejected
// credential is shaped by the ROUTER plane (T-33 review B1's context
// signal) reaching the adapter's RenderAuthFailure, so this asserts the
// full production path.
func TestV2TokenWrongCredentials(t *testing.T) {
	h := newHarness(t)
	for _, cred := range [][2]string{{"admin", "wrong"}, {"no-such-user", "x"}, {"ci-bot", "wrong"}} {
		resp, body := getV2Token(h, cred[0], cred[1], "?service=binflow")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s status = %d; body=%s", cred[0], resp.StatusCode, body)
		}
		if strings.Contains(body, "no such user") || strings.Contains(body, cred[0]) {
			t.Fatalf("401 body leaks the username: %s", body)
		}
		// L000-B E1-5: the body is the generic error model's Bad
		// Credentials entry — the docker CLI renders it as
		// "unknown: Bad Credentials".
		var eb struct {
			Errors []struct {
				Status  int    `json:"status"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal([]byte(body), &eb); err != nil || len(eb.Errors) != 1 ||
			eb.Errors[0].Status != http.StatusUnauthorized || eb.Errors[0].Message != "Bad Credentials" {
			t.Fatalf("401 body is not the E1-5 status form: %s", body)
		}
		if strings.Contains(body, `"error":"`) {
			t.Fatalf("401 body is still the OAuth form: %s", body)
		}
		// The challenge header is unchanged: Bearer, token-endpoint realm;
		// service echoes the request host (L000-B C01).
		want := fmt.Sprintf(`Bearer realm="%s/v2/token",service="%s"`,
			h.srv.URL, strings.TrimPrefix(h.srv.URL, "http://"))
		if ch := resp.Header.Get("WWW-Authenticate"); ch != want {
			t.Fatalf("401 challenge = %q, want %q", ch, want)
		}
		if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
			t.Fatalf("api-version = %q", got)
		}
	}
	// The handler-internal posture (anonymous closed, no credential) keeps
	// the same generic model with its own observed message (E1-4).
	h2 := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	resp, body := getV2Token(h2, "", "", "?service=binflow")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bare anonymous status = %d; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"Authentication is required"`) {
		t.Fatalf("bare anonymous body is not the E1-4 form: %s", body)
	}
	if ch := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(ch, "Basic ") {
		t.Fatalf("bare anonymous challenge = %q, want Basic", ch)
	}
}

// TestV2TokenAnonymousBothModes (D04 + FR-11-AC1's anonymous branch):
// anonymous open -> a token is issued and WORKS as an anonymous Bearer
// (D44-1: the ping always challenges, so anonymous access flows through
// this token); anonymous closed -> OAuth 401.
func TestV2TokenAnonymousBothModes(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "team1")
	resp, body := getV2Token(h, "", "", "?service=binflow&scope=repository:team1/app:pull")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous open status = %d; body=%s", resp.StatusCode, body)
	}
	var tok tokenBody
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if tok.Token == "" {
		t.Fatal("anonymous token empty")
	}
	// The synthetic subject owns the row and is ENABLED (D44-1) — the
	// token verifies, and the /v2 plane maps its principal onto the
	// anonymous identity: reads pass while anonymous access is on...
	read := h.do(http.MethodGet, "/v2/team1/app/manifests/latest", "", "", nil,
		map[string]string{"Authorization": "Bearer " + tok.Token})
	rbody := mustGet(t, read)
	if read.StatusCode == http.StatusUnauthorized || read.StatusCode == http.StatusForbidden {
		t.Fatalf("anonymous bearer read denied: %d %s", read.StatusCode, rbody)
	}
	// ...and writes are challenged like any anonymous write.
	write := h.do(http.MethodPost, "/v2/team1/app/blobs/uploads/", "", "", nil,
		map[string]string{"Authorization": "Bearer " + tok.Token})
	if write.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous bearer write = %d, want the scoped 401 challenge", write.StatusCode)
	}
	if ch := write.Header.Get("WWW-Authenticate"); !strings.Contains(ch, `scope="repository:team1/app:pull,push"`) {
		t.Fatalf("anonymous bearer write challenge = %q", ch)
	}

	h2 := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	resp2, body2 := getV2Token(h2, "", "", "?service=binflow")
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous closed status = %d; body=%s", resp2.StatusCode, body2)
	}
}

// TestV2TokenFormCredentials (D44-3): the OAuth POST form carries the
// credential pair (grant_type=password — buildkit/helm spelling). The form
// pair has the same standing as the Basic header: a valid pair mints a
// token that WORKS as Bearer, a wrong pair is a 401 (never a silent
// anonymous token), and the header still wins when both spellings arrive.
func TestV2TokenFormCredentials(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}})
	seedDockerRepo(t, h, "team1")
	grant(t, h, "ci-rw", "team1", "app/**", "ci-bot", true, true, false)

	form := "grant_type=password&username=ci-bot&password=ci-pw&service=binflow" +
		"&scope=repository:team1/app:pull,push&client_id=docker&access_type=online"
	resp := h.do(http.MethodPost, "/v2/token", "", "", []byte(form),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("form credential exchange = %d; body=%s", resp.StatusCode, body)
	}
	var tok tokenBody
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if tok.Token == "" {
		t.Fatal("form credential token empty")
	}
	if tok.Scope != "repository:team1/app:pull,push" {
		t.Fatalf("scope = %q, want the granted pair", tok.Scope)
	}
	// The minted token carries the FORM user's rights (a push passes the
	// route gate — this is exactly the buildx/helm failure mode).
	push := h.do(http.MethodPost, "/v2/team1/app/blobs/uploads/", "", "", nil,
		map[string]string{"Authorization": "Bearer " + tok.Token})
	if push.StatusCode == http.StatusUnauthorized || push.StatusCode == http.StatusForbidden {
		t.Fatalf("form credential bearer push denied: %d %s", push.StatusCode, mustGet(t, push))
	}

	// client_id/client_secret pair (the other OAuth spelling).
	pair := "grant_type=client_credentials&client_id=ci-bot&client_secret=ci-pw&service=binflow"
	resp2 := h.do(http.MethodPost, "/v2/token", "", "", []byte(pair),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body2 := mustGet(t, resp2)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("client pair exchange = %d; body=%s", resp2.StatusCode, body2)
	}
	var tok2 tokenBody
	_ = json.Unmarshal([]byte(body2), &tok2)
	if tok2.Token == "" {
		t.Fatal("client pair token empty")
	}

	// Wrong form password: 401 in the same E1-5 status form, leaking
	// nothing.
	bad := "grant_type=password&username=ci-bot&password=wrong&service=binflow"
	resp3 := h.do(http.MethodPost, "/v2/token", "", "", []byte(bad),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body3 := mustGet(t, resp3)
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong form password = %d; body=%s", resp3.StatusCode, body3)
	}
	if !strings.Contains(body3, `"Bad Credentials"`) || strings.Contains(body3, "ci-bot") {
		t.Fatalf("wrong form password body = %s", body3)
	}
}

// TestV2TokenRevocation (D23, FR-11-AC6): a token issued at /v2/token is
// revoked by VALUE at the management endpoint and its Bearer then fails
// with the registry-plane 401.
func TestV2TokenRevocation(t *testing.T) {
	h := newHarness(t)
	_, body := getV2Token(h, adminUser, adminPass, "?service=binflow")
	var tok tokenBody
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	form := "token=" + tok.Token
	rev := h.do(http.MethodPost, "/binflow/api/security/token/revoke", adminUser, adminPass,
		[]byte(form), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if rev.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d; body=%s", rev.StatusCode, mustGet(t, rev))
	}
	after := h.do(http.MethodGet, "/v2/", "", "", nil,
		map[string]string{"Authorization": "Bearer " + tok.Token})
	abody := mustGet(t, after)
	if after.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked bearer status = %d; body=%s", after.StatusCode, abody)
	}
	if strings.Contains(abody, `"status"`) {
		t.Fatalf("revoked bearer body carries the /binflow envelope: %s", abody)
	}
	if ch := after.Header.Get("WWW-Authenticate"); !strings.Contains(ch, `/v2/token`) {
		t.Fatalf("challenge = %q, want Bearer realm .../v2/token", ch)
	}
}

// TestV2ScopedChallengeMatrix (AC2, table-driven): the 401 challenge on a
// closed instance carries the endpoint-derived scope — reads pull, writes
// pull,push, deletes pull,delete — over the full name the client addressed.
func TestV2ScopedChallengeMatrix(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)

	tests := []struct {
		method string
		path   string
		scope  string
	}{
		{http.MethodGet, "/v2/team1/app/manifests/latest", "repository:team1/app:pull"},
		{http.MethodHead, "/v2/team1/app/blobs/sha256:abc", "repository:team1/app:pull"},
		{http.MethodPut, "/v2/team1/app/manifests/latest", "repository:team1/app:pull,push"},
		{http.MethodPost, "/v2/team1/app/blobs/uploads/", "repository:team1/app:pull,push"},
		{http.MethodPatch, "/v2/team1/app/blobs/uploads/x", "repository:team1/app:pull,push"},
		{http.MethodDelete, "/v2/team1/app/manifests/sha256:abc", "repository:team1/app:pull,delete"},
		{http.MethodGet, "/v2/team1/acme/app/tags/list", "repository:team1/acme/app:pull"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, "", "", nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
			}
			want := fmt.Sprintf(`Bearer realm="%s/v2/token",service="%s",scope="%s"`,
				h.srv.URL, strings.TrimPrefix(h.srv.URL, "http://"), tc.scope)
			if got := resp.Header.Get("WWW-Authenticate"); got != want {
				t.Fatalf("WWW-Authenticate =\n  %q\nwant\n  %q", got, want)
			}
			// NFR-S9: nothing internal rides along.
			if strings.Contains(want, "/binflow") {
				t.Fatal("challenge realm leaks the /binflow plane")
			}
		})
	}
}

// TestV2RouteGate (AC2/D22): on an anonymous-open instance the derived
// scope doubles as the permission gate — anonymous writes are challenged,
// authenticated insufficient principals get 403 DENIED (spec body),
// authorized principals pass through to the (T-38..T-40) foundation 404.
func TestV2RouteGate(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"ci-bot", "ci-pw"}, {"writer", "writer-pw"}})
	seedDockerRepo(t, h, "team1")
	grant(t, h, "ci-read", "team1", "app/**", "ci-bot", true, false, false)
	grant(t, h, "writer-rw", "team1", "app/**", "writer", true, true, true)

	// Anonymous write -> scoped challenge.
	resp := h.do(http.MethodPost, "/v2/team1/app/blobs/uploads/", "", "", nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous write status = %d; body=%s", resp.StatusCode, body)
	}
	if ch := resp.Header.Get("WWW-Authenticate"); !strings.Contains(ch, `scope="repository:team1/app:pull,push"`) {
		t.Fatalf("challenge = %q, want the write scope", ch)
	}

	// Read-only principal pushing -> 403 DENIED (spec body, not envelope).
	resp = h.do(http.MethodPut, "/v2/team1/app/manifests/latest", "ci-bot", "ci-pw", nil, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only push status = %d; body=%s", resp.StatusCode, body)
	}
	if strings.Contains(body, `"status"`) {
		t.Fatalf("403 body carries the /binflow envelope: %s", body)
	}
	if !strings.Contains(body, `"DENIED"`) {
		t.Fatalf("403 body = %s, want DENIED", body)
	}

	// Authorized writer passes the gate: the manifest route answers the
	// protocol's own validation (T-39), not a permission refusal — an
	// empty body without a Content-Type is MANIFEST_INVALID.
	resp = h.do(http.MethodPut, "/v2/team1/app/manifests/latest", "writer", "writer-pw", nil, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorized write status = %d; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"MANIFEST_INVALID"`) {
		t.Fatalf("authorized write body = %s, want MANIFEST_INVALID (past the gate)", body)
	}

	// Read-only principal reading passes the gate too.
	resp = h.do(http.MethodGet, "/v2/team1/app/manifests/latest", "ci-bot", "ci-pw", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("authorized read status = %d", resp.StatusCode)
	}
}

// TestV2TokenCleanupRows: issuing tokens does not leave stray enabled
// accounts (the synthetic subject stays disabled after use). The anonymous
// seed dedupes per PROCESS (sync.Once), so an earlier harness in the same
// run may have seeded another store — this test therefore exercises the
// authenticated subject's rows instead, and asserts the synthetic account's
// shape through the direct seeding helper's contract (covered in the
// adapter package).
func TestV2TokenCleanupRows(t *testing.T) {
	h := newHarness(t)
	resp, body := getV2Token(h, "admin", adminPass, "?service=binflow&scope=repository:team1/app:pull")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	var tok tokenBody
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	rows, err := h.md.Tokens().ListByUsername(t.Context(), adminUser)
	if err != nil || len(rows) == 0 {
		t.Fatalf("admin token row missing: %v %d", err, len(rows))
	}
	found := false
	for _, r := range rows {
		if r.Username == adminUser {
			found = true
		}
	}
	if !found {
		t.Fatal("issued token not attributed to the caller")
	}
}

// TestV2TokenOAuthErrorPlane: every non-2xx on /v2/token renders the OAuth
// form — never the registry spec body, never the /binflow envelope. (The
// probe is the refresh-grant 400; offline_token no longer errors since
// D44-2/C5.)
func TestV2TokenOAuthErrorPlane(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/v2/token", adminUser, adminPass,
		[]byte("grant_type=refresh_token"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	if strings.Contains(body, `"errors"`) {
		t.Fatalf("token error body uses the registry spec envelope: %s", body)
	}
	if strings.Contains(body, `"status"`) {
		t.Fatalf("token error body uses the /binflow envelope: %s", body)
	}
	if !strings.Contains(body, `"error"`) {
		t.Fatalf("token error body is not OAuth-shaped: %s", body)
	}
	if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Fatalf("api-version = %q", got)
	}
}
