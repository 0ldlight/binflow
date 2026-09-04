package auth

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Principal is the authenticated identity of one request (architecture
// section 3.4). A nil Principal means anonymous; callers must treat that as
// "no identity", never as "unknown user".
type Principal struct {
	Name    string
	Role    Role  // closed-set role (M7, ADR-0026); zero value derives from Admin, see EffectiveRole
	Admin   bool  // derived convenience mirror of Role == RoleAdmin — management-plane checks must use CanManage/CanManageRepo (architecture 3.4a)
	TokenID int64 // > 0 when the request authenticated via API token
	// Groups holds the group names the user belonged to at authentication
	// time (T-97 / SE-07, architecture 3.4: the user_groups JOIN filled on
	// every Authenticate call — per-request resolution is what makes
	// membership changes effective on the next request, NFR-S25). Nil or
	// empty for anonymous principals and for services built without a group
	// source; the Authorizer unions these names' grants with the user's own.
	Groups []string
	// ViaSession marks the third authentication arm (M4, ADR-0014): the
	// credential was the binflow_session cookie, not a header. Session and
	// header credentials are EQUIVALENT for every authorization decision —
	// the flag exists so the CSRF plane can police cookie-borne writes
	// (Origin check) without touching Basic/Token traffic. Sessions carry no
	// TokenID (there is no token row).
	ViaSession bool
	// SessionHash is the sha256 hex of the session id when (and only when)
	// ViaSession (M7, ADR-0027 decision 4): it identifies the session ROW —
	// the digest the store keys on, never the cookie's plaintext — so a
	// step-up mint grant can bind to {username, session_id}. Empty for
	// every header arm and for session principals built by hand.
	SessionHash string
	// Source is the identity provider that authenticated this request
	// (M6, ADR-0020). "local" for password/token users, "oidc" for OIDC
	// Bearer arm, "ldap" for LDAP web sessions. The Authorizer does not
	// inspect this field — it is informational for audit and debugging.
	Source Provider
	// DeployScope is non-nil only when the request authenticated through a
	// NARROW token (M12 T-349, FR-113.3): a token whose permission domain
	// was narrowed at mint time to one landing operation. The HTTP layer
	// enforces it before routing — every request that is not the scoped
	// operation answers 403 — while the identity (name/role/groups) stays
	// the owner's, so the regular authorization gates keep running on the
	// one admitted request too. Nil means an unrestricted credential; the
	// field is informational ONLY, never a grant.
	DeployScope *DeployScope
}

// DeployScope is the narrow permission domain one token carries (M12 T-349,
// FR-113.3): the single checksum-deploy landing a minted token may perform.
// Kind is the closed set's discriminator — "checksum-deploy" is the only
// kind today (the MPU finish task's 5-minute token, ADR-0039 residual 1).
// Repos lists every admissible FIRST path segment for the landing: the
// session's resolved target repository plus, when the client created through
// a virtual with a defaultDeploymentRepo, the virtual key it addressed (the
// client only knows its own spelling; routeVirtualWrite lands both on the
// same member). Path is the session's own repo path.
type DeployScope struct {
	Kind  string
	Repos []string
	Path  string
}

// DeployScopeChecksumDeploy is the one narrow-scope kind of this build: the
// token admits exactly one PUT with X-Checksum-Deploy at Repo/Path.
const DeployScopeChecksumDeploy = "checksum-deploy"

// Actions accepted by Authorizer.Can (architecture section 3.4 uses the
// short forms; metadata permission rows map read/write/delete onto them).
// ActionManage (M7, ADR-0026) is the repo-level admin action: it matches a
// target on its repos list only — includes/excludes never apply — and implies
// none of r/w/d. It never appears in the docker scope vocabulary (invariant 3,
// architecture 3.4a: pull/push/delete are the whole word set of /v2/token).
//
// ActionAnnotate (M16 T-444, ADR-0044 K68 / architecture section 25.6) is the
// property-write action: the M10 ?properties family's two mutating verbs
// (PUT/DELETE) gate on it — the read verb keeps the item-info `r` gate
// unchanged. It is a PATH-plane action like r/w/d (includes/excludes apply)
// and opens no content-byte face (uploads/landings stay `w`, the
// overwrite-check family stays `d` — "properties are metadata, not content").
// Carrying m implies no annotate (the no-privilege-chain invariant), and
// annotate never appears in the docker scope vocabulary either (ADR-0026
// decision 5's rule, K68 point 6: /v2 has no property plane).
const (
	ActionRead     = "r"
	ActionWrite    = "w"
	ActionDelete   = "d"
	ActionManage   = "m"
	ActionAnnotate = "a"
)

// Sentinel errors. Authenticate/Verify failures are distinguishable for
// logging and tests, but every credential failure satisfies
// errors.Is(err, ErrInvalidCredentials) so the HTTP layer can map the whole
// family to 401 without knowing each mode. Error messages never contain the
// presented secret (NFR-S3).
var (
	// ErrInvalidCredentials means a credential was presented and rejected
	// (wrong password, unknown/expired/revoked token, disabled user,
	// token principal mismatch, unsupported scheme, malformed header).
	// "No credential presented" is NOT an error: Authenticate answers
	// (nil, nil) and the caller decides between anonymous access and a 401
	// challenge based on the route (content GET/HEAD vs everything else,
	// ADR-0009).
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrEmptyPassword is returned by ChangePassword for a blank new
	// password (auth-model.md section 2.2: "New passwords cannot be empty").
	ErrEmptyPassword = errors.New("auth: new password is empty")
	// ErrSamePassword is returned by ChangePassword when the new password
	// equals the old one (auth-model.md section 2.2).
	ErrSamePassword = errors.New("auth: new password equals the old password")
)

// Authenticator resolves a request to a Principal (architecture section 3.4).
// Supported entry points (auth-model.md section 3.6, high confidence; the
// third arm is M4, ADR-0014 as amended by the T-108 errata):
//
//	Authorization: Basic <user:password>   password check (argon2id), with a
//	                                      fallback that treats the password
//	                                      field as an API token; in that mode
//	                                      the Basic username must equal the
//	                                      token subject case-insensitively or
//	                                      the request is rejected
//	Authorization: Bearer <token>         token check
//	X-JFrog-Art-Api: <token>              token check (legacy X-Api-Key is
//	                                      accepted as its synonym)
//	Cookie: binflow_session=<opaque>       web session check (web_sessions
//	                                      row: sha256(id) keyed, revocable,
//	                                      absolute TTL cap) — the LAST arm:
//	                                      header credentials win over it, and
//	                                      a presented-but-invalid cookie is a
//	                                      rejected credential, never anonymous
//
// (nil, nil) means anonymous. Any non-nil error is a rejected credential.
// The Authorization header wins when both it and X-JFrog-Art-Api are present;
// every header arm wins over the session cookie.
type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (*Principal, error)
}

// Authorizer decides one (principal, repo, path, action) question
// (architecture section 3.4). Rules, in order:
//
//  1. admin principals pass everything;
//  2. readonly_admin principals (M7, ADR-0026) are globally read-only — r
//     passes everywhere, w/d/m never, and permission targets are not
//     consulted for them (role short-circuit; a target grant they would
//     match is ineffective, not an error);
//  3. named permission targets (PRD E-24): repo must be listed in the
//     target's repos, the path must match an include pattern and no exclude
//     pattern (Ant-style ** and *, exclude wins), and the principal row for
//     this user must carry the requested action;
//  4. no matching grant denies;
//  5. a nil principal (anonymous) is only allowed for ActionRead when
//     anonymous access is enabled, and only on content paths — Can is the
//     content-path decision point, callers must keep the management plane
//     (/binflow/api/**) behind an "authenticated or 401" gate of its own
//     (ADR-0009).
//
// Path convention (T-11 review B-2): a path ending in '/' addresses a
// folder, anything else addresses a file (same convention as the repo
// layer's folder nodes). The Ant matchStart prefix rule — a pattern naming
// a directory covers paths below it — applies ONLY to folder paths,
// mirroring upstream's isFolder() gate. A file path must match a pattern
// fully: includes ["ci-out"] grants the folder listing "ci-out/" but NOT
// the file "ci-out/a.bin"; use "ci-out/**" to cover files below a prefix.
// Callers (T-14/T-13) must preserve the trailing slash when routing folder
// requests.
//
// Store failures deny (fail closed) and are logged.
type Authorizer interface {
	Can(ctx context.Context, p *Principal, repoKey, path, action string) bool
}

// IssuedToken is what TokenRegistry.Issue returns. The HTTP layer (T-15)
// serializes it per PRD E-17 v1.3: access_token / token_type="Bearer" /
// expires_in (omitted when 0 = never expires) / scope, plus token_id as the
// BinFlow superset field (real Artifactory omits it, auth-model.md section
// 3.1). AccessToken is the plaintext and is visible exactly once here
// (NFR-S2); only sha256(plaintext) is persisted.
type IssuedToken struct {
	AccessToken string
	TokenType   string // always TokenTypeBearer
	ExpiresIn   int64  // seconds; 0 = never expires
	Scope       string // always ScopeAPI
	TokenID     int64  // for RevokeByID
	Username    string // token subject
}

// TokenTypeBearer is the fixed token_type of every issued token.
const TokenTypeBearer = "Bearer"

// ScopeAPI is the implicitly granted scope (auth-model.md section 3.1:
// "api:* is always implicitly granted").
const ScopeAPI = "api:*"

// TokenRegistry issues, verifies and revokes API tokens (architecture
// section 3.4, signatures extended by ticket T-11: Issue returns the full
// IssuedToken and revocation works by value or by id).
type TokenRegistry interface {
	// Issue creates a fresh 32-byte crypto/rand token for username. ttl <= 0
	// — including negative values — means the token never expires; the HTTP
	// layer must reject negative expires_in itself (auth-model.md section
	// 3.1: negative -> 400 invalid_request) before calling Issue. The
	// plaintext appears only in the returned value; the store keeps
	// sha256(plaintext).
	Issue(ctx context.Context, username string, ttl time.Duration) (*IssuedToken, error)
	// Verify resolves a plaintext token to its principal. Expired, revoked
	// (revocation deletes the row), unknown and disabled-owner tokens all
	// fail with an error satisfying errors.Is(err, ErrInvalidCredentials).
	Verify(ctx context.Context, plaintext string) (*Principal, error)
	// Revoke deletes the token row matching the plaintext value. Unknown
	// values return an error wrapping ErrTokenNotFound — the same sentinel
	// as metadata.ErrTokenNotFound, so errors.Is matches either spelling
	// (the HTTP layer maps it to the idempotent "Token not found" 200).
	Revoke(ctx context.Context, plaintext string) error
	// RevokeByID deletes the token row with the given id (same not-found
	// semantics as Revoke).
	RevokeByID(ctx context.Context, tokenID int64) error
}

// PasswordChanger rotates a local account password. Checks run in the
// spec's trigger order (auth-model.md section 2.2): unknown user and wrong
// old password are reported first, then the new-password shape rules. Error
// mapping for the HTTP layer (all 400): wrong old password or unknown user
// -> ErrInvalidCredentials ("Incorrect username/password"), blank new
// password -> ErrEmptyPassword, unchanged password -> ErrSamePassword.
type PasswordChanger interface {
	ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error
}

// CookieSessionName is the browser session cookie (PRD FR-23 / ADR-0014
// erratum 2): HttpOnly, Path=/binflow, SameSite=Lax; the value is the
// plaintext session id, stored server-side only as sha256 (NFR-S19: ≥128
// bits of entropy, never logged).
const CookieSessionName = "binflow_session" //nolint:gosec // cookie name, not a credential

// IssuedSession is what SessionRegistry.IssueSession returns: the cookie
// value (plaintext, visible exactly once — the same rule as API tokens,
// NFR-S2) plus the absolute expiry stamped into the row. The id is 32 bytes
// of crypto/rand, hex-encoded (256 bits).
type IssuedSession struct {
	ID        string
	ExpiresAt time.Time
}

// SessionRegistry is the console-login facet of the auth service (ADR-0014
// decision 2): mint, check and revoke server-side browser sessions. The
// HTTP layer consumes this seam for POST/DELETE /api/v1/session; the verify
// side is not part of it — cookie verification rides Authenticator itself
// so every plane (content, protocol adapters, management) shares one
// authentication path.
type SessionRegistry interface {
	// AuthenticateCredentials checks one username/password pair with the
	// SAME rules as the Basic arm (argon2id password first, API-token
	// duality fallback included) — the login endpoint must not grow a
	// second, drifting password implementation.
	AuthenticateCredentials(ctx context.Context, username, password string) (*Principal, error)
	// IssueSession creates a fresh session row for an existing enabled
	// user; ttl is the absolute cap (PRD R4 dual key resolved by config).
	IssueSession(ctx context.Context, username string, ttl time.Duration) (*IssuedSession, error)
	// RevokeSession marks the session behind the plaintext id logged out
	// (idempotent). Unknown ids return an error wrapping
	// metadata.ErrWebSessionNotFound.
	RevokeSession(ctx context.Context, plaintext string) error
}
