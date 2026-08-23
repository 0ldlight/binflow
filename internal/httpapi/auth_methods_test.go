// T-179 acceptance surface, HTTP arm 2: GET /binflow/api/v1/auth/methods —
// the public capability map of the authentication entry points (AC 5, three
// wiring states), its anonymous posture on a closed instance, the route shape
// (GET only), and whoami's source per authentication arm (AC 4 / FR-56-AC1
// H36: local, oidc and ldap each report their provider).

package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// methodsOf fetches and decodes GET /binflow/api/v1/auth/methods.
func methodsOf(t *testing.T, tsURL string) (int, map[string]bool) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, tsURL+"/binflow/api/v1/auth/methods", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET auth/methods: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	var body map[string]bool
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
	}
	return resp.StatusCode, body
}

// newBothOnStack assembles the third wiring state: OIDC (mock IdP, the same
// construction newOIDCStack uses) AND LDAP (mock directory, the same
// construction newLDAPStack uses) on one service — cmd's both-enabled shape.
func newBothOnStack(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	idp := newMockOIDCIDP(t)
	oidcProv, err := auth.NewOIDCProvider(ctx, &auth.OIDCConfig{
		IssuerURL:   idp.issuer,
		ClientID:    "test-client-id",
		RedirectURL: "http://binflow.example.com/binflow/api/v1/oidc/callback",
	}, auth.NewOIDCResolver(md.Users()))
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	dir := &mockDirectory{users: map[string]mockDirUser{
		"jdoe": {dn: userDN("jdoe"), password: "jdoe-secret"},
	}}
	ldapProv, err := auth.NewLDAPProvider(&auth.LDAPConfig{
		Enabled: true, URL: "ldap://ldap.example.com:389", BaseDN: mockBaseDN,
		UserFilter: "(uid=%s)", UserIDAttr: "uid", PoolSize: 2,
		ConnectTimeout: 2 * time.Second, RequestTimeout: 5 * time.Second,
	}, auth.NewLDAPResolver(md.Users()),
		ldapTestDialer(dir, ldapDialFaults{}))
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess).
		WithOIDC(oidcProv, auth.NewUserCreator(md.Users())).
		WithLDAP(ldapProv)
	s := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc,
		Metadata: md, Repos: md.Repos(), OIDC: oidcProv,
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestAuthMethodsThreeStates (AC 5): the endpoint's booleans mirror the
// assembly's wiring — all off (default), OIDC only, both on. password is
// constant true (the seeded admin always exists).
func TestAuthMethodsThreeStates(t *testing.T) {
	for _, tc := range []struct {
		name string
		ts   func(t *testing.T) string // returns the server's base URL
		want map[string]bool
	}{
		{
			"all off (default assembly)",
			func(t *testing.T) string { return newHarness(t).srv.URL },
			map[string]bool{"password": true, "oidc": false, "ldap": false},
		},
		{
			"oidc only",
			func(t *testing.T) string { return newOIDCStack(t, "").ts.URL },
			map[string]bool{"password": true, "oidc": true, "ldap": false},
		},
		{
			"both on",
			func(t *testing.T) string { return newBothOnStack(t).URL },
			map[string]bool{"password": true, "oidc": true, "ldap": true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := methodsOf(t, tc.ts(t))
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			if len(body) != len(tc.want) {
				t.Errorf("body = %v, want exactly the keys of %v", body, tc.want)
			}
			for k, want := range tc.want {
				if got := body[k]; got != want {
					t.Errorf("body[%q] = %v, want %v", k, got, want)
				}
			}
		})
	}
}

// TestAuthMethodsAnonymousOnClosedInstance: the endpoint stays anonymous even
// when security.anonymous_access=false — it is product self-description in
// the /healthz posture (the login page needs it before any credential
// exists), and a 401 here would lock the console out of its own entry-point
// map.
func TestAuthMethodsAnonymousOnClosedInstance(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false }, nil)
	status, body := methodsOf(t, h.srv.URL)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 on an anonymous-read-off instance", status)
	}
	if !body["password"] || body["oidc"] || body["ldap"] {
		t.Errorf("body = %v, want password-only on the default wiring", body)
	}
}

// TestAuthMethodsRouteShape: GET is the only verb with a route — every other
// spelling falls to the E-26 envelope 404 (same posture as the rest of the
// reserved-slot endpoints).
func TestAuthMethodsRouteShape(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/binflow/api/v1/auth/methods"},
		{http.MethodPut, "/binflow/api/v1/auth/methods"},
		{http.MethodGet, "/binflow/api/v1/auth/methods/extra"},
		{http.MethodGet, "/binflow/api/v1/auth"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, h.srv.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
		})
	}
}

// whoamiViaPassword drives one password/form login and returns the whoami
// body's (username, source) pair.
func whoamiViaPassword(t *testing.T, tsURL, user, pass string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, err := http.NewRequest(http.MethodPost, tsURL+"/binflow/api/v1/session", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build login: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieSessionName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("login carried no session cookie")
	}
	return whoamiWithCookie(t, tsURL, cookie.Value)
}

// whoamiWithCookie resolves GET /api/v1/session with the given session
// cookie and returns the body's (username, source) pair.
func whoamiWithCookie(t *testing.T, tsURL, sessionID string) (string, string) {
	t.Helper()
	get, err := http.NewRequest(http.MethodGet, tsURL+"/binflow/api/v1/session", nil)
	if err != nil {
		t.Fatalf("build whoami: %v", err)
	}
	get.AddCookie(&http.Cookie{Name: auth.CookieSessionName, Value: sessionID})
	who, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	defer func() { _ = who.Body.Close() }()
	if who.StatusCode != http.StatusOK {
		t.Fatalf("whoami status = %d, want 200", who.StatusCode)
	}
	var parsed struct {
		Username string `json:"username"`
		Source   string `json:"source"`
	}
	if err := json.NewDecoder(who.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode whoami: %v", err)
	}
	return parsed.Username, parsed.Source
}

// TestWhoamiSourcePerArm (AC 4 / H36): the whoami body's source field
// reports the owning provider for each of the three authentication arms —
// local password, LDAP bind, OIDC callback.
func TestWhoamiSourcePerArm(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		h := newHarness(t)
		user, src := whoamiViaPassword(t, h.srv.URL, adminUser, adminPass)
		if user != adminUser || src != "local" {
			t.Errorf("whoami = %s/%s, want %s/local", user, src, adminUser)
		}
	})

	t.Run("ldap", func(t *testing.T) {
		st := newLDAPStack(t, true)
		user, src := whoamiViaPassword(t, st.ts.URL, "jdoe", "jdoe-secret")
		if user != "jdoe" || src != "ldap" {
			t.Errorf("whoami = %s/%s, want jdoe/ldap", user, src)
		}
	})

	t.Run("oidc", func(t *testing.T) {
		st := newOIDCStack(t, "")
		st.idp.tokenClaims = st.idp.claimsFor("test-sub-123", "ssouser", `"developers"`)
		state, tx := oidcLoginLeg(t, st.ts.URL, "")
		cb := "/binflow/api/v1/oidc/callback?code=auth-code-1&state=" + url.QueryEscape(state)
		resp := oidcDo(t, st.ts.URL, http.MethodGet, cb, tx)
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("callback status = %d, want 302", resp.StatusCode)
		}
		sess := responseCookie(resp, auth.CookieSessionName)
		if sess == nil || sess.value == "" {
			t.Fatalf("callback carried no session cookie: %v", resp.Header.Values("Set-Cookie"))
		}
		user, src := whoamiWithCookie(t, st.ts.URL, sess.value)
		if user != "ssouser" || src != "oidc" {
			t.Errorf("whoami = %s/%s, want ssouser/oidc", user, src)
		}
	})
}
