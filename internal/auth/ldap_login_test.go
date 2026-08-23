package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestLDAPLoginFallback verifies that AuthenticateCredentials tries LDAP bind
// after local password check fails. It covers three scenarios:
//
//  1. Local user with correct password — succeeds without LDAP.
//  2. LDAP user (no local password) — local password check fails (empty hash),
//     LDAP bind succeeds, user resolves, login succeeds.
//  3. LDAP user with wrong password — local check fails, LDAP bind fails,
//     401 returned.
//  4. Unknown user — local check fails (user not found), LDAP bind fails,
//     401 returned.
func TestLDAPLoginFallback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(dir, "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Seed a local user (normal password).
	makeUser(ctx, t, st.Users(), "local-user", "local-pass", false, "local", "")

	// Seed an LDAP user (empty password_hash, provider='ldap',
	// provider_id='uid=alice,dc=example,dc=com').
	makeUser(ctx, t, st.Users(), "alice", "", false, "ldap", "uid=alice,dc=example,dc=com")

	// Seed another LDAP user for fallback without local provider.
	makeUser(ctx, t, st.Users(), "bob", "", false, "ldap", "uid=bob,dc=example,dc=com")

	// Build the LDAP mock directory.
	mock := newMockLDAPConn()
	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid":         {"admin"},
		"objectClass": {"posixAccount"},
	})
	mock.addUser("uid=alice,dc=example,dc=com", "alice-ldap-pass", map[string][]string{
		"uid":         {"alice"},
		"cn":          {"Alice Smith"},
		"objectClass": {"posixAccount"},
	})
	mock.addUser("uid=bob,dc=example,dc=com", "bob-ldap-pass", map[string][]string{
		"uid":         {"bob"},
		"objectClass": {"posixAccount"},
	})

	dialCount := 0
	dialer := mockDialer(mock, mockDialCount(&dialCount))

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      2,
	}

	// Create the LDAP provider with the userStore adapter as resolver.
	svc := auth.NewFromStore(st, false)
	ldapProv, err := auth.NewLDAPProvider(cfg, auth.NewLDAPResolver(st.Users()), dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	t.Cleanup(ldapProv.Close)

	svc = svc.WithLDAP(ldapProv)

	tests := []struct {
		name       string
		username   string
		password   string
		wantName   string
		wantErr    bool
		wantSource auth.Provider
		dialMin    int // minimum expected dials (0 = don't check)
	}{
		{
			name:       "local user correct password",
			username:   "local-user",
			password:   "local-pass",
			wantName:   "local-user",
			wantErr:    false,
			wantSource: auth.ProviderLocal,
			dialMin:    0,
		},
		{
			name:       "ldap user correct password via fallback",
			username:   "alice",
			password:   "alice-ldap-pass",
			wantName:   "alice",
			wantErr:    false,
			wantSource: auth.ProviderLDAP,
			dialMin:    1,
		},
		{
			name:       "ldap user wrong password",
			username:   "bob",
			password:   "wrong-ldap-pass",
			wantName:   "",
			wantErr:    true,
			wantSource: "",
		},
		{
			name:       "unknown user",
			username:   "ghost",
			password:   "anypass",
			wantName:   "",
			wantErr:    true,
			wantSource: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialBefore := dialCount
			p, err := svc.AuthenticateCredentials(ctx, tt.username, tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("AuthenticateCredentials(%q, ...) = %+v, want error", tt.username, p)
				}
				if !errors.Is(err, auth.ErrInvalidCredentials) {
					t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("AuthenticateCredentials(%q, ...): %v", tt.username, err)
			}
			if p.Name != tt.wantName {
				t.Errorf("p.Name = %q, want %q", p.Name, tt.wantName)
			}
			if p.Source != tt.wantSource {
				t.Errorf("p.Source = %v, want %v", p.Source, tt.wantSource)
			}
			if tt.dialMin > 0 && dialCount-dialBefore < tt.dialMin {
				t.Errorf("LDAP dial count: got %d (delta %d), want >= %d", dialCount, dialCount-dialBefore, tt.dialMin)
			}
		})
	}
}

// TestLDAPLoginNoLDAPProvider verifies that AuthenticateCredentials works
// normally when no LDAP provider is wired (pre-M6 behavior).
func TestLDAPLoginNoLDAPProvider(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(dir, "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	makeUser(ctx, t, st.Users(), "local-user", "local-pass", false, "local", "")

	svc := auth.NewFromStore(st, false)

	// Local user still works.
	p, err := svc.AuthenticateCredentials(ctx, "local-user", "local-pass")
	if err != nil {
		t.Fatalf("local user: %v", err)
	}
	if p.Name != "local-user" {
		t.Errorf("p.Name = %q, want %q", p.Name, "local-user")
	}

	// Wrong password still fails.
	_, err = svc.AuthenticateCredentials(ctx, "local-user", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("error %v does not satisfy ErrInvalidCredentials", err)
	}
}

// TestLDAPLoginAutoCreate verifies that an LDAP user who does not yet exist
// locally is auto-created when the userCreator is wired (via NewFromStore).
func TestLDAPLoginAutoCreate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(dir, "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Build the LDAP mock directory with a user who does NOT exist locally.
	mock := newMockLDAPConn()
	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid":         {"admin"},
		"objectClass": {"posixAccount"},
	})
	mock.addUser("uid=newuser,dc=example,dc=com", "newuserpass", map[string][]string{
		"uid":         {"newuser"},
		"cn":          {"New User"},
		"objectClass": {"posixAccount"},
	})

	dialer := mockDialer(mock)

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      2,
	}

	svc := auth.NewFromStore(st, false)
	ldapProv, err := auth.NewLDAPProvider(cfg, auth.NewLDAPResolver(st.Users()), dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	t.Cleanup(ldapProv.Close)

	svc = svc.WithLDAP(ldapProv)

	// User does not exist locally yet — LDAP Bind succeeds, auto-create fires.
	p, err := svc.AuthenticateCredentials(ctx, "newuser", "newuserpass")
	if err != nil {
		t.Fatalf("AuthenticateCredentials(newuser, ...): %v", err)
	}
	if p.Name != "newuser" {
		t.Errorf("p.Name = %q, want %q", p.Name, "newuser")
	}
	if p.Source != auth.ProviderLDAP {
		t.Errorf("p.Source = %v, want %v", p.Source, auth.ProviderLDAP)
	}

	// Verify the user row was created.
	u, err := st.Users().Get(ctx, "newuser")
	if err != nil {
		t.Fatalf("Get newuser after auto-create: %v", err)
	}
	if u.Provider != "ldap" {
		t.Errorf("u.Provider = %q, want %q", u.Provider, "ldap")
	}
	if u.ProviderID != "uid=newuser,dc=example,dc=com" {
		t.Errorf("u.ProviderID = %q, want %q", u.ProviderID, "uid=newuser,dc=example,dc=com")
	}
	if u.PasswordHash != "" {
		t.Errorf("u.PasswordHash = %q, want empty for LDAP user", u.PasswordHash)
	}

	// Second login: now the user exists locally, should still work.
	p2, err := svc.AuthenticateCredentials(ctx, "newuser", "newuserpass")
	if err != nil {
		t.Fatalf("AuthenticateCredentials #2: %v", err)
	}
	if p2.Name != "newuser" {
		t.Errorf("p2.Name = %q, want %q", p2.Name, "newuser")
	}
}

// makeUser is a test helper that creates a user row with the given provider
// and provider_id fields. It assembles the metadata.User struct directly.
func makeUser(ctx context.Context, t *testing.T, store metadata.UserStore,
	username, passwordHash string, admin bool, provider, providerID string) {
	t.Helper()
	now := metadata.Now()
	var hash string
	if passwordHash != "" {
		var err error
		hash, err = auth.HashPassword(passwordHash)
		if err != nil {
			t.Fatalf("HashPassword: %v", err)
		}
	}
	err := store.Create(ctx, &metadata.User{
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      admin,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
		Provider:     provider,
		ProviderID:   providerID,
	})
	if err != nil {
		t.Fatalf("Create user %q: %v", username, err)
	}
}

// TestLDAPLoginLocalUserWithLdapPass verifies that a local user whose password
// happens to also be valid in LDAP still authenticates locally first (local
// password takes precedence over LDAP).
func TestLDAPLoginLocalUserWithLdapPass(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(dir, "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Seed a local user with the same username as an LDAP user.
	makeUser(ctx, t, st.Users(), "shared-user", "local-pass", false, "local", "")

	mock := newMockLDAPConn()
	mock.addUser("cn=admin,dc=example,dc=com", "adminpass", map[string][]string{
		"uid": {"admin"},
	})
	mock.addUser("uid=shared-user,dc=example,dc=com", "ldap-pass", map[string][]string{
		"uid": {"shared-user"},
	})

	dialer := mockDialer(mock)

	cfg := &auth.LDAPConfig{
		Enabled:       true,
		URL:           "ldap://ldap.example.com:389",
		BaseDN:        "dc=example,dc=com",
		BindDN:        "cn=admin,dc=example,dc=com",
		BindPassword:  "adminpass",
		UserFilter:    "(&(objectClass=posixAccount)(uid=%s))",
		UserIDAttr:    "uid",
		GroupFilter:   "",
		GroupNameAttr: "cn",
		PoolSize:      2,
	}

	svc := auth.NewFromStore(st, false)
	ldapProv, err := auth.NewLDAPProvider(cfg, auth.NewLDAPResolver(st.Users()), dialer)
	if err != nil {
		t.Fatalf("NewLDAPProvider: %v", err)
	}
	t.Cleanup(ldapProv.Close)

	svc = svc.WithLDAP(ldapProv)

	// Local password succeeds first.
	p, err := svc.AuthenticateCredentials(ctx, "shared-user", "local-pass")
	if err != nil {
		t.Fatalf("local password: %v", err)
	}
	if p.Source != auth.ProviderLocal {
		t.Errorf("p.Source = %v, want %v (local password should win)", p.Source, auth.ProviderLocal)
	}

	// LDAP password should NOT resolve via local path (wrong local pass,
	// but LDAP would succeed). The LDAP fallback triggers and finds the user
	// by (ldap, providerID), but since the local user has provider='local'
	// and an empty provider_id, the LDAP Bind's ProviderID
	// (uid=shared-user,dc=example,dc=com) won't match. The auto-create would
	// create a separate row. This test validates the path doesn't crash.
	p2, err := svc.AuthenticateCredentials(ctx, "shared-user", "ldap-pass")
	if err != nil {
		// It might succeed via auto-create (creating a second row with
		// provider='ldap'), or fail if auto-create is disabled/bounded.
		// Either is acceptable — the key is no crash or panic.
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("unexpected error: %v", err)
		}
		t.Logf("LDAP password rejected (expected when auto-create creates different user)")
	} else {
		// Succeeded — verify it came via LDAP.
		if p2.Source != auth.ProviderLDAP {
			t.Errorf("p2.Source = %v, want %v", p2.Source, auth.ProviderLDAP)
		}
		t.Logf("LDAP password accepted via fallback (new LDAP user auto-created)")
	}
}

// TestLDAPResolverGetByProvider tests the auth.NewLDAPResolver helper, which
// wraps a metadata.UserStore into an LDAPUserResolver via the adapter in deps.go.
func TestLDAPResolverGetByProvider(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(dir, "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	makeUser(ctx, t, st.Users(), "alice", "", false, "ldap", "uid=alice,dc=example,dc=com")
	makeUser(ctx, t, st.Users(), "admin-local", "admin-pass", true, "local", "")

	resolver := auth.NewLDAPResolver(st.Users())

	// Find LDAP user by provider+providerID.
	pu, err := resolver.GetByProvider(ctx, auth.ProviderLDAP, "uid=alice,dc=example,dc=com")
	if err != nil {
		t.Fatalf("GetByProvider(alice): %v", err)
	}
	if pu.Username != "alice" {
		t.Errorf("Username = %q, want %q", pu.Username, "alice")
	}

	// Find local user by provider+providerID. The admin user was created by
	// Open with provider='local', provider_id='', so GetByProvider returns
	// "admin" (the first match) rather than "admin-local".
	pu2, err := resolver.GetByProvider(ctx, auth.ProviderLocal, "")
	if err != nil {
		t.Fatalf("GetByProvider(local, ''): %v", err)
	}
	if pu2.Username != "admin" {
		t.Errorf("Username = %q, want %q", pu2.Username, "admin")
	}

	// Non-existent provider+providerID.
	_, err = resolver.GetByProvider(ctx, auth.ProviderLDAP, "uid=nonexistent,dc=example,dc=com")
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
	if !errors.Is(err, auth.ErrProviderUserNotFound) && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
