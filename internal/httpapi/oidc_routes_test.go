// T-157 acceptance surface (PRD FR-54 / OD-01/OD-02, H24~H26, H29): the
// OIDC browser login pair — the 302 redirect contract of /api/v1/oidc/login
// (Authorization Code Grant + PKCE S256), the full callback round trip
// (state/verifier checks, code exchange, ID Token verification, session
// issuance, 302 /binflow/ui/), the disabled-instance 404 posture, and the
// callback's failure table.

package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// ---- mock identity provider ----

// mockOIDCIDP is a minimal OpenID Connect IdP serving discovery, JWKS and
// the token endpoint (the same construction internal/auth/oidc_test.go
// uses). The token endpoint signs a configurable claim set into an ID Token
// and captures the exchange's form values so tests can assert the PKCE
// verifier actually traveled.
type mockOIDCIDP struct {
	srv    *httptest.Server
	priv   *rsa.PrivateKey
	keyID  string
	issuer string

	// tokenClaims overrides the default claim set; tokenErrorCode makes the
	// token endpoint answer the OAuth error shape.
	tokenClaims    string
	tokenErrorCode string
	lastForm       url.Values
}

func newMockOIDCIDP(t *testing.T) *mockOIDCIDP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	m := &mockOIDCIDP{priv: priv, keyID: "test-key-1"}
	m.srv = httptest.NewServer(m)
	m.issuer = m.srv.URL
	t.Cleanup(m.Close)
	return m
}

func (m *mockOIDCIDP) Close() { m.srv.Close() }

func (m *mockOIDCIDP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                m.issuer,
			"authorization_endpoint":                m.issuer + "/auth",
			"token_endpoint":                        m.issuer + "/token",
			"jwks_uri":                              m.issuer + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/keys":
		set := &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: m.priv.Public(), KeyID: m.keyID, Algorithm: "RS256", Use: "sig",
		}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	case "/token":
		m.serveToken(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (m *mockOIDCIDP) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	m.lastForm = r.PostForm
	if m.tokenErrorCode != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": m.tokenErrorCode})
		return
	}
	claims := m.tokenClaims
	if claims == "" {
		claims = m.defaultClaims()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "mock-access-token",
		"token_type":   "Bearer",
		"id_token":     m.signIDToken(claims),
		"expires_in":   3600,
	})
}

func (m *mockOIDCIDP) defaultClaims() string {
	now := time.Now()
	return fmt.Sprintf(`{
		"iss": %q,
		"aud": "test-client-id",
		"sub": "test-sub-123",
		"preferred_username": "ssouser",
		"email": "ssouser@example.com",
		"groups": ["developers"],
		"iat": %d,
		"exp": %d
	}`, m.issuer, now.Unix(), now.Add(time.Hour).Unix())
}

func (m *mockOIDCIDP) claimsFor(sub, username, groups string) string {
	now := time.Now()
	return fmt.Sprintf(`{
		"iss": %q,
		"aud": "test-client-id",
		"sub": %q,
		"preferred_username": %q,
		"groups": [%s],
		"iat": %d,
		"exp": %d
	}`, m.issuer, sub, username, groups, now.Unix(), now.Add(time.Hour).Unix())
}

func (m *mockOIDCIDP) signIDToken(claims string) string {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: m.priv},
		&jose.SignerOptions{ExtraHeaders: map[jose.HeaderKey]any{
			jose.HeaderKey("kid"): m.keyID,
		}},
	)
	if err != nil {
		panic("oidc mock: signer: " + err.Error())
	}
	sig, err := signer.Sign([]byte(claims))
	if err != nil {
		panic("oidc mock: sign: " + err.Error())
	}
	jwt, err := sig.CompactSerialize()
	if err != nil {
		panic("oidc mock: serialize: " + err.Error())
	}
	return jwt
}

// ---- assembly ----

// storeUserCreator adapts metadata.UserStore onto the auth auto-create seam
// (the role userCreatorAdapter plays inside the auth package; it is
// unexported there, so the test rebuilds the same shape).
type storeUserCreator struct{ store metadata.UserStore }

func (c storeUserCreator) Create(ctx context.Context, p auth.NewUserParams) error {
	now := metadata.Now()
	return c.store.Create(ctx, &metadata.User{
		Username: p.Username, PasswordHash: p.PasswordHash, IsAdmin: p.IsAdmin,
		Enabled: p.Enabled, CreatedAt: now, UpdatedAt: now,
		Provider: string(p.Provider), ProviderID: p.ProviderID,
	})
}

// oidcStack is an OIDC-enabled assembly shaped the way cmd will wire it:
// the SAME provider object feeds the auth service's Bearer arm (WithOIDC)
// and the HTTP login seam (Deps.OIDC).
type oidcStack struct {
	ts  *httptest.Server
	md  metadata.Store
	idp *mockOIDCIDP
}

// newOIDCStack builds the stack. adminGroup maps the IdP group of the same
// name onto the admin flag ("" disables the mapping).
func newOIDCStack(t *testing.T, adminGroup string) *oidcStack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	idp := newMockOIDCIDP(t)
	provider, err := auth.NewOIDCProvider(ctx, &auth.OIDCConfig{
		IssuerURL:    idp.issuer,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://binflow.example.com/binflow/api/v1/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		UserClaim:    "preferred_username",
		GroupClaim:   "groups",
		AdminGroup:   adminGroup,
	}, auth.NewLDAPResolver(md.Users()))
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess).
		WithOIDC(provider, storeUserCreator{md.Users()})
	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		OIDC:     provider,
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &oidcStack{ts: ts, md: md, idp: idp}
}

// noRedirectClient refuses to follow the flow's redirects so each leg is
// asserted on its own response.
func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// oidcDo issues one request against the stack without following redirects.
func oidcDo(t *testing.T, tsURL, method, path, cookie string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, tsURL+path, nil)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// oidcLoginLeg runs the login leg and returns the flow transaction: the
// state to echo in the callback and the tx cookie header value.
func oidcLoginLeg(t *testing.T, tsURL string, extraCookie string) (state, txCookie string) {
	t.Helper()
	resp := oidcDo(t, tsURL, http.MethodGet, "/binflow/api/v1/oidc/login", extraCookie)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login leg status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	state = loc.Query().Get("state")
	if state == "" {
		t.Fatal("authorization URL carries no state")
	}
	raw := resp.Header.Get("Set-Cookie")
	if raw == "" {
		t.Fatal("login response carries no tx Set-Cookie")
	}
	c := parseSetCookie(raw)
	if c.value == "" {
		t.Fatal("tx cookie carries no value")
	}
	return state, "binflow_oidc_tx=" + c.value
}

// txVerifier splits a tx cookie header value into the state and the PKCE
// verifier it carries (the value is state.verifier[.purpose]; the optional
// purpose field is dropped here).
func txVerifier(tx string) (state, verifier string) {
	v := strings.TrimPrefix(tx, "binflow_oidc_tx=")
	s, rest, _ := strings.Cut(v, ".")
	verifier, _, _ = strings.Cut(rest, ".")
	return s, verifier
}

// responseCookie finds one named cookie among a response's possibly many
// Set-Cookie lines (the callback emits two: the cleared tx cookie and the
// minted session cookie). nil when absent.
func responseCookie(resp *http.Response, name string) *sessionCookie {
	for _, raw := range resp.Header.Values("Set-Cookie") {
		c := parseSetCookie(raw)
		if strings.HasPrefix(raw, name+"=") {
			return c
		}
	}
	return nil
}

// ---- OD-01: the login redirect contract ----

func TestOIDCLoginRedirectContract(t *testing.T) {
	st := newOIDCStack(t, "admins")

	resp := oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/oidc/login", "")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if want := st.idp.issuer + "/auth"; loc.Scheme+"://"+loc.Host+loc.Path != want {
		t.Fatalf("Location address = %q, want %q", loc.Scheme+"://"+loc.Host+loc.Path, want)
	}
	q := loc.Query()
	for _, tc := range []struct{ key, want string }{
		{"response_type", "code"},
		{"client_id", "test-client-id"},
		{"redirect_uri", "http://binflow.example.com/binflow/api/v1/oidc/callback"},
		{"scope", "openid profile email"},
		{"code_challenge_method", "S256"},
	} {
		if got := q.Get(tc.key); got != tc.want {
			t.Errorf("query %s = %q, want %q", tc.key, got, tc.want)
		}
	}
	if q.Get("state") == "" {
		t.Error("query state is empty")
	}
	if len(q.Get("code_challenge")) < 43 { // RFC 7636 minimum S256 challenge
		t.Errorf("code_challenge too short: %q", q.Get("code_challenge"))
	}

	// Transaction cookie contract: HttpOnly, SameSite=Lax, scoped to the
	// oidc segment, bounded lifetime.
	c := parseSetCookie(resp.Header.Get("Set-Cookie"))
	if c.attrs["httponly"] != "" {
		t.Errorf("tx cookie HttpOnly = %q, want present", c.attrs["httponly"])
	}
	if !strings.Contains(strings.ToLower(c.raw), "samesite=lax") {
		t.Errorf("tx cookie SameSite missing from %q", c.raw)
	}
	if got := c.attrs["path"]; got != "/binflow/api/v1/oidc" {
		t.Errorf("tx cookie Path = %q, want /binflow/api/v1/oidc", got)
	}
}

// ---- OD-02: the full callback round trip (H24) ----

func TestOIDCCallbackFullFlow(t *testing.T) {
	st := newOIDCStack(t, "admins")
	st.idp.tokenClaims = st.idp.claimsFor("test-sub-123", "ssouser", `"developers"`)

	state, tx := oidcLoginLeg(t, st.ts.URL, "")
	cb := "/binflow/api/v1/oidc/callback?code=auth-code-1&state=" + url.QueryEscape(state)
	resp := oidcDo(t, st.ts.URL, http.MethodGet, cb, tx)
	if resp.StatusCode != http.StatusFound {
		body := mustGet(t, resp)
		t.Fatalf("callback status = %d, want 302, body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/binflow/ui/" {
		t.Fatalf("callback Location = %q, want /binflow/ui/", got)
	}

	// Session cookie minted with the login contract, and the single-use tx
	// cookie cleared on the way out (two Set-Cookie lines on one response).
	sess := responseCookie(resp, auth.CookieSessionName)
	if sess == nil || sess.value == "" {
		t.Fatalf("callback carries no %s cookie: %q", auth.CookieSessionName, resp.Header.Values("Set-Cookie"))
	}
	if sess.attrs["httponly"] != "" || !strings.Contains(strings.ToLower(sess.raw), "samesite=lax") {
		t.Errorf("session cookie attributes missing: %q", sess.raw)
	}
	if c := responseCookie(resp, "binflow_oidc_tx"); c == nil || c.value != "" {
		t.Errorf("tx cookie not cleared: %+v", resp.Header.Values("Set-Cookie"))
	}

	// The PKCE verifier from the tx cookie is what reached the token
	// endpoint (RFC 7636: the IdP sees the verifier of THIS transaction).
	_, verifier := txVerifier(tx)
	if got := st.idp.lastForm.Get("code_verifier"); got != verifier {
		t.Errorf("token endpoint code_verifier = %q, want the tx cookie's %q", got, verifier)
	}

	// The session works on the management plane (H24: whoami answers the
	// OIDC-created identity).
	who := oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/session",
		auth.CookieSessionName+"="+sess.value)
	if who.StatusCode != http.StatusOK {
		body := mustGet(t, who)
		t.Fatalf("whoami status = %d, body=%s", who.StatusCode, body)
	}
	var body struct {
		Username string `json:"username"`
		Admin    bool   `json:"admin"`
	}
	if err := json.NewDecoder(who.Body).Decode(&body); err != nil {
		t.Fatalf("decode whoami: %v", err)
	}
	if body.Username != "ssouser" {
		t.Errorf("whoami username = %q, want ssouser", body.Username)
	}
	if body.Admin {
		t.Errorf("whoami admin = true, want false (developers is not the admin group)")
	}

	// First login auto-created the user row (H25/FR-54-AC2): provider=oidc,
	// provider_id=sub, no local password.
	u, err := st.md.Users().Get(context.Background(), "ssouser")
	if err != nil {
		t.Fatalf("auto-created user row missing: %v", err)
	}
	if u.Provider != "oidc" || u.ProviderID != "test-sub-123" || u.PasswordHash != "" {
		t.Errorf("user row = provider %q provider_id %q hash %q, want oidc/test-sub-123/empty",
			u.Provider, u.ProviderID, u.PasswordHash)
	}
	if !u.Enabled {
		t.Error("auto-created user is disabled")
	}
}

// TestOIDCCallbackAdminGroup: an AdminGroup member's first login lands with
// the admin flag mapped (FR-54-AC4/H27 posture at the login plane).
func TestOIDCCallbackAdminGroup(t *testing.T) {
	st := newOIDCStack(t, "admins")
	st.idp.tokenClaims = st.idp.claimsFor("sub-boss", "boss", `"admins"`)

	state, tx := oidcLoginLeg(t, st.ts.URL, "")
	resp := oidcDo(t, st.ts.URL, http.MethodGet,
		"/binflow/api/v1/oidc/callback?code=auth-code-1&state="+url.QueryEscape(state), tx)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	sess := responseCookie(resp, auth.CookieSessionName)
	if sess == nil {
		t.Fatalf("callback carries no session cookie: %q", resp.Header.Values("Set-Cookie"))
	}
	who := oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/session",
		auth.CookieSessionName+"="+sess.value)
	var body struct {
		Username string `json:"username"`
		Admin    bool   `json:"admin"`
	}
	if err := json.NewDecoder(who.Body).Decode(&body); err != nil {
		t.Fatalf("decode whoami: %v", err)
	}
	if body.Username != "boss" || !body.Admin {
		t.Errorf("whoami = %+v, want username=boss admin=true", body)
	}
}

// ---- failure table ----

func TestOIDCCallbackFailures(t *testing.T) {
	cases := []struct {
		name string
		// login runs the login leg first and threads its state through.
		login bool
		// tamperState corrupts the echoed state (CSRF/forgery posture).
		tamperState bool
		// dropTx omits the transaction cookie the login leg set.
		dropTx bool
		// rawTx replaces the transaction cookie with a malformed value.
		rawTx string
		// query carries extra/overriding query parameters.
		query url.Values
		// tokenErr makes the IdP's token endpoint answer the OAuth error.
		tokenErr string
		// claims overrides the signed ID Token claim set.
		claims string
		want   int
	}{
		{
			name:  "idp error parameter",
			query: url.Values{"error": {"access_denied"}},
			want:  http.StatusBadRequest,
		},
		{
			name:  "missing code and state",
			query: url.Values{},
			want:  http.StatusBadRequest,
		},
		{
			name:   "missing transaction cookie",
			login:  true,
			dropTx: true,
			query:  url.Values{"code": {"c1"}},
			want:   http.StatusBadRequest,
		},
		{
			name:  "malformed transaction cookie",
			login: true,
			rawTx: "binflow_oidc_tx=no-separator-here",
			query: url.Values{"code": {"c1"}},
			want:  http.StatusBadRequest,
		},
		{
			name:        "state mismatch",
			login:       true,
			tamperState: true,
			query:       url.Values{"code": {"c1"}},
			want:        http.StatusBadRequest,
		},
		{
			name:     "token endpoint failure",
			login:    true,
			query:    url.Values{"code": {"c1"}},
			tokenErr: "invalid_grant",
			want:     http.StatusBadGateway,
		},
		{
			name:   "id token rejected",
			login:  true,
			query:  url.Values{"code": {"c1"}},
			claims: "", // overridden below with a wrong-audience token
			want:   http.StatusUnauthorized,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newOIDCStack(t, "")
			if tc.tokenErr != "" {
				st.idp.tokenErrorCode = tc.tokenErr
			}
			if tc.name == "id token rejected" {
				st.idp.tokenClaims = strings.Replace(st.idp.defaultClaims(),
					`"aud": "test-client-id"`, `"aud": "some-other-client"`, 1)
			} else if tc.claims != "" {
				st.idp.tokenClaims = tc.claims
			}

			q := url.Values{}
			for k, v := range tc.query {
				q[k] = v
			}
			cookie := ""
			if tc.login {
				state, tx := oidcLoginLeg(t, st.ts.URL, "")
				if tc.tamperState {
					state += "x"
				}
				q.Set("state", state)
				if !tc.dropTx {
					cookie = tx
				}
			}
			if tc.rawTx != "" {
				cookie = tc.rawTx
			}
			path := "/binflow/api/v1/oidc/callback"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			resp := oidcDo(t, st.ts.URL, http.MethodGet, path, cookie)
			if resp.StatusCode != tc.want {
				body := mustGet(t, resp)
				t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, tc.want, body)
			}
			// The envelope shape is the unified errors[] form.
			var env struct {
				Errors []struct {
					Status  int    `json:"status"`
					Message string `json:"message"`
				} `json:"errors"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if len(env.Errors) != 1 || env.Errors[0].Status != tc.want {
				t.Errorf("error envelope = %+v, want one entry with status %d", env.Errors, tc.want)
			}
			// Every exit path clears the single-use transaction cookie.
			if c := responseCookie(resp, "binflow_oidc_tx"); c == nil || c.value != "" {
				t.Errorf("response does not clear the tx cookie: %q", resp.Header.Values("Set-Cookie"))
			}
		})
	}
}

// ---- H29/FR-54-AC6: a disabled instance does not expose the pair ----

func TestOIDCDisabledEndpoints404(t *testing.T) {
	h := newHarness(t) // the standard assembly wires no Deps.OIDC
	for _, path := range []string{"/binflow/api/v1/oidc/login", "/binflow/api/v1/oidc/callback"} {
		resp := h.do(http.MethodGet, path, "", "", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, resp.StatusCode)
		}
	}
	// The only verb with a route is GET; every other spelling falls to the
	// E-26 family too.
	resp := h.do(http.MethodPost, "/binflow/api/v1/oidc/login", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST login status = %d, want 404", resp.StatusCode)
	}
}

// ---- the T-91 B1 exemption, extended to the SSO entry points ----

// TestOIDCLoginSurvivesStaleSessionCookie: a presented-but-rejected
// binflow_session cookie (expired, revoked or tossed by a hostile sibling
// subdomain) must not lock the browser out of the SSO entry — the login leg
// answers 302, the callback still completes, exactly like POST /api/v1/session.
func TestOIDCLoginSurvivesStaleSessionCookie(t *testing.T) {
	st := newOIDCStack(t, "")
	stale := auth.CookieSessionName + "=tossed-garbage-cookie"

	resp := oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/oidc/login", stale)
	if resp.StatusCode != http.StatusFound {
		body := mustGet(t, resp)
		t.Fatalf("login with stale session cookie status = %d, want 302, body=%s",
			resp.StatusCode, body)
	}
	state, tx := oidcLoginLeg(t, st.ts.URL, stale)
	cb := "/binflow/api/v1/oidc/callback?code=c1&state=" + url.QueryEscape(state)
	resp = oidcDo(t, st.ts.URL, http.MethodGet, cb, stale+"; "+tx)
	if resp.StatusCode != http.StatusFound {
		body := mustGet(t, resp)
		t.Fatalf("callback with stale session cookie status = %d, want 302, body=%s",
			resp.StatusCode, body)
	}
}
