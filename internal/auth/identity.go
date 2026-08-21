// Identity provider plane of the auth service (M6, ADR-0020): OIDC and LDAP
// authentication extensions. The IdentityProvider interface is a stable seam
// that the Authenticator's OIDC Bearer arm consumes; the concrete OIDC
// implementation (go-oidc/v3, T-154) and LDAP implementation (go-ldap/v3,
// T-157) are built against this interface.
//
// The Authenticator interface stays UNCHANGED (ADR-0020). The new OIDC
// Bearer arm is inserted in the Service.authenticate dispatch between the
// existing API token Bearer arm and the Cookie arm.

package auth

import (
	"context"
	"errors"
	"fmt"
)

// Provider is the identity provider that owns a user row (ADR-0020,
// migration 008). The provider name is persisted in the users.provider
// column and controls which authentication arms are valid for the user.
type Provider string

const (
	// ProviderLocal is the default provider for password/token users created
	// through the admin API or the initial admin seed. Local users have a
	// non-empty password_hash and authenticate via the Basic arm.
	ProviderLocal Provider = "local"

	// ProviderOIDC identifies users authenticated through an OIDC provider.
	// OIDC users have an empty password_hash and cannot authenticate via the
	// Basic arm; they authenticate via the OIDC Bearer arm (ID Token) or via
	// web session (after OIDC login flow).
	ProviderOIDC Provider = "oidc"

	// ProviderLDAP identifies users authenticated through an LDAP directory.
	// LDAP users have an empty password_hash and cannot authenticate via the
	// Basic arm; they authenticate via web session (after LDAP bind at the
	// login endpoint, T-157).
	ProviderLDAP Provider = "ldap"
)

// Claims carries the identity information extracted from a validated ID
// Token (OIDC) or LDAP bind result. It is the building block for a
// Principal: the Name becomes Principal.Name, the Groups become
// Principal.Groups, and Admin becomes Principal.Admin. The ProviderID is
// the stable identifier from the identity provider (the OIDC 'sub' claim
// or the LDAP DN).
type Claims struct {
	// Name is the human-readable username for this identity (derived from
	// the configured user_claim in the OIDC ID Token, or from the LDAP
	// user_id_attribute).
	Name string
	// Groups holds the group names this identity belongs to (derived from
	// the configured group_claim in the OIDC ID Token, or from LDAP group
	// search).
	Groups []string
	// Admin reports whether this identity should be granted administrative
	// privileges (mapped from the configured admin_group or admin_users
	// list in the provider configuration).
	Admin bool
	// ProviderID is the stable, unique identifier assigned by the identity
	// provider. For OIDC this is the 'sub' claim; for LDAP this is the
	// user's DN.
	ProviderID string
}

// ProviderUser is the user record returned by IdentityProvider.Resolve.
// It carries the fields the auth layer needs to build a Principal from a
// previously auto-created user row.
type ProviderUser struct {
	Username string
	IsAdmin  bool
	Enabled  bool
}

// IdentityProvider is the seam for external identity providers (OIDC and
// LDAP, ADR-0020). Each provider implementation validates its own token
// format and maps claims to the BinFlow identity model.
//
// The Authenticate method is called by the OIDC Bearer arm: it validates
// the ID Token signature (JWKS for OIDC), extracts claims, and returns a
// Claims struct. The arm then calls Resolve to find or create a local user
// row, and builds a Principal from the combined information.
//
// LDAP does NOT get its own Bearer arm (ADR-0020): LDAP authentication
// happens only at the login endpoint (POST /api/v1/session, T-157).
type IdentityProvider interface {
	// ProviderName returns the Provider constant for this provider
	// ("oidc" or "ldap").
	ProviderName() Provider

	// Authenticate validates a raw token from the Authorization: Bearer
	// header. For OIDC, the token is an ID Token (JWT) and validation
	// includes signature verification against the provider's JWKS, issuer
	// check, audience check, and expiry check. On success the extracted
	// claims are returned; on failure the error wraps
	// ErrInvalidCredentials.
	Authenticate(ctx context.Context, token string) (*Claims, error)

	// Resolve looks up an existing user row by provider and provider_id.
	// It returns ErrProviderUserNotFound when no user row exists for the
	// given (provider, providerID) pair. The returned ProviderUser is used
	// to refresh the Principal's admin flag from the authoritative source
	// (the database, not the claims).
	Resolve(ctx context.Context, provider Provider, providerID string) (*ProviderUser, error)
}

// ErrProviderUserNotFound is returned by IdentityProvider.Resolve when
// no user row exists for the given (provider, providerID) pair. The OIDC
// Bearer arm treats this as a trigger to auto-create a user row.
var ErrProviderUserNotFound = errors.New("auth: provider user not found")

// errProviderUserNotFound wraps a provider-specific error as
// ErrProviderUserNotFound for internal use.
func errProviderUserNotFound(provider Provider, providerID string) error {
	return fmt.Errorf("auth: %s user %q: %w", provider, providerID, ErrProviderUserNotFound)
}