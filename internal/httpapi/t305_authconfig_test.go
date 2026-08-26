package httpapi_test

// T-305 REST acceptance (FR-92 / ADR-0035): the nine
// /api/v1/admin/security/* routes — capability gates (readonly_admin reads,
// plain user 403s, anonymous 401s), the PUT/GET round-trip with the masked
// sentinel echo, the strict-schema 400s, the test-connection forms, the
// honest 503 on a manager-less stack, the audit trail (auth.config.update
// with values redacted), and the auth-methods bit flipping with the live
// section (change-effective-immediately through the REST plane itself).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"golang.org/x/oauth2"
)

// authConfigStack is the self-contained T-305 assembly (the license-stack
// precedent: the collaborator needs the store BEFORE the auth service's hot
// arms and Deps are wired, so this file owns its own minimal stack — no
// storage engine, the auth-config routes need none).
type authConfigStack struct {
	t   *testing.T
	ts  *httptest.Server
	md  metadata.Store
	mgr *auth.ConfigManager
}

func newAuthConfigStack(t *testing.T) *authConfigStack {
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
	cfg.Security.AnonymousAccess = false

	// Role fixtures (the permission legs).
	seedLicenseUser(t, md, "roat", "roat-pw", "readonly_admin")
	seedLicenseUser(t, md, "u1", "u1-pw", "user")

	// The ConfigManager over the real enc:v1 chain (fixed test key),
	// loaded before the arms are wired so the opening snapshot exists.
	c, err := remote.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("remote.NewCipher: %v", err)
	}
	mgr, err := auth.NewAuthConfigManager(auth.ConfigOptions{
		Store:     auth.NewConfigStoreAdapter(md.AuthConfigs()),
		Cipher:    c,
		PoolGrace: -1,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewAuthConfigManager: %v", err)
	}
	if err := mgr.Load(ctx, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The cmd assembly's shape: the service's external arms resolve per
	// request through the manager, Deps carries the management facet and
	// the login seam delegates to the live provider.
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess).WithAuthConfig(mgr)
	s := httpapi.New(httpapi.Deps{
		Config:      cfg,
		Auth:        authSvc,
		Authz:       authSvc,
		Metadata:    md,
		Repos:       md.Repos(),
		AuthConfigs: mgr,
		OIDC:        t305OIDCSeam{m: mgr},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &authConfigStack{t: t, ts: ts, md: md, mgr: mgr}
}

// t305OIDCSeam is the test-side twin of cmd's hotOIDCLogin adapter: the
// login seam answers the CURRENT provider's OAuth2 config (nil when the
// live section is disabled).
type t305OIDCSeam struct{ m *auth.ConfigManager }

func (s t305OIDCSeam) OAuth2Config() *oauth2.Config {
	if p := s.m.CurrentOIDC(); p != nil {
		return p.OAuth2Config()
	}
	return nil
}

// do issues one request with the given Basic credential.
func (a *authConfigStack) do(method, path, user, pass string, body []byte) *http.Response {
	a.t.Helper()
	t := a.t
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, a.ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := a.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// TestAuthConfigRouteGates: the capability matrix of the read/write/test
// trios (92.4: readonly_admin sees the masked sections; writes and probes
// are admin actions).
func TestAuthConfigRouteGates(t *testing.T) {
	a := newAuthConfigStack(t)

	tests := []struct {
		name     string
		method   string
		path     string
		user     string
		pass     string
		wantCode int
	}{
		{"anonymous get", http.MethodGet, "/binflow/api/v1/admin/security/ldap", "", "", http.StatusUnauthorized},
		{"plain user get", http.MethodGet, "/binflow/api/v1/admin/security/ldap", "u1", "u1-pw", http.StatusForbidden},
		{"readonly get ldap", http.MethodGet, "/binflow/api/v1/admin/security/ldap", "roat", "roat-pw", http.StatusOK},
		{"readonly get oauth", http.MethodGet, "/binflow/api/v1/admin/security/oauth", "roat", "roat-pw", http.StatusOK},
		{"readonly get saml", http.MethodGet, "/binflow/api/v1/admin/security/saml/config", "roat", "roat-pw", http.StatusOK},
		{"readonly put denied", http.MethodPut, "/binflow/api/v1/admin/security/ldap", "roat", "roat-pw", http.StatusForbidden},
		{"readonly test denied", http.MethodPost, "/binflow/api/v1/admin/security/ldap/test", "roat", "roat-pw", http.StatusForbidden},
		{"plain user get oauth", http.MethodGet, "/binflow/api/v1/admin/security/oauth", "u1", "u1-pw", http.StatusForbidden},
		{"unknown verb on the family", http.MethodDelete, "/binflow/api/v1/admin/security/ldap", "admin", adminPass, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := a.do(tt.method, tt.path, tt.user, tt.pass, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantCode {
				t.Fatalf("%s %s as %q = %d, want %d", tt.method, tt.path, tt.user, resp.StatusCode, tt.wantCode)
			}
		})
	}
}

// TestAuthConfigPutGetRoundTrip (AC2): PUT echoes the masked section, GET
// agrees, the stored row is sealed, and the SAML empty state is the
// anchored {}.
func TestAuthConfigPutGetRoundTrip(t *testing.T) {
	a := newAuthConfigStack(t)
	ctx := context.Background()

	// SAML before any write: the anchored empty object.
	resp := a.do(http.MethodGet, "/binflow/api/v1/admin/security/saml/config", "admin", adminPass, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET saml = %d", resp.StatusCode)
	}
	var samlEmpty map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&samlEmpty); err != nil {
		t.Fatalf("decode empty saml: %v", err)
	}
	_ = resp.Body.Close()
	if len(samlEmpty) != 0 {
		t.Fatalf("unset saml = %v, want {} (the anchored §3.2 empty state)", samlEmpty)
	}

	// LDAP round-trip with a secret.
	body := []byte(`{"enabled":true,"ldapUrl":"ldap://dir.example.com:389/dc=example,dc=com","search":{"searchFilter":"(uid={0})","managerDn":"cn=admin,dc=example,dc=com","managerPassword":"wire-secret"}}`)
	resp = a.do(http.MethodPut, "/binflow/api/v1/admin/security/ldap", "admin", adminPass, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT ldap = %d", resp.StatusCode)
	}
	var echo map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&echo); err != nil {
		t.Fatalf("decode echo: %v", err)
	}
	_ = resp.Body.Close()
	search, _ := echo["search"].(map[string]any)
	if got := search["managerPassword"]; got != auth.MaskedSecretEcho {
		t.Fatalf("echo managerPassword = %v, want the sentinel", got)
	}

	resp = a.do(http.MethodGet, "/binflow/api/v1/admin/security/ldap", "admin", adminPass, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET ldap = %d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	_ = resp.Body.Close()
	if fmt.Sprint(got) != fmt.Sprint(echo) {
		t.Fatalf("GET drifts from the PUT echo:\n%v\n%v", echo, got)
	}

	// The stored row is sealed (AC2: 明文 grep 零命中).
	rec, err := a.md.AuthConfigs().GetAuthConfig(ctx, auth.SectionLDAP)
	if err != nil || rec == nil {
		t.Fatalf("stored row: %v %v", rec, err)
	}
	if strings.Contains(rec.Doc, "wire-secret") || !strings.Contains(rec.Doc, "enc:v1:") {
		t.Fatalf("stored doc is not sealed: %s", rec.Doc)
	}
	if rec.UpdatedBy != "admin" {
		t.Errorf("UpdatedBy = %q, want admin", rec.UpdatedBy)
	}
}

// TestAuthConfigStrictSchema400: unknown keys and invalid shapes answer the
// errors[] 400 envelope.
func TestAuthConfigStrictSchema400(t *testing.T) {
	a := newAuthConfigStack(t)

	resp := a.do(http.MethodPut, "/binflow/api/v1/admin/security/oauth", "admin", adminPass,
		[]byte(`{"enabled":false,"issuer":"https://x"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT unknown key = %d, want 400", resp.StatusCode)
	}
	var env struct {
		Errors []struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil || len(env.Errors) != 1 {
		t.Fatalf("error envelope = %+v (%v)", env, err)
	}
	_ = resp.Body.Close()
	if !strings.Contains(env.Errors[0].Message, "unknown field") {
		t.Fatalf("message = %q, want the unknown-field wording", env.Errors[0].Message)
	}

	resp = a.do(http.MethodPut, "/binflow/api/v1/admin/security/ldap", "admin", adminPass,
		[]byte(`{"enabled":true}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT invalid ldap = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestAuthConfigManagerless503: a stack assembled without the manager keeps
// the nine routes at the honest 503.
func TestAuthConfigManagerless503(t *testing.T) {
	a := newAuthConfigStackNoManager(t)
	for _, p := range []string{
		"/binflow/api/v1/admin/security/ldap",
		"/binflow/api/v1/admin/security/oauth",
		"/binflow/api/v1/admin/security/saml/config",
	} {
		resp := a.do(http.MethodGet, p, "admin", adminPass, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("GET %s without a manager = %d, want 503", p, resp.StatusCode)
		}
	}
}

// newAuthConfigStackNoManager builds the same minimal stack WITHOUT the
// collaborator (the pre-T-305 unit-stack posture).
func newAuthConfigStackNoManager(t *testing.T) *authConfigStack {
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
	cfg.Security.AnonymousAccess = false
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	s := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc,
		Metadata: md, Repos: md.Repos(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &authConfigStack{t: t, ts: ts, md: md}
}

// TestAuthConfigAuditTrail (AC7): every PUT leaves an auth.config.update
// row whose detail carries the section and changed-key names but no values.
func TestAuthConfigAuditTrail(t *testing.T) {
	a := newAuthConfigStack(t)
	ctx := context.Background()

	body := []byte(`{"enabled":true,"ldapUrl":"ldap://dir.example.com:389/dc=example,dc=com","search":{"searchFilter":"(uid={0})","managerDn":"cn=admin,dc=example,dc=com","managerPassword":"audit-secret"}}`)
	resp := a.do(http.MethodPut, "/binflow/api/v1/admin/security/ldap", "admin", adminPass, body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d", resp.StatusCode)
	}

	events, err := a.md.Audits().List(ctx, "", "", 100)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	var found bool
	for _, e := range events {
		if e.Action != "auth.config.update" {
			continue
		}
		found = true
		if !strings.Contains(e.Detail, `"ldap"`) {
			t.Errorf("detail misses the section: %s", e.Detail)
		}
		if strings.Contains(e.Detail, "audit-secret") || strings.Contains(e.Detail, "dir.example.com") {
			t.Errorf("detail carries section values (redact violation): %s", e.Detail)
		}
	}
	if !found {
		t.Fatal("no auth.config.update event recorded")
	}
}

// TestAuthConfigTestEndpoints (AC4): the OIDC probe's both forms against a
// real in-process discovery IdP, and the SAML probe's failure form.
func TestAuthConfigTestEndpoints(t *testing.T) {
	a := newAuthConfigStack(t)

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		issuer := "http://" + r.Host
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q,"response_types_supported":["code"],"subject_types_supported":["public"],"id_token_signing_alg_values_supported":["RS256"]}`,
			issuer, issuer+"/auth", issuer+"/token", issuer+"/keys")
	}))
	t.Cleanup(idp.Close)

	// Success form: a candidate body probes the discovery document.
	resp := a.do(http.MethodPost, "/binflow/api/v1/admin/security/oauth/test", "admin", adminPass,
		[]byte(fmt.Sprintf(`{"enabled":true,"issuer_url":%q,"client_id":"c","redirect_url":"https://x/cb"}`, idp.URL)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("oauth/test(ok) = %d, want 200", resp.StatusCode)
	}
	var rep struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	_ = resp.Body.Close()
	if !rep.OK {
		t.Fatalf("report = %+v, want ok", rep)
	}

	// Failure form: an unreachable issuer answers the 400 diagnostic shape.
	resp = a.do(http.MethodPost, "/binflow/api/v1/admin/security/oauth/test", "admin", adminPass,
		[]byte(`{"enabled":true,"issuer_url":"http://127.0.0.1:1","client_id":"c","redirect_url":"https://x/cb"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oauth/test(unreachable) = %d, want 400", resp.StatusCode)
	}
	var bad struct {
		OK       bool   `json:"ok"`
		Category string `json:"category"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&bad)
	_ = resp.Body.Close()
	if bad.OK || bad.Category != "unreachable" {
		t.Fatalf("failure report = %+v, want ok=false unreachable", bad)
	}

	// The stored-section form: no body probes what is stored (an unset
	// section answers the unset posture).
	resp = a.do(http.MethodPost, "/binflow/api/v1/admin/security/saml/config/test", "admin", adminPass, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("saml/config/test(unset) = %d, want 400", resp.StatusCode)
	}
	var unset struct {
		Category string `json:"category"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&unset)
	_ = resp.Body.Close()
	if unset.Category != "unset" {
		t.Fatalf("unset report = %+v, want category unset", unset)
	}
}

// TestAuthConfigHotMethodsBit (AC3 through the REST plane): enabling the
// OIDC section via PUT flips /api/v1/auth/methods on the NEXT request — no
// restart, the same process.
func TestAuthConfigHotMethodsBit(t *testing.T) {
	a := newAuthConfigStack(t)

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		issuer := "http://" + r.Host
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q,"response_types_supported":["code"],"subject_types_supported":["public"],"id_token_signing_alg_values_supported":["RS256"]}`,
			issuer, issuer+"/auth", issuer+"/token", issuer+"/keys")
	}))
	t.Cleanup(idp.Close)

	methods := func() bool {
		resp := a.do(http.MethodGet, "/binflow/api/v1/auth/methods", "", "", nil)
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			OIDC bool `json:"oidc"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode methods: %v", err)
		}
		return body.OIDC
	}

	if methods() {
		t.Fatal("oidc advertised before any configuration")
	}
	resp := a.do(http.MethodPut, "/binflow/api/v1/admin/security/oauth", "admin", adminPass,
		[]byte(fmt.Sprintf(`{"enabled":true,"issuer_url":%q,"client_id":"c","redirect_url":"https://x/cb"}`, idp.URL)))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT oauth = %d", resp.StatusCode)
	}
	if !methods() {
		t.Fatal("oidc not advertised after the enabling PUT — the live snapshot did not flip the entry point")
	}
	if a.mgr.CurrentOIDC() == nil {
		t.Fatal("the live OIDC provider is absent after the enabling PUT")
	}

	// And the disable leg: the same write path, immediately effective.
	resp = a.do(http.MethodPut, "/binflow/api/v1/admin/security/oauth", "admin", adminPass,
		[]byte(fmt.Sprintf(`{"enabled":false,"issuer_url":%q,"client_id":"c","redirect_url":"https://x/cb"}`, idp.URL)))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT oauth(disable) = %d", resp.StatusCode)
	}
	if methods() {
		t.Fatal("oidc still advertised after the disabling PUT")
	}
}
