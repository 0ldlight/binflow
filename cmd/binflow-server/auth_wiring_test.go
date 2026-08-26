// T-179/T-305 acceptance surface, cmd arm: the config-driven assembly of
// the authentication plane — wireAuthConfigManager (disabled → inert
// snapshot; enabled → live providers; unreachable seeded OIDC issuer →
// fail-fast boot; first-boot seeding + the override WARN) and the
// enabled/disabled smoke through the real openStack → assembled Deps
// chain, including the login seam's nil posture (disabled → the E-26 404
// routes) and the /api/v1/auth/methods body both ways.

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
	"github.com/lzwzzy/binflow/internal/remote"
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

// TestWireAuthConfigManagerDisabled (AC 2, disabled → inert): the default
// config yields no live providers — the pre-M6 posture, zero behavior
// change, and nothing seeds the DB (an unset section is not "configured").
func TestWireAuthConfigManagerDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	md := openTestMetadata(t)

	mgr, err := wireAuthConfigManager(context.Background(), cfg, md,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("wireAuthConfigManager(defaults) = %v, want nil error", err)
	}
	if mgr.CurrentOIDC() != nil || mgr.CurrentLDAP() != nil {
		t.Fatalf("live providers = %v/%v, want nil/nil on the default config",
			mgr.CurrentOIDC(), mgr.CurrentLDAP())
	}
	rows, err := md.AuthConfigs().ListAuthConfigs(context.Background())
	if err != nil {
		t.Fatalf("ListAuthConfigs: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("default config seeded %d rows, want 0", len(rows))
	}
}

// testMasterKey is a valid 32-byte base64 master key for the enc:v1 chain
// (tests only — a fixed value keeps the sealed-doc assertions stable).
const testMasterKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

// TestWireAuthConfigManagerOIDCSeeds (K31 rule ②): an enabled file section
// seeds the DB row (secret sealed enc:v1), the live provider answers, and
// the second Load over the same store WARNs instead of re-seeding (rule ①:
// DB wins).
func TestWireAuthConfigManagerOIDCSeeds(t *testing.T) {
	idp := discoveryIDP(t)
	withEnv(t, map[string]string{
		config.OIDCClientSecretEnvVar: "env-secret-1",
		remote.CredentialsEnvVar:      testMasterKey,
	})
	md := openTestMetadata(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	mgr, err := wireAuthConfigManager(context.Background(),
		oidcEnabledConfig(t, idp.URL), md, logger)
	if err != nil {
		t.Fatalf("wireAuthConfigManager(oidc) = %v, want nil", err)
	}
	if mgr.CurrentOIDC() == nil {
		t.Fatal("live oidc provider nil, want constructed from the seed")
	}
	if got := mgr.CurrentOIDC().OAuth2Config().ClientID; got != "binflow-test-client" {
		t.Errorf("ClientID = %q, want binflow-test-client", got)
	}

	// The stored row exists, the secret is sealed, and no plaintext rides it.
	rec, err := md.AuthConfigs().GetAuthConfig(context.Background(), auth.SectionOIDC)
	if err != nil || rec == nil {
		t.Fatalf("GetAuthConfig(oidc) = %v, %v; want the seeded row", rec, err)
	}
	if !strings.Contains(rec.Doc, "enc:v1:") {
		t.Fatalf("seeded doc carries no enc:v1 secret: %s", rec.Doc)
	}
	if strings.Contains(rec.Doc, "env-secret-1") {
		t.Fatalf("seeded doc leaks the plaintext secret: %s", rec.Doc)
	}
	if rec.UpdatedBy != "system-seed" {
		t.Errorf("seed UpdatedBy = %q, want system-seed", rec.UpdatedBy)
	}

	// Second boot: the row exists, so the file section is OVERRIDDEN (WARN)
	// — not re-seeded, not fail-fast. The row is byte-identical.
	before := rec.Doc
	mgr2, err := wireAuthConfigManager(context.Background(),
		oidcEnabledConfig(t, idp.URL), md, logger)
	if err != nil {
		t.Fatalf("second wireAuthConfigManager = %v, want the override posture (WARN, not an error)", err)
	}
	if mgr2.CurrentOIDC() == nil {
		t.Fatal("second boot live oidc provider nil, want the DB-backed provider")
	}
	rec2, err := md.AuthConfigs().GetAuthConfig(context.Background(), auth.SectionOIDC)
	if err != nil || rec2 == nil {
		t.Fatalf("GetAuthConfig(oidc, second boot): %v %v", rec2, err)
	}
	if rec2.Doc != before {
		t.Fatalf("second boot rewrote the doc (rule ① violation):\n%s\n%s", before, rec2.Doc)
	}
}

// TestWireAuthConfigManagerOIDCFailFast (M6 posture preserved): an enabled
// section with an unreachable issuer refuses the boot with a pointed error
// instead of constructing a half-provider.
func TestWireAuthConfigManagerOIDCFailFast(t *testing.T) {
	withEnv(t, map[string]string{config.OIDCClientSecretEnvVar: ""})
	md := openTestMetadata(t)
	// Port 1 on localhost: reserved, nothing listens there — discovery
	// cannot succeed.
	_, err := wireAuthConfigManager(context.Background(),
		oidcEnabledConfig(t, "http://127.0.0.1:1"), md,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("wireAuthConfigManager(unreachable issuer) = nil, want fail-fast error")
	}
	if !strings.Contains(err.Error(), "wiring auth config") {
		t.Errorf("error = %v, want it wrapped by the auth-config wiring context", err)
	}
}

// TestWireAuthConfigManagerLDAPSeeds: an enabled LDAP section seeds and the
// live provider answers; Close is idempotent.
func TestWireAuthConfigManagerLDAPSeeds(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Auth.LDAP = config.LDAPConfig{
		Enabled:  true,
		URL:      "ldap://ldap.example.com:389",
		BaseDN:   "dc=example,dc=com",
		BindDN:   "cn=binflow,dc=example,dc=com",
		PoolSize: 7,
	}
	withEnv(t, map[string]string{
		config.LDAPBindPasswordEnvVar: "env-bind-pw",
		remote.CredentialsEnvVar:      testMasterKey,
	})
	md := openTestMetadata(t)

	mgr, err := wireAuthConfigManager(context.Background(), cfg, md,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("wireAuthConfigManager(ldap) = %v, want nil", err)
	}
	live := mgr.CurrentLDAP()
	if live == nil {
		t.Fatal("live ldap provider nil, want constructed")
	}
	if got := live.ProviderName(); got != auth.ProviderLDAP {
		t.Errorf("ProviderName() = %q, want %q", got, auth.ProviderLDAP)
	}
	mgr.Close()
	mgr.Close() // idempotent
}

// authTestServer builds the HTTP surface over a real openStack result the
// way newAssembledServer does for the auth plane. The full newAssembledServer
// cannot run twice in one test process (the process-wide adapter registry
// panics on the second npm/maven registration — T-168's known full-suite
// red, same workaround s3_stack_test.go uses); this assembly carries every
// auth-relevant Deps (Config/Auth/Authz/Metadata/Repos/OIDC/AuthConfigs)
// without touching the registry, so both wiring states are smoke-testable.
func authTestServer(t *testing.T, cfg *config.Config, st *stack) *httptest.Server {
	t.Helper()
	s := httpapi.New(httpapi.Deps{
		Config:      cfg,
		Auth:        st.authSvc,
		Authz:       st.authSvc,
		Metadata:    st.md,
		Repos:       st.md.Repos(),
		OIDC:        hotOIDCLoginSeam(st.authCfg),
		AuthConfigs: st.authCfg,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestHotOIDCLoginSeamNilGuard: the load-bearing typed-nil guard — a nil
// manager must yield a NIL interface; a manager whose live section is
// disabled yields a NIL config (httpapi's activeOIDCConfig decides the 404
// posture); an enabled one yields the OAuth2 surface.
func TestHotOIDCLoginSeamNilGuard(t *testing.T) {
	if got := hotOIDCLoginSeam(nil); got != nil {
		t.Fatalf("hotOIDCLoginSeam(nil) = %v, want a nil interface", got)
	}
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	md := openTestMetadata(t)
	mgr, err := wireAuthConfigManager(context.Background(), cfg, md,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("wireAuthConfigManager: %v", err)
	}
	if got := hotOIDCLoginSeam(mgr).OAuth2Config(); got != nil {
		t.Fatalf("disabled section OAuth2Config() = %v, want nil (the 404 posture)", got)
	}
	idp := discoveryIDP(t)
	mgr2, err := wireAuthConfigManager(context.Background(),
		oidcEnabledConfig(t, idp.URL), openTestMetadata(t),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("wireAuthConfigManager(oidc): %v", err)
	}
	if got := hotOIDCLoginSeam(mgr2).OAuth2Config(); got == nil {
		t.Fatal("enabled section OAuth2Config() = nil, want the wired config")
	}
}

// TestServeAuthMethodsSmokeDisabled (AC 2/AC 5, disabled leg): the default
// stack serves the password-only methods body and keeps the two OIDC
// browser routes at the E-26 404 (nil live config).
func TestServeAuthMethodsSmokeDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	st := testStack(t, cfg)

	if st.authCfg.CurrentOIDC() != nil || st.authCfg.CurrentLDAP() != nil {
		t.Fatalf("live providers = %v/%v, want nil/nil",
			st.authCfg.CurrentOIDC(), st.authCfg.CurrentLDAP())
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

// TestServeAuthMethodsSmokeOIDCEnabled (AC 2/AC 5, enabled leg): the seeded
// stack arms the Bearer arm (facet), injects the login seam (the login
// route answers 302, not 404/500) and the methods body advertises oidc.
func TestServeAuthMethodsSmokeOIDCEnabled(t *testing.T) {
	idp := discoveryIDP(t)
	cfg := oidcEnabledConfig(t, idp.URL)
	st := testStack(t, cfg)

	if st.authCfg.CurrentOIDC() == nil {
		t.Fatal("live oidc provider nil, want the seeded provider")
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
