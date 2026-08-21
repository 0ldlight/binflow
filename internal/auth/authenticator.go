package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// headerAPIKey is the Artifactory-compatible token header; headerAPIKeyLO
// is its historical synonym (auth-model.md section 3.6, high confidence).
// The values are HTTP header names, not secrets.
const (
	headerAPIKey   = "X-JFrog-Art-Api" //nolint:gosec // header name, not a credential
	headerAPIKeyLO = "X-Api-Key"       //nolint:gosec // header name, not a credential
)

// Service implements Authenticator, Authorizer, TokenRegistry,
// PasswordChanger and SessionRegistry over the metadata sub-stores
// (architecture section 3.4). It is stateless apart from the injected stores
// and config flag, so one instance serves the whole process; all methods
// take ctx explicitly.
type Service struct {
	users         userSource
	tokens        tokenSource
	permissions   permissionSource
	anonymousRead bool
	verifier      *TokenVerifier
	// sessions backs the cookie arm (nil = arm inert; see WithSessions).
	sessions webSessionSource
	// groups backs the membership fill of Principal.Groups (nil = fill
	// inert; see WithGroups, T-97).
	groups groupSource
	// oidcProvider backs the OIDC Bearer arm (nil = arm inert; see
	// WithOIDC, ADR-0020). When non-nil, a Bearer token that is not a
	// known API token is tried as an OIDC ID Token through this provider.
	oidcProvider IdentityProvider
	// ldapProvider backs the LDAP login fallback (nil = arm inert; see
	// WithLDAP, ADR-0020). When non-nil, AuthenticateCredentials tries
	// LDAP bind after local password check fails. LDAP does NOT have a
	// Bearer arm (ADR-0020): LDAP authentication happens only at the
	// login endpoint.
	ldapProvider IdentityProvider
	// userCreator is the write seam for auto-creating users on first OIDC
	// authentication (ADR-0020 decision 4). nil = auto-create is disabled
	// (the arm still works for existing users).
	userCreator userCreator
}

// userSource is the consumer-side slice of metadata.UserStore the
// authenticator needs (interface defined at the consumer, per project
// convention).
type userSource interface {
	Get(ctx context.Context, username string) (user, error)
	UpdatePassword(ctx context.Context, username, passwordHash string) error
}

// user mirrors metadata.User without importing the whole row type; the
// adapter in deps.go converts. The Provider and ProviderID fields (M6,
// ADR-0020 / migration 008) identify the identity provider that owns this
// user: 'local' users have Provider=ProviderLocal and a non-empty
// PasswordHash; 'oidc' and 'ldap' users have Provider=ProviderOIDC or
// Provider=ProviderLDAP and an empty PasswordHash (they cannot
// authenticate via the Basic arm).
type user struct {
	Username     string
	PasswordHash string
	IsAdmin      bool
	Enabled      bool
	Provider     Provider
	ProviderID   string
}

// tokenSource is the consumer-side slice of metadata.TokenStore.
type tokenSource interface {
	Create(ctx context.Context, t token) (int64, error)
	GetBySHA256(ctx context.Context, sha256 string) (token, error)
	Touch(ctx context.Context, id int64, lastUsedAt string) error
	Delete(ctx context.Context, id int64) error
}

// token mirrors metadata.Token.
type token struct {
	ID          int64
	Username    string
	TokenSHA256 string
	ExpiresAt   string
	CreatedAt   string
	LastUsedAt  string
}

// Target is one named permission target (mirrors metadata.PermissionTarget):
// the repos it covers plus include/exclude pattern lists, all JSON array
// strings as stored.
type Target struct {
	Name     string
	Repos    string
	Includes string
	Excludes string
}

// PermissionRow is one (target, principal) grant row (mirrors
// metadata.PermissionPrincipal). PrincipalType is "user" in M1; groups are
// M4 and non-user rows are ignored by the authorizer.
type PermissionRow struct {
	ID            int64
	TargetName    string
	Principal     string
	PrincipalType string
	CanRead       bool
	CanWrite      bool
	CanDelete     bool
}

// permissionSource is the consumer-side slice of metadata.PermissionStore.
// The row types Target and PermissionRow are exported so callers building
// their own stores (tests, alternative deployments) can implement the
// interface; the metadata adapter in deps.go converts.
type permissionSource interface {
	ListTargets(ctx context.Context) ([]Target, error)
	PrincipalsFor(ctx context.Context, repoKey string) ([]PermissionRow, error)
}

// userCreator is the write seam for creating user rows. It is separate from
// userSource so that the auto-create path is an explicit opt-in (ADR-0020
// decision 4: auto-create users on first OIDC auth). The exported
// NewUserParams carries the Provider and ProviderID fields.
type userCreator interface {
	Create(ctx context.Context, params NewUserParams) error
}

// NewUserParams is the parameter struct for userCreator.Create. It carries
// the fields needed to create a user row on first OIDC/LDAP authentication
// (ADR-0020 decision 4: provider, provider_id, empty password_hash).
type NewUserParams struct {
	Username     string
	PasswordHash string
	IsAdmin      bool
	Enabled      bool
	Provider     Provider
	ProviderID   string
}

// New builds the auth Service. anonymousRead is config.Security.
// AnonymousAccess (ADR-0009): when true, content GET/HEAD may proceed
// anonymously (the Authorizer side of the flag; authentication itself never
// consults it).
func New(users userSource, tokens tokenSource, perms permissionSource, anonymousRead bool) *Service {
	s := &Service{
		users:         users,
		tokens:        tokens,
		permissions:   perms,
		anonymousRead: anonymousRead,
	}
	s.verifier = &TokenVerifier{tokens: tokens, users: users}
	return s
}

// WithOIDC returns a copy of svc whose OIDC Bearer arm is backed by prov
// (ADR-0020). Without this call the OIDC arm is inert: a Bearer token that
// is not a known API token falls through to the Cookie arm (preserving the
// pre-M6 behavior). The creator is the write seam for auto-creating users
// on first OIDC authentication; when nil, the arm validates tokens but
// only resolves existing users (no auto-create).
func (s *Service) WithOIDC(prov IdentityProvider, creator userCreator) *Service {
	clone := *s
	clone.oidcProvider = prov
	clone.userCreator = creator
	return &clone
}

// WithLDAP returns a copy of svc whose LDAP login fallback arm is backed
// by prov (ADR-0020). Without this call the LDAP arm is inert:
// AuthenticateCredentials only tries the local password (preserving pre-M6
// behavior). LDAP does NOT support auto-create at login per ADR-0020 — the
// user must exist locally with provider='ldap' and an empty password_hash.
func (s *Service) WithLDAP(prov IdentityProvider) *Service {
	clone := *s
	clone.ldapProvider = prov
	return &clone
}

var (
	_ Authenticator   = (*Service)(nil)
	_ Authorizer      = (*Service)(nil)
	_ TokenRegistry   = (*Service)(nil)
	_ PasswordChanger = (*Service)(nil)
	_ SessionRegistry = (*Service)(nil)
)

// Authenticate resolves the request credential (architecture section 3.4;
// auth-model.md section 3.6; the session arm is ADR-0014/T-91). See the
// Authenticator interface doc for the entry-point precedence. No credential
// at all yields (nil, nil). Whichever arm resolved the principal, the group
// membership fill runs once on the way out (T-97): every request re-reads
// the membership, which is what makes group changes effective immediately
// (NFR-S25 — no cache, no window).
func (s *Service) Authenticate(ctx context.Context, r *http.Request) (*Principal, error) {
	p, err := s.authenticate(ctx, r)
	if err != nil {
		return nil, err
	}
	s.fillGroups(ctx, p)
	return p, nil
}

// authenticate is the arm dispatch behind Authenticate.
func (s *Service) authenticate(ctx context.Context, r *http.Request) (*Principal, error) {
	user, pass, hasBasic, err := basicAuth(r)
	if err != nil {
		return nil, err
	}
	if hasBasic {
		return s.authenticateBasic(ctx, user, pass)
	}
	if tok := apiKeyHeader(r); tok != "" {
		return s.verifier.Verify(ctx, tok)
	}
	if tok := bearerHeader(r); tok != "" {
		// First arm: existing API token (the pre-M6 behavior).
		p, verr := s.verifier.Verify(ctx, tok)
		if verr == nil {
			return p, nil
		}
		// Second arm (M6, ADR-0020): OIDC ID Token. Only when the
		// provider is wired — an unwired service treats the unknown
		// bearer token as a rejected credential (not anonymous).
		if s.oidcProvider != nil {
			return s.authenticateOIDC(ctx, tok)
		}
		return nil, verr
	}
	// Third arm, last in precedence: the binflow_session cookie. Only when
	// the arm is wired — an unwired service treats the cookie as a non-
	// credential (anonymous), matching pre-M4 behavior. A wired arm treats
	// a presented-but-invalid cookie as a REJECTED credential (never a
	// silent downgrade to anonymous), so an expired or logged-out browser
	// session sees 401 and can re-authenticate.
	if s.sessions != nil {
		if id, ok := sessionCookie(r); ok {
			return s.authenticateSession(ctx, id)
		}
	}
	return nil, nil // anonymous
}

// authenticateBasic implements the Basic duality (auth-model.md section 3.6,
// high confidence): the password field is first tried as a real password;
// when that fails and the value verifies as an API token, the Basic username
// must equal the token subject case-insensitively ("Token principal
// mismatch" otherwise).
//
// M6 update (ADR-0020): users with an empty password_hash (OIDC/LDAP users)
// cannot authenticate via the Basic arm. The password check is skipped for
// them — an empty hash is never a valid argon2id PHC string, so
// VerifyPassword returns false, and the fallback to API token is also
// rejected because OIDC/LDAP users should not have API tokens presented
// through the Basic header. The user must authenticate through their
// provider's arm (OIDC Bearer, or web session after OIDC/LDAP login).
func (s *Service) authenticateBasic(ctx context.Context, username, password string) (*Principal, error) {
	u, err := s.users.Get(ctx, username)
	switch {
	case err == nil && u.Enabled && u.PasswordHash != "" && VerifyPassword(password, u.PasswordHash):
		return &Principal{Name: u.Username, Admin: u.IsAdmin, Source: ProviderLocal}, nil
	case err == nil && u.Enabled && u.PasswordHash == "":
		// OIDC/LDAP user with no local password — cannot use Basic arm.
		// The empty hash is structurally not a valid argon2id PHC string,
		// so VerifyPassword would return false anyway, but the explicit
		// check produces a clearer rejection message.
		return nil, invalidf("auth: user %q has no local password (use OIDC or web session)", u.Username)
	case err == nil && !u.Enabled:
		return nil, invalidf("auth: user %q is disabled", u.Username)
	case err != nil && !errors.Is(err, ErrUserNotFound):
		return nil, fmt.Errorf("auth: looking up user: %w", err)
	}

	// Password check failed (unknown user or wrong password): fall back to
	// treating the password field as an API token.
	p, terr := s.verifier.Verify(ctx, password)
	if terr != nil {
		// Unknown/revoked/expired token, unavailable owner — all plain
		// invalid credentials.
		return nil, invalidf("auth: basic credential rejected")
	}
	if !strings.EqualFold(username, p.Name) {
		return nil, invalidf("auth: token principal mismatch")
	}
	return p, nil
}

// authenticateOIDC implements the OIDC Bearer arm (M6, ADR-0020): the
// bearer token is treated as an OIDC ID Token. The flow:
//
//  1. oidcProvider.Authenticate validates the ID Token (JWKS signature,
//     issuer, audience, expiry) and extracts Claims.
//  2. oidcProvider.Resolve looks up an existing user row by (provider,
//     provider_id). If found, the Principal is built from the user row
//     (the database is authoritative for the admin flag) with the claims'
//     groups.
//  3. If not found and userCreator is wired, a new user row is auto-created
//     (ADR-0020 decision 4): username=claims.Name, password_hash empty,
//     is_admin=claims.Admin, provider=ProviderOIDC, provider_id=claims.ProviderID,
//     enabled=true. The Principal is then built from the claims.
//  4. If not found and userCreator is nil, the token is rejected.
func (s *Service) authenticateOIDC(ctx context.Context, token string) (*Principal, error) {
	claims, err := s.oidcProvider.Authenticate(ctx, token)
	if err != nil {
		// The provider wraps its own errors; ensure they satisfy
		// ErrInvalidCredentials for the HTTP layer.
		if errors.Is(err, ErrInvalidCredentials) {
			return nil, err
		}
		return nil, invalidf("auth: oidc token validation: %v", err)
	}
	if claims == nil {
		return nil, invalidf("auth: oidc provider returned nil claims")
	}
	if claims.ProviderID == "" {
		return nil, invalidf("auth: oidc claims missing provider_id (sub)")
	}
	if claims.Name == "" {
		return nil, invalidf("auth: oidc claims missing username")
	}

	// Try to resolve an existing user.
	pu, err := s.oidcProvider.Resolve(ctx, ProviderOIDC, claims.ProviderID)
	if err == nil {
		if !pu.Enabled {
			return nil, invalidf("auth: oidc user %q is disabled", pu.Username)
		}
		return &Principal{
			Name:   pu.Username,
			Admin:  pu.IsAdmin,
			Groups: claims.Groups,
			Source: ProviderOIDC,
		}, nil
	}
	if !errors.Is(err, ErrProviderUserNotFound) {
		return nil, fmt.Errorf("auth: oidc user resolve: %w", err)
	}

	// User not found — auto-create if the creator is wired.
	if s.userCreator == nil {
		return nil, invalidf("auth: oidc user %q not found (auto-create disabled)", claims.Name)
	}
	if err := s.userCreator.Create(ctx, NewUserParams{
		Username:     claims.Name,
		PasswordHash: "", // no local password for OIDC users
		IsAdmin:      claims.Admin,
		Enabled:      true,
		Provider:     ProviderOIDC,
		ProviderID:   claims.ProviderID,
	}); err != nil {
		return nil, fmt.Errorf("auth: auto-creating oidc user %q: %w", claims.Name, err)
	}

	return &Principal{
		Name:   claims.Name,
		Admin:  claims.Admin,
		Groups: claims.Groups,
		Source: ProviderOIDC,
	}, nil
}

// authenticateLDAP implements the LDAP login arm: bind against the LDAP
// directory, then resolve the user locally and return a Principal.
//
// This is only called from AuthenticateCredentials when local password
// check has already failed. LDAP does NOT have its own Bearer arm
// (ADR-0020), so this method is not part of authenticate() dispatch.
//
// The flow:
//  1. LDAPProvider.Bind validates username/password against the directory
//     and returns Claims (with Name, Groups, Admin, ProviderID).
//  2. Resolve the user by (ProviderLDAP, claims.ProviderID). If found, the
//     database is authoritative for is_admin, enabled.
//  3. If not found and userCreator is wired, auto-create the user row
//     (username=claims.Name, password_hash empty, provider='ldap',
//     provider_id=claims.ProviderID, enabled=true).
//  4. If not found and userCreator is nil, reject the credential.
func (s *Service) authenticateLDAP(ctx context.Context, bindFn interface {
	Bind(ctx context.Context, username, password string) (*Claims, error)
}, username, password string) (*Principal, error) {
	claims, err := bindFn.Bind(ctx, username, password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return nil, err
		}
		return nil, invalidf("auth: ldap bind: %v", err)
	}
	if claims == nil {
		return nil, invalidf("auth: ldap bind returned nil claims")
	}
	if claims.ProviderID == "" {
		return nil, invalidf("auth: ldap claims missing provider_id")
	}
	if claims.Name == "" {
		return nil, invalidf("auth: ldap claims missing username")
	}

	// s.ldapProvider is guaranteed non-nil by the caller (AuthenticateCredentials
	// only calls this when ldapProvider != nil). It satisfies IdentityProvider
	// which has a Resolve method.
	pu, err := s.ldapProvider.Resolve(ctx, ProviderLDAP, claims.ProviderID)
	if err == nil {
		if !pu.Enabled {
			return nil, invalidf("auth: ldap user %q is disabled", pu.Username)
		}
		return &Principal{
			Name:   pu.Username,
			Admin:  pu.IsAdmin,
			Groups: claims.Groups,
			Source: ProviderLDAP,
		}, nil
	}
	if !errors.Is(err, ErrProviderUserNotFound) {
		return nil, fmt.Errorf("auth: ldap user resolve: %w", err)
	}

	// User not found locally: auto-create if the creator is wired.
	if s.userCreator != nil {
		if err := s.userCreator.Create(ctx, NewUserParams{
			Username:     claims.Name,
			PasswordHash: "", // no local password for LDAP users
			IsAdmin:      claims.Admin,
			Enabled:      true,
			Provider:     ProviderLDAP,
			ProviderID:   claims.ProviderID,
		}); err != nil {
			return nil, fmt.Errorf("auth: auto-creating ldap user %q: %w", claims.Name, err)
		}
		return &Principal{
			Name:   claims.Name,
			Admin:  claims.Admin,
			Groups: claims.Groups,
			Source: ProviderLDAP,
		}, nil
	}
	return nil, invalidf("auth: ldap user %q not found (auto-create disabled)", claims.Name)
}

// basicAuth extracts the Basic credential. A present but undecodable Basic
// header is an invalid credential, not anonymous.
func basicAuth(r *http.Request) (username, password string, ok bool, err error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", "", false, nil
	}
	scheme, rest, found := strings.Cut(h, " ")
	if !found {
		return "", "", false, invalidf("auth: malformed Authorization header")
	}
	if !strings.EqualFold(scheme, "Basic") {
		return "", "", false, nil // other schemes handled elsewhere
	}
	raw, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(rest))
	if derr != nil {
		return "", "", false, invalidf("auth: malformed basic credential")
	}
	user, pass, found := strings.Cut(string(raw), ":")
	if !found {
		return "", "", false, invalidf("auth: malformed basic credential")
	}
	return user, pass, true, nil
}

// apiKeyHeader reads X-JFrog-Art-Api (synonym X-Api-Key).
func apiKeyHeader(r *http.Request) string {
	if v := r.Header.Get(headerAPIKey); v != "" {
		return v
	}
	return r.Header.Get(headerAPIKeyLO)
}

// bearerHeader reads the Bearer credential (docker token flow, M2; accepted
// already so token auth works for early clients).
func bearerHeader(r *http.Request) string {
	h := r.Header.Get("Authorization")
	scheme, rest, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(rest)
}

// invalidf builds a credential-rejection error. The message must never
// embed the presented secret (NFR-S3); callers pass at most the username.
func invalidf(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), ErrInvalidCredentials)
}
