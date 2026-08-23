// T-190 acceptance surface (PRD M6 v1.2 §7 Q11, T-188 ruling — route A):
// POST /api/security/token opens to every authenticated caller. The matrix
// below runs the three authentication arms (local Basic, OIDC Bearer ID
// Token, LDAP web session) through one real stack:
//
//	AC1  three arms mint for themselves          -> 200, usable token
//	AC1  non-admin naming anyone else            -> 403 OAuth form
//	AC1  anonymous                               -> 401
//	AC2  non-admin TTL: 0 / over-cap / default-over-cap -> 401 invalid_request
//	AC2  config cap override (auth.token_nonadmin_max_ttl) honored
//	AC3  disabled owner row kills the minted token at verify time -> 401
//	AC4  token.issue audit carries actor, subject, source arm, TTL
//	AC5  revoke stays admin-only
//	AC6  admin unrestricted: never-expiring, over-cap, other subjects
//
// The OIDC and LDAP legs ride the same mocks the T-157 suites use
// (mockOIDCIDP, mockDirectory) so the arms exercised here are the real
// provider code paths, not principal fakes.

package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// t190Stack is a three-arm assembly: local accounts, the OIDC Bearer arm over
// a mock IdP, and the LDAP login fallback over a mock directory — plus the
// token endpoints and the audit store behind them.
type t190Stack struct {
	ts   *httptest.Server
	md   metadata.Store
	idp  *mockOIDCIDP
	db   string // sqlite file, for the disabled-row leg
	arm  map[string]t190Arm
	ldap string // jdoe's session cookie value
}

// t190Arm decorates one request with a specific authentication arm's
// credential and names the principal it must resolve to.
type t190Arm struct {
	name     string
	username string
	source   string // expected whoami source (the owning provider)
	decorate func(*http.Request)
}

// newT190Stack builds the stack. mutate adjusts the config before assembly
// (the cap-override test lowers TokenNonAdminMaxTTL).
func newT190Stack(t *testing.T, mutate func(*config.Config)) *t190Stack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	dbPath := dataDir + "/binflow.db"
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	if mutate != nil {
		mutate(cfg)
	}

	// OIDC Bearer arm: the same mock IdP + provider the T-157 suite uses,
	// without an admin group (the arms here are all non-admin).
	idp := newMockOIDCIDP(t)
	oidcProvider, err := auth.NewOIDCProvider(ctx, &auth.OIDCConfig{
		IssuerURL:    idp.issuer,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://binflow.example.com/binflow/api/v1/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		UserClaim:    "preferred_username",
		GroupClaim:   "groups",
	}, auth.NewOIDCResolver(md.Users()))
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	// LDAP login arm: the mock directory from the T-157 LDAP suite (jdoe is
	// its non-admin member).
	dir := &mockDirectory{
		users: map[string]mockDirUser{
			"jdoe": {dn: userDN("jdoe"), password: "jdoe-secret"},
		},
		groups: map[string][]string{"developers": {"jdoe"}},
	}
	ldapProvider, err := auth.NewLDAPProvider(&auth.LDAPConfig{
		Enabled:        true,
		URL:            "ldap://ldap.example.com:389",
		BaseDN:         mockBaseDN,
		UserFilter:     "(uid=%s)",
		UserIDAttr:     "uid",
		GroupFilter:    "(&(objectClass=posixGroup)(memberUid=%s))",
		GroupNameAttr:  "cn",
		PoolSize:       2,
		ConnectTimeout: 2 * time.Second,
		RequestTimeout: 5 * time.Second,
	}, auth.NewLDAPResolver(md.Users()),
		ldapTestDialer(dir, ldapDialFaults{}))
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess).
		WithOIDC(oidcProvider, auth.NewUserCreator(md.Users())).
		WithLDAP(ldapProvider)

	// One local non-admin account for the local arm.
	hash, err := auth.HashPassword("ci-pw")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := metadata.Now()
	if err := md.Users().Create(ctx, &metadata.User{
		Username: "ci-bot", PasswordHash: hash, IsAdmin: false, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed ci-bot: %v", err)
	}

	s := httpapi.New(httpapi.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		Passwords: authSvc,
		Tokens:    authSvc,
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// The OIDC arm's credential: a signed ID Token for a non-admin identity.
	idToken := idp.signIDToken(idp.claimsFor("t190-oidc-sub", "ssouser", ""))

	st := &t190Stack{ts: ts, md: md, idp: idp, db: dbPath}
	st.arm = map[string]t190Arm{
		"local": {
			name: "local", username: "ci-bot", source: "local",
			decorate: func(r *http.Request) { r.SetBasicAuth("ci-bot", "ci-pw") },
		},
		"oidc": {
			name: "oidc", username: "ssouser", source: "oidc",
			decorate: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+idToken)
			},
		},
		"ldap": {
			name: "ldap", username: "jdoe", source: "ldap",
			decorate: func(r *http.Request) {
				r.Header.Set("Cookie", auth.CookieSessionName+"="+st.ldapSession(t))
			},
		},
	}
	return st
}

// ldapSession logs jdoe in through the real login endpoint and caches the
// session cookie value (one row, reused across the arm's requests).
func (st *t190Stack) ldapSession(t *testing.T) string {
	t.Helper()
	if st.ldap != "" {
		return st.ldap
	}
	resp := ldapLogin(t, st.ts.URL, "jdoe", "jdoe-secret")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("jdoe ldap login = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	c := responseCookie(resp, auth.CookieSessionName)
	if c == nil || c.value == "" {
		t.Fatalf("ldap login minted no session cookie: %v", resp.Header.Values("Set-Cookie"))
	}
	st.ldap = c.value
	return st.ldap
}

// t190Do issues one decorated request and drains the body.
func (st *t190Stack) t190Do(t *testing.T, method, path string, arm t190Arm, body string) (*http.Response, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, st.ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if arm.decorate != nil {
		arm.decorate(req)
	}
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp, mustGet(t, resp)
}

// t190Whoami resolves the current principal of one token value (any arm).
func (st *t190Stack) t190Whoami(t *testing.T, token string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, st.ts.URL+"/binflow/api/v1/session", nil)
	if err != nil {
		t.Fatalf("build whoami: %v", err)
	}
	req.Header.Set("X-JFrog-Art-Api", token)
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, body, ""
	}
	var who struct {
		Username string `json:"username"`
		Source   string `json:"source"`
	}
	if err := json.Unmarshal([]byte(body), &who); err != nil {
		t.Fatalf("whoami body %q: %v", body, err)
	}
	return resp.StatusCode, who.Username, who.Source
}

// t190IssueEvents returns the token.issue audit rows of one actor.
func (st *t190Stack) t190IssueEvents(t *testing.T, actor string) []audit.Event {
	t.Helper()
	lg := audit.New(st.md, true)
	page, err := lg.Query(context.Background(), audit.Filter{Actor: actor, Action: audit.ActionTokenIssue, Limit: 100})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	return page.Events
}

// ---- AC1 + AC4: three arms mint for themselves; audit carries the arm ----

// TestT190ThreeArmsSelfMint: every authenticated arm mints a working
// self-subject token, and each mint lands a token.issue audit row naming the
// actor, the subject, the TTL and the SOURCE ARM (Q11 guardrail 4).
func TestT190ThreeArmsSelfMint(t *testing.T) {
	st := newT190Stack(t, nil)
	for _, arm := range []t190Arm{st.arm["local"], st.arm["oidc"], st.arm["ldap"]} {
		t.Run(arm.name, func(t *testing.T) {
			resp, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token", arm,
				"grant_type=client_credentials")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
			}
			var tok tokenResp
			if err := json.Unmarshal([]byte(body), &tok); err != nil {
				t.Fatalf("body %q: %v", body, err)
			}
			if tok.AccessToken == "" || tok.TokenID == 0 || tok.TokenType != "Bearer" {
				t.Fatalf("token incomplete: %+v", tok)
			}
			// The default TTL is finite and within the default cap.
			if tok.ExpiresIn == nil || *tok.ExpiresIn <= 0 {
				t.Fatalf("expires_in = %v, want a positive default TTL", tok.ExpiresIn)
			}

			// AC1's usable-token leg: the minted token authenticates its
			// SUBJECT (the caller) on the management read plane.
			code, user, source := st.t190Whoami(t, tok.AccessToken)
			if code != http.StatusOK {
				t.Fatalf("minted token whoami = %d (%s)", code, user)
			}
			if user != arm.username {
				t.Errorf("token subject = %q, want the caller %q", user, arm.username)
			}
			if source != arm.source {
				t.Errorf("whoami source = %q, want %q (token principals carry the owning provider)", source, arm.source)
			}

			// AC4: the audit event names actor/subject/source/TTL.
			events := st.t190IssueEvents(t, arm.username)
			if len(events) == 0 {
				t.Fatalf("no token.issue audit event for %s", arm.username)
			}
			var detail map[string]any
			if err := json.Unmarshal([]byte(events[0].Detail), &detail); err != nil {
				t.Fatalf("detail %q: %v", events[0].Detail, err)
			}
			if got := detail["subject"]; got != arm.username {
				t.Errorf("audit subject = %v, want %q", got, arm.username)
			}
			if got := detail["source"]; got != arm.source {
				t.Errorf("audit source = %v, want %q (the authentication arm)", got, arm.source)
			}
			if ttl, ok := detail["ttl_seconds"].(float64); !ok || ttl <= 0 {
				t.Errorf("audit ttl_seconds = %v, want positive", detail["ttl_seconds"])
			}
			if fp, ok := detail["fingerprint"].(string); !ok || len(fp) != 8 {
				t.Errorf("audit fingerprint = %v, want the 8-hex digest", detail["fingerprint"])
			}
		})
	}
}

// ---- AC1/AC2: the negative table (403 / 401 / TTL cap) ----

// TestT190NonAdminNegativeTable: the Q11 draft AC's negative legs — other
// subject 403 on all three arms, anonymous 401, never-expiring 401,
// over-cap 401, and an in-cap positive control.
func TestT190NonAdminNegativeTable(t *testing.T) {
	st := newT190Stack(t, nil)

	type tc struct {
		name   string
		arm    t190Arm
		form   string
		want   int
		descIn string // substring expected in error_description ("" = none)
	}
	cases := []tc{
		{
			name: "anonymous is challenged", arm: t190Arm{},
			form: "grant_type=client_credentials", want: http.StatusUnauthorized,
		},
		{
			name: "never-expiring is 401 invalid_request", arm: st.arm["local"],
			form: "grant_type=client_credentials&expires_in=0", want: http.StatusUnauthorized,
			descIn: "can only create user token with expires in larger than 0",
		},
		{
			name: "over the default cap is 401 invalid_request", arm: st.arm["local"],
			form: "grant_type=client_credentials&expires_in=31536001", want: http.StatusUnauthorized,
			descIn: "smaller than 31536000 seconds (requested: 31536001)",
		},
		{
			name: "negative stays the 400 param error for everyone", arm: st.arm["local"],
			form: "grant_type=client_credentials&expires_in=-5", want: http.StatusBadRequest,
			descIn: "Invalid expires_in value: -5",
		},
		{
			name: "in-cap explicit TTL is the positive control", arm: st.arm["local"],
			form: "grant_type=client_credentials&expires_in=3600", want: http.StatusOK,
		},
	}
	// The other-subject 403 runs on ALL three arms (three arms share one
	// permission model, Q11).
	for _, arm := range []t190Arm{st.arm["local"], st.arm["oidc"], st.arm["ldap"]} {
		cases = append(cases, tc{
			name: arm.name + " arm naming another user is 403", arm: arm,
			form: "grant_type=client_credentials&username=admin", want: http.StatusForbidden,
			descIn: "administrator privileges required",
		})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token", c.arm, c.form)
			if resp.StatusCode != c.want {
				t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, c.want, body)
			}
			if c.want == http.StatusOK {
				return
			}
			// Every failure keeps the OAuth error body (the token plane's
			// uniform non-2xx contract).
			var oe struct {
				Error            string `json:"error"`
				ErrorDescription string `json:"error_description"`
			}
			if err := json.Unmarshal([]byte(body), &oe); err != nil {
				t.Fatalf("body %q is not the OAuth error shape: %v", body, err)
			}
			if oe.Error != "invalid_request" {
				t.Errorf("error = %q, want invalid_request (body %s)", oe.Error, body)
			}
			if c.descIn != "" && !strings.Contains(oe.ErrorDescription, c.descIn) {
				t.Errorf("error_description = %q, want it to contain %q", oe.ErrorDescription, c.descIn)
			}
		})
	}
}

// ---- AC2: the config cap (K9) ----

// TestT190ConfigCapOverride: a lowered auth.token_nonadmin_max_ttl binds
// non-admin requests — including the DEFAULT TTL leg (an absent expires_in
// inherits the default, and the cap bounds the effective lifetime) — while
// the admin stays unrestricted.
func TestT190ConfigCapOverride(t *testing.T) {
	st := newT190Stack(t, func(c *config.Config) {
		c.Auth.TokenNonAdminMaxTTL = 2 * time.Hour
	})

	// Exactly at the lowered cap: fine.
	resp, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token", st.arm["local"],
		"grant_type=client_credentials&expires_in=7200")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("at-cap status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	// One second over: 401 naming the configured cap.
	resp, body = st.t190Do(t, http.MethodPost, "/binflow/api/security/token", st.arm["local"],
		"grant_type=client_credentials&expires_in=7201")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("over-cap status = %d, want 401; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "smaller than 7200 seconds (requested: 7201)") {
		t.Errorf("body = %q, want the configured-cap wording", body)
	}
	// Absent expires_in inherits the 30d default, which now exceeds the cap:
	// the effective TTL is what the guardrail judges.
	resp, body = st.t190Do(t, http.MethodPost, "/binflow/api/security/token", st.arm["local"],
		"grant_type=client_credentials")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("default-TTL-over-cap status = %d, want 401; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "(requested: 2592000)") {
		t.Errorf("body = %q, want the default TTL (2592000s) as the requested value", body)
	}
	// The cap never binds the admin: never-expiring stays mintable.
	admin := t190Arm{name: "admin", decorate: func(r *http.Request) { r.SetBasicAuth(adminUser, adminPass) }}
	resp, body = st.t190Do(t, http.MethodPost, "/binflow/api/security/token", admin,
		"grant_type=client_credentials&expires_in=0")
	if resp.StatusCode != http.StatusOK || strings.Contains(body, "expires_in") {
		t.Fatalf("admin never-expiring = %d %q, want 200 with expires_in omitted", resp.StatusCode, body)
	}
}

// ---- AC6: admin unrestricted ----

// TestT190AdminUnrestricted: the admin keeps the full M1 surface — minting
// for another subject (audited with actor != subject), never-expiring tokens
// and lifetimes beyond the non-admin cap.
func TestT190AdminUnrestricted(t *testing.T) {
	st := newT190Stack(t, nil)
	admin := t190Arm{name: "admin", decorate: func(r *http.Request) { r.SetBasicAuth(adminUser, adminPass) }}

	t.Run("mints for another subject", func(t *testing.T) {
		resp, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token", admin,
			"grant_type=client_credentials&username=ci-bot&expires_in=3600")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
		}
		var tok tokenResp
		if err := json.Unmarshal([]byte(body), &tok); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if code, user, _ := st.t190Whoami(t, tok.AccessToken); code != http.StatusOK || user != "ci-bot" {
			t.Fatalf("minted-for-other whoami = %d %q, want ci-bot", code, user)
		}
		// The audit row separates actor from subject (Q11 guardrail 4).
		events := st.t190IssueEvents(t, adminUser)
		if len(events) == 0 {
			t.Fatal("no token.issue audit event for the admin mint")
		}
		var detail map[string]any
		if err := json.Unmarshal([]byte(events[0].Detail), &detail); err != nil {
			t.Fatalf("detail %q: %v", events[0].Detail, err)
		}
		if got := detail["subject"]; got != "ci-bot" {
			t.Errorf("audit subject = %v, want ci-bot (actor stays %q)", got, adminUser)
		}
	})

	t.Run("lifetime beyond the cap", func(t *testing.T) {
		resp, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token", admin,
			"grant_type=client_credentials&expires_in=3153600000")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (cap binds non-admins only); body=%s", resp.StatusCode, body)
		}
	})

	t.Run("never-expiring", func(t *testing.T) {
		resp, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token", admin,
			"grant_type=client_credentials&expires_in=0")
		if resp.StatusCode != http.StatusOK || strings.Contains(body, "expires_in") {
			t.Fatalf("status = %d body = %q, want 200 with expires_in omitted", resp.StatusCode, body)
		}
	})
}

// ---- AC5: revoke stays admin-only ----

// TestT190RevokeStaysAdminOnly: the list/revoke family keeps its admin gate —
// a non-admin (any arm) revoking meets the same 403 as before T-190.
func TestT190RevokeStaysAdminOnly(t *testing.T) {
	st := newT190Stack(t, nil)
	for _, arm := range []t190Arm{st.arm["local"], st.arm["oidc"], st.arm["ldap"]} {
		t.Run(arm.name, func(t *testing.T) {
			resp, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token/revoke", arm,
				"token=anything")
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (admin-only, unchanged); body=%s", resp.StatusCode, body)
			}
			if !strings.Contains(body, `"invalid_request"`) {
				t.Errorf("body = %q, want the OAuth error shape on the token family", body)
			}
		})
	}

	// The admin's revocation semantics are untouched (C21c idempotence).
	admin := t190Arm{name: "admin", decorate: func(r *http.Request) { r.SetBasicAuth(adminUser, adminPass) }}
	_, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token", admin, "grant_type=client_credentials")
	var tok tokenResp
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("admin mint body %q: %v", body, err)
	}
	_, body = st.t190Do(t, http.MethodPost, "/binflow/api/security/token/revoke", admin,
		"token="+tok.AccessToken)
	if strings.TrimSpace(body) != "Token revoked" {
		t.Fatalf("admin revoke body = %q, want 'Token revoked'", body)
	}
	_, body = st.t190Do(t, http.MethodPost, "/binflow/api/security/token/revoke", admin,
		"token="+tok.AccessToken)
	if strings.TrimSpace(body) != "Token not found" {
		t.Fatalf("replay revoke body = %q, want the idempotent 'Token not found'", body)
	}
}

// ---- AC3: verify-time guard on the owner row ----

// TestT190DisabledOwnerTokenRejected: Q11 guardrail 3 — the token's owner
// row is re-checked on every verification. Disabling the row (there is no
// REST surface for it yet, so the leg drives the store directly, the way an
// operator's migration would) invalidates the minted token with a 401, and
// so does deleting the row outright.
func TestT190DisabledOwnerTokenRejected(t *testing.T) {
	st := newT190Stack(t, nil)

	resp, body := st.t190Do(t, http.MethodPost, "/binflow/api/security/token", st.arm["local"],
		"grant_type=client_credentials&expires_in=3600")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mint status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var tok tokenResp
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if code, _, _ := st.t190Whoami(t, tok.AccessToken); code != http.StatusOK {
		t.Fatal("fresh token does not authenticate; setup broken")
	}

	// Disable the owner row.
	db, err := sql.Open("sqlite", st.db)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`UPDATE users SET enabled = 0 WHERE username = ?`, "ci-bot"); err != nil {
		t.Fatalf("disable ci-bot: %v", err)
	}
	if code, respBody, _ := st.t190Whoami(t, tok.AccessToken); code != http.StatusUnauthorized {
		t.Fatalf("disabled-owner token whoami = %d (%s), want 401", code, respBody)
	}

	// Re-enable, re-mint, then DELETE the row: the "owner unavailable" leg
	// of the same guardrail.
	if _, err := db.Exec(`UPDATE users SET enabled = 1 WHERE username = ?`, "ci-bot"); err != nil {
		t.Fatalf("re-enable ci-bot: %v", err)
	}
	_, body = st.t190Do(t, http.MethodPost, "/binflow/api/security/token", st.arm["local"],
		"grant_type=client_credentials&expires_in=3600")
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("re-mint body %q: %v", body, err)
	}
	if _, err := db.Exec(`DELETE FROM users WHERE username = ?`, "ci-bot"); err != nil {
		t.Fatalf("delete ci-bot: %v", err)
	}
	if code, respBody, _ := st.t190Whoami(t, tok.AccessToken); code != http.StatusUnauthorized {
		t.Fatalf("deleted-owner token whoami = %d (%s), want 401", code, respBody)
	}
}
