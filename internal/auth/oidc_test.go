package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/lzwzzy/binflow/internal/auth"
)

// mockOIDCServer is a minimal OpenID Connect mock that serves the discovery,
// JWKS, and token endpoints. It also supports signing ID Tokens with a
// generated RSA key pair.
type mockOIDCServer struct {
	srv    *httptest.Server
	priv   *rsa.PrivateKey
	keyID  string
	issuer string

	// Configurable behavior for the token endpoint.
	// tokenResponse is the rawClaims JSON that will be signed into an ID Token.
	tokenResponse string
	// tokenErrorCode is returned as a 400 with the given error.
	tokenErrorCode string
}

// newMockOIDCServer starts an httptest server that serves discovery, JWKS, and
// token endpoints. The caller must call Close() to clean up.
func newMockOIDCServer(t *testing.T) *mockOIDCServer {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	m := &mockOIDCServer{
		priv:  priv,
		keyID: "test-key-1",
	}

	m.srv = httptest.NewServer(m)
	m.issuer = m.srv.URL
	return m
}

func (m *mockOIDCServer) Close() { m.srv.Close() }

func (m *mockOIDCServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		m.serveDiscovery(w, r)
	case "/keys":
		m.serveKeys(w, r)
	case "/token":
		m.serveToken(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// writeJSON encodes v into w; a mock-server write failure is a broken test
// fixture, so it panics loudly instead of failing every downstream assertion
// with a confusing decode error.
func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic("oidc mock: encode response: " + err.Error())
	}
}

func (m *mockOIDCServer) serveDiscovery(w http.ResponseWriter, _ *http.Request) {
	disc := map[string]any{
		"issuer":                                m.issuer,
		"authorization_endpoint":                m.issuer + "/auth",
		"token_endpoint":                        m.issuer + "/token",
		"jwks_uri":                              m.issuer + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, disc)
}

func (m *mockOIDCServer) serveKeys(w http.ResponseWriter, _ *http.Request) {
	set := &jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{
				Key:       m.priv.Public(),
				KeyID:     m.keyID,
				Algorithm: "RS256",
				Use:       "sig",
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, set)
}

func (m *mockOIDCServer) serveToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if m.tokenErrorCode != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{
			"error":             m.tokenErrorCode,
			"error_description": "mock token error",
		})
		return
	}

	// Sign the claims as an ID Token.
	claims := m.tokenResponse
	if claims == "" {
		claims = m.defaultClaims()
	}
	idToken := m.signIDToken(claims)

	resp := map[string]any{
		"access_token": "mock-access-token",
		"token_type":   "Bearer",
		"id_token":     idToken,
		"expires_in":   3600,
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, resp)
}

func (m *mockOIDCServer) defaultClaims() string {
	now := time.Now()
	return fmt.Sprintf(`{
		"iss": %q,
		"aud": "test-client-id",
		"sub": "test-sub-123",
		"preferred_username": "mockuser",
		"email": "mockuser@example.com",
		"email_verified": true,
		"groups": ["developers", "viewers"],
		"iat": %d,
		"exp": %d
	}`, m.issuer, now.Unix(), now.Add(time.Hour).Unix())
}

func (m *mockOIDCServer) signIDToken(claims string) string {
	key := jose.SigningKey{
		Algorithm: jose.RS256,
		Key:       m.priv,
	}
	opts := &jose.SignerOptions{
		ExtraHeaders: map[jose.HeaderKey]any{
			jose.HeaderKey("kid"): m.keyID,
		},
	}
	signer, err := jose.NewSigner(key, opts)
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

// newProvider creates a go-oidc Provider from the mock server and a
// OIDCProvider from that.
func (m *mockOIDCServer) newProvider(ctx context.Context, t *testing.T, cfg *auth.OIDCConfig, resolver auth.OIDCUserResolver) *auth.OIDCProvider {
	t.Helper()
	cfg.IssuerURL = m.issuer
	p, err := auth.NewOIDCProvider(ctx, cfg, resolver)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return p
}

// newOIDCConfig returns a minimal config pointing at the mock server.
func (m *mockOIDCServer) newOIDCConfig() *auth.OIDCConfig {
	return &auth.OIDCConfig{
		IssuerURL:    m.issuer,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/binflow/api/v1/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		UserClaim:    "preferred_username",
		GroupClaim:   "groups",
		AdminGroup:   "admins",
	}
}

// mockResolver implements OIDCUserResolver.
type mockResolver struct {
	users map[string]*auth.ProviderUser // keyed by providerID
}

func newMockResolver() *mockResolver {
	return &mockResolver{users: make(map[string]*auth.ProviderUser)}
}

func (r *mockResolver) GetByProvider(_ context.Context, _ auth.Provider, providerID string) (*auth.ProviderUser, error) {
	u, ok := r.users[providerID]
	if !ok {
		return nil, auth.ErrProviderUserNotFound
	}
	return &auth.ProviderUser{
		Username: u.Username,
		IsAdmin:  u.IsAdmin,
		Enabled:  u.Enabled,
	}, nil
}

func (r *mockResolver) addUser(providerID string, u *auth.ProviderUser) {
	r.users[providerID] = u
}

// --- Tests ---

func TestOIDCProvider_NewProvider(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()

	t.Run("successful discovery", func(t *testing.T) {
		cfg := m.newOIDCConfig()
		p, err := auth.NewOIDCProvider(ctx, cfg, nil)
		if err != nil {
			t.Fatalf("NewOIDCProvider failed: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil OIDCProvider")
		}
		if p.ProviderName() != auth.ProviderOIDC {
			t.Fatalf("ProviderName() = %q, want %q", p.ProviderName(), auth.ProviderOIDC)
		}
	})

	t.Run("missing issuer_url", func(t *testing.T) {
		cfg := &auth.OIDCConfig{
			ClientID:    "test-client-id",
			RedirectURL: "http://localhost/callback",
		}
		_, err := auth.NewOIDCProvider(ctx, cfg, nil)
		if err == nil {
			t.Fatal("expected error for missing issuer_url")
		}
	})

	t.Run("missing client_id", func(t *testing.T) {
		cfg := &auth.OIDCConfig{
			IssuerURL:   m.issuer,
			RedirectURL: "http://localhost/callback",
		}
		_, err := auth.NewOIDCProvider(ctx, cfg, nil)
		if err == nil {
			t.Fatal("expected error for missing client_id")
		}
	})

	t.Run("missing redirect_url", func(t *testing.T) {
		cfg := &auth.OIDCConfig{
			IssuerURL: m.issuer,
			ClientID:  "test-client-id",
		}
		_, err := auth.NewOIDCProvider(ctx, cfg, nil)
		if err == nil {
			t.Fatal("expected error for missing redirect_url")
		}
	})

	t.Run("defaults applied", func(t *testing.T) {
		cfg := &auth.OIDCConfig{
			IssuerURL:    m.issuer,
			ClientID:     "test-client-id",
			ClientSecret: "test-secret",
			RedirectURL:  "http://localhost/callback",
		}
		// Scopes, UserClaim, GroupClaim are empty -> should get defaults.
		p, err := auth.NewOIDCProvider(ctx, cfg, nil)
		if err != nil {
			t.Fatalf("NewOIDCProvider: %v", err)
		}
		oc := p.OAuth2Config()
		if len(oc.Scopes) != 3 {
			t.Fatalf("expected 3 default scopes, got %d: %v", len(oc.Scopes), oc.Scopes)
		}
	})
}

func TestOIDCProvider_Authenticate_Success(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	cfg.AdminGroup = "admins" // mockuser is not in admins

	prov := m.newProvider(ctx, t, cfg, nil)

	// Sign a valid ID token.
	now := time.Now()
	claims := fmt.Sprintf(`{
		"iss": %q,
		"aud": "test-client-id",
		"sub": "test-sub-123",
		"preferred_username": "mockuser",
		"email": "mockuser@example.com",
		"groups": ["developers", "viewers"],
		"iat": %d,
		"exp": %d
	}`, m.issuer, now.Unix(), now.Add(time.Hour).Unix())
	token := m.signIDToken(claims)

	result, err := prov.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if result.Name != "mockuser" {
		t.Fatalf("Name = %q, want %q", result.Name, "mockuser")
	}
	if result.ProviderID != "test-sub-123" {
		t.Fatalf("ProviderID = %q, want %q", result.ProviderID, "test-sub-123")
	}
	if len(result.Groups) != 2 {
		t.Fatalf("len(Groups) = %d, want 2", len(result.Groups))
	}
	if result.Admin {
		t.Fatal("Admin should be false (mockuser is not in admins)")
	}
}

func TestOIDCProvider_Authenticate_AdminGroup(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	cfg.AdminGroup = "admins"

	prov := m.newProvider(ctx, t, cfg, nil)

	now := time.Now()
	claims := fmt.Sprintf(`{
		"iss": %q,
		"aud": "test-client-id",
		"sub": "admin-sub",
		"preferred_username": "adminuser",
		"groups": ["developers", "admins", "viewers"],
		"iat": %d,
		"exp": %d
	}`, m.issuer, now.Unix(), now.Add(time.Hour).Unix())
	token := m.signIDToken(claims)

	result, err := prov.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if !result.Admin {
		t.Fatal("Admin should be true (adminuser is in admins group)")
	}
	if result.Name != "adminuser" {
		t.Fatalf("Name = %q, want %q", result.Name, "adminuser")
	}
}

func TestOIDCProvider_Authenticate_CustomClaims(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	cfg.UserClaim = "email"
	cfg.GroupClaim = "roles"

	prov := m.newProvider(ctx, t, cfg, nil)

	now := time.Now()
	claims := fmt.Sprintf(`{
		"iss": %q,
		"aud": "test-client-id",
		"sub": "custom-sub",
		"email": "custom@example.com",
		"roles": ["role-a", "role-b"],
		"iat": %d,
		"exp": %d
	}`, m.issuer, now.Unix(), now.Add(time.Hour).Unix())
	token := m.signIDToken(claims)

	result, err := prov.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if result.Name != "custom@example.com" {
		t.Fatalf("Name = %q, want %q", result.Name, "custom@example.com")
	}
	if len(result.Groups) != 2 {
		t.Fatalf("len(Groups) = %d, want 2", len(result.Groups))
	}
}

func TestOIDCProvider_Authenticate_FallbackToSub(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	cfg.UserClaim = "nonexistent_claim"

	prov := m.newProvider(ctx, t, cfg, nil)

	now := time.Now()
	claims := fmt.Sprintf(`{
		"iss": %q,
		"aud": "test-client-id",
		"sub": "fallback-sub",
		"iat": %d,
		"exp": %d
	}`, m.issuer, now.Unix(), now.Add(time.Hour).Unix())
	token := m.signIDToken(claims)

	result, err := prov.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if result.Name != "fallback-sub" {
		t.Fatalf("Name = %q, want %q (fallback to sub)", result.Name, "fallback-sub")
	}
}

func TestOIDCProvider_Authenticate_ErrorCases(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	prov := m.newProvider(ctx, t, cfg, nil)

	tests := []struct {
		name   string
		token  string
		errMsg string
	}{
		{
			name: "expired token",
			token: func() string {
				now := time.Now()
				claims := fmt.Sprintf(`{
					"iss": %q,
					"aud": "test-client-id",
					"sub": "expired-sub",
					"preferred_username": "expireduser",
					"iat": %d,
					"exp": %d
				}`, m.issuer, now.Add(-2*time.Hour).Unix(), now.Add(-1*time.Hour).Unix())
				return m.signIDToken(claims)
			}(),
			errMsg: "expired",
		},
		{
			name: "wrong issuer",
			token: func() string {
				now := time.Now()
				claims := fmt.Sprintf(`{
					"iss": "https://evil.example.com",
					"aud": "test-client-id",
					"sub": "evil-sub",
					"preferred_username": "eviluser",
					"iat": %d,
					"exp": %d
				}`, now.Unix(), now.Add(time.Hour).Unix())
				return m.signIDToken(claims)
			}(),
			errMsg: "invalid",
		},
		{
			name: "wrong audience",
			token: func() string {
				now := time.Now()
				claims := fmt.Sprintf(`{
					"iss": %q,
					"aud": "wrong-client-id",
					"sub": "wrong-aud-sub",
					"preferred_username": "wrongauduser",
					"iat": %d,
					"exp": %d
				}`, m.issuer, now.Unix(), now.Add(time.Hour).Unix())
				return m.signIDToken(claims)
			}(),
			errMsg: "invalid",
		},
		{
			name:   "garbage token",
			token:  "not-a-jwt-token-at-all",
			errMsg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := prov.Authenticate(ctx, tt.token)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.errMsg != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.errMsg)) {
				t.Logf("error does not contain %q: %v", tt.errMsg, err)
			}
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("error should wrap ErrInvalidCredentials, got: %v", err)
			}
		})
	}
}

func TestOIDCProvider_Authenticate_GroupClaimVariants(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	prov := m.newProvider(ctx, t, cfg, nil)

	tests := []struct {
		name       string
		groupValue string // JSON value for the "groups" claim
		wantLen    int
	}{
		{
			name:       "array of strings",
			groupValue: `["g1", "g2", "g3"]`,
			wantLen:    3,
		},
		{
			name:       "single string",
			groupValue: `"single-group"`,
			wantLen:    1,
		},
		{
			name:       "empty array",
			groupValue: `[]`,
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			claims := fmt.Sprintf(`{
				"iss": %q,
				"aud": "test-client-id",
				"sub": "group-sub",
				"preferred_username": "groupuser",
				"groups": %s,
				"iat": %d,
				"exp": %d
			}`, m.issuer, tt.groupValue, now.Unix(), now.Add(time.Hour).Unix())
			token := m.signIDToken(claims)

			result, err := prov.Authenticate(ctx, token)
			if err != nil {
				t.Fatalf("Authenticate failed: %v", err)
			}
			if len(result.Groups) != tt.wantLen {
				t.Fatalf("len(Groups) = %d, want %d", len(result.Groups), tt.wantLen)
			}
		})
	}
}

func TestOIDCProvider_Resolve(t *testing.T) {
	ctx := context.Background()
	resolver := newMockResolver()
	resolver.addUser("test-sub-123", &auth.ProviderUser{
		Username: "existing-user",
		IsAdmin:  false,
		Enabled:  true,
	})

	m := newMockOIDCServer(t)
	defer m.Close()

	cfg := m.newOIDCConfig()
	prov := m.newProvider(ctx, t, cfg, resolver)

	t.Run("resolve existing user", func(t *testing.T) {
		pu, err := prov.Resolve(ctx, auth.ProviderOIDC, "test-sub-123")
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if pu.Username != "existing-user" {
			t.Fatalf("Username = %q, want %q", pu.Username, "existing-user")
		}
		if !pu.Enabled {
			t.Fatal("Enabled should be true")
		}
	})

	t.Run("resolve non-existent user", func(t *testing.T) {
		_, err := prov.Resolve(ctx, auth.ProviderOIDC, "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent user")
		}
		if !errors.Is(err, auth.ErrProviderUserNotFound) {
			t.Fatalf("error should be ErrProviderUserNotFound, got: %v", err)
		}
	})

	t.Run("resolve with nil resolver", func(t *testing.T) {
		provNoResolver := m.newProvider(ctx, t, cfg, nil)
		_, err := provNoResolver.Resolve(ctx, auth.ProviderOIDC, "any-id")
		if err == nil {
			t.Fatal("expected error when resolver is nil")
		}
		if !errors.Is(err, auth.ErrProviderUserNotFound) {
			t.Fatalf("error should be ErrProviderUserNotFound, got: %v", err)
		}
	})
}

func TestOIDCProvider_ProviderName(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	prov := m.newProvider(ctx, t, cfg, nil)

	if prov.ProviderName() != auth.ProviderOIDC {
		t.Fatalf("ProviderName() = %q, want %q", prov.ProviderName(), auth.ProviderOIDC)
	}
}

func TestOIDCProvider_ImplementsIdentityProvider(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	prov := m.newProvider(ctx, t, cfg, nil)

	// Verify interface satisfaction at compile time.
	var _ auth.IdentityProvider = prov
}

func TestPKCE(t *testing.T) {
	t.Run("generate PKCE pair", func(t *testing.T) {
		verifier, challenge, err := auth.GeneratePKCEPair()
		if err != nil {
			t.Fatalf("GeneratePKCEPair failed: %v", err)
		}
		if verifier == "" {
			t.Fatal("verifier is empty")
		}
		if challenge == "" {
			t.Fatal("challenge is empty")
		}
		// Verifier must be at least 43 characters (RFC 7636 minimum).
		if len(verifier) < 43 {
			t.Fatalf("verifier too short: %d chars", len(verifier))
		}
		if len(verifier) > 128 {
			t.Fatalf("verifier too long: %d chars", len(verifier))
		}
		// Verifier and challenge should be different.
		if verifier == challenge {
			t.Fatal("verifier and challenge should be different")
		}
	})

	t.Run("verify S256 challenge", func(t *testing.T) {
		// Generate a known verifier and verify the challenge is correct.
		verifier, challenge, err := auth.GeneratePKCEPair()
		if err != nil {
			t.Fatalf("GeneratePKCEPair failed: %v", err)
		}

		// Manually compute the S256 challenge.
		h := sha256.New()
		h.Write([]byte(verifier))
		expected := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

		if challenge != expected {
			t.Fatalf("challenge = %q, want %q", challenge, expected)
		}
	})

	t.Run("multiple PKCE pairs are unique", func(t *testing.T) {
		v1, _, err1 := auth.GeneratePKCEPair()
		v2, _, err2 := auth.GeneratePKCEPair()
		if err1 != nil || err2 != nil {
			t.Fatalf("GeneratePKCEPair failed: %v, %v", err1, err2)
		}
		if v1 == v2 {
			t.Fatal("two verifiers should be unique")
		}
	})
}

func TestOIDCConfig_Defaults(t *testing.T) {
	t.Run("zero config gets defaults", func(t *testing.T) {
		cfg := &auth.OIDCConfig{
			IssuerURL:    "https://example.com",
			ClientID:     "test-client",
			ClientSecret: "secret",
			RedirectURL:  "http://localhost/callback",
		}
		// Scopes, UserClaim, GroupClaim are empty.
		m := newMockOIDCServer(t)
		defer m.Close()

		cfg.IssuerURL = m.issuer
		prov, err := auth.NewOIDCProvider(context.Background(), cfg, nil)
		if err != nil {
			t.Fatalf("NewOIDCProvider: %v", err)
		}

		oc := prov.OAuth2Config()
		if len(oc.Scopes) != 3 {
			t.Fatalf("expected 3 default scopes, got %d", len(oc.Scopes))
		}
		if oc.Scopes[0] != "openid" || oc.Scopes[1] != "profile" || oc.Scopes[2] != "email" {
			t.Fatalf("default scopes mismatch: %v", oc.Scopes)
		}
	})

	t.Run("custom scopes override defaults", func(t *testing.T) {
		cfg := &auth.OIDCConfig{
			IssuerURL:    "https://example.com",
			ClientID:     "test-client",
			ClientSecret: "secret",
			RedirectURL:  "http://localhost/callback",
			Scopes:       []string{"openid", "offline_access"},
		}
		m := newMockOIDCServer(t)
		defer m.Close()

		cfg.IssuerURL = m.issuer
		prov, err := auth.NewOIDCProvider(context.Background(), cfg, nil)
		if err != nil {
			t.Fatalf("NewOIDCProvider: %v", err)
		}

		oc := prov.OAuth2Config()
		if len(oc.Scopes) != 2 {
			t.Fatalf("expected 2 custom scopes, got %d", len(oc.Scopes))
		}
	})
}

func TestOIDCProvider_OAuth2(t *testing.T) {
	m := newMockOIDCServer(t)
	defer m.Close()

	ctx := context.Background()
	cfg := m.newOIDCConfig()
	prov := m.newProvider(ctx, t, cfg, nil)

	oauth2Cfg := prov.OAuth2Config()
	if oauth2Cfg.ClientID != "test-client-id" {
		t.Fatalf("ClientID = %q, want %q", oauth2Cfg.ClientID, "test-client-id")
	}
	if oauth2Cfg.ClientSecret != "test-client-secret" {
		t.Fatalf("ClientSecret = %q, want %q", oauth2Cfg.ClientSecret, "test-client-secret")
	}
	if oauth2Cfg.RedirectURL != "http://localhost:8080/binflow/api/v1/oidc/callback" {
		t.Fatalf("RedirectURL = %q", oauth2Cfg.RedirectURL)
	}
	if oauth2Cfg.Endpoint.AuthURL != m.issuer+"/auth" {
		t.Fatalf("AuthURL = %q, want %q", oauth2Cfg.Endpoint.AuthURL, m.issuer+"/auth")
	}
	if oauth2Cfg.Endpoint.TokenURL != m.issuer+"/token" {
		t.Fatalf("TokenURL = %q, want %q", oauth2Cfg.Endpoint.TokenURL, m.issuer+"/token")
	}
}

func TestOIDCProvider_Authenticate_DisabledUser(t *testing.T) {
	// This test verifies that the OIDC Bearer arm correctly rejects disabled
	// users. The disabled check is in the Service.authenticateOIDC method,
	// not in the OIDCProvider itself, but we can verify the Resolve path
	// returns a disabled user and the caller handles it.
	ctx := context.Background()
	resolver := newMockResolver()
	resolver.addUser("disabled-sub", &auth.ProviderUser{
		Username: "disabled-user",
		IsAdmin:  false,
		Enabled:  false,
	})

	m := newMockOIDCServer(t)
	defer m.Close()

	cfg := m.newOIDCConfig()
	prov := m.newProvider(ctx, t, cfg, resolver)

	pu, err := prov.Resolve(ctx, auth.ProviderOIDC, "disabled-sub")
	if err != nil {
		t.Fatalf("Resolve should succeed for disabled user (check is in caller): %v", err)
	}
	if pu.Enabled {
		t.Fatal("Enabled should be false for disabled user")
	}
}
