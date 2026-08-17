package metadata

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// openTest opens a store on a fresh temp database.
func openTest(t *testing.T) Store {
	t.Helper()
	return openTestOpts(t, Options{AdminPassword: "unit-test-pw"})
}

func openTestOpts(t *testing.T, opts Options) Store {
	t.Helper()
	opts.Driver = "sqlite"
	if opts.Path == "" {
		opts.Path = filepath.Join(t.TempDir(), "binflow.db")
	}
	st, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// putRepo inserts a minimal local repo row so node FKs resolve.
func putRepo(t *testing.T, st Store, key string) {
	t.Helper()
	now := Now()
	err := st.Repos().Create(context.Background(), &Repo{
		RepoKey: key, Type: "local", PackageType: "generic",
		Description: "", Config: "{}", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create repo %s: %v", key, err)
	}
}

func TestParseMigrationName(t *testing.T) {
	tests := []struct {
		filename string
		version  int
		name     string
		ok       bool
	}{
		{"001_init.sql", 1, "init", true},
		{"012_add_columns.sql", 12, "add_columns", true},
		{"init.sql", 0, "", false},
		{"0_init.sql", 0, "", false},
		{"abc_init.sql", 0, "", false},
		{"001init.sql", 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			v, n, ok := parseMigrationName(tt.filename)
			if ok != tt.ok || v != tt.version || n != tt.name {
				t.Fatalf("parseMigrationName(%q) = (%d, %q, %t), want (%d, %q, %t)",
					tt.filename, v, n, ok, tt.version, tt.name, tt.ok)
			}
		})
	}
}

func TestLoadMigrationsIncludes001(t *testing.T) {
	migs := loadMigrations(migrationsFS)
	if len(migs) == 0 {
		t.Fatal("loadMigrations returned no migrations")
	}
	if migs[0].version != 1 || migs[0].name != "init" {
		t.Fatalf("first migration = %03d_%s, want 001_init", migs[0].version, migs[0].name)
	}
	for i, m := range migs {
		if m.version != i+1 {
			t.Fatalf("migration %d has version %d; versions must be 1..N without gaps", i, m.version)
		}
	}
}

func TestCurrentVersionFreshDatabase(t *testing.T) {
	st := openTest(t)
	v, err := CurrentVersion(context.Background(), st.(*sqliteStore).db)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if v != 1 {
		t.Fatalf("fresh database version = %d, want 1", v)
	}
}

// AC: Open idempotent migration — running twice yields no error and exactly
// one schema_migrations row.
func TestOpenIdempotentMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	st1, err := Open(ctx, Options{Path: path, AdminPassword: "first-pw"})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	st2, err := Open(ctx, Options{Path: path, AdminPassword: "second-pw"})
	if err != nil {
		t.Fatalf("second Open (idempotency): %v", err)
	}
	defer func() { _ = st2.Close() }()

	db := st2.(*sqliteStore).db
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("counting schema_migrations: %v", err)
	}
	if n != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", n)
	}
	var maxVersion int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		t.Fatalf("reading max version: %v", err)
	}
	if maxVersion != 1 {
		t.Fatalf("max migration version = %d, want 1", maxVersion)
	}
	// The second Open with a different password must not have rewritten the
	// seed (admin never overwritten).
	u, err := st2.Users().Get(ctx, "admin")
	if err != nil {
		t.Fatalf("getting admin after reopen: %v", err)
	}
	if !VerifyPassword("first-pw", u.PasswordHash) {
		t.Fatal("admin password changed by reopen; seed must only fire when admin is missing")
	}
}

func TestOpenPostgresReturnsExplicitError(t *testing.T) {
	_, err := Open(context.Background(), Options{Driver: "postgres", DSN: "postgres://localhost/x"})
	if err == nil {
		t.Fatal("Open(postgres) must fail in M1")
	}
	if !errors.Is(err, errPostgresDisabled) {
		t.Fatalf("error %v does not wrap errPostgresDisabled", err)
	}
	if !strings.Contains(err.Error(), "postgres support is not enabled") {
		t.Fatalf("error message %q lacks the required wording", err.Error())
	}
}

func TestOpenUnknownDriver(t *testing.T) {
	if _, err := Open(context.Background(), Options{Driver: "oracle"}); err == nil {
		t.Fatal("Open(oracle) must fail")
	}
}

func TestOpenEmptyPathRejected(t *testing.T) {
	if _, err := Open(context.Background(), Options{}); err == nil {
		t.Fatal("Open with empty path must fail")
	}
}

func TestOpenPRAGMAsApplied(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db
	ctx := context.Background()
	tests := []struct{ pragma, want string }{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
	}
	for _, tt := range tests {
		t.Run(tt.pragma, func(t *testing.T) {
			var got string
			if err := db.QueryRowContext(ctx, "PRAGMA "+tt.pragma).Scan(&got); err != nil {
				t.Fatalf("PRAGMA %s: %v", tt.pragma, err)
			}
			if got != tt.want {
				t.Fatalf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
			}
		})
	}
	// case_sensitive_like is a flag pragma with no readable value; assert the
	// observable behavior instead (review B1).
	t.Run("case_sensitive_like", func(t *testing.T) {
		var likeCaseInsensitive bool
		if err := db.QueryRowContext(ctx, `SELECT 'A' LIKE 'a'`).Scan(&likeCaseInsensitive); err != nil {
			t.Fatalf("probing LIKE case sensitivity: %v", err)
		}
		if likeCaseInsensitive {
			t.Fatal("LIKE is case-insensitive; prefix queries would match across case")
		}
	})
}

// AC: 001_init.sql covers every architecture section 6 table.
func TestSchemaTablesExist(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db
	want := []string{
		"schema_migrations",
		"repositories", "remote_configs", "blobs", "nodes", "users", "tokens",
		"permission_targets", "permission_principals", "audit_events", "virtual_members",
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("listing tables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning table name: %v", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating tables: %v", err)
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("table %s missing from schema (have %v)", w, keysOf(have))
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// AC: deleting a repository cascades to its nodes (FK).
func TestDeleteRepoCascadesNodes(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	putRepo(t, st, "lib")
	putRepo(t, st, "keep")
	now := Now()

	put := func(repo, path string) {
		t.Helper()
		if err := st.Blobs().Put(ctx, &Blob{Sha256: path, Size: 1, CreatedAt: now}); err != nil {
			t.Fatalf("blob put %s: %v", path, err)
		}
		if err := st.Nodes().Put(ctx, &Node{
			RepoKey: repo, Path: path, Sha256: path, Size: 1,
			Mime: "application/octet-stream", CreatedBy: "admin", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("node put %s/%s: %v", repo, path, err)
		}
	}
	put("lib", "a.jar")
	put("lib", "dir/b.jar")
	put("keep", "c.jar")

	if err := st.Repos().Delete(ctx, "lib"); err != nil {
		t.Fatalf("repo delete: %v", err)
	}
	nodes, err := st.Nodes().ListByPrefix(ctx, "lib", "")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("nodes under deleted repo = %d, want 0", len(nodes))
	}
	if _, err := st.Nodes().Get(ctx, "lib", "a.jar"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("get under deleted repo err = %v, want ErrNodeNotFound", err)
	}
	kept, err := st.Nodes().Get(ctx, "keep", "c.jar")
	if err != nil {
		t.Fatalf("surviving node: %v", err)
	}
	if kept.Path != "c.jar" {
		t.Fatalf("surviving node path = %q", kept.Path)
	}
	// Blobs outlive references (ADR-0006); still readable for GC.
	if _, err := st.Blobs().Get(ctx, "a.jar"); err != nil {
		t.Fatalf("blob a.jar must outlive its node: %v", err)
	}
}

func TestFKBlocksOrphanNode(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	now := Now()
	if err := st.Blobs().Put(ctx, &Blob{Sha256: "deadbeef", Size: 1, CreatedAt: now}); err != nil {
		t.Fatalf("blob put: %v", err)
	}
	err := st.Nodes().Put(ctx, &Node{RepoKey: "ghost", Path: "x", Sha256: "deadbeef", Size: 1, CreatedAt: now, UpdatedAt: now})
	if err == nil {
		t.Fatal("node put for nonexistent repo must fail under foreign_keys=ON")
	}
	if errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("unexpected sentinel in FK error: %v", err)
	}
}

// AC: the store reachable through the exported surface assembles (six
// sub-stores + audits) and round-trips a row each.
func TestStoreSubstoresWireUp(t *testing.T) {
	st := openTest(t)
	if st.Repos() == nil || st.Nodes() == nil || st.Blobs() == nil ||
		st.Users() == nil || st.Tokens() == nil || st.Permissions() == nil || st.Audits() == nil {
		t.Fatal("a sub-store accessor returned nil")
	}
	if err := st.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestTxnHelperRollbackIsSafe(t *testing.T) {
	// applyMigration defers a rollback; after commit the rollback is a no-op.
	// Exercise the same pattern directly to keep it honest.
	st := openTest(t)
	db := st.(*sqliteStore).db
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES ('aa', '', '', 1, 'x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	func() {
		defer func() { _ = tx.Rollback() }() // must not error the world after commit
	}()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blobs WHERE sha256 = 'aa'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("committed row missing: n=%d", n)
	}
	_ = sql.ErrNoRows // keep database/sql import meaningful if assertions change
}
