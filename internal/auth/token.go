package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// tokenBytes is the entropy budget of every issued token: 32 bytes from
// crypto/rand (NFR-S2; 256 bits, no dictionary surface, sha256 storage is
// sufficient per architecture section 3.4 comment).
const tokenBytes = 32

// TokenVerifier resolves plaintext tokens to principals. It is exported
// because the HTTP layer may want to verify a token presented through a
// non-standard entry point (docker login exchange, M2) using the same rules.
type TokenVerifier struct {
	tokens tokenSource
	users  userSource
}

// Verify implements TokenRegistry.Verify: digest lookup, expiry check,
// revocation check (revocation deletes the row, so "no row" covers both
// unknown and revoked), owner lookup (disabled owners invalidate their
// tokens). last_used_at is best-effort refreshed.
func (v *TokenVerifier) Verify(ctx context.Context, plaintext string) (*Principal, error) {
	if plaintext == "" {
		return nil, invalidf("auth: empty token")
	}
	digest := sha256.Sum256([]byte(plaintext))
	t, err := v.tokens.GetBySHA256(ctx, hex.EncodeToString(digest[:]))
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			return nil, invalidf("auth: unknown or revoked token")
		}
		return nil, fmt.Errorf("auth: token lookup: %w", err)
	}
	if expired(t.ExpiresAt) {
		return nil, invalidf("auth: token expired")
	}
	u, err := v.users.Get(ctx, t.Username)
	if err != nil {
		return nil, invalidf("auth: token owner unavailable")
	}
	if !u.Enabled {
		return nil, invalidf("auth: token owner disabled")
	}
	// Best-effort last_used_at bookkeeping, throttled (M-3): one write per
	// token per touchThrottle window instead of one write per request —
	// every authenticated request would otherwise serialize on the single
	// SQLite writer. The stored value is already in hand; a parse failure
	// (never used / corrupt) stamps now.
	if shouldTouch(t.LastUsedAt) {
		_ = v.tokens.Touch(ctx, t.ID, nowRFC3339())
	}
	// Source reflects the OWNING provider of the user row — the same policy
	// the session arm settled (T-157 leftover 3): a token minted by an OIDC
	// or LDAP user keeps reporting that source on every request, so the
	// whoami plane and the token.issue audit name the arm consistently
	// (T-190 / Q11 guardrail 4). adaptUser normalizes unknown providers to
	// local, so hand-built rows cannot smuggle an arbitrary value.
	//
	// The role rides the SAME re-read (ADR-0026 decision 6, the T-208
	// enabled seam): an existing token's management-plane permissions follow
	// role changes immediately — Verify never freezes the role at mint time.
	p := newPrincipal(u.Username, u.Role, u.Provider)
	p.TokenID = t.ID
	// Narrow tokens (M12 T-349): a stored scope rides the row, so the
	// narrowing survives restarts and is enforced wherever the credential
	// is presented. Unparseable scope documents fail CLOSED — the token is
	// refused outright rather than read as unrestricted.
	if t.DeployScope != "" {
		ds, serr := parseDeployScope(t.DeployScope)
		if serr != nil {
			return nil, invalidf("auth: token scope unusable: %v", serr)
		}
		p.DeployScope = ds
	}
	return p, nil
}

// touchThrottle is the minimum spacing between last_used_at writes for one
// token. Audit precision of "which minute" is enough for revocation
// forensics; request-rate precision is not worth a hot-row write per call.
const touchThrottle = time.Minute

// shouldTouch reports whether the stored stamp is older than the throttle
// window (or absent/unparseable).
func shouldTouch(lastUsedAt string) bool {
	if lastUsedAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, lastUsedAt)
	if err != nil {
		return true
	}
	return time.Since(t) >= touchThrottle
}

// expired parses the RFC3339 expires_at (sentinel 9999-… = never) and
// reports whether it is in the past. Unparseable values fail closed.
func expired(expiresAt string) bool {
	if expiresAt == "" || expiresAt == metadata.NeverExpires {
		return false
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return true // corrupt row: treat as expired
	}
	return !t.After(time.Now())
}

// TokenFingerprint returns the first 8 hex characters of sha256(plaintext).
// It is the opaque, collision-resistant short identifier used in audit events
// so the plaintext token never appears in a stored detail payload (NFR-S3).
func TokenFingerprint(plaintext string) string {
	digest := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(digest[:])[:8]
}

// nowRFC3339 matches the metadata timestamp convention (ADR-0007).
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// Issue implements TokenRegistry.Issue: 32 random bytes, hex-encoded, one
// row storing only sha256(plaintext). ttl <= 0 means never expires.
func (s *Service) Issue(ctx context.Context, username string, ttl time.Duration) (*IssuedToken, error) {
	return s.issue(ctx, username, ttl, "")
}

// IssueChecksumDeployScoped mints the MPU plane's narrow landing credential
// (M12 T-349, FR-113.3 — ADR-0039 residual 1): same entropy, TTL and
// verification lane as a plain API token, but the row carries a deploy scope
// that narrows the token's permission domain to exactly one checksum-deploy
// PUT at (repos, path) — the session's own target spellings (the resolved
// repository plus the virtual key when the client created through one).
// Every other use of the credential answers 403 at the HTTP layer, so a
// leaked token is worth one landing of content the client itself assembled,
// nothing else (NFR-S63, lateral-use defense).
func (s *Service) IssueChecksumDeployScoped(ctx context.Context, username string, ttl time.Duration, repos []string, path string) (*IssuedToken, error) {
	clean := make([]string, 0, len(repos))
	for _, r := range repos {
		if r != "" && !slices.Contains(clean, r) {
			clean = append(clean, r)
		}
	}
	if len(clean) == 0 || path == "" {
		return nil, invalidf("auth: checksum-deploy scope needs a repo key and a path")
	}
	doc, err := json.Marshal(scopeDoc{Kind: DeployScopeChecksumDeploy, Repos: clean, Path: path})
	if err != nil {
		return nil, fmt.Errorf("auth: encoding checksum-deploy scope: %w", err)
	}
	return s.issue(ctx, username, ttl, string(doc))
}

// issue is the single mint path for both token families; scopeJSON is the
// deploy_scope column value (” = unrestricted).
func (s *Service) issue(ctx context.Context, username string, ttl time.Duration, scopeJSON string) (*IssuedToken, error) {
	if username == "" {
		return nil, invalidf("auth: token subject is required")
	}
	if _, err := s.users.Get(ctx, username); err != nil {
		return nil, fmt.Errorf("auth: token subject lookup: %w", err)
	}
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("auth: token entropy: %w", err)
	}
	plaintext := hex.EncodeToString(buf)
	digest := sha256.Sum256([]byte(plaintext))
	expiresAt := metadata.NeverExpires
	var expiresIn int64
	if ttl > 0 {
		expiresIn = int64(ttl.Seconds())
		expiresAt = time.Now().UTC().Add(ttl).Format(time.RFC3339)
	}
	now := nowRFC3339()
	id, err := s.tokens.Create(ctx, token{
		Username:    username,
		TokenSHA256: hex.EncodeToString(digest[:]),
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
		LastUsedAt:  "",
		DeployScope: scopeJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: persisting token: %w", err)
	}
	return &IssuedToken{
		AccessToken: plaintext,
		TokenType:   TokenTypeBearer,
		ExpiresIn:   expiresIn,
		Scope:       ScopeAPI,
		TokenID:     id,
		Username:    username,
	}, nil
}

// scopeDoc is the JSON document the tokens.deploy_scope column carries
// (migration 017). Self-describing on purpose: further narrow-scope kinds
// extend the document without another schema change.
type scopeDoc struct {
	Kind  string   `json:"kind"`
	Repos []string `json:"repos"`
	Path  string   `json:"path"`
}

// parseDeployScope turns a stored scope value into the structured domain.
// Anything malformed — bad JSON, an unknown kind, empty coordinates — is an
// ERROR, never a silent nil: the verifier fails the token closed, so a
// corrupt row can never widen back into a full-power credential.
func parseDeployScope(v string) (*DeployScope, error) {
	var doc scopeDoc
	if err := json.Unmarshal([]byte(v), &doc); err != nil {
		return nil, fmt.Errorf("malformed scope document: %w", err)
	}
	switch doc.Kind {
	case DeployScopeChecksumDeploy:
		if len(doc.Repos) == 0 || doc.Path == "" {
			return nil, fmt.Errorf("checksum-deploy scope with empty coordinates (repos %v path %q)", doc.Repos, doc.Path)
		}
		for _, r := range doc.Repos {
			if r == "" {
				return nil, fmt.Errorf("checksum-deploy scope with an empty repo spelling (repos %v)", doc.Repos)
			}
		}
		return &DeployScope{Kind: doc.Kind, Repos: doc.Repos, Path: doc.Path}, nil
	default:
		return nil, fmt.Errorf("unknown scope kind %q", doc.Kind)
	}
}

// Verify on Service delegates to the shared verifier (single implementation
// of the token rules) and fills the group memberships on the resolved
// principal — the same enrichment Authenticate applies, so direct Verify
// consumers (docker token exchange) see the identical Principal shape.
func (s *Service) Verify(ctx context.Context, plaintext string) (*Principal, error) {
	p, err := s.verifier.Verify(ctx, plaintext)
	if err != nil {
		return nil, err
	}
	s.fillGroups(ctx, p)
	return p, nil
}

// Revoke deletes the token row matching the plaintext. Unknown tokens
// return an error wrapping ErrTokenNotFound (== metadata.ErrTokenNotFound)
// so the HTTP layer can answer the idempotent "Token not found" 200
// (auth-model.md section 3.4 item 5); match with
// errors.Is(err, auth.ErrTokenNotFound) or errors.Is(err,
// metadata.ErrTokenNotFound) — both spellings work, they are the same
// sentinel.
func (s *Service) Revoke(ctx context.Context, plaintext string) error {
	digest := sha256.Sum256([]byte(plaintext))
	t, err := s.tokens.GetBySHA256(ctx, hex.EncodeToString(digest[:]))
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			return fmt.Errorf("auth: revoke by value: %w", ErrTokenNotFound)
		}
		return fmt.Errorf("auth: revoke lookup: %w", err)
	}
	if err := s.tokens.Delete(ctx, t.ID); err != nil {
		return fmt.Errorf("auth: revoke token %d: %w", t.ID, err)
	}
	return nil
}

// RevokeByID deletes the token row with the given id. Not-found carries the
// same ErrTokenNotFound semantics as Revoke.
func (s *Service) RevokeByID(ctx context.Context, tokenID int64) error {
	if err := s.tokens.Delete(ctx, tokenID); err != nil {
		return fmt.Errorf("auth: revoke token %d: %w", tokenID, err)
	}
	return nil
}

// ChangePassword implements PasswordChanger (auth-model.md section 2.2):
// the old password is verified FIRST (wrong old password or unknown user
// outranks the new-password shape checks, matching the spec's trigger
// order), then the new-password rules apply, then a fresh argon2id hash is
// stored. All validation failures are typed so the HTTP layer emits 400
// with the spec wording.
func (s *Service) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	u, err := s.users.Get(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return fmt.Errorf("%w for user %q", ErrInvalidCredentials, username)
		}
		return fmt.Errorf("auth: change-password lookup: %w", err)
	}
	// T-192: the old-password check and the new-password hash both run
	// inside the argon2 gate — each costs the same ~64 MiB derivation as a
	// login, and a disconnect mid-queue abandons the attempt cleanly (the
	// gate error is a transport-level abandonment, not a bad-password 400).
	ok, verr := s.VerifyPassword(ctx, oldPassword, u.PasswordHash)
	if verr != nil {
		return verr
	}
	if !ok {
		return fmt.Errorf("%w: incorrect username/password", ErrInvalidCredentials)
	}
	switch {
	case newPassword == "":
		return ErrEmptyPassword
	case oldPassword == newPassword:
		return ErrSamePassword
	}
	hash, err := s.hashPassword(ctx, newPassword)
	if err != nil {
		return fmt.Errorf("auth: hashing new password: %w", err)
	}
	if err := s.users.UpdatePassword(ctx, username, hash); err != nil {
		return fmt.Errorf("auth: storing new password: %w", err)
	}
	return nil
}
