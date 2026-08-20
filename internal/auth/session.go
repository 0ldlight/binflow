// Browser session arm of the authenticator (M4, ADR-0014 decision 2 as
// amended by the T-108 errata; PRD FR-23 CE-03..05).
//
// Server-side sessions live in web_sessions (schema 004): the cookie carries
// a 256-bit random id, the row stores sha256(id) — the same storage rule as
// API tokens — plus created/expires/last_used/revoked timestamps. A session
// is a full-authentication credential: the Principal it resolves is
// equivalent to a Basic login (no TokenID, ViaSession=true for the CSRF
// plane).

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// webSessionSource is the consumer-side slice of metadata.WebSessionStore
// the session arm needs. metadata.WebSessionStore satisfies it directly (the
// extra sweep/delete methods are simply not required here).
type webSessionSource interface {
	Create(ctx context.Context, s *metadata.WebSession) error
	GetBySHA256(ctx context.Context, idHash string) (*metadata.WebSession, error)
	Touch(ctx context.Context, idHash, lastUsedAt string) error
	Revoke(ctx context.Context, idHash, revokedAt string) error
}

// WebSessionSource re-exports the consumer-side seam so callers building
// alternative stores (tests) can implement it without the unexported type.
type WebSessionSource = webSessionSource

// sessionBytes matches the token entropy budget: 32 crypto/rand bytes
// (256 bits, comfortably over the NFR-S19 >=128-bit floor; no dictionary
// surface, sha256 storage is sufficient).
const sessionBytes = tokenBytes

// WithSessions returns a copy of svc whose session arm is backed by src.
// Without this call the cookie arm is inert: a presented binflow_session
// cookie is ignored (anonymous), which keeps New()-built services in older
// tests byte-compatible. NewFromStore applies it for every production and
// harness assembly.
func (s *Service) WithSessions(src WebSessionSource) *Service {
	clone := *s
	clone.sessions = src
	return &clone
}

// authenticateSession resolves the binflow_session cookie into a Principal.
// Every failure is a rejected credential (ErrInvalidCredentials family): a
// stale, expired, revoked or ownerless cookie must yield 401 — "expired
// session behaves like unauthenticated" (PRD FR-23) — never a silent
// downgrade to anonymous.
func (s *Service) authenticateSession(ctx context.Context, id string) (*Principal, error) {
	if id == "" {
		return nil, invalidf("auth: empty session id")
	}
	idHash := sessionHash(id)
	row, err := s.sessions.GetBySHA256(ctx, idHash)
	if err != nil {
		if isNotFound(err, metadata.ErrWebSessionNotFound) {
			return nil, invalidf("auth: unknown session")
		}
		return nil, fmt.Errorf("auth: session lookup: %w", err)
	}
	if row.RevokedAt != "" {
		// Logout revokes server-side; replaying the cookie must fail (W07).
		return nil, invalidf("auth: session revoked")
	}
	if expired(row.ExpiresAt) {
		// The absolute cap (created_at + TTL). The last_used heartbeat can
		// slide the idle window but never past this stamp — see the expiry
		// note below.
		return nil, invalidf("auth: session expired")
	}
	// Re-resolve the owner per request: admin-flag changes must apply
	// immediately and a disabled account must kill its sessions (the row's
	// FK removes sessions of deleted users already).
	u, err := s.users.Get(ctx, row.Username)
	if err != nil {
		return nil, invalidf("auth: session owner unavailable")
	}
	if !u.Enabled {
		return nil, invalidf("auth: session owner disabled")
	}
	// Heartbeat: refresh last_used (throttled like tokens — one write per
	// minute per session, not one per request). Best-effort: a failed
	// heartbeat never fails the request; expiry is judged from the row
	// above either way.
	//
	// Expiry semantics (ADR-0014 decision 2, erratum 3): the sliding window
	// renews on use but is CAPPED by the absolute expires_at. While the idle
	// window and the absolute cap share the single console TTL key, the cap
	// mathematically dominates: last_used >= created_at implies
	// last_used + TTL >= created_at + TTL = expires_at, so any renewal would
	// clamp to the cap and the session always dies at created_at + TTL. The
	// heartbeat column is the seam a future split (idle < absolute) keys on.
	if shouldTouch(row.LastUsedAt) {
		_ = s.sessions.Touch(ctx, idHash, nowRFC3339()) //nolint:errcheck // heartbeat, best-effort by design
	}
	return &Principal{Name: u.Username, Admin: u.IsAdmin, ViaSession: true}, nil
}

// IssueSession implements SessionRegistry.IssueSession: 32 random bytes,
// hex-encoded, one row storing sha256(plaintext). expires_at is the absolute
// cap (now + ttl); last_used starts at creation (the login is the first
// use), revoked_at empty.
func (s *Service) IssueSession(ctx context.Context, username string, ttl time.Duration) (*IssuedSession, error) {
	if username == "" {
		return nil, invalidf("auth: session subject is required")
	}
	u, err := s.users.Get(ctx, username)
	if err != nil {
		return nil, invalidf("auth: session subject unavailable")
	}
	if !u.Enabled {
		return nil, invalidf("auth: session subject disabled")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("auth: session ttl must be positive, got %s", ttl)
	}
	buf := make([]byte, sessionBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("auth: session entropy: %w", err)
	}
	id := hex.EncodeToString(buf)
	now := time.Now().UTC()
	row := &metadata.WebSession{
		IDHash:     sessionHash(id),
		Username:   u.Username,
		CreatedAt:  now.Format(time.RFC3339),
		ExpiresAt:  now.Add(ttl).Format(time.RFC3339),
		LastUsedAt: now.Format(time.RFC3339),
	}
	if err := s.sessions.Create(ctx, row); err != nil {
		return nil, fmt.Errorf("auth: persisting session: %w", err)
	}
	return &IssuedSession{ID: id, ExpiresAt: now.Add(ttl)}, nil
}

// RevokeSession implements SessionRegistry.RevokeSession: mark the row
// logged out. Unknown ids surface metadata.ErrWebSessionNotFound so the HTTP
// layer can decide the idempotent response.
func (s *Service) RevokeSession(ctx context.Context, plaintext string) error {
	if plaintext == "" {
		return fmt.Errorf("auth: revoke session: %w", metadata.ErrWebSessionNotFound)
	}
	idHash := sessionHash(plaintext)
	if err := s.sessions.Revoke(ctx, idHash, nowRFC3339()); err != nil {
		if isNotFound(err, metadata.ErrWebSessionNotFound) {
			return fmt.Errorf("auth: revoke session: %w", metadata.ErrWebSessionNotFound)
		}
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// AuthenticateCredentials implements SessionRegistry.AuthenticateCredentials:
// the login endpoint's username/password check IS the Basic arm — same
// argon2id verification, same API-token duality fallback, same error family
// — so the console can never drift from the header plane on what a correct
// credential is.
func (s *Service) AuthenticateCredentials(ctx context.Context, username, password string) (*Principal, error) {
	return s.authenticateBasic(ctx, username, password)
}

// sessionCookie extracts the binflow_session cookie value ("", false when
// the request carries no session cookie).
func sessionCookie(r *http.Request) (string, bool) {
	c, err := r.Cookie(CookieSessionName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

// sessionHash is the storage digest of a session id (sha256, hex).
func sessionHash(id string) string {
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:])
}
