package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// fixture is one auth test world: a fresh SQLite store, seeded users,
// one permission target, and the Service built over them.
type fixture struct {
	t   *testing.T
	st  metadata.Store
	svc *auth.Service
	ctx context.Context
}

const (
	adminPW   = "it-admin-pw"
	ciPW      = "ci-bot-pw" //nolint:gosec // throwaway test fixture password
	otherUser = "other"
)

func newFixture(t *testing.T, anonymousRead bool) *fixture {
	t.Helper()
	st, err := metadata.Open(context.Background(), metadata.Options{
		Driver:        "sqlite",
		Path:          filepath.Join(t.TempDir(), "binflow.db"),
		AdminPassword: adminPW,
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	f := &fixture{t: t, st: st, svc: auth.NewFromStore(st, anonymousRead), ctx: context.Background()}
	f.createUser("ci-bot", ciPW, false)
	f.createUser(otherUser, "other-pw", false)
	return f
}

func (f *fixture) createUser(name, password string, admin bool) {
	f.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		f.t.Fatalf("HashPassword(%q): %v", name, err)
	}
	now := metadata.Now()
	err = f.st.Users().Create(f.ctx, &metadata.User{
		Username: name, PasswordHash: hash, IsAdmin: admin, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		f.t.Fatalf("create user %q: %v", name, err)
	}
}

// putTarget installs one named permission target (PRD E-24 shape).
func (f *fixture) putTarget(name string, repos, includes, excludes []string, principal string, read, write, del bool) {
	f.t.Helper()
	mk := func(list []string) string {
		if list == nil {
			list = []string{}
		}
		b, err := json.Marshal(list)
		if err != nil {
			f.t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	now := metadata.Now()
	err := f.st.Permissions().PutTarget(f.ctx,
		&metadata.PermissionTarget{
			Name: name, Repos: mk(repos), Includes: mk(includes), Excludes: mk(excludes),
			CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{{
			TargetName: name, Principal: principal, PrincipalType: "user",
			CanRead: read, CanWrite: write, CanDelete: del,
		}})
	if err != nil {
		f.t.Fatalf("put target %q: %v", name, err)
	}
}

func req(authz, apiKey string) *http.Request {
	r, err := http.NewRequest(http.MethodGet, "/binflow/generic-local/a.bin", nil)
	if err != nil {
		panic(err)
	}
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}
	if apiKey != "" {
		r.Header.Set("X-JFrog-Art-Api", apiKey)
	}
	return r
}

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// --- AC 1: Authenticator ---

// Table: every entry point resolves, every wrong form fails closed.
func TestAuthenticate(t *testing.T) {
	f := newFixture(t, true)

	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	tests := []struct {
		name      string
		authz     string
		apiKey    string
		wantName  string
		wantAdmin bool
		wantToken bool
		wantErr   bool
	}{
		{"basic password admin", basic("admin", adminPW), "", "admin", true, false, false},
		{"basic password ci-bot", basic("ci-bot", ciPW), "", "ci-bot", false, false, false},
		{"basic wrong password", basic("admin", "nope"), "", "", false, false, true},
		{"basic unknown user", basic("ghost", "nope"), "", "", false, false, true},
		{"basic token as password", basic("ci-bot", tok.AccessToken), "", "ci-bot", false, true, false},
		{"basic token wrong subject", basic("other", tok.AccessToken), "", "", false, false, true},
		{"basic token subject case-insensitive", basic("CI-BOT", tok.AccessToken), "", "ci-bot", false, true, false},
		{"basic malformed base64", "Basic %%%%", "", "", false, false, true},
		{"basic no colon", "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")), "", "", false, false, true},
		{"api key header", "", tok.AccessToken, "ci-bot", false, true, false},
		{"api key legacy header", "", tok.AccessToken, "ci-bot", false, true, false},
		{"api key garbage", "", "not-a-token", "", false, false, true},
		{"bearer token", "Bearer " + tok.AccessToken, "", "ci-bot", false, true, false},
		{"bearer garbage", "Bearer not-a-token", "", "", false, false, true},
		{"unknown scheme falls to api key", "Digest xyz", tok.AccessToken, "ci-bot", false, true, false},
		{"anonymous", "", "", "", false, false, false},
		{"unknown scheme no other credential", "Digest xyz", "", "", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := f.svc.Authenticate(f.ctx, req(tt.authz, tt.apiKey))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got principal %+v", p)
				}
				if !errors.Is(err, auth.ErrInvalidCredentials) {
					t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantName == "" {
				if p != nil {
					t.Fatalf("expected anonymous, got %+v", p)
				}
				return
			}
			if p == nil {
				t.Fatal("expected a principal, got anonymous")
			}
			if p.Name != tt.wantName || p.Admin != tt.wantAdmin {
				t.Fatalf("principal = %+v, want name=%q admin=%v", p, tt.wantName, tt.wantAdmin)
			}
			if tt.wantToken && p.TokenID == 0 {
				t.Fatalf("expected TokenID > 0, got %+v", p)
			}
		})
	}
}

// NFR-S3: no error message leaks the presented credential.
func TestAuthenticateNeverLeaksSecret(t *testing.T) {
	f := newFixture(t, true)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	secret := tok.AccessToken
	// Three rejected presentations of the same secret: as a wrong Basic
	// password, as an API key with the value mangled to be unknown (fresh
	// secret, unknown digest), and as a Basic password with the wrong
	// subject (principal mismatch).
	mangled := secret[:len(secret)-1] + "0"
	if mangled == secret {
		mangled = secret[:len(secret)-1] + "1"
	}
	for _, tc := range []struct {
		name string
		r    *http.Request
	}{
		{"basic token with wrong subject", req(basic("admin", secret), "")},
		{"unknown api key", req("", mangled)},
		{"malformed basic", req("Basic %%%%", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err = f.svc.Authenticate(f.ctx, tc.r)
			if err == nil {
				t.Fatal("expected failure")
			}
			for _, leak := range []string{secret, mangled} {
				if strings.Contains(err.Error(), leak) {
					t.Fatalf("error leaks the secret: %q", err)
				}
			}
		})
	}
}

// Disabled users fail closed on every entry point. UserStore has no
// SetEnabled in M1, so the fixture recreates the world with the row's
// enabled flag flipped through a delete+recreate preserving the hash.
func TestAuthenticateDisabledUser(t *testing.T) {
	f := newFixture(t, true)
	// Snapshot the hash, then re-create the row disabled.
	u, err := f.st.Users().Get(f.ctx, "ci-bot")
	if err != nil {
		t.Fatalf("get ci-bot: %v", err)
	}
	if err := f.st.Users().Delete(f.ctx, "ci-bot"); err != nil {
		t.Fatalf("delete ci-bot: %v", err)
	}
	now := metadata.Now()
	if err := f.st.Users().Create(f.ctx, &metadata.User{
		Username: "ci-bot", PasswordHash: u.PasswordHash, Enabled: false,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("recreate disabled: %v", err)
	}

	if _, err := f.svc.Authenticate(f.ctx, req(basic("ci-bot", ciPW), "")); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled user basic err = %v, want ErrInvalidCredentials", err)
	}
}

// A token whose owner was disabled stops working (owner check in Verify).
func TestTokenDisabledOwner(t *testing.T) {
	f := newFixture(t, true)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); err != nil {
		t.Fatalf("Verify before disable: %v", err)
	}
	u, err := f.st.Users().Get(f.ctx, "ci-bot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := f.st.Users().Delete(f.ctx, "ci-bot"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	now := metadata.Now()
	if err := f.st.Users().Create(f.ctx, &metadata.User{
		Username: "ci-bot", PasswordHash: u.PasswordHash, Enabled: false,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("recreate disabled: %v", err)
	}
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Verify after disable err = %v, want ErrInvalidCredentials", err)
	}
	// Basic with the token as password must fail the same way.
	if _, err := f.svc.Authenticate(f.ctx, req(basic("ci-bot", tok.AccessToken), "")); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("basic-token after disable err = %v, want ErrInvalidCredentials", err)
	}
}

// --- M6 AC: OIDC Bearer arm (ADR-0020, T-153) ---

// mockOIDCProvider is a test double for auth.IdentityProvider. It validates
// against a hard-coded token map and resolves against a user map.
// NOTE: this duplicates the mock from identity_test.go because the test
// package is auth_test (external), so each test file needs its own definition.
type authenticatorMockOIDCProvider struct {
	tokens   map[string]*auth.Claims
	resolved map[string]auth.ProviderUser
}

func newAuthenticatorMockOIDCProvider() *authenticatorMockOIDCProvider {
	return &authenticatorMockOIDCProvider{
		tokens:   make(map[string]*auth.Claims),
		resolved: make(map[string]auth.ProviderUser),
	}
}

func (m *authenticatorMockOIDCProvider) ProviderName() auth.Provider { return auth.ProviderOIDC }
func (m *authenticatorMockOIDCProvider) Authenticate(_ context.Context, token string) (*auth.Claims, error) {
	claims, ok := m.tokens[token]
	if !ok {
		return nil, auth.ErrInvalidCredentials
	}
	return claims, nil
}
func (m *authenticatorMockOIDCProvider) Resolve(_ context.Context, provider auth.Provider, providerID string) (*auth.ProviderUser, error) {
	if provider != auth.ProviderOIDC {
		return nil, auth.ErrProviderUserNotFound
	}
	u, ok := m.resolved[providerID]
	if !ok {
		return nil, auth.ErrProviderUserNotFound
	}
	return &auth.ProviderUser{Username: u.Username, IsAdmin: u.IsAdmin, Enabled: u.Enabled}, nil
}
func (m *authenticatorMockOIDCProvider) addValidToken(token string, claims *auth.Claims) {
	m.tokens[token] = claims
}
func (m *authenticatorMockOIDCProvider) addResolvedUser(providerID string, u auth.ProviderUser) {
	m.resolved[providerID] = u
}

// mockUserCreator captures the last user passed to Create and returns success.
type authenticatorMockUserCreator struct {
	created []auth.NewUserParams
}

func (m *authenticatorMockUserCreator) Create(ctx context.Context, params auth.NewUserParams) error {
	m.created = append(m.created, params)
	return nil
}

// TestAuthenticateOIDCValidToken: a valid OIDC ID Token is accepted and
// produces the correct Principal with Source=oidc.
func TestAuthenticateOIDCValidToken(t *testing.T) {
	f := newFixture(t, true)
	oidcProv := newAuthenticatorMockOIDCProvider()
	oidcProv.addValidToken("valid-oidc-token", &auth.Claims{
		Name:       "oidc-user",
		Groups:     []string{"developers"},
		Admin:      false,
		ProviderID: "sub-abc123",
	})
	// Pre-create the user in the local store so Resolve finds it.
	hash, _ := auth.HashPassword("dummy")
	err := f.st.Users().Create(f.ctx, &metadata.User{
		Username: "oidc-user", PasswordHash: hash, IsAdmin: false, Enabled: true,
		CreatedAt: metadata.Now(), UpdatedAt: metadata.Now(),
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	oidcProv.addResolvedUser("sub-abc123", auth.ProviderUser{Username: "oidc-user", IsAdmin: false, Enabled: true})

	svc := f.svc.WithOIDC(oidcProv, nil)
	p, err := svc.Authenticate(f.ctx, req("Bearer valid-oidc-token", ""))
	if err != nil {
		t.Fatalf("Authenticate(valid token) = %v", err)
	}
	if p == nil {
		t.Fatal("expected principal, got nil")
	}
	if p.Name != "oidc-user" {
		t.Fatalf("Name = %q, want %q", p.Name, "oidc-user")
	}
	if p.Source != auth.ProviderOIDC {
		t.Fatalf("Source = %q, want %q", p.Source, auth.ProviderOIDC)
	}
	if p.Admin {
		t.Fatal("Admin should be false")
	}
	if len(p.Groups) != 1 || p.Groups[0] != "developers" {
		t.Fatalf("Groups = %v, want [developers]", p.Groups)
	}
}

// TestAuthenticateOIDCInvalidToken: an invalid OIDC token is rejected.
func TestAuthenticateOIDCInvalidToken(t *testing.T) {
	f := newFixture(t, true)
	oidcProv := newAuthenticatorMockOIDCProvider()
	// No tokens registered — every token is invalid.
	svc := f.svc.WithOIDC(oidcProv, nil)
	_, err := svc.Authenticate(f.ctx, req("Bearer invalid-token", ""))
	if err == nil {
		t.Fatal("expected error for invalid OIDC token, got nil")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
	}
}

// TestAuthenticateOIDCWithAPIToken: an API token presented as Bearer is
// still handled by the API token arm (the OIDC arm only activates when the
// API token verification fails). This ensures the existing Bearer arm
// precedence is preserved.
func TestAuthenticateOIDCWithAPIToken(t *testing.T) {
	f := newFixture(t, true)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	oidcProv := newAuthenticatorMockOIDCProvider()
	svc := f.svc.WithOIDC(oidcProv, nil)
	p, err := svc.Authenticate(f.ctx, req("Bearer "+tok.AccessToken, ""))
	if err != nil {
		t.Fatalf("Authenticate(API token) = %v", err)
	}
	if p == nil {
		t.Fatal("expected principal, got nil")
	}
	if p.Name != "ci-bot" {
		t.Fatalf("Name = %q, want %q", p.Name, "ci-bot")
	}
	if p.Source != auth.ProviderLocal {
		t.Fatalf("Source = %q, want %q (API token should be local)", p.Source, auth.ProviderLocal)
	}
	if p.TokenID == 0 {
		t.Fatal("expected TokenID > 0 for API token")
	}
}

// TestAuthenticateOIDCAutoCreate: a valid OIDC token for a user that does
// not yet exist auto-creates the user row (AC2: OIDC users auto-create
// users rows on first auth).
func TestAuthenticateOIDCAutoCreate(t *testing.T) {
	f := newFixture(t, true)
	oidcProv := newAuthenticatorMockOIDCProvider()
	oidcProv.addValidToken("first-login-token", &auth.Claims{
		Name:       "new-oidc-user",
		Groups:     nil,
		Admin:      true,
		ProviderID: "sub-newuser",
	})
	// No resolved user — triggers auto-create.
	svc := f.svc.WithOIDC(oidcProv, &simpleUserCreator{})
	p, err := svc.Authenticate(f.ctx, req("Bearer first-login-token", ""))
	if err != nil {
		t.Fatalf("Authenticate with auto-create = %v", err)
	}
	if p == nil {
		t.Fatal("expected principal, got nil")
	}
	if p.Name != "new-oidc-user" {
		t.Fatalf("Name = %q, want %q", p.Name, "new-oidc-user")
	}
	if p.Source != auth.ProviderOIDC {
		t.Fatalf("Source = %q, want %q", p.Source, auth.ProviderOIDC)
	}
	if !p.Admin {
		t.Fatal("Admin should be true (from claims)")
	}
}

// TestAuthenticateOIDCNoAutoCreate: when auto-create is disabled, a valid
// token for an unknown user is rejected.
func TestAuthenticateOIDCNoAutoCreate(t *testing.T) {
	f := newFixture(t, true)
	oidcProv := newAuthenticatorMockOIDCProvider()
	oidcProv.addValidToken("valid-but-unknown", &auth.Claims{
		Name:       "ghost",
		Groups:     nil,
		Admin:      false,
		ProviderID: "sub-ghost",
	})
	// No resolved user AND nil userCreator — auto-create disabled.
	svc := f.svc.WithOIDC(oidcProv, nil)
	_, err := svc.Authenticate(f.ctx, req("Bearer valid-but-unknown", ""))
	if err == nil {
		t.Fatal("expected error for unresolvable OIDC user without auto-create, got nil")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
	}
}

// TestAuthenticateOIDCDisabledUser: a valid OIDC token for an existing but
// disabled user is rejected.
func TestAuthenticateOIDCDisabledUser(t *testing.T) {
	f := newFixture(t, true)
	oidcProv := newAuthenticatorMockOIDCProvider()
	oidcProv.addValidToken("disabled-token", &auth.Claims{
		Name:       "disabled-oidc",
		Groups:     nil,
		Admin:      false,
		ProviderID: "sub-disabled",
	})
	// Pre-create the user disabled.
	hash, _ := auth.HashPassword("dummy")
	err := f.st.Users().Create(f.ctx, &metadata.User{
		Username: "disabled-oidc", PasswordHash: hash, IsAdmin: false, Enabled: false,
		CreatedAt: metadata.Now(), UpdatedAt: metadata.Now(),
	})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	oidcProv.addResolvedUser("sub-disabled", auth.ProviderUser{Username: "disabled-oidc", IsAdmin: false, Enabled: false})

	svc := f.svc.WithOIDC(oidcProv, nil)
	_, err = svc.Authenticate(f.ctx, req("Bearer disabled-token", ""))
	if err == nil {
		t.Fatal("expected error for disabled user, got nil")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
	}
}

// TestAuthenticateOIDCUnwired: when the OIDC arm is not wired, unknown
// Bearer tokens still produce an error (the API token arm rejects them).
func TestAuthenticateOIDCUnwired(t *testing.T) {
	f := newFixture(t, true)
	// No WithOIDC call — arm is inert.
	_, err := f.svc.Authenticate(f.ctx, req("Bearer unknown-token", ""))
	if err == nil {
		t.Fatal("expected error for unknown Bearer token without OIDC arm, got nil")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
	}
}

// simpleUserCreator implements the userCreator interface for tests.
// It records created users but does not persist them to the store (the
// OIDC arm only needs the interface; the actual persistence is the
// adapter's job in production).
type simpleUserCreator struct {
	created []auth.NewUserParams
}

func (c *simpleUserCreator) Create(ctx context.Context, params auth.NewUserParams) error {
	c.created = append(c.created, params)
	return nil
}
