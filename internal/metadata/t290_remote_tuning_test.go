package metadata_test

// T-290 (FR-90.2, migration 014): the smart remote effective-field widening
// of remote_configs exists in BOTH dialect files (ADR-0007 lockstep), the
// migrator lands version 014, and the RemoteStore round-trips the three new
// columns (zero meaning unset, so pre-014 rows keep their legacy behavior).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestT290Migration014BothDialects: the lockstep rule — sqlite (embedded)
// and postgres files both carry the three ALTERs idempotently spelled the
// same (dialect-common statements).
func TestT290Migration014BothDialects(t *testing.T) {
	for _, path := range []string{
		"migrations/sqlite/014_remote_tuning.sql",
		"migrations/postgres/014_remote_tuning.sql",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("dialect file %s: %v", path, err)
		}
		for _, col := range []string{
			"ADD COLUMN socket_timeout_ms",
			"ADD COLUMN metadata_retrieval_timeout_secs",
			"ADD COLUMN unused_cleanup_period_hours",
		} {
			if !strings.Contains(string(b), col) {
				t.Fatalf("%s does not carry %q", path, col)
			}
		}
	}
}

// TestT290MigrationAppliedAndColumnsRoundTrip: a fresh open lands 014 and
// CreateConfig/GetConfig/UpdateConfig round-trip the tuning columns.
func TestT290MigrationAppliedAndColumnsRoundTrip(t *testing.T) {
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
	if v < 14 {
		t.Fatalf("schema version = %d, want >= 14 (014_remote_tuning applied)", v)
	}

	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "t290-remote", Type: "remote", PackageType: "generic",
		Config: `{}`, CreatedAt: "2026-08-26T00:00:00Z", UpdatedAt: "2026-08-26T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo row: %v", err)
	}

	// Create with the tuning values set.
	if err := md.Remote().CreateConfig(ctx, &metadata.RemoteConfig{
		RepoKey: "t290-remote", URL: "http://up",
		SocketTimeoutMs: 2500, MetadataRetrievalTimeoutSecs: 30, UnusedCleanupPeriodHours: 72,
	}); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	got, err := md.Remote().GetConfig(ctx, "t290-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got.SocketTimeoutMs != 2500 || got.MetadataRetrievalTimeoutSecs != 30 || got.UnusedCleanupPeriodHours != 72 {
		t.Fatalf("tuning columns = %d/%d/%d, want 2500/30/72",
			got.SocketTimeoutMs, got.MetadataRetrievalTimeoutSecs, got.UnusedCleanupPeriodHours)
	}

	// Update replaces them (full-replace semantics).
	if err := md.Remote().UpdateConfig(ctx, &metadata.RemoteConfig{
		RepoKey: "t290-remote", URL: "http://up2",
		SocketTimeoutMs: 0, MetadataRetrievalTimeoutSecs: 0, UnusedCleanupPeriodHours: 0,
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	got, err = md.Remote().GetConfig(ctx, "t290-remote")
	if err != nil {
		t.Fatalf("GetConfig after update: %v", err)
	}
	if got.SocketTimeoutMs != 0 || got.MetadataRetrievalTimeoutSecs != 0 || got.UnusedCleanupPeriodHours != 0 {
		t.Fatalf("tuning columns after update = %d/%d/%d, want 0/0/0 (unset)",
			got.SocketTimeoutMs, got.MetadataRetrievalTimeoutSecs, got.UnusedCleanupPeriodHours)
	}
}
