package metadata

import (
	"context"
	"path/filepath"
	"testing"
)

// T-95: the 005_quota_usage_backfill migration. Internal on purpose: forcing
// the upgrade shape (nodes present, 005 not yet applied) rewinds the
// migration ledger, which needs the raw database handle the public Store
// interface does not expose — the same posture as the 004 migration tests
// (console_governance_internal_test.go).
func TestUsageBackfillMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")

	st, err := Open(ctx, Options{Driver: "sqlite", Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, r := range []struct {
		key   string
		sizes []int64
	}{
		{"legacy-a", []int64{100, 200}},
		{"legacy-b", []int64{50}},
	} {
		if err := st.Repos().Create(ctx, &Repo{
			RepoKey: r.key, Type: "local", PackageType: "generic",
			Config: "{}", CreatedAt: "2026-08-19T00:00:00Z", UpdatedAt: "2026-08-19T00:00:00Z",
		}); err != nil {
			t.Fatalf("seed repo %s: %v", r.key, err)
		}
		for i, size := range r.sizes {
			sha := r.key + string(rune('a'+i))
			if err := st.Blobs().Put(ctx, &Blob{Sha256: sha, Size: size, CreatedAt: "2026-08-19T00:00:00Z"}); err != nil {
				t.Fatalf("seed blob %s: %v", sha, err)
			}
			if err := st.Nodes().Put(ctx, &Node{
				RepoKey: r.key, Path: string(rune('a'+i)) + ".bin", Sha256: sha,
				Size: size, CreatedAt: "2026-08-19T00:00:00Z", UpdatedAt: "2026-08-19T00:00:00Z",
			}); err != nil {
				t.Fatalf("seed node %s/%d: %v", r.key, i, err)
			}
		}
	}
	// A repository with zero nodes: its honest total is zero either way.
	if err := st.Repos().Create(ctx, &Repo{
		RepoKey: "legacy-empty", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: "2026-08-19T00:00:00Z", UpdatedAt: "2026-08-19T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed empty repo: %v", err)
	}

	// Rewind to the M1~M3 upgrade shape: nodes exist, 005 has not happened.
	// A real upgrade differs only in WHICH binary wrote the rows; the
	// migrator's re-run path is identical. Since 006 shipped, the rewind
	// must also clear its ledger row and its index: the migrator applies
	// every version above the ledger's MAX, so leaving 006 recorded (MAX=6)
	// would suppress the 005 re-run entirely — and re-applying 006 over a
	// surviving index would fail its CREATE. The version ceiling tracks
	// latestMigrationVersion() everywhere else; this rewind is the one
	// place that must name the pre-005 cut explicitly.
	db := st.(*sqliteStore).db
	for _, stmt := range []string{
		`DELETE FROM schema_migrations WHERE version >= 5`,
		`DROP INDEX IF EXISTS idx_user_groups_username`,
		`DELETE FROM repo_usage`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("rewind (%q): %v", stmt, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	// Reopen: 005 applies over the rewound ledger and backfills.
	st2, err := Open(ctx, Options{Driver: "sqlite", Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("reopen (005 applies): %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	for key, want := range map[string]int64{"legacy-a": 300, "legacy-b": 50, "legacy-empty": 0} {
		u, err := st2.Usage().Get(ctx, key)
		if err != nil {
			t.Fatalf("usage %s after backfill: %v", key, err)
		}
		if u.LogicalBytes != want {
			t.Errorf("usage %s after backfill = %d, want %d", key, u.LogicalBytes, want)
		}
	}
	v, err := CurrentVersion(ctx, st2.(*sqliteStore).db)
	if err != nil || v != latestMigrationVersion() {
		t.Errorf("version after reopen = %d (%v), want %d", v, err, latestMigrationVersion())
	}

	// The backfilled counter keeps working with metered writes on top of
	// it: a delta applies from the seeded total.
	now := "2026-08-19T02:00:00Z"
	if err := st2.Blobs().Put(ctx, &Blob{Sha256: "ff", Size: 25, CreatedAt: now}); err != nil {
		t.Fatalf("seed blob ff: %v", err)
	}
	if err := st2.Usage().PutNodeWithUsage(ctx, &Node{
		RepoKey: "legacy-b", Path: "new.bin", Sha256: "ff", Size: 25,
		CreatedAt: now, UpdatedAt: now,
	}, now); err != nil {
		t.Fatalf("metered write over backfill: %v", err)
	}
	if u, _ := st2.Usage().Get(ctx, "legacy-b"); u.LogicalBytes != 75 {
		t.Errorf("legacy-b after metered write = %d, want 75", u.LogicalBytes)
	}

	// Third open: idempotent, nothing doubles.
	st3, err := Open(ctx, Options{Driver: "sqlite", Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("reopen (idempotency): %v", err)
	}
	defer func() { _ = st3.Close() }() //nolint:staticcheck // test-local triple open
	if u, _ := st3.Usage().Get(ctx, "legacy-a"); u.LogicalBytes != 300 {
		t.Errorf("legacy-a after third open = %d, want 300 (005 must not re-run)", u.LogicalBytes)
	}
}
