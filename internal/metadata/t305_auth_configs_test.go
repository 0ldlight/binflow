package metadata_test

// T-305 (FR-92, migration 015): the auth_configs descriptor table exists in
// BOTH dialect files (ADR-0007 lockstep), the migrator lands version 015,
// and the AuthConfigStore sub-store honors the section-granularity contract
// — Get absent = (nil, nil), Put is an integral per-section replacement,
// List is name-ordered.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestT305Migration015BothDialects: the lockstep rule — sqlite (embedded)
// and postgres files both carry the table idempotently spelled the same.
func TestT305Migration015BothDialects(t *testing.T) {
	for _, path := range []string{
		"migrations/sqlite/015_auth_configs.sql",
		"migrations/postgres/015_auth_configs.sql",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("dialect file %s: %v", path, err)
		}
		if !strings.Contains(string(b), "CREATE TABLE IF NOT EXISTS auth_configs") {
			t.Fatalf("%s does not create the auth_configs table idempotently", path)
		}
		for _, col := range []string{"section", "doc", "updated_at", "updated_by"} {
			if !strings.Contains(string(b), col) {
				t.Fatalf("%s does not carry column %q", path, col)
			}
		}
	}
}

// TestT305MigrationAppliedAndSectionRoundTrip: a fresh open lands 015 and
// the sub-store round-trips the integral-replacement contract.
func TestT305MigrationAppliedAndSectionRoundTrip(t *testing.T) {
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	var v int
	if err := rawQueryInt(md, `SELECT MAX(version) FROM schema_migrations`, &v); err != nil {
		t.Fatalf("reading schema version: %v", err)
	}
	if v < 15 {
		t.Fatalf("schema version = %d, want >= 15 (015_auth_configs applied)", v)
	}

	store := md.AuthConfigs()

	// Absent section: (nil, nil).
	rec, err := store.GetAuthConfig(ctx, "ldap")
	if err != nil || rec != nil {
		t.Fatalf("GetAuthConfig(absent) = %v, %v; want nil, nil", rec, err)
	}

	// Create and read back.
	if err := store.PutAuthConfig(ctx, &metadata.AuthConfigRecord{
		Section: "ldap", Doc: `{"enabled":true}`,
		UpdatedAt: "2026-08-26T00:00:00Z", UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("PutAuthConfig: %v", err)
	}
	rec, err = store.GetAuthConfig(ctx, "ldap")
	if err != nil || rec == nil {
		t.Fatalf("GetAuthConfig: %v %v", rec, err)
	}
	if rec.Doc != `{"enabled":true}` || rec.UpdatedBy != "admin" {
		t.Fatalf("round-trip drifted: %+v", rec)
	}

	// Integral replacement: the second Put replaces, never merges.
	if err := store.PutAuthConfig(ctx, &metadata.AuthConfigRecord{
		Section: "ldap", Doc: `{"enabled":false,"ldapUrl":"ldap://h/dc=x"}`,
		UpdatedAt: "2026-08-26T01:00:00Z", UpdatedBy: "other-admin",
	}); err != nil {
		t.Fatalf("PutAuthConfig(replace): %v", err)
	}
	rec, _ = store.GetAuthConfig(ctx, "ldap")
	if rec.Doc != `{"enabled":false,"ldapUrl":"ldap://h/dc=x"}` || rec.UpdatedBy != "other-admin" {
		t.Fatalf("replace drifted: %+v", rec)
	}

	// Sections are independent rows; List is name-ordered.
	if err := store.PutAuthConfig(ctx, &metadata.AuthConfigRecord{
		Section: "oidc", Doc: `{}`, UpdatedAt: "t", UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("PutAuthConfig(oidc): %v", err)
	}
	rows, err := store.ListAuthConfigs(ctx)
	if err != nil {
		t.Fatalf("ListAuthConfigs: %v", err)
	}
	if len(rows) != 2 || rows[0].Section != "ldap" || rows[1].Section != "oidc" {
		t.Fatalf("rows = %+v, want [ldap, oidc]", rows)
	}
}
