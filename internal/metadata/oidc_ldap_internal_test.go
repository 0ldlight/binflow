package metadata

import (
	"context"
	"path/filepath"
	"testing"
)

// TestOIDCLDAPMigrationIdempotent verifies that the 008 migration (provider
// and provider_id columns on users) is idempotent: opening the same database
// twice does not re-apply the migration or produce errors.
func TestOIDCLDAPMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		st, err := Open(ctx, Options{Path: path, AdminPassword: "pw-008"})
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		v, err := CurrentVersion(ctx, st.(*sqliteStore).db)
		if err != nil {
			t.Fatalf("CurrentVersion #%d: %v", i+1, err)
		}
		if want := latestMigrationVersion(); v != want {
			t.Fatalf("Open #%d left version at %d, want %d (008 must not re-run)", i+1, v, want)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}

// TestOIDCLDAPMigrationColumnsExist verifies that the provider and provider_id
// columns are present on the users table and have the expected defaults.
func TestOIDCLDAPMigrationColumnsExist(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")

	st, err := Open(ctx, Options{Path: path, AdminPassword: "pw-008"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	// Admin user should have provider='local', provider_id=''.
	admin, err := st.Users().Get(ctx, "admin")
	if err != nil {
		t.Fatalf("Get admin: %v", err)
	}
	if admin.Provider != "local" {
		t.Errorf("admin.Provider = %q, want %q", admin.Provider, "local")
	}
	if admin.ProviderID != "" {
		t.Errorf("admin.ProviderID = %q, want %q", admin.ProviderID, "")
	}

	// Create a new user and verify the columns are populated.
	now := Now()
	if err := st.Users().Create(ctx, &User{
		Username: "test-user", PasswordHash: "ignored", IsAdmin: false, Enabled: true,
		CreatedAt: now, UpdatedAt: now, Provider: "local", ProviderID: "",
	}); err != nil {
		t.Fatalf("Create test-user: %v", err)
	}
	u, err := st.Users().Get(ctx, "test-user")
	if err != nil {
		t.Fatalf("Get test-user: %v", err)
	}
	if u.Provider != "local" {
		t.Errorf("test-user.Provider = %q, want %q", u.Provider, "local")
	}
	if u.ProviderID != "" {
		t.Errorf("test-user.ProviderID = %q, want %q", u.ProviderID, "")
	}
}
