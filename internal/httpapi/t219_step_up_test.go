// T-219 acceptance surface (M7 FR-68, V21~V26; ADR-0027 Accepted revision):
// the step-up gate on POST /api/security/token — the three non-admin session
// arms (local, LDAP, OIDC) × both switch states × the two error codes, the
// exempt arms (Basic CI, admin session, Bearer, docker /v2/token), the OIDC
// mint-grant flow end to end (prompt=login re-auth -> grant -> mint ->
// single-use), and the token.issue audit dimensions. The Q11 guardrails run
// unchanged in the T-190 suite; the default-off legs here pin the
// byte-identical posture (V25).

package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// ---- assembly ----

// t219Stack is a three-session-arm assembly (local, LDAP, OIDC — all
// non-admin) plus the seeded admin row, shaped the way cmd wires it. The
// step-up switch rides cfg so both states run the same code paths.
type t219Stack struct {
	ts      *httptest.Server
	md      metadata.Store
	idp     *mockOIDCIDP
	authSvc *auth.Service
	sess    map[string]string // arm -> live session cookie value
}

func newT219Stack(t *testing.T, stepUp bool) *t219Stack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	cfg.Auth.TokenStepUp = stepUp

	// LDAP arm over the shared mock directory (jdoe, non-admin).
	dir := &mockDirectory{
		users: map[string]mockDirUser{
			"jdoe": {dn: userDN("jdoe"), password: "jdoe-ldap-pw"},
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

	// OIDC arms (Bearer for the exempt leg, browser flow for the session
	// arm) over the shared mock IdP.
	idp := newMockOIDCIDP(t)
	idp.tokenClaims = idp.claimsFor("t219-oidc-sub", "ssouser", "")
	oidcProvider, err := auth.NewOIDCProvider(ctx, &auth.OIDCConfig{
		IssuerURL:    idp.issuer,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://binflow.example.com/binflow/api/v1/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		UserClaim:    "preferred_username",
		GroupClaim:   "groups",
	}, auth.NewLDAPResolver(md.Users()))
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess).
		WithOIDC(oidcProvider, auth.NewUserCreator(md.Users())).
		WithLDAP(ldapProvider)

	// A local non-admin with a password (metadata.Open seeds the admin row).
	hash, err := auth.HashPassword("loc-pw")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := metadata.Now()
	if err := md.Users().Create(ctx, &metadata.User{
		Username: "loc", PasswordHash: hash, IsAdmin: false, Enabled: true,
		Provider: "local", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed loc: %v", err)
	}

	s := httpapi.New(httpapi.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		Passwords: authSvc,
		Tokens:    authSvc,
		OIDC:      oidcProvider,
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &t219Stack{ts: ts, md: md, idp: idp, authSvc: authSvc, sess: map[string]string{}}
}

// sessionOf returns the arm's live session cookie, logging in lazily once.
func (st *t219Stack) sessionOf(t *testing.T, arm string) string {
	t.Helper()
	if c, ok := st.sess[arm]; ok {
		return c
	}
	var resp *http.Response
	switch arm {
	case "local":
		resp = ldapLogin(t, st.ts.URL, "loc", "loc-pw")
	case "ldap":
		resp = ldapLogin(t, st.ts.URL, "jdoe", "jdoe-ldap-pw")
	case "admin":
		resp = ldapLogin(t, st.ts.URL, adminUser, adminPass)
	case "oidc":
		// The browser flow ends in a 302 whose Set-Cookie carries the
		// session (oidcLoginFlow asserts the redirect itself).
		resp = st.oidcLoginFlow(t)
	default:
		t.Fatalf("unknown arm %q", arm)
	}
	if arm != "oidc" && resp.StatusCode != http.StatusOK {
		t.Fatalf("%s login = %d, body=%s", arm, resp.StatusCode, mustGet(t, resp))
	}
	c := responseCookie(resp, auth.CookieSessionName)
	if c == nil || c.value == "" {
		t.Fatalf("%s login minted no session cookie: %v", arm, resp.Header.Values("Set-Cookie"))
	}
	st.sess[arm] = c.value
	return c.value
}

// oidcLoginFlow runs the browser login pair end to end and returns the
// callback response (whose Set-Cookie carries the session).
func (st *t219Stack) oidcLoginFlow(t *testing.T) *http.Response {
	t.Helper()
	state, tx := oidcLoginLeg(t, st.ts.URL, "")
	cb := "/binflow/api/v1/oidc/callback?code=t219-code&state=" + url.QueryEscape(state)
	resp := oidcDo(t, st.ts.URL, http.MethodGet, cb, tx)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("oidc callback = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	return resp
}

// t219Mint posts one token create as the given arm (session cookie or Basic
// credential) with optional step-up form fields, and drains the body.
func (st *t219Stack) t219Mint(t *testing.T, arm, stepUpPassword, stepUpGrant string) (int, string) {
	t.Helper()
	form := url.Values{"grant_type": {"client_credentials"}}
	if stepUpPassword != "" {
		form.Set("step_up_password", stepUpPassword)
	}
	if stepUpGrant != "" {
		form.Set("step_up_grant", stepUpGrant)
	}
	req, err := http.NewRequest(http.MethodPost, st.ts.URL+"/binflow/api/security/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build mint request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	switch {
	case strings.HasPrefix(arm, "basic:"):
		user, pass, _ := strings.Cut(strings.TrimPrefix(arm, "basic:"), ":")
		req.SetBasicAuth(user, pass)
	case arm == "bearer":
		req.Header.Set("Authorization", "Bearer "+st.idp.signIDToken(st.idp.claimsFor("t219-oidc-sub", "ssouser", "")))
	default:
		req.Header.Set("Cookie", auth.CookieSessionName+"="+st.sessionOf(t, arm))
	}
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return resp.StatusCode, mustGet(t, resp)
}

// t219IssueEvents returns the token.issue audit rows of one actor.
func (st *t219Stack) t219IssueEvents(t *testing.T, actor string) []audit.Event {
	t.Helper()
	lg := audit.New(st.md, true)
	page, err := lg.Query(context.Background(), audit.Filter{Actor: actor, Action: audit.ActionTokenIssue, Limit: 100})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	return page.Events
}

// ---- V25: switch off keeps the Q11 posture byte-for-byte ----

// TestT219SwitchOffKeepsQ11Posture: with the default-off switch every
// session arm mints WITHOUT any second credential (the pre-T-219 behavior,
// which the T-190 suite also keeps pinning).
func TestT219SwitchOffKeepsQ11Posture(t *testing.T) {
	st := newT219Stack(t, false)
	for _, arm := range []string{"local", "ldap", "oidc"} {
		t.Run(arm, func(t *testing.T) {
			code, body := st.t219Mint(t, arm, "", "")
			if code != http.StatusOK {
				t.Fatalf("mint = %d, body=%s", code, body)
			}
		})
	}
	// The step-up fields are absent from the audit detail on this path (the
	// loop's local mint is the one audited event for loc).
	events := st.t219IssueEvents(t, "loc")
	if len(events) != 1 {
		t.Fatalf("token.issue events = %d, want 1", len(events))
	}
	if strings.Contains(events[0].Detail, "step_up") {
		t.Errorf("switch-off audit detail carries step_up: %s", events[0].Detail)
	}
}

// ---- V21/V22: the password legs (local, LDAP) under the switch on ----

// TestT219PasswordLegsMatrix: local and LDAP sessions owe step_up_password —
// missing -> 401 step_up_required, wrong -> 401 step_up_invalid, right ->
// 200 with a usable token (V22's ping). The wrong-leg field (a grant)
// does not satisfy the password leg.
func TestT219PasswordLegsMatrix(t *testing.T) {
	st := newT219Stack(t, true)
	cases := []struct {
		name     string
		arm      string
		pass     string
		grant    string
		want     int
		wantCode string
	}{
		{name: "local missing password", arm: "local", want: http.StatusUnauthorized, wantCode: "step_up_required"},
		{name: "local wrong password", arm: "local", pass: "wrong", want: http.StatusUnauthorized, wantCode: "step_up_invalid"},
		{name: "local right password", arm: "local", pass: "loc-pw", want: http.StatusOK},
		{name: "local wrong-leg grant does not satisfy", arm: "local", grant: "whatever-grant", want: http.StatusUnauthorized, wantCode: "step_up_required"},
		{name: "ldap missing password", arm: "ldap", want: http.StatusUnauthorized, wantCode: "step_up_required"},
		{name: "ldap wrong password", arm: "ldap", pass: "wrong", want: http.StatusUnauthorized, wantCode: "step_up_invalid"},
		{name: "ldap right password", arm: "ldap", pass: "jdoe-ldap-pw", want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := st.t219Mint(t, tc.arm, tc.pass, tc.grant)
			if code != tc.want {
				t.Fatalf("mint = %d, want %d, body=%s", code, tc.want, body)
			}
			if tc.wantCode != "" {
				var oe struct {
					Error            string `json:"error"`
					ErrorDescription string `json:"error_description"`
				}
				if err := json.Unmarshal([]byte(body), &oe); err != nil {
					t.Fatalf("body %q is not the OAuth error form: %v", body, err)
				}
				if oe.Error != tc.wantCode {
					t.Errorf("error = %q, want %q", oe.Error, tc.wantCode)
				}
				if oe.ErrorDescription == "" {
					t.Errorf("error_description empty in %q", body)
				}
			}
		})
	}
}

// TestT219MintedTokenUsable: the token a step-up mint returns works on the
// API plane exactly like any other (V22's second half).
func TestT219MintedTokenUsable(t *testing.T) {
	st := newT219Stack(t, true)
	code, body := st.t219Mint(t, "local", "loc-pw", "")
	if code != http.StatusOK {
		t.Fatalf("mint = %d, body=%s", code, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(body), &tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("mint body %q carries no access_token", body)
	}
	req, _ := http.NewRequest(http.MethodGet, st.ts.URL+"/binflow/api/system/ping", nil)
	req.Header.Set("X-JFrog-Art-Api", tok.AccessToken)
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ping with minted token = %d", resp.StatusCode)
	}
}

// ---- V23: the OIDC mint-grant leg end to end ----

// TestT219OIDCGrantFlow: session re-authentication through
// ?purpose=step_up -> prompt=login on the authorize URL -> callback pays a
// single-use grant bound to {user, session} -> mint 200 -> replay 401
// step_up_invalid. Also: the grant is refused over a DIFFERENT session, and
// a password does not satisfy the OIDC leg.
func TestT219OIDCGrantFlow(t *testing.T) {
	st := newT219Stack(t, true)
	sess := st.sessionOf(t, "oidc")

	// The mint without a grant is the V21 posture.
	code, body := st.t219Mint(t, "oidc", "", "")
	if code != http.StatusUnauthorized || !strings.Contains(body, "step_up_required") {
		t.Fatalf("grantless mint = %d %q, want 401 step_up_required", code, body)
	}
	// A password is the wrong credential for the OIDC leg.
	code, body = st.t219Mint(t, "oidc", "some-password", "")
	if code != http.StatusUnauthorized || !strings.Contains(body, "step_up_required") {
		t.Fatalf("password mint on the oidc leg = %d %q, want 401 step_up_required", code, body)
	}
	// A garbage grant is step_up_invalid.
	code, body = st.t219Mint(t, "oidc", "", "garbage-grant")
	if code != http.StatusUnauthorized || !strings.Contains(body, "step_up_invalid") {
		t.Fatalf("garbage grant = %d %q, want 401 step_up_invalid", code, body)
	}

	// Re-auth: the init demands the session and forces prompt=login.
	sessCookie := auth.CookieSessionName + "=" + sess
	resp := oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/oidc/login?purpose=step_up", sessCookie)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("step-up login = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := loc.Query().Get("prompt"); got != "login" {
		t.Errorf("authorize URL prompt = %q, want login", got)
	}
	state := loc.Query().Get("state")
	tx := "binflow_oidc_tx=" + parseSetCookie(resp.Header.Get("Set-Cookie")).value

	cb := "/binflow/api/v1/oidc/callback?code=t219-reauth&state=" + url.QueryEscape(state)
	resp = oidcDo(t, st.ts.URL, http.MethodGet, cb, sessCookie+"; "+tx)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("step-up callback = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	redirect := resp.Header.Get("Location")
	const fragPrefix = "/binflow/ui/#step_up_grant="
	if !strings.HasPrefix(redirect, fragPrefix) {
		t.Fatalf("callback Location = %q, want the grant fragment under %q", redirect, fragPrefix)
	}
	grant := strings.TrimPrefix(redirect, fragPrefix)
	if grant == "" {
		t.Fatal("grant fragment carries no value")
	}
	// The step-up callback must NOT mint a new session.
	if c := responseCookie(resp, auth.CookieSessionName); c != nil {
		t.Errorf("step-up callback minted a session cookie: %v", resp.Header.Values("Set-Cookie"))
	}

	// Mint inside the window: 200, and the audit carries the method.
	code, body = st.t219Mint(t, "oidc", "", grant)
	if code != http.StatusOK {
		t.Fatalf("mint with fresh grant = %d, body=%s", code, body)
	}
	events := st.t219IssueEvents(t, "ssouser")
	if len(events) == 0 {
		t.Fatal("no token.issue event for ssouser")
	}
	var detail struct {
		StepUp       bool   `json:"step_up"`
		StepUpMethod string `json:"step_up_method"`
	}
	if err := json.Unmarshal([]byte(events[len(events)-1].Detail), &detail); err != nil {
		t.Fatalf("audit detail %q: %v", events[len(events)-1].Detail, err)
	}
	if !detail.StepUp || detail.StepUpMethod != "oidc_reauth" {
		t.Errorf("audit detail = %+v, want step_up=true method=oidc_reauth (%s)", detail, events[len(events)-1].Detail)
	}

	// Replay: the grant is dead.
	code, body = st.t219Mint(t, "oidc", "", grant)
	if code != http.StatusUnauthorized || !strings.Contains(body, "step_up_invalid") {
		t.Fatalf("replayed grant mint = %d %q, want 401 step_up_invalid", code, body)
	}
}

// TestT219GrantBoundToSession: a grant paid over session A cannot be spent
// over session B (the {username, session_id} binding of ADR-0027 decision 4)
// — a second login for the same OIDC identity is a different row.
func TestT219GrantBoundToSession(t *testing.T) {
	st := newT219Stack(t, true)
	sessA := st.sessionOf(t, "oidc")

	grantA := st.stepUpGrant(t, sessA)

	// A second, independent OIDC login (session B).
	resp := st.oidcLoginFlow(t)
	c := responseCookie(resp, auth.CookieSessionName)
	if c == nil {
		t.Fatal("second oidc login minted no session")
	}
	sessB := c.value

	req, _ := http.NewRequest(http.MethodPost, st.ts.URL+"/binflow/api/security/token",
		strings.NewReader(url.Values{
			"grant_type":    {"client_credentials"},
			"step_up_grant": {grantA},
		}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", auth.CookieSessionName+"="+sessB)
	rsp, err := st.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("mint over session B: %v", err)
	}
	body := mustGet(t, rsp)
	if rsp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "step_up_invalid") {
		t.Fatalf("cross-session grant mint = %d %q, want 401 step_up_invalid", rsp.StatusCode, body)
	}
}

// stepUpGrant drives one purpose=step_up re-auth over the given session and
// returns the grant the redirect fragment carries.
func (st *t219Stack) stepUpGrant(t *testing.T, session string) string {
	t.Helper()
	sessCookie := auth.CookieSessionName + "=" + session
	resp := oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/oidc/login?purpose=step_up", sessCookie)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("step-up login = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	tx := "binflow_oidc_tx=" + parseSetCookie(resp.Header.Get("Set-Cookie")).value
	cb := "/binflow/api/v1/oidc/callback?code=reauth&state=" + url.QueryEscape(loc.Query().Get("state"))
	resp = oidcDo(t, st.ts.URL, http.MethodGet, cb, sessCookie+"; "+tx)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("step-up callback = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	redirect := resp.Header.Get("Location")
	const fragPrefix = "/binflow/ui/#step_up_grant="
	if !strings.HasPrefix(redirect, fragPrefix) {
		t.Fatalf("callback Location = %q, want grant fragment", redirect)
	}
	return strings.TrimPrefix(redirect, fragPrefix)
}

// TestT219StepUpLoginDemandsSession: the purpose=step_up init without an
// active session is refused before the IdP round trip; an unknown purpose is
// a 400, never a silently downgraded login.
func TestT219StepUpLoginDemandsSession(t *testing.T) {
	st := newT219Stack(t, true)
	resp := oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/oidc/login?purpose=step_up", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sessionless step-up init = %d, want 401", resp.StatusCode)
	}
	resp = oidcDo(t, st.ts.URL, http.MethodGet, "/binflow/api/v1/oidc/login?purpose=bogus", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown purpose = %d, want 400", resp.StatusCode)
	}
}

// ---- V24: the exempt arms ----

// TestT219ExemptArms: with the switch ON, every non-session arm mints
// untouched — the CI Basic posture, the admin session, the OIDC Bearer arm
// (first factor already fresh) — and the admin session's audit carries no
// step_up dimension.
func TestT219ExemptArms(t *testing.T) {
	st := newT219Stack(t, true)
	for name, arm := range map[string]string{
		"basic ci":       "basic:loc:loc-pw",
		"admin session":  "admin",
		"admin basic":    fmt.Sprintf("basic:%s:%s", adminUser, adminPass),
		"oidc bearer id": "bearer",
	} {
		t.Run(name, func(t *testing.T) {
			code, body := st.t219Mint(t, arm, "", "")
			if code != http.StatusOK {
				t.Fatalf("mint = %d, body=%s", code, body)
			}
		})
	}
	events := st.t219IssueEvents(t, adminUser)
	if len(events) == 0 {
		t.Fatal("no token.issue event for the admin")
	}
	if strings.Contains(events[len(events)-1].Detail, "step_up") {
		t.Errorf("exempt-arm audit detail carries step_up: %s", events[len(events)-1].Detail)
	}
}

// TestT219DockerTokenPlaneUntouched: the /v2/token exchange (docker login's
// first leg) is Basic-backed by construction and owes no step-up — proven on
// a step-up-ON stack over the full harness assembly.
func TestT219DockerTokenPlaneUntouched(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Auth.TokenStepUp = true },
		[][2]string{{"ci", "ci-pw"}})
	seedDockerRepo(t, h, "team1")

	resp, body := getV2Token(h, "ci", "ci-pw", "?service=binflow&scope=repository:team1/app:pull,push")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/v2/token = %d, body=%s", resp.StatusCode, body)
	}
	var tok tokenBody
	if err := json.Unmarshal([]byte(body), &tok); err != nil || tok.Token == "" {
		t.Fatalf("/v2/token body %q carries no token", body)
	}
	// And the mint over Basic on the same stack is exempt too.
	resp2 := h.do(http.MethodPost, "/binflow/api/security/token",
		"ci", "ci-pw", []byte("grant_type=client_credentials"), map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		})
	mbody := mustGet(h.t, resp2)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("Basic mint on docker stack = %d, body=%s", resp2.StatusCode, mbody)
	}
}

// ---- V26: the audit dimension on the password leg ----

// TestT219AuditPasswordMethod: the local leg's token.issue carries
// step_up=true + step_up_method=password.
func TestT219AuditPasswordMethod(t *testing.T) {
	st := newT219Stack(t, true)
	code, body := st.t219Mint(t, "local", "loc-pw", "")
	if code != http.StatusOK {
		t.Fatalf("mint = %d, body=%s", code, body)
	}
	events := st.t219IssueEvents(t, "loc")
	if len(events) != 1 {
		t.Fatalf("token.issue events = %d, want 1", len(events))
	}
	var detail struct {
		StepUp       bool   `json:"step_up"`
		StepUpMethod string `json:"step_up_method"`
		Source       string `json:"source"`
	}
	if err := json.Unmarshal([]byte(events[0].Detail), &detail); err != nil {
		t.Fatalf("audit detail %q: %v", events[0].Detail, err)
	}
	if !detail.StepUp || detail.StepUpMethod != "password" {
		t.Errorf("audit detail = %+v, want step_up=true method=password", detail)
	}
	if detail.Source != "local" {
		t.Errorf("audit source = %q, want local (the arm dimension stands)", detail.Source)
	}
}

// TestT219StepUpJSONBody: the JSON projection of the mint body carries the
// step-up fields with the same semantics as the form shape.
func TestT219StepUpJSONBody(t *testing.T) {
	st := newT219Stack(t, true)
	cases := []struct {
		name     string
		body     string
		want     int
		wantCode string
	}{
		{name: "json password missing", body: `{"grant_type":"client_credentials"}`, want: http.StatusUnauthorized, wantCode: "step_up_required"},
		{name: "json password wrong", body: `{"grant_type":"client_credentials","step_up_password":"nope"}`, want: http.StatusUnauthorized, wantCode: "step_up_invalid"},
		{name: "json password right", body: `{"grant_type":"client_credentials","step_up_password":"loc-pw"}`, want: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, st.ts.URL+"/binflow/api/security/token",
				strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Cookie", auth.CookieSessionName+"="+st.sessionOf(t, "local"))
			resp, err := st.ts.Client().Do(req)
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			body := mustGet(t, resp)
			if resp.StatusCode != tc.want {
				t.Fatalf("mint = %d, want %d, body=%s", resp.StatusCode, tc.want, body)
			}
			if tc.wantCode != "" && !strings.Contains(body, tc.wantCode) {
				t.Errorf("body %q lacks error %q", body, tc.wantCode)
			}
		})
	}
}
