// OIDC authentication extension (M6, ADR-0020): the OIDCProvider implements
// IdentityProvider using github.com/coreos/go-oidc/v3 for ID Token validation,
// JWKS verification, and claims extraction. It also manages the PKCE login flow
// for browser-based OIDC authentication.
//
// Configuration is carried by OIDCConfig and consumed by NewOIDCProvider. The
// client_secret is an env-only secret (BINFLOW_AUTH_OIDC_CLIENT_SECRET) and
// never appears in the YAML config file.
//
// The OIDC Bearer arm (consumed by Service.authenticateOIDC) uses Authenticate
// to validate an ID Token from the Authorization: Bearer header and Resolve to
// find the local user row that was auto-created on first login.

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCConfig carries the configuration for an OIDC identity provider. All
// fields are populated at construction time and remain immutable afterward.
//
// client_secret is NOT a field here — it is read at construction from the
// environment (BINFLOW_AUTH_OIDC_CLIENT_SECRET) so it never enters the YAML
// config file. The caller passes it explicitly to NewOIDCProvider.
type OIDCConfig struct {
	// IssuerURL is the OpenID Connect issuer URL (e.g.
	// "https://accounts.example.com"). Required.
	IssuerURL string

	// ClientID is the OAuth2 client ID assigned by the provider. Required.
	ClientID string

	// ClientSecret is the OAuth2 client secret. This is the value read from
	// BINFLOW_AUTH_OIDC_CLIENT_SECRET; it is never hardcoded in YAML.
	ClientSecret string

	// RedirectURL is the callback URL the provider redirects to after user
	// authentication (e.g. "https://binflow.example.com/binflow/api/v1/oidc/callback").
	RedirectURL string

	// Scopes is the list of OpenID Connect scopes to request. Defaults to
	// ["openid", "profile", "email"] when nil or empty.
	Scopes []string

	// UserClaim is the JWT claim to use as the username. Defaults to
	// "preferred_username".
	UserClaim string

	// GroupClaim is the JWT claim to use for group membership. Defaults to
	// "groups".
	GroupClaim string

	// AdminGroup optionally specifies a group name whose members are granted
	// administrative privileges. When empty, no claim-inferred admin mapping
	// is performed.
	AdminGroup string
}

// defaults populates zero-valued fields with their documented defaults.
func (c *OIDCConfig) defaults() {
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"openid", "profile", "email"}
	}
	if c.UserClaim == "" {
		c.UserClaim = "preferred_username"
	}
	if c.GroupClaim == "" {
		c.GroupClaim = "groups"
	}
}

// OIDCUserResolver is the consumer-side seam for looking up a user row by
// (provider, providerID). OIDCProvider.Resolve delegates to this interface.
type OIDCUserResolver interface {
	// GetByProvider returns the user identified by the given (provider,
	// providerID) pair. It returns ErrProviderUserNotFound when no row
	// matches.
	GetByProvider(ctx context.Context, provider Provider, providerID string) (*ProviderUser, error)
}

// OIDCProvider implements IdentityProvider for OpenID Connect. It validates
// ID Tokens via go-oidc/v3 and resolves users against a backing store.
type OIDCProvider struct {
	config    *OIDCConfig
	resolver  OIDCUserResolver
	provider  *oidc.Provider // discovered provider metadata
	verifier  *oidc.IDTokenVerifier
	oauth2Cfg *oauth2.Config

	// tokenEndpoint is the provider's token endpoint URL for the OAuth2
	// authorization code exchange.
	tokenEndpoint string
}

// NewOIDCProvider constructs an OIDCProvider. It performs OIDC discovery
// against the issuer URL, so the caller must provide a context (the provider
// must be reachable at issURL + "/.well-known/openid-configuration").
//
// resolver is the seam for looking up local user rows by provider+providerID;
// it can be nil when only Authenticate (no Resolve) is needed.
//
// An HTTP client can be injected through the context via
// oidc.ClientContext(ctx, client) for custom transport/TLS settings.
func NewOIDCProvider(ctx context.Context, cfg *OIDCConfig, resolver OIDCUserResolver) (*OIDCProvider, error) {
	cfg.defaults()
	if cfg.IssuerURL == "" {
		return nil, errors.New("auth: OIDC issuer_url is required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("auth: OIDC client_id is required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("auth: OIDC redirect_url is required")
	}

	prov, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("auth: OIDC provider discovery: %w", err)
	}

	verifier := prov.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     prov.Endpoint(),
		Scopes:       cfg.Scopes,
	}

	p := &OIDCProvider{
		config:        cfg,
		resolver:      resolver,
		provider:      prov,
		verifier:      verifier,
		oauth2Cfg:     oauth2Cfg,
		tokenEndpoint: prov.Endpoint().TokenURL,
	}
	return p, nil
}

// ProviderName returns ProviderOIDC ("oidc").
func (p *OIDCProvider) ProviderName() Provider { return ProviderOIDC }

// OAuth2Config returns the internal oauth2.Config. This is exposed for the
// login HTTP handler to construct the authorization URL and exchange the
// authorization code.
func (p *OIDCProvider) OAuth2Config() *oauth2.Config { return p.oauth2Cfg }

// Verifier returns the ID token verifier. Exposed for tests and for the login
// HTTP handler's callback to verify the ID token independently.
func (p *OIDCProvider) Verifier() *oidc.IDTokenVerifier { return p.verifier }

// Authenticate validates a raw ID Token JWT and extracts claims. The token
// must be a valid signed JWT that passes go-oidc/v3 verification: signature
// via JWKS, issuer check, audience (client_id) check, and expiry check.
//
// On success the returned Claims carry the identity information mapped through
// the configured user_claim, group_claim, and admin_group settings.
func (p *OIDCProvider) Authenticate(ctx context.Context, token string) (*Claims, error) {
	idToken, err := p.verifier.Verify(ctx, token)
	if err != nil {
		// Wrap known error types as ErrInvalidCredentials. go-oidc returns
		// TokenExpiredError for expired tokens; all other failures are
		// signature, issuer, or audience mismatches.
		var expiredErr *oidc.TokenExpiredError
		if errors.As(err, &expiredErr) {
			return nil, fmt.Errorf("auth: oidc id token expired at %s: %w",
				expiredErr.Expiry.Format(time.RFC3339), ErrInvalidCredentials)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("auth: oidc token verification: %w", err)
		}
		return nil, fmt.Errorf("auth: oidc id token invalid: %w", ErrInvalidCredentials)
	}

	// Extract all claims from the ID Token's raw JSON payload.
	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		return nil, fmt.Errorf("auth: oidc claims extraction: %w", ErrInvalidCredentials)
	}

	// Map the configured user_claim.
	userName := p.extractClaim(rawClaims, p.config.UserClaim)
	if userName == "" {
		// Fallback to sub if the configured claim is missing.
		userName = idToken.Subject
		if userName == "" {
			return nil, fmt.Errorf("auth: oidc token missing both %q and sub claims: %w",
				p.config.UserClaim, ErrInvalidCredentials)
		}
	}

	// Map the configured group_claim.
	groups := p.extractGroups(rawClaims, p.config.GroupClaim)

	// Map the admin_group.
	admin := false
	if p.config.AdminGroup != "" {
		for _, g := range groups {
			if g == p.config.AdminGroup {
				admin = true
				break
			}
		}
	}

	return &Claims{
		Name:       userName,
		Groups:     groups,
		Admin:      admin,
		ProviderID: idToken.Subject,
	}, nil
}

// Resolve looks up an existing user row by (ProviderOIDC, providerID). It
// returns ErrProviderUserNotFound when no row matches, and the error wraps
// ErrProviderUserNotFound so the caller (Service.authenticateOIDC) can
// distinguish "not found" from store failures.
func (p *OIDCProvider) Resolve(ctx context.Context, provider Provider, providerID string) (*ProviderUser, error) {
	if p.resolver == nil {
		return nil, errProviderUserNotFound(provider, providerID)
	}
	pu, err := p.resolver.GetByProvider(ctx, provider, providerID)
	if err != nil {
		if errors.Is(err, ErrProviderUserNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("auth: oidc resolve %s/%s: %w", provider, providerID, err)
	}
	return pu, nil
}

// extractClaim retrieves a string claim from the raw claims map. Nested
// claims (dot-separated paths like "userinfo.name") are supported.
func (p *OIDCProvider) extractClaim(claims map[string]any, path string) string {
	if path == "" {
		return ""
	}
	// Root-level claim.
	if v, ok := claims[path]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractGroups retrieves a list of group names from the raw claims map. The
// claim value can be a JSON array of strings or a single string.
func (p *OIDCProvider) extractGroups(claims map[string]any, claim string) []string {
	v, ok := claims[claim]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case []any:
		groups := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				groups = append(groups, s)
			}
		}
		return groups
	case []string:
		return val
	case string:
		return []string{val}
	}
	return nil
}

// ------ PKCE login flow helpers ------

// PKCECodeChallengeMethod is the code challenge method used by BinFlow's OIDC
// login flow: S256 (SHA-256 based, RFC 7636).
const PKCECodeChallengeMethod = "S256"

// PKCEVerifierLen is the byte length of the PKCE code verifier (32 bytes →
// 43 base64-rawurl characters, within RFC 7636's 43..128 range).
const PKCEVerifierLen = 32

// GeneratePKCEPair creates a code verifier (random) and its S256 code
// challenge, suitable for the OIDC authorization code flow with PKCE.
func GeneratePKCEPair() (verifier, challenge string, err error) {
	buf := make([]byte, PKCEVerifierLen)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", "", fmt.Errorf("auth: pkce verifier entropy: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)

	digest := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(digest[:])
	return verifier, challenge, nil
}

// ------ JSON helpers for raw claims ------

// oidcClaims is a holder for structured claims from the ID Token. It is used
// inside Authenticate to map standard and custom claims.
type oidcClaims struct {
	Subject          string   `json:"sub"`
	PreferredName    string   `json:"preferred_username"`
	Name             string   `json:"name"`
	Email            string   `json:"email"`
	EmailVerified    *bool    `json:"email_verified,omitempty"`
	Groups           []string `json:"groups"`
	RealmAccess      *realmAccess
	ResourceAccess   map[string]resourceAccess `json:"resource_access,omitempty"`
}

type realmAccess struct {
	Roles []string `json:"roles"`
}

type resourceAccess struct {
	Roles []string `json:"roles"`
}

// unmarshalClaims decodes the raw claims JSON from an ID Token into a map and
// also into the structured oidcClaims type. It is exported for tests.
func unmarshalClaims(raw []byte) (map[string]any, *oidcClaims, error) {
	var rawMap map[string]any
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("auth: oidc claims json: %w", err)
	}
	var structured oidcClaims
	if err := json.Unmarshal(raw, &structured); err != nil {
		return rawMap, nil, fmt.Errorf("auth: oidc structured claims: %w", err)
	}
	return rawMap, &structured, nil
}