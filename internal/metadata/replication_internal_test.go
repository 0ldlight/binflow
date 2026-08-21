package metadata

// T-161 AC ①/③ (migration side): the 009_replication.sql migration is
// idempotent, lands the contract objects of architecture section 6, and
// keeps rows written before a reopen. Mirrors the T-156 pattern
// (oidc_ldap_internal_test.go) for the replication tables.

import (
	"context"
	"path/filepath"
	"testing"
)

// wantReplicationObjects is the 009 output the upgrade path and the
// replication store both depend on (architecture section 6, 009 block).
var wantReplicationObjects = []struct{ typ, name string }{
	{"table", "replications"},
	{"table", "replication_tasks"},
	{"index", "idx_replications_source"},
	{"index", "idx_replication_tasks_status"},
	{"index", "idx_replication_tasks_pending"},
}

// TestReplicationMigrationIdempotent verifies that opening the same database
// twice does not re-apply 009 (version pinned at the latest migration, the
// contract objects appear exactly once, and rows written before the reopen
// survive).
func TestReplicationMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	// Round 1: migrate, then write one row of each 009 table through the
	// real schema (the FK chain repositories -> replications ->
	// replication_tasks must hold).
	st1, err := Open(ctx, Options{Path: path, AdminPassword: "pw-009"})
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	putRepo(t, st1, "repl-src")
	now := Now()
	db1 := st1.(*sqliteStore).db
	if _, err := db1.ExecContext(ctx,
		`INSERT INTO replications (name, source_repo, target_url, target_repo, enabled, created_at, updated_at)
		 VALUES ('dr-libs', 'repl-src', 'https://remote.example.com', 'libs-dr', 1, ?, ?)`,
		now, now); err != nil {
		t.Fatalf("seed replication row: %v", err)
	}
	if _, err := db1.ExecContext(ctx,
		`INSERT INTO replication_tasks (replication_id, blob_sha256, node_path, status, created_at)
		 VALUES (1, 'aa', 'org/acme/lib.jar', 'pending', ?)`, now); err != nil {
		t.Fatalf("seed replication task row: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}

	// Round 2: reopen — 009 must not re-run (CREATE TABLE would collide) and
	// the seeded rows must still be there.
	st2, err := Open(ctx, Options{Path: path, AdminPassword: "pw-009"})
	if err != nil {
		t.Fatalf("Open #2: %v", err)
	}
	defer func() { _ = st2.Close() }()

	v, err := CurrentVersion(ctx, st2.(*sqliteStore).db)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if want := latestMigrationVersion(); v != want {
		t.Fatalf("Open #2 left version at %d, want %d (009 must not re-run)", v, want)
	}

	db2 := st2.(*sqliteStore).db
	for _, obj := range wantReplicationObjects {
		var n int
		if err := db2.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`,
			obj.typ, obj.name).Scan(&n); err != nil {
			t.Fatalf("sqlite_master lookup %s %s: %v", obj.typ, obj.name, err)
		}
		if n != 1 {
			t.Errorf("%s %s count = %d, want 1", obj.typ, obj.name, n)
		}
	}
	var configs, tasks int
	if err := db2.QueryRowContext(ctx, `SELECT COUNT(*) FROM replications`).Scan(&configs); err != nil {
		t.Fatalf("count replications: %v", err)
	}
	if err := db2.QueryRowContext(ctx, `SELECT COUNT(*) FROM replication_tasks`).Scan(&tasks); err != nil {
		t.Fatalf("count replication_tasks: %v", err)
	}
	if configs != 1 || tasks != 1 {
		t.Errorf("rows after reopen = %d configs, %d tasks; want 1, 1 (reopen preserves data)", configs, tasks)
	}
}

// TestReplicationMigrationSchemaShape locks the 009 column layout against
// accidental drift: PRAGMA table_info must report exactly the contract
// columns, in order (the replication store's SELECT lists depend on it).
func TestReplicationMigrationSchemaShape(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "binflow.db"), AdminPassword: "pw-009"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	db := st.(*sqliteStore).db

	for _, tc := range []struct {
		table string
		cols  []string
	}{
		{"replications", []string{
			"id", "name", "source_repo", "target_url", "target_repo",
			"target_username", "target_password_enc",
			"max_bandwidth_bytes_per_sec", "max_items_per_push",
			"enabled", "created_at", "updated_at",
		}},
		{"replication_tasks", []string{
			"id", "replication_id", "blob_sha256", "node_path", "status",
			"attempts", "last_error", "created_at", "completed_at",
		}},
	} {
		t.Run(tc.table, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+tc.table+`)`)
			if err != nil {
				t.Fatalf("PRAGMA table_info(%s): %v", tc.table, err)
			}
			defer func() { _ = rows.Close() }()
			var got []string
			for rows.Next() {
				var cid int
				var name, colType string
				var notNull, pk int
				var dflt any
				if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
					t.Fatalf("scan table_info row: %v", err)
				}
				got = append(got, name)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("table_info rows: %v", err)
			}
			if len(got) != len(tc.cols) {
				t.Fatalf("%s columns = %v, want %v", tc.table, got, tc.cols)
			}
			for i := range tc.cols {
				if got[i] != tc.cols[i] {
					t.Errorf("%s column[%d] = %q, want %q", tc.table, i, got[i], tc.cols[i])
				}
			}
		})
	}
}
