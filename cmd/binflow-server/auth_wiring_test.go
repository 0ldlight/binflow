// T-179 acceptance surface, cmd arm (AC 2/AC 5): the config-driven assembly
// of the identity providers — wireAuthProviders (disabled → nils; enabled →
// constructed providers; unreachable OIDC issuer → fail-fast boot) and the
// enabled/disabled smoke through the real openStack → newAssembledServer
// chain, including Deps.OIDC's nil posture (disabled → the E-26 404 routes)
// and the /api/v1/auth/methods body both ways.

package main

import (
	"context"
	"encoding/json"
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
)

// discoveryIDP is the minimal OIDC discovery surface NewOIDCProvider needs
// at construction (go-oidc fetches the discovery document only; JWKS is
// lazy). Signing lives in the httpapi test suite; here only the boot leg is
// under test.
func discoveryIDP(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		doc := map[string]any{
			"issuer":                                srvIssuer(r),
			"authorization_endpoint":                srvIssuer(r) + "/auth",
			"token_endpoint":                        srvIssuer(r) + "/token",
			"jwks_uri":                              srvIssuer(r) + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// srvIssuer derives the issuer from the request (the httptest server's own
// URL; kept as a helper so the handler literal stays self-contained).
func srvIssuer(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// testStack opens a full stack over a hand-built config (the pattern
// TestServeAssembledStackPing uses), failing the test on any error.
func testStack(t *testing.T, cfg *config.Config) *stack {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	t.Cleanup(func() { st.close(logger) })
	return st
}

// oidcEnabledConfig returns a defaults config with auth.oidc armed against
// the given issuer.
func oidcEnabledConfig(t *testing.T, issuer string) *config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Auth.OIDC = config.OIDCConfig{
		Enabled:     true,
		IssuerURL:   issuer,
		ClientID:    "binflow-test-client",
		RedirectURL: "http://binflow.example.com/binflow/api/v1/oidc/callback",
		Scopes:      []string{"openid", "profile", "email"},
		UserClaim:   "preferred_username",
		GroupClaim:  "groups",
		AdminGroup:  "binflow-admins",
	}
	return cfg
}

// TestWireAuthProvidersDisabled (AC 2, disabled → nils): the default config
// yields no providers — the pre-M6 posture, zero behavior change.
func TestWireAuthProvidersDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	md := openTestMetadata(t)
	oidcProv, ldapProv, err := wireAuthProviders(context.Background(), cfg, md)
	if err != nil {
		t.Fatalf("wireAuthProviders(defaults) = %v, want nil error", err)
	}
	if oidcProv != nil || ldapProv != nil {
		t.Fatalf("providers = %v/%v, want nil/nil on the default config", oidcProv, ldapProv)
	}
}

// TestWireAuthProvidersOIDC (AC 2): an enabled section constructs the
// provider (discovery round trip included) and the env secret rides the
// belt-and-braces read when the config field is empty (hand-built configs).
func TestWireAuthProvidersOIDC(t *testing.T) {
	idp := discoveryIDP(t)
	withEnv(t, map[string]string{config.OIDCClientSecretEnvVar: "env-secret-1"})

	md := openTestMetadata(t)
	oidcProv, ldapProv, err := wireAuthProviders(context.Background(),
		oidcEnabledConfig(t, idp.URL), md)
	if err != nil {
		t.Fatalf("wireAuthProviders(oidc) = %v, want nil", err)
	}
	if oidcProv == nil {
		t.Fatal("oidc provider nil, want constructed")
	}
	if ldapProv != nil {
		t.Fatal("ldap provider non-nil, want nil (section disabled)")
	}
	if got := oidcProv.ProviderName(); got != auth.ProviderOIDC {
		t.Errorf("ProviderName() = %q, want %q", got, auth.ProviderOIDC)
	}
	// The OAuth2 surface carries the config (the login handler's seam).
	oc := oidcProv.OAuth2Config()
	if oc.ClientID != "binflow-test-client" {
		t.Errorf("ClientID = %q, want binflow-test-client", oc.ClientID)
	}
	if oc.RedirectURL != "http://binflow.example.com/binflow/api/v1/oidc/callback" {
		t.Errorf("RedirectURL = %q, want the configured callback", oc.RedirectURL)
	}
}

// TestWireAuthProvidersOIDCFailFast (AC 2, error leg): an enabled section
// with an unreachable issuer refuses the boot with a pointed error instead
// of constructing a half-provider.
func TestWireAuthProvidersOIDCFailFast(t *testing.T) {
	withEnv(t, map[string]string{config.OIDCClientSecretEnvVar: ""})
	md := openTestMetadata(t)
	// Port 1 on localhost: reserved, nothing listens there — discovery
	// cannot succeed.
	_, _, err := wireAuthProviders(context.Background(),
		oidcEnabledConfig(t, "http://127.0.0.1:1"), md)
	if err == nil {
		t.Fatal("wireAuthProviders(unreachable issuer) = nil, want fail-fast error")
	}
	if !strings.Contains(err.Error(), "wiring auth.oidc") {
		t.Errorf("error = %v, want it wrapped by the oidc wiring context", err)
	}
}

// TestWireAuthProvidersLDAP (AC 2): an enabled section constructs the LDAP
// provider (lazy pool — no directory round trip at boot) and Close is
// idempotent.
func TestWireAuthProvidersLDAP(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Auth.LDAP = config.LDAPConfig{
		Enabled:  true,
		URL:      "ldap://ldap.example.com:389",
		BaseDN:   "dc=example,dc=com",
		BindDN:   "cn=binflow,dc=example,dc=com",
		PoolSize: 7,
	}
	withEnv(t, map[string]string{config.LDAPBindPasswordEnvVar: "env-bind-pw"})

	md := openTestMetadata(t)
	oidcProv, ldapProv, err := wireAuthProviders(context.Background(), cfg, md)
	if err != nil {
		t.Fatalf("wireAuthProviders(ldap) = %v, want nil", err)
	}
	if oidcProv != nil {
		t.Fatal("oidc provider non-nil, want nil (section disabled)")
	}
	if ldapProv == nil {
		t.Fatal("ldap provider nil, want constructed")
	}
	if got := ldapProv.ProviderName(); got != auth.ProviderLDAP {
		t.Errorf("ProviderName() = %q, want %q", got, auth.ProviderLDAP)
	}
	ldapProv.Close()
	ldapProv.Close() // idempotent
}

// authTestServer builds the HTTP surface over a real openStack result the
// way newAssembledServer does for the auth plane. The full newAssembledServer
// cannot run twice in one test process (the process-wide adapter registry
// panics on the second npm/maven registration — T-168's known full-suite
// red, same workaround s3_stack_test.go uses); this assembly carries every
// auth-relevant Deps (Config/Auth/Authz/Metadata/Repos/OIDC) without
// touching the registry, so both wiring states are smoke-testable.
func authTestServer(t *testing.T, cfg *config.Config, st *stack) *httptest.Server {
	t.Helper()
	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     st.authSvc,
		Authz:    st.authSvc,
		Metadata: st.md,
		Repos:    st.md.Repos(),
		OIDC:     oidcLoginSeam(st.oidcProv),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestOIDCLoginSeamNilGuard: the load-bearing typed-nil guard — a nil
// provider must yield a NIL interface (the login handlers' `Deps.OIDC ==
// nil` check decides the 404 posture), a real provider a non-nil one.
func TestOIDCLoginSeamNilGuard(t *testing.T) {
	if got := oidcLoginSeam(nil); got != nil {
		t.Fatalf("oidcLoginSeam(nil) = %v, want a nil interface (typed nil would 500 the routes)", got)
	}
	idp := discoveryIDP(t)
	prov, err := auth.NewOIDCProvider(context.Background(), &auth.OIDCConfig{
		IssuerURL: idp.URL, ClientID: "c", RedirectURL: "http://binflow.example.com/cb",
	}, nil)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	if got := oidcLoginSeam(prov); got == nil {
		t.Fatal("oidcLoginSeam(provider) = nil, want the wired seam")
	}
}

// TestServeAuthMethodsSmokeDisabled (AC 2/AC 5, disabled leg): the default
// stack serves the password-only methods body and keeps the two OIDC
// browser routes at the E-26 404 (Deps.OIDC nil — the typed-nil guard).
func TestServeAuthMethodsSmokeDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	st := testStack(t, cfg)

	if st.oidcProv != nil || st.ldapProv != nil {
		t.Fatalf("stack providers = %v/%v, want nil/nil", st.oidcProv, st.ldapProv)
	}
	if st.authSvc.OIDCWired() || st.authSvc.LDAPWired() {
		t.Fatal("default stack reports a wired provider facet, want both inert")
	}
	ts := authTestServer(t, cfg, st)

	assertMethods(t, ts.URL, false)
	resp, err := http.Get(ts.URL + "/binflow/api/v1/oidc/login")
	if err != nil {
		t.Fatalf("GET oidc/login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("oidc/login status = %d, want 404 on the disabled wiring", resp.StatusCode)
	}
}

// TestServeAuthMethodsSmokeOIDCEnabled (AC 2/AC 5, enabled leg): the enabled
// stack arms the Bearer arm (facet), injects Deps.OIDC (the login route
// answers 302, not 404/500 — the typed-nil guard's positive leg) and the
// methods body advertises oidc.
func TestServeAuthMethodsSmokeOIDCEnabled(t *testing.T) {
	idp := discoveryIDP(t)
	cfg := oidcEnabledConfig(t, idp.URL)
	st := testStack(t, cfg)

	if st.oidcProv == nil {
		t.Fatal("stack.oidcProv nil, want the wired provider")
	}
	if !st.authSvc.OIDCWired() {
		t.Fatal("auth service OIDC facet false, want the Bearer arm armed")
	}
	if st.authSvc.LDAPWired() {
		t.Fatal("auth service LDAP facet true, want inert")
	}

	ts := authTestServer(t, cfg, st)
	assertMethods(t, ts.URL, true)

	// The login route exists and redirects (no redirect-following client).
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/binflow/api/v1/oidc/login")
	if err != nil {
		t.Fatalf("GET oidc/login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("oidc/login status = %d, want 302 to the IdP", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, idp.URL+"/auth?") {
		t.Errorf("oidc/login Location = %q, want the IdP authorization endpoint", loc)
	}
}

// openTestMetadata opens a throwaway sqlite store for wiring-level tests.
func openTestMetadata(t *testing.T) metadata.Store {
	t.Helper()
	md, err := metadata.Open(context.Background(), metadata.Options{
		Driver: "sqlite",
		Path:   t.TempDir() + "/binflow.db",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	return md
}

// assertMethods fetches /api/v1/auth/methods and asserts the password-only
// baseline plus the oidc bit (ldap stays false in both smoke states — the
// full both-on matrix lives in the httpapi suite).
func assertMethods(t *testing.T, tsURL string, oidc bool) {
	t.Helper()
	resp, err := http.Get(tsURL + "/binflow/api/v1/auth/methods")
	if err != nil {
		t.Fatalf("GET auth/methods: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth/methods status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Password bool `json:"password"`
		OIDC     bool `json:"oidc"`
		LDAP     bool `json:"ldap"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode methods body: %v", err)
	}
	if !body.Password || body.LDAP {
		t.Errorf("methods = %+v, want password=true ldap=false", body)
	}
	if body.OIDC != oidc {
		t.Errorf("methods.oidc = %v, want %v", body.OIDC, oidc)
	}
}
