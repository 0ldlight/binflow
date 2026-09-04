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
	// groupSync backs the IdP group membership sync into user_groups
	// (T-185 / T-174 D4; see idp_sync.go). nil = sync inert.
	groupSync groupSyncSource
	// roleWriter backs the per-authentication role refresh for
	// provider-owned rows (T-212 widening T-185 / T-174 D4; ADR-0020: the
	// provider is the authority on every authentication, not just the
	// first). nil = inert.
	roleWriter roleWriter
	// userDelete backs the user-delete use case (M9, ADR-0030 E4; see
	// user_delete.go): the admin census and the same-transaction cascade.
	// nil = DeleteUser fails closed (bare New services, unit fakes).
	userDelete userDeleteSource
	// hashGate bounds concurrent argon2 derivations across this service's
	// password paths (T-192 / T-172 D-1; see hashgate.go). Never nil after
	// New; WithHashConcurrency replaces it with a differently sized copy.
	hashGate *hashGate
	// hashVerify is the derivation seam the gate wraps. The default runs
	// the real argon2id comparison; internal tests swap it to observe the
	// gate without timing games. It runs ONLY while a slot is held.
	hashVerify func(ctx context.Context, password, encoded string) bool
	// stepUpGrants is the in-process single-use mint-grant ledger (M7,
	// ADR-0027 decision 4; see stepup.go). Initialized by New and shared by
	// every With* clone of this service — one process, one ledger.
	stepUpGrants *stepUpLedger
	// hot is the live auth-configuration source (M11 T-305, ADR-0035
	// decision 3): when set, the OIDC/LDAP arms resolve their provider PER
	// REQUEST through it (the ConfigManager's atomic snapshot) instead of
	// the construction-time fields, and the section policy facets gate
	// first-login auto-create. nil keeps the static ADR-0020 wiring
	// byte-for-byte.
	hot ConfigHotSource
}

// ConfigHotSource is the ConfigManager facet the arms consume per
// request (defined here at the consumer, per project convention). The
// current-provider getters answer nil for an absent/disabled section — the
// arm's inert posture, identical to the unwired pre-M6 service — so a
// config PUT that flips enabled=false deactivates the arm on the NEXT
// request without any restart.
type ConfigHotSource interface {
	// CurrentOIDC returns the live OIDC provider, or nil when inactive.
	CurrentOIDC() *OIDCProvider
	// CurrentLDAP returns the live LDAP provider, or nil when inactive.
	CurrentLDAP() IdentityProvider
	// OIDCAutoCreate/LDAPAutoCreate gate first-login auto-create on the
	// section flags (default true — the M6 wired posture).
	OIDCAutoCreate() bool
	LDAPAutoCreate() bool
}

// WithAuthConfig arms both external arms for hot configuration (M11 T-305):
// the providers come from the manager's snapshot per request and the
// section's auto-create flags are honored. The static oidcProvider/
// ldapProvider fields are bypassed (not mutated), so unwiring is a matter
// of not calling this. userCreator is untouched — the auto-create SEAM
// stays whatever NewFromStore/WithOIDC wired.
func (s *Service) WithAuthConfig(hot ConfigHotSource) *Service {
	clone := *s
	clone.hot = hot
	return &clone
}

// currentOIDC resolves the OIDC arm's provider for THIS request: the hot
// snapshot when the manager is wired, the static field otherwise.
func (s *Service) currentOIDC() IdentityProvider {
	if s.hot != nil {
		if p := s.hot.CurrentOIDC(); p != nil {
			return p
		}
		return nil
	}
	return s.oidcProvider
}

// currentLDAP resolves the LDAP arm's provider for THIS request.
func (s *Service) currentLDAP() IdentityProvider {
	if s.hot != nil {
		return s.hot.CurrentLDAP()
	}
	return s.ldapProvider
}

// oidcAutoCreate is the live auto_create_users verdict (default true —
// the M6 wired posture an unconfigured boot keeps byte-for-byte).
func (s *Service) oidcAutoCreate() bool {
	return s.hot == nil || s.hot.OIDCAutoCreate()
}

// ldapAutoCreate is the live autoCreateUser verdict (§1.1 #6, default
// true).
func (s *Service) ldapAutoCreate() bool {
	return s.hot == nil || s.hot.LDAPAutoCreate()
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
// authenticate via the Basic arm). Role (M7, ADR-0026 / migration 011) is
// the closed-set system role; IsAdmin stays its admin mirror.
type user struct {
	Username     string
	PasswordHash string
	IsAdmin      bool
	Enabled      bool
	Provider     Provider
	ProviderID   string
	Role         Role
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
	DeployScope string
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
// M4 and non-user rows are ignored by the authorizer. CanManage (M7,
// ADR-0026) is the repo-scoped admin bit — see ActionManage. CanAnnotate
// (M16 T-444, ADR-0044 K68) is the property-write bit — see ActionAnnotate;
// migration 023 backfilled it onto every can_write row so the write→
// deploy-cache/annotate split is privilege-preserving for existing grants.
type PermissionRow struct {
	ID            int64
	TargetName    string
	Principal     string
	PrincipalType string
	CanRead       bool
	CanWrite      bool
	CanDelete     bool
	CanManage     bool
	CanAnnotate   bool
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
// (ADR-0020 decision 4: provider, provider_id, empty password_hash). Role
// (M7, ADR-0026) is the closed-set role the claims resolved; empty derives
// from IsAdmin so pre-RBAC creators keep working.
type NewUserParams struct {
	Username     string
	PasswordHash string
	IsAdmin      bool
	Enabled      bool
	Provider     Provider
	ProviderID   string
	Role         Role
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
		stepUpGrants:  newStepUpLedger(),
	}
	s.verifier = &TokenVerifier{tokens: tokens, users: users}
	s.hashGate = newHashGate(defaultHashConcurrency())
	s.hashVerify = func(_ context.Context, password, encoded string) bool {
		return VerifyPassword(password, encoded)
	}
	return s
}

// WithHashConcurrency returns a copy of svc whose argon2 gate admits
// limit concurrent derivations (T-192 / T-172 D-1). The limit is the
// operator knob for the memory/CPU ceiling of password verification —
// each in-flight derivation costs ~64 MiB (parameters of record,
// ADR-0009), so limit x 64 MiB is the worst-case transient heap. It
// panics for limit < 1: a non-positive bound is an assembly bug, not an
// operator value (the config plane validates before reaching here).
// Derivations already running on the old gate drain on their own; the
// copy's gate starts fresh.
func (s *Service) WithHashConcurrency(limit int) *Service {
	if limit < 1 {
		panic(fmt.Sprintf("auth: hash concurrency limit must be >= 1, got %d", limit))
	}
	clone := *s
	clone.hashGate = newHashGate(limit)
	return &clone
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
		// bearer token as a rejected credential (not anonymous). The
		// provider resolves per request (T-305: a hot config PUT that
		// enables/disables the section flips this arm on the next
		// request).
		if prov := s.currentOIDC(); prov != nil {
			return s.authenticateOIDC(ctx, prov, tok)
		}
		return nil, verr
	}
	// Bare-token arm (M11/T-294, cargo's sparse HTTP registries): a
	// SCHEME-LESS Authorization value — `Authorization: <token>` — is
	// the official wire form cargo sends for registry tokens (the
	// crates.io compatibility shape; probed live, cargo 1.98). Every
	// space-scheme spelling was consumed above, so a value with no plain
	// space can only be this form; a bare value that fails verification
	// is a REJECTED credential, never a downgrade to anonymous (the
	// presented-but-rejected posture, unchanged). A no-space value that
	// carries OTHER whitespace (a tab where a scheme separator should
	// be — `Bearer\t<tok>`, `foo\tbar`) is neither a scheme form nor a
	// legal token and is refused as malformed here: T-294 review M1 —
	// letting it fall through to anonymous drifted the posture basicAuth
	// upheld before the bare arm existed.
	if tok, malformed := bareTokenHeader(r); tok != "" {
		return s.verifier.Verify(ctx, tok)
	} else if malformed {
		return nil, invalidf("auth: malformed Authorization header")
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
// them — an empty hash is never a valid argon2id PHC string, so the gated
// verify returns false, and the fallback to API token is also rejected
// because OIDC/LDAP users should not have API tokens presented through
// the Basic header. The user must authenticate through their provider's
// arm (OIDC Bearer, or web session after OIDC/LDAP login).
//
// T-192 (T-172 D-1): the argon2id comparison runs inside the service's
// concurrency gate — a storm of Basic credentials can no longer start an
// unbounded number of 64 MiB derivations, and a client that disconnects
// while queued abandons its slot instead of computing for a dead socket.
// The gate error is returned verbatim: it is a transport-level
// abandonment, not a credential verdict (and not a login failure for the
// audit plane).
func (s *Service) authenticateBasic(ctx context.Context, username, password string) (*Principal, error) {
	u, err := s.users.Get(ctx, username)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, fmt.Errorf("auth: looking up user: %w", err)
	}
	if err == nil {
		// Cheap rejections first — a disabled account or a provider-owned
		// row never reaches the argon2 gate.
		switch {
		case !u.Enabled:
			return nil, invalidf("auth: user %q is disabled", u.Username)
		case u.PasswordHash == "":
			// OIDC/LDAP user with no local password — cannot use Basic arm.
			// The empty hash is structurally not a valid argon2id PHC
			// string, so the gated verify would return false anyway, but
			// the explicit check produces a clearer rejection message.
			return nil, invalidf("auth: user %q has no local password (use OIDC or web session)", u.Username)
		}
		ok, verr := s.VerifyPassword(ctx, password, u.PasswordHash)
		if verr != nil {
			return nil, verr
		}
		if ok {
			return newPrincipal(u.Username, u.Role, ProviderLocal), nil
		}
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
//     (the database is authoritative for the role) with the claims'
//     groups.
//  3. If not found and userCreator is wired, a new user row is auto-created
//     (ADR-0020 decision 4): username=claims.Name, password_hash empty,
//     role=claims role (admin/readonly_admin/user, ADR-0026), provider=
//     ProviderOIDC, provider_id=claims.ProviderID, enabled=true. The
//     Principal is then built from the claims.
//  4. If not found and userCreator is nil, the token is rejected.
//
// T-305: prov is the request-scoped provider (the hot snapshot's current
// frame — a login flow finishing across a config swap keeps ITS frame for
// the rest of the request; the next request walks the new one).
func (s *Service) authenticateOIDC(ctx context.Context, prov IdentityProvider, token string) (*Principal, error) {
	claims, err := prov.Authenticate(ctx, token)
	if err != nil {
		// The provider wraps its own errors; ensure they satisfy
		// ErrInvalidCredentials for the HTTP layer — and classify for the
		// audit plane (T-187): a rejected token is bad_credentials, a
		// provider-side failure (JWKS fetch, TLS) is provider_error /
		// tls_handshake.
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			return nil, newFailure(ProviderOIDC, ReasonBadCredentials, err)
		case errors.Is(err, ErrTLSHandshake):
			return nil, newFailure(ProviderOIDC, ReasonTLSHandshake, err)
		}
		return nil, newFailure(ProviderOIDC, ProviderFailureReason(err),
			fmt.Errorf("auth: oidc token validation: %w", err))
	}
	if claims == nil {
		return nil, newFailure(ProviderOIDC, ReasonProviderError,
			errors.New("auth: oidc provider returned nil claims"))
	}
	if claims.ProviderID == "" {
		return nil, newFailure(ProviderOIDC, ReasonBadCredentials,
			errors.New("auth: oidc claims missing provider_id (sub)"))
	}
	if claims.Name == "" {
		return nil, newFailure(ProviderOIDC, ReasonBadCredentials,
			errors.New("auth: oidc claims missing username"))
	}

	// Try to resolve an existing user.
	pu, err := prov.Resolve(ctx, ProviderOIDC, claims.ProviderID)
	if err == nil {
		if !pu.Enabled {
			return nil, newFailure(ProviderOIDC, ReasonUserDisabled,
				fmt.Errorf("auth: oidc user %q is disabled", pu.Username))
		}
		// T-185 (T-174 D4), widened to roles by T-212 (ADR-0026 decision 6):
		// the provider is authoritative on EVERY authentication — refresh a
		// drifted role instead of freezing it at first login (admin_group >
		// readonly_group > user) — and mirror the claims' groups into
		// user_groups so the session arm (and every other DB-backed fill)
		// sees them.
		role := s.refreshProviderRole(ctx, claims, pu.Username, pu.Role)
		s.syncProviderGroups(ctx, claims, pu.Username)
		p := newPrincipal(pu.Username, role, ProviderOIDC)
		p.Groups = claims.Groups
		return p, nil
	}
	if !errors.Is(err, ErrProviderUserNotFound) {
		return nil, fmt.Errorf("auth: oidc user resolve: %w", err)
	}

	// User not found — auto-create if the creator is wired (and the live
	// section allows it, T-305: auto_create_users defaults true).
	if s.userCreator == nil || !s.oidcAutoCreate() {
		return nil, newFailure(ProviderOIDC, ReasonUserNotFound,
			fmt.Errorf("auth: oidc user %q not found (auto-create disabled)", claims.Name))
	}
	role := claimsRole(claims)
	if err := s.userCreator.Create(ctx, NewUserParams{
		Username:     claims.Name,
		PasswordHash: "", // no local password for OIDC users
		IsAdmin:      role == RoleAdmin,
		Enabled:      true,
		Provider:     ProviderOIDC,
		ProviderID:   claims.ProviderID,
		Role:         role,
	}); err != nil {
		return nil, fmt.Errorf("auth: auto-creating oidc user %q: %w", claims.Name, err)
	}
	s.syncProviderGroups(ctx, claims, claims.Name)

	p := newPrincipal(claims.Name, role, ProviderOIDC)
	p.Groups = claims.Groups
	return p, nil
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
//
// T-305: resolveProv is the request-scoped provider frame (Resolve rides
// it); bindFn is the same object narrowed to the Bind method the flow
// starts from. They arrive as one identity from the same snapshot.
func (s *Service) authenticateLDAP(ctx context.Context, resolveProv IdentityProvider, bindFn interface {
	Bind(ctx context.Context, username, password string) (*Claims, error)
}, username, password string) (*Principal, error) {
	claims, err := bindFn.Bind(ctx, username, password)
	if err != nil {
		// LDAPProvider.Bind returns classified Failures; a raw error (test
		// fakes, future providers) is classified from its sentinel chain,
		// and everything else keeps the uniform rejection (T-187 / T-174
		// D8: the audit plane sees method+reason, the caller still a 401).
		var f *Failure
		switch {
		case errors.As(err, &f):
			return nil, err
		case errors.Is(err, ErrTLSHandshake):
			return nil, newFailure(ProviderLDAP, ReasonTLSHandshake, err)
		case errors.Is(err, ErrProviderUnreachable):
			return nil, newFailure(ProviderLDAP, ReasonProviderError, err)
		case errors.Is(err, ErrInvalidCredentials):
			return nil, err
		}
		return nil, invalidf("auth: ldap bind: %v", err)
	}
	if claims == nil {
		return nil, newFailure(ProviderLDAP, ReasonProviderError,
			errors.New("auth: ldap bind returned nil claims"))
	}
	if claims.ProviderID == "" {
		return nil, newFailure(ProviderLDAP, ReasonProviderError,
			errors.New("auth: ldap claims missing provider_id"))
	}
	if claims.Name == "" {
		return nil, newFailure(ProviderLDAP, ReasonProviderError,
			errors.New("auth: ldap claims missing username"))
	}

	// resolveProv is guaranteed non-nil by the caller (AuthenticateCredentials
	// only calls this when the current LDAP provider exists). It satisfies
	// IdentityProvider which has a Resolve method.
	pu, err := resolveProv.Resolve(ctx, ProviderLDAP, claims.ProviderID)
	if err == nil {
		if !pu.Enabled {
			return nil, newFailure(ProviderLDAP, ReasonUserDisabled,
				fmt.Errorf("auth: ldap user %q is disabled", pu.Username))
		}
		// T-185 (T-174 D4), widened to roles by T-212: the same refresh the
		// OIDC arm applies — the directory is authoritative for the role on
		// every login (admin_group > readonly_group > user), and the
		// searched group set lands in user_groups so the session arm keeps
		// it after the login request is gone.
		role := s.refreshProviderRole(ctx, claims, pu.Username, pu.Role)
		s.syncProviderGroups(ctx, claims, pu.Username)
		p := newPrincipal(pu.Username, role, ProviderLDAP)
		p.Groups = claims.Groups
		return p, nil
	}
	if !errors.Is(err, ErrProviderUserNotFound) {
		return nil, fmt.Errorf("auth: ldap user resolve: %w", err)
	}

	// User not found locally: auto-create if the creator is wired (and the
	// live section allows it, T-305: autoCreateUser defaults true).
	if s.userCreator != nil && s.ldapAutoCreate() {
		role := claimsRole(claims)
		if err := s.userCreator.Create(ctx, NewUserParams{
			Username:     claims.Name,
			PasswordHash: "", // no local password for LDAP users
			IsAdmin:      role == RoleAdmin,
			Enabled:      true,
			Provider:     ProviderLDAP,
			ProviderID:   claims.ProviderID,
			Role:         role,
		}); err != nil {
			return nil, fmt.Errorf("auth: auto-creating ldap user %q: %w", claims.Name, err)
		}
		s.syncProviderGroups(ctx, claims, claims.Name)
		p := newPrincipal(claims.Name, role, ProviderLDAP)
		p.Groups = claims.Groups
		return p, nil
	}
	return nil, newFailure(ProviderLDAP, ReasonUserNotFound,
		fmt.Errorf("auth: ldap user %q not found (auto-create disabled)", claims.Name))
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
		// No scheme at all: NOT basic's to reject (T-294) — the
		// bare-token arm downstream owns the scheme-less form; a value
		// that fails verification is rejected THERE, keeping the
		// presented-but-rejected posture for garbage spellings.
		return "", "", false, nil
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

// bareTokenHeader reads the scheme-less credential (the cargo arm above).
// Three answers: a legal bare token (tok, false); "" when no Authorization
// value exists or the value carries a plain space (a scheme-shaped
// spelling the earlier arms already judged — `Digest xyz` keeps its
// historical anonymous fall-through); and ("" , true) for a no-space value
// carrying OTHER whitespace (tab, CR, …) — the malformed family the caller
// refuses.
func bareTokenHeader(r *http.Request) (tok string, malformed bool) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if h == "" || strings.Contains(h, " ") {
		return "", false
	}
	if strings.ContainsFunc(h, isHeaderWhitespace) {
		return "", true
	}
	return h, false
}

// isHeaderWhitespace reports the whitespace a header value may smuggle
// that is not the plain space the scheme grammar separates on.
func isHeaderWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// invalidf builds a credential-rejection error. The message must never
// embed the presented secret (NFR-S3); callers pass at most the username.
func invalidf(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), ErrInvalidCredentials)
}
