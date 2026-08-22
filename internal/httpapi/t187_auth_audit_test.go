// T-187 acceptance surface (T-174 D7/D8 + O-2), HTTP side: the audit trail
// of authentication failures. The matrix walks the three arms' failure
// forms and asserts the RECORDED events — action name auth.failed (the M6
// PRD vocabulary, no dual spelling), method (local/oidc/ldap) and reason
// (bad_credentials / user_not_found / user_disabled / provider_error /
// tls_handshake / bad_request) inside the detail object — plus the FR-56-AC3
// query anchor (?action=auth.failed) and the O-2 WARN for provider-side
// failures.

package httpapi_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	ldap "github.com/go-ldap/ldap/v3"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// ---- a knob-able password-login stack (local + optional LDAP arm) ----

// t187Stack is newLDAPStack's shape plus the two failure knobs the O-2 legs
// need: a dialer that cannot reach the directory, and a StartTLS upgrade the
// directory refuses.
type t187Stack struct {
	ts *httptest.Server
	md metadata.Store
}

// startTLSFailConn makes the upgrade fail while the rest of the in-memory
// directory keeps working (the conn mock of ldap_session_test.go hardwires
// StartTLS to nil).
type startTLSFailConn struct{ *mockLDAPConn }

func (c startTLSFailConn) StartTLS(*tls.Config) error {
	return errors.New("ldap: LDAP Result Code 2 \"Protocol Error\": StartTLS not supported")
}

// t187StackOpt mutates the stack while it is being assembled.
type t187StackOpt func(*t187StackCfg)

type t187StackCfg struct {
	wireLDAP    bool
	startTLS    bool
	dialErr     error
	upgradeFail bool
}

func newT187Stack(t *testing.T, opts ...t187StackOpt) *t187Stack {
	t.Helper()
	var cfg t187StackCfg
	for _, o := range opts {
		o(&cfg)
	}

	ctx := context.Background()
	dataDir := t.TempDir()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	// Seed: one admin (audit endpoint access), one plain local user, one
	// disabled LDAP-owned row whose directory bind would succeed.
	for _, u := range []struct {
		name, hash, provider, providerID string
		admin, enabled                   bool
	}{
		{"root", mustHash(t, "root-pw"), "local", "", true, true},
		{"alice", mustHash(t, "alice-pw"), "local", "", false, true},
		{"frozen", "", "ldap", userDN("frozen"), false, false},
	} {
		now := metadata.Now()
		if err := md.Users().Create(ctx, &metadata.User{
			Username: u.name, PasswordHash: u.hash, IsAdmin: u.admin, Enabled: u.enabled,
			CreatedAt: now, UpdatedAt: now, Provider: u.provider, ProviderID: u.providerID,
		}); err != nil {
			t.Fatalf("seed user %s: %v", u.name, err)
		}
	}

	dir := &mockDirectory{
		users: map[string]mockDirUser{
			"jdoe":   {dn: userDN("jdoe"), password: "jdoe-secret"},
			"frozen": {dn: userDN("frozen"), password: "frozen-secret"},
		},
		groups: map[string][]string{},
	}

	appCfg := config.Defaults()
	appCfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, appCfg.Security.AnonymousAccess)
	if cfg.wireLDAP {
		provider, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled:        true,
			URL:            "ldap://ldap.example.com:389",
			BaseDN:         mockBaseDN,
			UserFilter:     "(uid=%s)",
			UserIDAttr:     "uid",
			PoolSize:       2,
			ConnectTimeout: 2 * time.Second,
			RequestTimeout: 5 * time.Second,
			StartTLS:       cfg.startTLS,
		}, auth.NewLDAPResolver(md.Users()),
			auth.LDAPDialer(func(_ context.Context, _ string, _ ...ldap.DialOpt) (auth.LDAPConn, error) {
				if cfg.dialErr != nil {
					return nil, cfg.dialErr
				}
				if cfg.upgradeFail {
					return startTLSFailConn{&mockLDAPConn{dir: dir}}, nil
				}
				return &mockLDAPConn{dir: dir}, nil
			}))
		if err != nil {
			t.Fatalf("NewLDAPProvider: %v", err)
		}
		authSvc = authSvc.WithLDAP(provider)
	}

	s := httpapi.New(httpapi.Deps{
		Config:   appCfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &t187Stack{ts: ts, md: md}
}

// t187AuditEvents returns the decoded detail objects of the actor's events
// of one action, oldest-last as stored (the query is newest-first).
func t187AuditEvents(t *testing.T, md metadata.Store, actor, action string) []map[string]any {
	t.Helper()
	events, err := md.Audits().Query(context.Background(), metadata.AuditQuery{
		Actor: actor, Action: action, Limit: 10,
	})
	if err != nil {
		t.Fatalf("audit query %s/%s: %v", actor, action, err)
	}
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		var d map[string]any
		if err := json.Unmarshal([]byte(e.Detail), &d); err != nil {
			t.Fatalf("detail %q is not an object: %v", e.Detail, err)
		}
		out = append(out, d)
	}
	return out
}

// ---- the local/LDAP matrix: action + method + reason of auth.failed ----

// TestAuthFailedAuditMatrix is the T-187 AC core: every failure form of the
// password plane records exactly one auth.failed event whose detail carries
// the arm and the reason.
func TestAuthFailedAuditMatrix(t *testing.T) {
	cases := []struct {
		name string

		wireLDAP    bool
		startTLS    bool
		dialErr     error
		upgradeFail bool

		username, password string
		wantStatus         int
		wantMethod         string
		wantReason         string
	}{
		{
			name: "local user wrong password", username: "alice", password: "wrong",
			wantStatus: 401, wantMethod: "local", wantReason: "bad_credentials",
		},
		{
			name: "unknown user without an ldap arm", username: "ghost", password: "x",
			wantStatus: 401, wantMethod: "local", wantReason: "user_not_found",
		},
		{
			name:     "local user wrong password with ldap wired stays local",
			wireLDAP: true, username: "alice", password: "wrong",
			wantStatus: 401, wantMethod: "local", wantReason: "bad_credentials",
		},
		{
			name:     "unknown user everywhere with ldap wired",
			wireLDAP: true, username: "ghost", password: "x",
			wantStatus: 401, wantMethod: "ldap", wantReason: "user_not_found",
		},
		{
			name:     "ldap user wrong password",
			wireLDAP: true, username: "jdoe", password: "wrong",
			wantStatus: 401, wantMethod: "ldap", wantReason: "bad_credentials",
		},
		{
			name:     "disabled ldap row refuses a valid bind",
			wireLDAP: true, username: "frozen", password: "frozen-secret",
			wantStatus: 401, wantMethod: "ldap", wantReason: "user_disabled",
		},
		{
			name:     "directory unreachable is provider_error (O-2)",
			wireLDAP: true,
			dialErr:  errors.New("dial tcp 10.9.9.9:389: connect: connection refused"),
			username: "jdoe", password: "jdoe-secret",
			wantStatus: 401, wantMethod: "ldap", wantReason: "provider_error",
		},
		{
			name:     "failed starttls upgrade is tls_handshake (O-2, T-186 leftover)",
			wireLDAP: true, startTLS: true, upgradeFail: true,
			username: "jdoe", password: "jdoe-secret",
			wantStatus: 401, wantMethod: "ldap", wantReason: "tls_handshake",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newT187Stack(t, func(c *t187StackCfg) {
				c.wireLDAP = tc.wireLDAP
				c.startTLS = tc.startTLS
				c.dialErr = tc.dialErr
				c.upgradeFail = tc.upgradeFail
			})

			resp := ldapLogin(t, st.ts.URL, tc.username, tc.password)
			body := mustGet(t, resp)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, tc.wantStatus, body)
			}
			// Uniform wording: the failure classes share one body (FR-23-AC5
			// survives the classification) — no reason, no method, no
			// password in the response or the stored detail.
			if tc.wantStatus == http.StatusUnauthorized {
				for _, leak := range []string{tc.wantReason, tc.wantMethod, tc.password} {
					if strings.Contains(body, leak) {
						t.Fatalf("401 body leaks %q: %s", leak, body)
					}
				}
			}

			events := t187AuditEvents(t, st.md, tc.username, "auth.failed")
			if len(events) == 0 {
				t.Fatalf("no auth.failed event for %s", tc.username)
			}
			d := events[0]
			if d["method"] != tc.wantMethod || d["reason"] != tc.wantReason {
				t.Errorf("detail = %v, want method=%s reason=%s", d, tc.wantMethod, tc.wantReason)
			}
			detailJSON, err := json.Marshal(d)
			if err != nil {
				t.Fatalf("marshal detail: %v", err)
			}
			if strings.Contains(string(detailJSON), tc.password) {
				t.Errorf("audit detail leaks the password: %s", detailJSON)
			}
			// The old spelling is gone from the write plane (D7).
			if old := t187AuditEvents(t, st.md, tc.username, "login.failed"); len(old) != 0 {
				t.Errorf("login.failed events still recorded: %v", old)
			}
		})
	}
}

// TestAuthSuccessAuditCarriesMethod: login.success events now name the arm
// that issued the session (local and LDAP legs of the password plane).
func TestAuthSuccessAuditCarriesMethod(t *testing.T) {
	st := newT187Stack(t, func(c *t187StackCfg) { c.wireLDAP = true })

	for _, tc := range []struct{ user, pass, wantMethod string }{
		{"alice", "alice-pw", "local"},
		{"jdoe", "jdoe-secret", "ldap"},
	} {
		resp := ldapLogin(t, st.ts.URL, tc.user, tc.pass)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login %s: status %d, body=%s", tc.user, resp.StatusCode, mustGet(t, resp))
		}
	}
	for _, tc := range []struct{ user, wantMethod string }{
		{"alice", "local"}, {"jdoe", "ldap"},
	} {
		events := t187AuditEvents(t, st.md, tc.user, "login.success")
		if len(events) == 0 {
			t.Fatalf("no login.success event for %s", tc.user)
		}
		if events[0]["method"] != tc.wantMethod {
			t.Errorf("%s login.success detail = %v, want method=%s", tc.user, events[0], tc.wantMethod)
		}
		if _, hasReason := events[0]["reason"]; hasReason {
			t.Errorf("%s login.success carries a reason: %v", tc.user, events[0])
		}
	}
}

// TestAuthFailedQueryAnchorH38 (FR-56-AC3): the failure events are queryable
// through the audit endpoint under the PRD action name, and the response
// renders method/reason inside the detail object.
func TestAuthFailedQueryAnchorH38(t *testing.T) {
	st := newT187Stack(t)
	if resp := ldapLogin(t, st.ts.URL, "alice", "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", resp.StatusCode)
	}

	// Admin basic auth against the management plane.
	req, err := http.NewRequest(http.MethodGet,
		st.ts.URL+"/binflow/api/v1/audit?action=auth.failed", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth("root", "root-pw")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit query status = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	var page struct {
		Events []struct {
			Actor  string         `json:"actor"`
			Action string         `json:"action"`
			Detail map[string]any `json:"detail"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}
	if len(page.Events) == 0 {
		t.Fatal("?action=auth.failed returned no events (PRD FR-56-AC3 anchor)")
	}
	var sawMethodReason bool
	for _, e := range page.Events {
		if e.Action != "auth.failed" {
			t.Fatalf("action filter leaked %q", e.Action)
		}
		if e.Detail["method"] == "local" && e.Detail["reason"] == "bad_credentials" {
			sawMethodReason = true
		}
	}
	if !sawMethodReason {
		t.Fatalf("no alice event carries method/reason: %+v", page.Events)
	}
}

// TestProviderFailureLogsWarn (O-2): a provider-side login failure logs one
// WARN naming method and reason, while the plain wrong-password leg stays
// silent — the operator gets the distinction the 401 cannot carry.
func TestProviderFailureLogsWarn(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	st := newT187Stack(t, func(c *t187StackCfg) {
		c.wireLDAP = true
		c.dialErr = errors.New("dial tcp 10.9.9.9:389: connect: connection refused")
	})
	if resp := ldapLogin(t, st.ts.URL, "jdoe", "jdoe-secret"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "identity provider problem") {
		t.Fatalf("provider failure logged no WARN: %q", out)
	}
	if !strings.Contains(out, "provider_error") || !strings.Contains(out, "ldap") {
		t.Fatalf("WARN lacks method/reason: %q", out)
	}

	// Control: a wrong password must NOT warn.
	buf.Reset()
	st2 := newT187Stack(t, func(c *t187StackCfg) { c.wireLDAP = true })
	if resp := ldapLogin(t, st2.ts.URL, "jdoe", "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("wrong-password leg warned: %q", buf.String())
	}
}

// ---- the OIDC arm: callback failures classified under method=oidc ----

// TestOIDCAuthFailedAuditMatrix walks the callback's failure table and
// asserts the auth.failed event of each leg.
func TestOIDCAuthFailedAuditMatrix(t *testing.T) {
	cases := []struct {
		name        string
		idpError    bool   // the callback query carries an IdP error parameter
		noCode      bool   // neither code nor state
		tamperState bool   // echoed state corrupted
		tokenErr    string // the token endpoint answers the OAuth error shape
		badAudience bool   // the ID Token is signed for another client
		wantReason  string
	}{
		{name: "idp refused the login", idpError: true, wantReason: "provider_error"},
		{name: "missing code and state", noCode: true, wantReason: "bad_request"},
		{name: "state mismatch", tamperState: true, wantReason: "bad_credentials"},
		{name: "token endpoint failure", tokenErr: "invalid_grant", wantReason: "provider_error"},
		{name: "id token rejected", badAudience: true, wantReason: "bad_credentials"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newOIDCStack(t, "")
			if tc.tokenErr != "" {
				st.idp.tokenErrorCode = tc.tokenErr
			}
			if tc.badAudience {
				st.idp.tokenClaims = strings.Replace(st.idp.defaultClaims(),
					`"aud": "test-client-id"`, `"aud": "some-other-client"`, 1)
			}

			q := url.Values{}
			cookie := ""
			switch {
			case tc.idpError:
				q.Set("error", "access_denied")
			case tc.noCode:
				// empty query
			default:
				state, tx := oidcLoginLeg(t, st.ts.URL, "")
				if tc.tamperState {
					state += "x"
				}
				q.Set("code", "auth-code-1")
				q.Set("state", state)
				cookie = tx
			}
			path := "/binflow/api/v1/oidc/callback"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			resp := oidcDo(t, st.ts.URL, http.MethodGet, path, cookie)
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound {
				t.Fatalf("status = %d, want a failure", resp.StatusCode)
			}

			events := t187AuditEvents(t, st.md, "oidc", "auth.failed")
			if len(events) == 0 {
				t.Fatal("no auth.failed event for the oidc arm")
			}
			d := events[0]
			if d["method"] != "oidc" || d["reason"] != tc.wantReason {
				t.Errorf("detail = %v, want method=oidc reason=%s", d, tc.wantReason)
			}
		})
	}
}

// TestOIDCLoginSuccessCarriesMethod: the happy callback records
// login.success with method=oidc.
func TestOIDCLoginSuccessCarriesMethod(t *testing.T) {
	st := newOIDCStack(t, "")
	st.idp.tokenClaims = st.idp.claimsFor("sub-t187", "t187user", `"developers"`)

	state, tx := oidcLoginLeg(t, st.ts.URL, "")
	cb := "/binflow/api/v1/oidc/callback?code=auth-code-1&state=" + url.QueryEscape(state)
	resp := oidcDo(t, st.ts.URL, http.MethodGet, cb, tx)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	events := t187AuditEvents(t, st.md, "t187user", "login.success")
	if len(events) == 0 {
		t.Fatal("no login.success event for the oidc login")
	}
	if events[0]["method"] != "oidc" {
		t.Errorf("login.success detail = %v, want method=oidc", events[0])
	}
}

// TestAuthEventDetailShape pins the audit helper's wire shape: method
// always, reason only when set.
func TestAuthEventDetailShape(t *testing.T) {
	d := audit.AuthEventDetail("ldap", "tls_handshake")
	if d != `{"method":"ldap","reason":"tls_handshake"}` {
		t.Errorf("failure detail = %s", d)
	}
	s := audit.AuthEventDetail("oidc", "")
	if s != `{"method":"oidc"}` {
		t.Errorf("success detail = %s", s)
	}
}
