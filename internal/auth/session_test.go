package auth_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// sessionReq builds a request carrying only the session cookie.
func sessionReq(id string) *http.Request {
	r, err := http.NewRequest(http.MethodGet, "/binflow/api/v1/session", nil)
	if err != nil {
		panic(err)
	}
	if id != "" {
		r.AddCookie(&http.Cookie{Name: auth.CookieSessionName, Value: id})
	}
	return r
}

// sessionDigest mirrors the storage rule: sha256(id), hex.
func sessionDigest(id string) string {
	d := sha256.Sum256([]byte(id))
	return hex.EncodeToString(d[:])
}

// seedSessionRow writes a web_sessions row directly through the store so a
// test can pin timestamps the public API always derives from now. Returns
// the plaintext id the row is keyed by.
func (f *fixture) seedSessionRow(t *testing.T, username string, expiresAt, lastUsed, revokedAt string) string {
	t.Helper()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("fixture entropy: %v", err)
	}
	id := hex.EncodeToString(buf)
	err := f.st.WebSessions().Create(context.Background(), &metadata.WebSession{
		IDHash:     sessionDigest(id),
		Username:   username,
		CreatedAt:  time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:  expiresAt,
		LastUsedAt: lastUsed,
		RevokedAt:  revokedAt,
	})
	if err != nil {
		t.Fatalf("seed session row: %v", err)
	}
	return id
}

// TestSessionArmEquivalence is the three-arm matrix at the authenticator
// level: Basic, token header and session cookie all resolve the same
// Principal (modulo the arm markers), and every wrong form fails closed with
// the ErrInvalidCredentials family (PRD FR-23: the session cookie is an
// equivalent credential, not a weaker one).
func TestSessionArmEquivalence(t *testing.T) {
	f := newFixture(t, true)

	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	bearer := req("", "")
	bearer.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	for _, tc := range []struct {
		name       string
		r          *http.Request
		viaSession bool
	}{
		{"basic arm", req(basic("ci-bot", ciPW), ""), false},
		{"token header arm", req("", tok.AccessToken), false},
		{"bearer arm", bearer, false},
		{"session cookie arm", sessionReq(sess.ID), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := f.svc.Authenticate(f.ctx, tc.r)
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			if p == nil {
				t.Fatal("principal nil, want ci-bot")
			}
			if p.Name != "ci-bot" {
				t.Fatalf("Name = %q, want ci-bot", p.Name)
			}
			if p.Admin {
				t.Fatal("ci-bot resolved as admin")
			}
			if p.ViaSession != tc.viaSession {
				t.Fatalf("ViaSession = %v, want %v", p.ViaSession, tc.viaSession)
			}
			if tc.viaSession && p.TokenID != 0 {
				t.Fatalf("TokenID = %d on the cookie arm, want 0", p.TokenID)
			}
		})
	}
}

// TestSessionRejections: every presented-but-invalid cookie is a REJECTED
// credential — never a downgrade to anonymous (an expired or logged-out
// browser session must see 401, PRD FR-23 "到期待遇同未认证").
func TestSessionRejections(t *testing.T) {
	f := newFixture(t, true)

	revoked, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession revoked: %v", err)
	}
	if err := f.svc.RevokeSession(f.ctx, revoked.ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	expired := f.seedSessionRow(t, "ci-bot",
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), "", "")
	// A directly-seeded revoked row (Create forces revoked_at='' per the
	// store contract, so the stamp goes in through Revoke — the same UPDATE
	// the API path issues).
	revokedRow := f.seedSessionRow(t, "ci-bot",
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "", "")
	if err := f.st.WebSessions().Revoke(context.Background(),
		sessionDigest(revokedRow), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("seed revoke: %v", err)
	}
	f.createUserDirect(t, "suspended", "suspended-pw", false, false)
	disabledOwner := f.seedSessionRow(t, "suspended",
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "", "")

	for _, tc := range []struct {
		name string
		id   string
	}{
		{"unknown session", "deadbeef"},
		{"revoked via api", revoked.ID},
		{"revoked row", revokedRow},
		{"expired session", expired},
		{"disabled owner", disabledOwner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := f.svc.Authenticate(f.ctx, sessionReq(tc.id))
			if err == nil {
				t.Fatalf("Authenticate accepted %s (principal %+v), want rejection", tc.name, p)
			}
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("Authenticate %s: %v, want ErrInvalidCredentials family", tc.name, err)
			}
			if p != nil {
				t.Fatalf("rejected credential returned principal %+v", p)
			}
		})
	}
}

// TestSessionTTLAbsoluteCapWins: the sliding heartbeat can never extend a
// session past created_at + TTL (ADR-0014 decision 2, erratum 3). A session
// used shortly before its cap still dies at the cap.
func TestSessionTTLAbsoluteCapWins(t *testing.T) {
	f := newFixture(t, true)
	ttl := 1200 * time.Millisecond
	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", ttl)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	// Heartbeat well inside the window: the request succeeds and records
	// last_used (throttled to one write per minute, so this row's stamp is
	// the issuance stamp — the authenticate leg still proves liveness).
	time.Sleep(300 * time.Millisecond)
	if _, err := f.svc.Authenticate(f.ctx, sessionReq(sess.ID)); err != nil {
		t.Fatalf("mid-life Authenticate: %v", err)
	}
	row, err := f.st.WebSessions().GetBySHA256(f.ctx, sessionDigest(sess.ID))
	if err != nil {
		t.Fatalf("GetBySHA256: %v", err)
	}
	if row.LastUsedAt == "" {
		t.Fatal("heartbeat did not record last_used")
	}

	// Past the absolute cap the session is dead even though the idle window
	// (last_used + ttl) would still be open — the cap dominates.
	time.Sleep(ttl)
	if _, err := f.svc.Authenticate(f.ctx, sessionReq(sess.ID)); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("past-cap Authenticate: %v, want invalid credentials", err)
	}
}

// TestSessionRestartPersistence: sessions are rows, not process memory — a
// fresh Service over the same store resolves the same cookie (W37a's
// authentication-side leg; the HTTP-side restart test lives in httpapi).
func TestSessionRestartPersistence(t *testing.T) {
	f := newFixture(t, true)
	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	rebuilt := auth.NewFromStore(f.st, true)
	p, err := rebuilt.Authenticate(f.ctx, sessionReq(sess.ID))
	if err != nil {
		t.Fatalf("Authenticate via rebuilt service: %v", err)
	}
	if p == nil || p.Name != "ci-bot" || !p.ViaSession {
		t.Fatalf("rebuilt service resolved %+v, want ci-bot via session", p)
	}
}

// TestSessionHeaderPrecedence: header credentials win over the cookie — a
// valid Basic next to a stale cookie authenticates, and a bad Basic next to
// a live cookie is still rejected (the header arm ran).
func TestSessionHeaderPrecedence(t *testing.T) {
	f := newFixture(t, true)
	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	r := sessionReq(sess.ID)
	r.Header.Set("Authorization", basic("admin", adminPW))
	p, err := f.svc.Authenticate(f.ctx, r)
	if err != nil {
		t.Fatalf("Authenticate with both credentials: %v", err)
	}
	if p.Name != "admin" || p.ViaSession {
		t.Fatalf("resolved %+v, want admin via the header arm", p)
	}

	r2 := sessionReq(sess.ID)
	r2.Header.Set("Authorization", basic("admin", "wrong"))
	if _, err := f.svc.Authenticate(f.ctx, r2); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("bad header with live cookie: %v, want rejection", err)
	}
}

// TestSessionUnwiredArmIgnored: a service built without WithSessions (the
// pre-M4 New shape) treats the cookie as a non-credential — anonymous, not
// a rejection. Backward-compatibility leg for New()-built assemblies.
func TestSessionUnwiredArmIgnored(t *testing.T) {
	f := newFixture(t, true)
	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	unwired := auth.New(nil, nil, nil, true)
	p, err := unwired.Authenticate(f.ctx, sessionReq(sess.ID))
	if err != nil {
		t.Fatalf("unwired Authenticate: %v, want anonymous", err)
	}
	if p != nil {
		t.Fatalf("unwired service resolved %+v, want nil (anonymous)", p)
	}
}

// TestSessionIDEntropyAndStorage: ids are 64 hex chars (256 bits >= the
// NFR-S19 128-bit floor) and the row stores only the sha256 digest — the
// plaintext never has a second home.
func TestSessionIDEntropyAndStorage(t *testing.T) {
	f := newFixture(t, true)
	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if len(sess.ID) != 64 {
		t.Fatalf("session id length = %d, want 64 hex chars", len(sess.ID))
	}
	row, err := f.st.WebSessions().GetBySHA256(f.ctx, sessionDigest(sess.ID))
	if err != nil {
		t.Fatalf("row by digest: %v", err)
	}
	if row.Username != "ci-bot" || row.RevokedAt != "" {
		t.Fatalf("row = %+v, want live ci-bot session", row)
	}
	if !time.Now().UTC().Before(sess.ExpiresAt) {
		t.Fatalf("ExpiresAt %s not in the future", sess.ExpiresAt)
	}
}

// TestSessionValueNeverLogged (NFR-S19): the plaintext session id never
// appears in this package's log output or error strings.
func TestSessionValueNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	f := newFixture(t, true)
	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	// Storm the failure paths.
	_, _ = f.svc.Authenticate(f.ctx, sessionReq("bogus-id"))
	_ = f.svc.RevokeSession(f.ctx, "bogus-id")
	_, _ = f.svc.IssueSession(f.ctx, "ghost", time.Hour)
	_, _ = f.svc.IssueSession(f.ctx, "", time.Hour)
	if _, err := f.svc.Authenticate(f.ctx, sessionReq(sess.ID)); err != nil {
		t.Fatalf("live session rejected: %v", err)
	}
	if strings.Contains(buf.String(), sess.ID) {
		t.Fatalf("log output leaks the session id:\n%s", buf.String())
	}
	if _, err := f.svc.Authenticate(f.ctx, sessionReq("bogus-id")); err != nil && strings.Contains(err.Error(), sess.ID) {
		t.Fatalf("error string leaks the session id: %v", err)
	}
}

// TestSessionIssueValidation: issuance refuses empty subjects, unknown
// users, disabled users and non-positive TTLs.
func TestSessionIssueValidation(t *testing.T) {
	f := newFixture(t, true)
	f.createUserDirect(t, "suspended", "suspended-pw", false, false)
	for _, tc := range []struct {
		name string
		user string
		ttl  time.Duration
	}{
		{"empty subject", "", time.Hour},
		{"unknown subject", "ghost", time.Hour},
		{"disabled subject", "suspended", time.Hour},
		{"zero ttl", "ci-bot", 0},
		{"negative ttl", "ci-bot", -time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.svc.IssueSession(f.ctx, tc.user, tc.ttl); err == nil {
				t.Fatal("IssueSession accepted, want error")
			}
		})
	}
}

// TestSessionRevokeIdempotence: revoking twice is fine; a revoked session
// does not authenticate; the not-found path surfaces the metadata sentinel.
func TestSessionRevokeIdempotence(t *testing.T) {
	f := newFixture(t, true)
	sess, err := f.svc.IssueSession(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if err := f.svc.RevokeSession(f.ctx, sess.ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if err := f.svc.RevokeSession(f.ctx, sess.ID); err != nil {
		t.Fatalf("RevokeSession twice: %v", err)
	}
	if _, err := f.svc.Authenticate(f.ctx, sessionReq(sess.ID)); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("revoked session: %v, want rejection", err)
	}
	if err := f.svc.RevokeSession(f.ctx, "never-issued"); !errors.Is(err, metadata.ErrWebSessionNotFound) {
		t.Fatalf("unknown session revoke: %v, want ErrWebSessionNotFound", err)
	}
}

// createUserDirect seeds a user with explicit flags (the disabled-owner
// paths need Enabled=false, which the fixture's createUser hardwires true).
func (f *fixture) createUserDirect(t *testing.T, name, password string, admin, enabled bool) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword(%q): %v", name, err)
	}
	now := metadata.Now()
	if err := f.st.Users().Create(f.ctx, &metadata.User{
		Username: name, PasswordHash: hash, IsAdmin: admin, Enabled: enabled,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
}
