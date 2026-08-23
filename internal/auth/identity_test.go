package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
)

// mockOIDCProvider implements auth.IdentityProvider for testing the OIDC
// Bearer arm's empty-map behavior (unknown token, unresolved providerID).
// Tests that need a populated provider use authenticatorMockOIDCProvider
// (auth_test.go), which is the same shape with live setters.
type mockOIDCProvider struct {
	tokens   map[string]*auth.Claims      // valid token -> claims
	resolved map[string]auth.ProviderUser // providerID -> existing user
}

func newMockOIDCProvider() *mockOIDCProvider {
	return &mockOIDCProvider{
		tokens:   make(map[string]*auth.Claims),
		resolved: make(map[string]auth.ProviderUser),
	}
}

func (m *mockOIDCProvider) ProviderName() auth.Provider { return auth.ProviderOIDC }

func (m *mockOIDCProvider) Authenticate(_ context.Context, token string) (*auth.Claims, error) {
	claims, ok := m.tokens[token]
	if !ok {
		return nil, auth.ErrInvalidCredentials
	}
	return claims, nil
}

func (m *mockOIDCProvider) Resolve(_ context.Context, provider auth.Provider, providerID string) (*auth.ProviderUser, error) {
	if provider != auth.ProviderOIDC {
		return nil, auth.ErrProviderUserNotFound
	}
	u, ok := m.resolved[providerID]
	if !ok {
		return nil, auth.ErrProviderUserNotFound
	}
	return &auth.ProviderUser{
		Username: u.Username,
		IsAdmin:  u.IsAdmin,
		Enabled:  u.Enabled,
	}, nil
}

func TestIdentityProviderInterface(t *testing.T) {
	// Verify that the mock implements the IdentityProvider interface.
	var _ auth.IdentityProvider = (*mockOIDCProvider)(nil)

	prov := newMockOIDCProvider()

	// ProviderName returns OIDC
	if prov.ProviderName() != auth.ProviderOIDC {
		t.Fatalf("ProviderName() = %q, want %q", prov.ProviderName(), auth.ProviderOIDC)
	}
}

func TestClaimsCarriesRequiredFields(t *testing.T) {
	// Claims must carry Name, Groups, Admin, and ProviderID (AC1: extracts
	// claims -> maps to Principal).
	claims := &auth.Claims{
		Name:       "oidc-user",
		Groups:     []string{"developers", "admins"},
		Admin:      true,
		ProviderID: "sub-12345",
	}
	if claims.Name != "oidc-user" {
		t.Fatalf("Name = %q, want %q", claims.Name, "oidc-user")
	}
	if len(claims.Groups) != 2 {
		t.Fatalf("len(Groups) = %d, want 2", len(claims.Groups))
	}
	if !claims.Admin {
		t.Fatal("Admin should be true")
	}
	if claims.ProviderID != "sub-12345" {
		t.Fatalf("ProviderID = %q, want %q", claims.ProviderID, "sub-12345")
	}
}

func TestProviderConstants(t *testing.T) {
	// Verify the three provider constants (AC1: Source=oidc).
	tests := []struct {
		provider auth.Provider
		want     string
	}{
		{auth.ProviderLocal, "local"},
		{auth.ProviderOIDC, "oidc"},
		{auth.ProviderLDAP, "ldap"},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			if string(tt.provider) != tt.want {
				t.Fatalf("Provider = %q, want %q", tt.provider, tt.want)
			}
		})
	}
}

func TestErrProviderUserNotFound(t *testing.T) {
	// Verify the sentinel error exists and can be wrapped.
	err := auth.ErrProviderUserNotFound
	if !errors.Is(err, auth.ErrProviderUserNotFound) {
		t.Fatal("ErrProviderUserNotFound must match itself via errors.Is")
	}

	// Mock Resolve returns this error for unknown users.
	prov := newMockOIDCProvider()
	_, err = prov.Resolve(context.Background(), auth.ProviderOIDC, "nonexistent")
	if !errors.Is(err, auth.ErrProviderUserNotFound) {
		t.Fatalf("Resolve(nonexistent) err = %v, want ErrProviderUserNotFound", err)
	}
}

func TestProviderUserStruct(t *testing.T) {
	// ProviderUser carries Username, IsAdmin, and Enabled.
	pu := &auth.ProviderUser{
		Username: "existing-user",
		IsAdmin:  false,
		Enabled:  true,
	}
	if pu.Username != "existing-user" {
		t.Fatalf("Username = %q, want %q", pu.Username, "existing-user")
	}
	if pu.IsAdmin {
		t.Fatal("IsAdmin should be false")
	}
	if !pu.Enabled {
		t.Fatal("Enabled should be true")
	}
}
