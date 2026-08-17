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

// Service implements Authenticator, Authorizer, TokenRegistry and
// PasswordChanger over the metadata sub-stores (architecture section 3.4).
// It is stateless apart from the injected stores and config flag, so one
// instance serves the whole process; all methods take ctx explicitly.
type Service struct {
	users         userSource
	tokens        tokenSource
	permissions   permissionSource
	anonymousRead bool
	verifier      *TokenVerifier
}

// userSource is the consumer-side slice of metadata.UserStore the
// authenticator needs (interface defined at the consumer, per project
// convention).
type userSource interface {
	Get(ctx context.Context, username string) (user, error)
	UpdatePassword(ctx context.Context, username, passwordHash string) error
}

// user mirrors metadata.User without importing the whole row type; the
// adapter in deps.go converts.
type user struct {
	Username     string
	PasswordHash string
	IsAdmin      bool
	Enabled      bool
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

var (
	_ Authenticator   = (*Service)(nil)
	_ Authorizer      = (*Service)(nil)
	_ TokenRegistry   = (*Service)(nil)
	_ PasswordChanger = (*Service)(nil)
)

// Authenticate resolves the request credential (architecture section 3.4;
// auth-model.md section 3.6). See the Authenticator interface doc for the
// entry-point precedence. No credential at all yields (nil, nil).
func (s *Service) Authenticate(ctx context.Context, r *http.Request) (*Principal, error) {
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
		return s.verifier.Verify(ctx, tok)
	}
	return nil, nil // anonymous
}

// authenticateBasic implements the Basic duality (auth-model.md section 3.6,
// high confidence): the password field is first tried as a real password;
// when that fails and the value verifies as an API token, the Basic username
// must equal the token subject case-insensitively ("Token principal
// mismatch" otherwise).
func (s *Service) authenticateBasic(ctx context.Context, username, password string) (*Principal, error) {
	u, err := s.users.Get(ctx, username)
	switch {
	case err == nil && u.Enabled && VerifyPassword(password, u.PasswordHash):
		return &Principal{Name: u.Username, Admin: u.IsAdmin}, nil
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
