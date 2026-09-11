package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// AC 2: issue -> verify -> revoke -> expiry, sha256-only storage,
// E-17 response shape (superset token_id), revocation by value and by id.
func TestTokenLifecycle(t *testing.T) {
	f := newFixture(t, true)

	// Issue: shape per PRD E-17 v1.3 superset.
	tok, err := f.svc.Issue(f.ctx, "ci-bot", 2*time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok.AccessToken == "" || len(tok.AccessToken) != 64 { // 32 bytes hex
		t.Fatalf("access token = %q (len %d), want 64-char hex of 32 bytes", tok.AccessToken, len(tok.AccessToken))
	}
	if _, err := hex.DecodeString(tok.AccessToken); err != nil {
		t.Fatalf("access token is not hex: %v", err)
	}
	if tok.TokenType != "Bearer" {
		t.Fatalf("token_type = %q, want Bearer", tok.TokenType)
	}
	if tok.Scope == "" {
		t.Fatal("scope must be non-empty")
	}
	if want := int64((2 * time.Hour).Seconds()); tok.ExpiresIn != want {
		t.Fatalf("expires_in = %d, want %d", tok.ExpiresIn, want)
	}
	if tok.TokenID <= 0 {
		t.Fatalf("token_id = %d, want > 0", tok.TokenID)
	}
	if tok.Username != "ci-bot" {
		t.Fatalf("username = %q", tok.Username)
	}

	// High entropy: two issues never collide.
	tok2, err := f.svc.Issue(f.ctx, "ci-bot", 0)
	if err != nil {
		t.Fatalf("Issue #2: %v", err)
	}
	if tok.AccessToken == tok2.AccessToken {
		t.Fatal("two issued tokens must differ")
	}
	if tok2.ExpiresIn != 0 {
		t.Fatalf("ttl<=0 means never expires; expires_in = %d, want 0", tok2.ExpiresIn)
	}

	// Verify: identity + TokenID.
	p, err := f.svc.Verify(f.ctx, tok.AccessToken)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if p.Name != "ci-bot" || p.TokenID != tok.TokenID {
		t.Fatalf("principal = %+v, want ci-bot / %d", p, tok.TokenID)
	}
	// last_used_at got stamped (first use always stamps: throttle starts
	// from the empty value).
	row, err := f.st.Tokens().Get(f.ctx, tok.TokenID)
	if err != nil {
		t.Fatalf("token row: %v", err)
	}
	if row.LastUsedAt == "" {
		t.Fatal("last_used_at must be stamped by Verify")
	}

	// DB stores only the digest; the plaintext never touches disk.
	digest := sha256.Sum256([]byte(tok.AccessToken))
	if row.TokenSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("stored digest mismatch")
	}
	dbPath := f.st.(interface{ DBPath() string }).DBPath()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(dbPath + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", dbPath+suffix, err)
		}
		if strings.Contains(string(data), tok.AccessToken) {
			t.Fatalf("plaintext token found in %s (NFR-S2)", dbPath+suffix)
		}
	}

	// Revoke by value: verify fails afterwards, second revoke reports
	// not-found.
	if err := f.svc.Revoke(f.ctx, tok.AccessToken); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("verify after revoke err = %v, want ErrInvalidCredentials", err)
	}
	// L004-1: revocation deletes the row, so the revoked arm is the typed
	// unknown family (the reference's separate "revoked" message is a
	// model-level divergence, registered in the L004-1 report).
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); !errors.Is(err, auth.ErrTokenUnknown) {
		t.Fatalf("verify after revoke err = %v, want errors.Is(auth.ErrTokenUnknown)", err)
	}
	// B-1: not-found must satisfy errors.Is for BOTH sentinel spellings —
	// auth.ErrTokenNotFound aliases metadata.ErrTokenNotFound; no string
	// fallback allowed.
	err = f.svc.Revoke(f.ctx, tok.AccessToken)
	if err == nil {
		t.Fatal("double revoke must report not-found")
	}
	if !errors.Is(err, auth.ErrTokenNotFound) {
		t.Fatalf("double revoke err = %v, want errors.Is(auth.ErrTokenNotFound)", err)
	}
	if !errors.Is(err, metadata.ErrTokenNotFound) {
		t.Fatalf("double revoke err = %v, want errors.Is(metadata.ErrTokenNotFound) — the T-15 contract", err)
	}

	// Revoke by id (token_id from the list, auth-model.md section 3.3/3.4).
	if err := f.svc.RevokeByID(f.ctx, tok2.TokenID); err != nil {
		t.Fatalf("RevokeByID: %v", err)
	}
	if _, err := f.svc.Verify(f.ctx, tok2.AccessToken); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("verify after revoke-by-id err = %v, want ErrInvalidCredentials", err)
	}
	err = f.svc.RevokeByID(f.ctx, tok2.TokenID)
	if err == nil {
		t.Fatal("revoke-by-id of a missing token must fail")
	}
	if !errors.Is(err, auth.ErrTokenNotFound) || !errors.Is(err, metadata.ErrTokenNotFound) {
		t.Fatalf("revoke-by-id not-found err = %v, want the aliased ErrTokenNotFound sentinel", err)
	}
}

// AC 2: expiry is enforced at verify time.
func TestTokenExpiry(t *testing.T) {
	f := newFixture(t, true)

	// A token already expired at issue (negative-ish: 1ns from now).
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Nanosecond)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expired token err = %v, want ErrInvalidCredentials", err)
	}
	// L004-1: the expired arm carries its typed refinement (the /v2 ping
	// plane's message split) while staying an ErrInvalidCredentials.
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); !errors.Is(err, auth.ErrTokenExpired) {
		t.Fatalf("expired token err = %v, want errors.Is(auth.ErrTokenExpired)", err)
	}
	if _, err := f.svc.Verify(f.ctx, "never-issued-value"); !errors.Is(err, auth.ErrTokenUnknown) {
		t.Fatalf("unknown token err = %v, want errors.Is(auth.ErrTokenUnknown)", err)
	}

	// ttl <= 0 means never: sentinel expires_at verifies forever.
	tok, err = f.svc.Issue(f.ctx, "ci-bot", 0)
	if err != nil {
		t.Fatalf("Issue never: %v", err)
	}
	row, err := f.st.Tokens().Get(f.ctx, tok.TokenID)
	if err != nil {
		t.Fatalf("row: %v", err)
	}
	if row.ExpiresAt != metadata.NeverExpires {
		t.Fatalf("never-expire row = %q, want sentinel %q", row.ExpiresAt, metadata.NeverExpires)
	}
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); err != nil {
		t.Fatalf("never-expiring token failed: %v", err)
	}
}

// AC 2 + auth-model.md section 3.6: a token used as a Basic password must
// have a matching (case-insensitive) Basic username.
func TestTokenAsBasicPasswordSubjectConsistency(t *testing.T) {
	f := newFixture(t, true)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	tests := []struct {
		user    string
		wantErr bool
	}{
		{"ci-bot", false},
		{"CI-BOT", false}, // ignore-case match (auth-model.md: verifyMatchingPrincipal)
		{"Ci-BoT", false},
		{"other", true}, // different user
		{"admin", true}, // even admin does not ride a ci-bot token
		{"", true},      // empty subject never matches
	}
	for _, tt := range tests {
		t.Run("user="+tt.user, func(t *testing.T) {
			p, err := f.svc.Authenticate(f.ctx, req(basic(tt.user, tok.AccessToken), ""))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected rejection, got %+v", p)
				}
				if !errors.Is(err, auth.ErrInvalidCredentials) {
					t.Fatalf("err = %v, want ErrInvalidCredentials", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Name != "ci-bot" || p.TokenID != tok.TokenID {
				t.Fatalf("principal = %+v", p)
			}
		})
	}
}

// AC 3 (change password): old password verified, hash rotated, old token
// unaffected (tokens are independent credentials), typed validation errors.
func TestChangePassword(t *testing.T) {
	f := newFixture(t, true)

	// Old password wrong -> typed invalid-credentials (HTTP maps to 400).
	err := f.svc.ChangePassword(f.ctx, "ci-bot", "wrong", "brand-new-pw")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong old password err = %v, want ErrInvalidCredentials", err)
	}
	// Blank new password.
	if err := f.svc.ChangePassword(f.ctx, "ci-bot", ciPW, ""); !errors.Is(err, auth.ErrEmptyPassword) {
		t.Fatalf("blank new err = %v, want ErrEmptyPassword", err)
	}
	// Same password.
	if err := f.svc.ChangePassword(f.ctx, "ci-bot", ciPW, ciPW); !errors.Is(err, auth.ErrSamePassword) {
		t.Fatalf("same new err = %v, want ErrSamePassword", err)
	}
	// Unknown user.
	if err := f.svc.ChangePassword(f.ctx, "ghost", "x", "y"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("unknown user err = %v, want ErrInvalidCredentials", err)
	}

	// Success: old password stops authenticating, new one works.
	if err := f.svc.ChangePassword(f.ctx, "ci-bot", ciPW, "brand-new-pw"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := f.svc.Authenticate(f.ctx, req(basic("ci-bot", ciPW), "")); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old password after rotation err = %v, want rejection", err)
	}
	p, err := f.svc.Authenticate(f.ctx, req(basic("ci-bot", "brand-new-pw"), ""))
	if err != nil || p == nil || p.Name != "ci-bot" {
		t.Fatalf("new password rejected: %v %+v", err, p)
	}

	// The stored hash is a fresh argon2id string (not the plaintext, not
	// the old hash).
	u, err := f.st.Users().Get(f.ctx, "ci-bot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") {
		t.Fatalf("stored hash = %q", u.PasswordHash)
	}
	if strings.Contains(u.PasswordHash, "brand-new-pw") {
		t.Fatal("hash leaks plaintext")
	}
	if auth.VerifyPassword(ciPW, u.PasswordHash) {
		t.Fatal("old password still verifies against the new hash")
	}
}

// Issue for an unknown subject fails cleanly.
func TestIssueUnknownUser(t *testing.T) {
	f := newFixture(t, true)
	if _, err := f.svc.Issue(f.ctx, "ghost", time.Hour); err == nil {
		t.Fatal("issuing for an unknown user must fail")
	}
}

// Verify with an empty string is a rejection, not a panic.
func TestVerifyEmptyToken(t *testing.T) {
	f := newFixture(t, true)
	if _, err := f.svc.Verify(context.Background(), ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

// M-3: last_used_at writes are throttled — the first Verify stamps, a
// Verify within the window does not rewrite the stored value.
func TestVerifyTouchThrottled(t *testing.T) {
	f := newFixture(t, true)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); err != nil {
		t.Fatalf("Verify #1: %v", err)
	}
	row1, err := f.st.Tokens().Get(f.ctx, tok.TokenID)
	if err != nil {
		t.Fatalf("row #1: %v", err)
	}
	if row1.LastUsedAt == "" {
		t.Fatal("first use must stamp last_used_at")
	}
	// Second verify inside the throttle window: stored stamp unchanged.
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); err != nil {
		t.Fatalf("Verify #2: %v", err)
	}
	row2, err := f.st.Tokens().Get(f.ctx, tok.TokenID)
	if err != nil {
		t.Fatalf("row #2: %v", err)
	}
	if row2.LastUsedAt != row1.LastUsedAt {
		t.Fatalf("last_used_at rewritten inside throttle window: %q -> %q", row1.LastUsedAt, row2.LastUsedAt)
	}
}
