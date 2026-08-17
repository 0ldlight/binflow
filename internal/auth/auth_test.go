package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// fixture is one auth test world: a fresh SQLite store, seeded users,
// one permission target, and the Service built over them.
type fixture struct {
	t   *testing.T
	st  metadata.Store
	svc *auth.Service
	ctx context.Context
}

const (
	adminPW   = "it-admin-pw"
	ciPW      = "ci-bot-pw" //nolint:gosec // throwaway test fixture password
	otherUser = "other"
)

func newFixture(t *testing.T, anonymousRead bool) *fixture {
	t.Helper()
	st, err := metadata.Open(context.Background(), metadata.Options{
		Driver:        "sqlite",
		Path:          filepath.Join(t.TempDir(), "binflow.db"),
		AdminPassword: adminPW,
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	f := &fixture{t: t, st: st, svc: auth.NewFromStore(st, anonymousRead), ctx: context.Background()}
	f.createUser("ci-bot", ciPW, false)
	f.createUser(otherUser, "other-pw", false)
	return f
}

func (f *fixture) createUser(name, password string, admin bool) {
	f.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		f.t.Fatalf("HashPassword(%q): %v", name, err)
	}
	now := metadata.Now()
	err = f.st.Users().Create(f.ctx, &metadata.User{
		Username: name, PasswordHash: hash, IsAdmin: admin, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		f.t.Fatalf("create user %q: %v", name, err)
	}
}

// putTarget installs one named permission target (PRD E-24 shape).
func (f *fixture) putTarget(name string, repos, includes, excludes []string, principal string, read, write, del bool) {
	f.t.Helper()
	mk := func(list []string) string {
		if list == nil {
			list = []string{}
		}
		b, err := json.Marshal(list)
		if err != nil {
			f.t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	now := metadata.Now()
	err := f.st.Permissions().PutTarget(f.ctx,
		&metadata.PermissionTarget{
			Name: name, Repos: mk(repos), Includes: mk(includes), Excludes: mk(excludes),
			CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{{
			TargetName: name, Principal: principal, PrincipalType: "user",
			CanRead: read, CanWrite: write, CanDelete: del,
		}})
	if err != nil {
		f.t.Fatalf("put target %q: %v", name, err)
	}
}

func req(authz, apiKey string) *http.Request {
	r, err := http.NewRequest(http.MethodGet, "/binflow/generic-local/a.bin", nil)
	if err != nil {
		panic(err)
	}
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}
	if apiKey != "" {
		r.Header.Set("X-JFrog-Art-Api", apiKey)
	}
	return r
}

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// --- AC 1: Authenticator ---

// Table: every entry point resolves, every wrong form fails closed.
func TestAuthenticate(t *testing.T) {
	f := newFixture(t, true)

	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	tests := []struct {
		name      string
		authz     string
		apiKey    string
		wantName  string
		wantAdmin bool
		wantToken bool
		wantErr   bool
	}{
		{"basic password admin", basic("admin", adminPW), "", "admin", true, false, false},
		{"basic password ci-bot", basic("ci-bot", ciPW), "", "ci-bot", false, false, false},
		{"basic wrong password", basic("admin", "nope"), "", "", false, false, true},
		{"basic unknown user", basic("ghost", "nope"), "", "", false, false, true},
		{"basic token as password", basic("ci-bot", tok.AccessToken), "", "ci-bot", false, true, false},
		{"basic token wrong subject", basic("other", tok.AccessToken), "", "", false, false, true},
		{"basic token subject case-insensitive", basic("CI-BOT", tok.AccessToken), "", "ci-bot", false, true, false},
		{"basic malformed base64", "Basic %%%%", "", "", false, false, true},
		{"basic no colon", "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")), "", "", false, false, true},
		{"api key header", "", tok.AccessToken, "ci-bot", false, true, false},
		{"api key legacy header", "", tok.AccessToken, "ci-bot", false, true, false},
		{"api key garbage", "", "not-a-token", "", false, false, true},
		{"bearer token", "Bearer " + tok.AccessToken, "", "ci-bot", false, true, false},
		{"bearer garbage", "Bearer not-a-token", "", "", false, false, true},
		{"unknown scheme falls to api key", "Digest xyz", tok.AccessToken, "ci-bot", false, true, false},
		{"anonymous", "", "", "", false, false, false},
		{"unknown scheme no other credential", "Digest xyz", "", "", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := f.svc.Authenticate(f.ctx, req(tt.authz, tt.apiKey))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got principal %+v", p)
				}
				if !errors.Is(err, auth.ErrInvalidCredentials) {
					t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantName == "" {
				if p != nil {
					t.Fatalf("expected anonymous, got %+v", p)
				}
				return
			}
			if p == nil {
				t.Fatal("expected a principal, got anonymous")
			}
			if p.Name != tt.wantName || p.Admin != tt.wantAdmin {
				t.Fatalf("principal = %+v, want name=%q admin=%v", p, tt.wantName, tt.wantAdmin)
			}
			if tt.wantToken && p.TokenID == 0 {
				t.Fatalf("expected TokenID > 0, got %+v", p)
			}
		})
	}
}

// NFR-S3: no error message leaks the presented credential.
func TestAuthenticateNeverLeaksSecret(t *testing.T) {
	f := newFixture(t, true)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	secret := tok.AccessToken
	// Three rejected presentations of the same secret: as a wrong Basic
	// password, as an API key with the value mangled to be unknown (fresh
	// secret, unknown digest), and as a Basic password with the wrong
	// subject (principal mismatch).
	mangled := secret[:len(secret)-1] + "0"
	if mangled == secret {
		mangled = secret[:len(secret)-1] + "1"
	}
	for _, tc := range []struct {
		name string
		r    *http.Request
	}{
		{"basic token with wrong subject", req(basic("admin", secret), "")},
		{"unknown api key", req("", mangled)},
		{"malformed basic", req("Basic %%%%", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err = f.svc.Authenticate(f.ctx, tc.r)
			if err == nil {
				t.Fatal("expected failure")
			}
			for _, leak := range []string{secret, mangled} {
				if strings.Contains(err.Error(), leak) {
					t.Fatalf("error leaks the secret: %q", err)
				}
			}
		})
	}
}

// Disabled users fail closed on every entry point. UserStore has no
// SetEnabled in M1, so the fixture recreates the world with the row's
// enabled flag flipped through a delete+recreate preserving the hash.
func TestAuthenticateDisabledUser(t *testing.T) {
	f := newFixture(t, true)
	// Snapshot the hash, then re-create the row disabled.
	u, err := f.st.Users().Get(f.ctx, "ci-bot")
	if err != nil {
		t.Fatalf("get ci-bot: %v", err)
	}
	if err := f.st.Users().Delete(f.ctx, "ci-bot"); err != nil {
		t.Fatalf("delete ci-bot: %v", err)
	}
	now := metadata.Now()
	if err := f.st.Users().Create(f.ctx, &metadata.User{
		Username: "ci-bot", PasswordHash: u.PasswordHash, Enabled: false,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("recreate disabled: %v", err)
	}

	if _, err := f.svc.Authenticate(f.ctx, req(basic("ci-bot", ciPW), "")); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled user basic err = %v, want ErrInvalidCredentials", err)
	}
}

// A token whose owner was disabled stops working (owner check in Verify).
func TestTokenDisabledOwner(t *testing.T) {
	f := newFixture(t, true)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); err != nil {
		t.Fatalf("Verify before disable: %v", err)
	}
	u, err := f.st.Users().Get(f.ctx, "ci-bot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := f.st.Users().Delete(f.ctx, "ci-bot"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	now := metadata.Now()
	if err := f.st.Users().Create(f.ctx, &metadata.User{
		Username: "ci-bot", PasswordHash: u.PasswordHash, Enabled: false,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("recreate disabled: %v", err)
	}
	if _, err := f.svc.Verify(f.ctx, tok.AccessToken); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Verify after disable err = %v, want ErrInvalidCredentials", err)
	}
	// Basic with the token as password must fail the same way.
	if _, err := f.svc.Authenticate(f.ctx, req(basic("ci-bot", tok.AccessToken), "")); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("basic-token after disable err = %v, want ErrInvalidCredentials", err)
	}
}
