// T-179 acceptance surface, auth layer: the exported auto-create constructor
// (T-157 leftover 2 — cmd could not obtain a userCreator outside the
// package, so WithOIDC's creator parameter was either nil, silently
// disabling auto-create, or a hand-rolled duplicate), the wiring facets the
// public auth-methods endpoint consumes, and the session arm's Source fix
// (T-157 leftover 3 — the cookie arm hardcoded local).

package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestNewUserCreatorWiresAutoCreate: the cmd-shaped composition —
// WithOIDC(provider, auth.NewUserCreator(store)) — auto-creates the user row
// through the REAL store adapter on first authentication, and the row is
// then resolvable through the exported NewOIDCResolver.
func TestNewUserCreatorWiresAutoCreate(t *testing.T) {
	f := newFixture(t, true)
	prov := newAuthenticatorMockOIDCProvider()
	prov.addValidToken("first-login-token", &auth.Claims{
		Name:       "sso-user",
		Groups:     []string{"developers"},
		Admin:      true,
		ProviderID: "sub-new-1",
	})
	// No resolved user: the arm must fall through to auto-create.

	svc := f.svc.WithOIDC(prov, auth.NewUserCreator(f.st.Users()))
	p, err := svc.Authenticate(f.ctx, req("Bearer first-login-token", ""))
	if err != nil {
		t.Fatalf("Authenticate(first login) = %v", err)
	}
	if p == nil || p.Name != "sso-user" || p.Source != auth.ProviderOIDC || !p.Admin {
		t.Fatalf("principal = %+v, want sso-user/oidc/admin", p)
	}

	// The row landed with the ADR-0020 decision-4 shape: provider=oidc,
	// provider_id=sub, empty password hash, enabled.
	u, err := f.st.Users().Get(f.ctx, "sso-user")
	if err != nil {
		t.Fatalf("Get auto-created user: %v", err)
	}
	if u.Provider != string(auth.ProviderOIDC) || u.ProviderID != "sub-new-1" {
		t.Errorf("row provider = %q/%q, want oidc/sub-new-1", u.Provider, u.ProviderID)
	}
	if u.PasswordHash != "" {
		t.Errorf("row password hash = %q, want empty for an OIDC user", u.PasswordHash)
	}
	if !u.Enabled || !u.IsAdmin {
		t.Errorf("row enabled/admin = %v/%v, want true/true", u.Enabled, u.IsAdmin)
	}

	// The exported resolver finds the row back by (provider, provider_id).
	pu, err := auth.NewOIDCResolver(f.st.Users()).GetByProvider(
		f.ctx, auth.ProviderOIDC, "sub-new-1")
	if err != nil {
		t.Fatalf("GetByProvider: %v", err)
	}
	if pu.Username != "sso-user" || !pu.Enabled {
		t.Errorf("resolved user = %+v, want sso-user enabled", pu)
	}
}

// TestNewUserCreatorWithNewFromStoreOrdering pins the exact foot-gun the
// exported constructor closes: WithOIDC replaces the creator NewFromStore
// wired, so cmd MUST pass the same store-backed creator — passing nil after
// the fix would still disable auto-create (documented WithOIDC semantics,
// not changed here).
func TestNewUserCreatorWithNewFromStoreOrdering(t *testing.T) {
	f := newFixture(t, true)
	prov := newAuthenticatorMockOIDCProvider()
	prov.addValidToken("t", &auth.Claims{Name: "u1", ProviderID: "s1"})

	if _, err := f.svc.WithOIDC(prov, nil).Authenticate(f.ctx, req("Bearer t", "")); err == nil {
		t.Fatal("WithOIDC(prov, nil) authenticated an unknown user, want auto-create disabled rejection")
	}
	if _, err := f.st.Users().Get(f.ctx, "u1"); err == nil {
		t.Fatal("no row may be created when the creator is nil")
	}
}

// TestProviderWiringFacets: the capability facets behind GET
// /api/v1/auth/methods (consumed through a consumer-side type assertion).
func TestProviderWiringFacets(t *testing.T) {
	f := newFixture(t, true)
	oidcProv := newAuthenticatorMockOIDCProvider()

	if f.svc.OIDCWired() || f.svc.LDAPWired() {
		t.Fatal("bare NewFromStore service reports a wired provider, want both inert")
	}
	withOIDC := f.svc.WithOIDC(oidcProv, nil)
	if !withOIDC.OIDCWired() || withOIDC.LDAPWired() {
		t.Fatal("WithOIDC must arm only the OIDC facet")
	}
	withBoth := withOIDC.WithLDAP(stubLDAPIdentityProvider{})
	if !withBoth.OIDCWired() || !withBoth.LDAPWired() {
		t.Fatal("WithOIDC+WithLDAP must arm both facets")
	}
	// The clones are copies: the original service stays inert.
	if f.svc.OIDCWired() || f.svc.LDAPWired() {
		t.Fatal("With* mutated the receiver's facets, want copy semantics")
	}
}

// stubLDAPIdentityProvider satisfies IdentityProvider for wiring-level
// assertions (its Authenticate is never called — LDAP has no Bearer arm).
type stubLDAPIdentityProvider struct{}

func (stubLDAPIdentityProvider) ProviderName() auth.Provider { return auth.ProviderLDAP }
func (stubLDAPIdentityProvider) Authenticate(context.Context, string) (*auth.Claims, error) {
	return nil, auth.ErrInvalidCredentials
}
func (stubLDAPIdentityProvider) Resolve(context.Context, auth.Provider, string) (*auth.ProviderUser, error) {
	return nil, auth.ErrProviderUserNotFound
}

// TestSessionSourceReflectsOwnerProvider (T-157 leftover 3): the session
// arm's Principal reports the OWNING user row's provider — local, oidc or
// ldap — instead of the hardcoded local.
func TestSessionSourceReflectsOwnerProvider(t *testing.T) {
	for _, tc := range []struct {
		provider auth.Provider
	}{
		{auth.ProviderLocal},
		{auth.ProviderOIDC},
		{auth.ProviderLDAP},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			f := newFixture(t, true)
			name := string(tc.provider) + "-session-user"
			now := metadata.Now()
			if err := f.st.Users().Create(f.ctx, &metadata.User{
				Username:   name,
				IsAdmin:    false,
				Enabled:    true,
				CreatedAt:  now,
				UpdatedAt:  now,
				Provider:   string(tc.provider),
				ProviderID: "pid-" + name,
			}); err != nil {
				t.Fatalf("seed user: %v", err)
			}
			sess, err := f.svc.IssueSession(f.ctx, name, time.Hour)
			if err != nil {
				t.Fatalf("IssueSession: %v", err)
			}
			p, err := f.svc.Authenticate(f.ctx, sessionReq(sess.ID))
			if err != nil {
				t.Fatalf("Authenticate(session) = %v", err)
			}
			if p == nil || p.Name != name {
				t.Fatalf("principal = %+v, want %s", p, name)
			}
			if !p.ViaSession {
				t.Fatal("ViaSession = false, want the cookie arm")
			}
			if p.Source != tc.provider {
				t.Fatalf("Source = %q, want the owner row's provider %q", p.Source, tc.provider)
			}
		})
	}
}
