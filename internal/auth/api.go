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
	Admin   bool
	TokenID int64 // > 0 when the request authenticated via API token
}

// Actions accepted by Authorizer.Can (architecture section 3.4 uses the
// short forms; metadata permission rows map read/write/delete onto them).
const (
	ActionRead   = "r"
	ActionWrite  = "w"
	ActionDelete = "d"
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
// Supported entry points (auth-model.md section 3.6, high confidence):
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
//
// (nil, nil) means anonymous. Any non-nil error is a rejected credential.
// The Authorization header wins when both it and X-JFrog-Art-Api are present.
type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (*Principal, error)
}

// Authorizer decides one (principal, repo, path, action) question
// (architecture section 3.4). Rules, in order:
//
//  1. admin principals pass everything;
//  2. named permission targets (PRD E-24): repo must be listed in the
//     target's repos, the path must match an include pattern and no exclude
//     pattern (Ant-style ** and *, exclude wins), and the principal row for
//     this user must carry the requested action;
//  3. no matching grant denies;
//  4. a nil principal (anonymous) is only allowed for ActionRead when
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
