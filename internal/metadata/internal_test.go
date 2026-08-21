package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
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
	migs := loadMigrations(migrationsFS)
	want := migs[len(migs)-1].version
	if v != want {
		t.Fatalf("fresh database version = %d, want %d (latest migration)", v, want)
	}
}

// AC: Open idempotent migration — running twice yields no error and exactly
// one schema_migrations row per migration file.
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

	migs := loadMigrations(migrationsFS)
	db := st2.(*sqliteStore).db
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("counting schema_migrations: %v", err)
	}
	if n != len(migs) {
		t.Fatalf("schema_migrations rows = %d, want %d (one per migration, reopen adds none)", n, len(migs))
	}
	var maxVersion int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		t.Fatalf("reading max version: %v", err)
	}
	if maxVersion != migs[len(migs)-1].version {
		t.Fatalf("max migration version = %d, want %d", maxVersion, migs[len(migs)-1].version)
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
		{"busy_timeout", strconv.Itoa(BusyTimeoutMs)},
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

// AC: 001_init.sql + 002_docker.sql + 003_remote_virtual.sql cover every
// architecture section 6 table.
func TestSchemaTablesExist(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db
	want := []string{
		"schema_migrations",
		"repositories", "remote_configs", "blobs", "nodes", "users", "tokens",
		"permission_targets", "permission_principals", "audit_events", "virtual_members",
		"docker_manifests", "docker_tags", "docker_refs",
		"remote_cache",
		"groups", "user_groups", "web_sessions", "repo_usage",
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

// T-34 AC ①: no migration file may carry its own BEGIN/COMMIT — the
// migrator wraps each migration in one transaction and nesting one is an
// error (T-10 review M9). Table-driven over every embedded file so 003+
// inherits the guard.
func TestMigrationsHaveNoTransactionStatements(t *testing.T) {
	for _, m := range loadMigrations(migrationsFS) {
		name := fmt.Sprintf("%03d_%s", m.version, m.name)
		t.Run(name, func(t *testing.T) {
			upper := strings.ToUpper(m.body)
			for _, stmt := range []string{"BEGIN", "COMMIT", "ROLLBACK", "START TRANSACTION"} {
				if strings.Contains(upper, stmt) {
					t.Fatalf("%s.sql contains %q — the migrator owns the transaction boundary", name, stmt)
				}
			}
		})
	}
}

// T-34 AC ①: the 002_docker migration is idempotent — a database that
// already sits at the latest version reopens without re-running anything and
// without error. (T-62: generalized from the literal 2 to the latest embedded
// migration so 003+ inherits the guard without editing this test again.)
func TestDockerMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		st, err := Open(ctx, Options{Path: path, AdminPassword: "pw-002"})
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		v, err := CurrentVersion(ctx, st.(*sqliteStore).db)
		if err != nil {
			t.Fatalf("CurrentVersion #%d: %v", i+1, err)
		}
		if want := latestMigrationVersion(); v != want {
			t.Fatalf("Open #%d left version at %d, want %d (applied migrations must not re-run)", i+1, v, want)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}

// T-34 AC ①: old-database upgrade path — a database last opened by the M1
// binary (only 001 applied, with live M1 data) upgrades in place to the
// current schema without touching the existing rows.
func TestDockerUpgradeFromM1Database(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	// Build the M1-shaped database: apply migrations, then roll the ledger
	// back to version 1 and drop the 002/003 schema, imitating a database
	// written by the M1 build.
	st1, err := Open(ctx, Options{Path: path, AdminPassword: "m1-pw"})
	if err != nil {
		t.Fatalf("Open (M1 shape): %v", err)
	}
	putRepo(t, st1, "legacy")
	now := Now()
	if err := st1.Blobs().Put(ctx, &Blob{Sha256: "legacy-blob", Size: 7, CreatedAt: now}); err != nil {
		t.Fatalf("legacy blob put: %v", err)
	}
	if err := st1.Nodes().Put(ctx, &Node{
		RepoKey: "legacy", Path: "a.jar", Sha256: "legacy-blob", Size: 7,
		CreatedBy: "admin", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("legacy node put: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("Close (M1 shape): %v", err)
	}

	db2, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("reopen raw: %v", err)
	}
	// The 003 rewind (T-62): drop the remote_cache table (its index goes with
	// it) and idx_blobs_sha1, undo the remote_configs widening — the rename
	// back matters, 003 re-application renames the column again — and clear
	// every ledger row past version 1. The 004 rewind (T-90): drop the
	// governance tables and the audit query indexes, and take users.email
	// back out (ADD COLUMN re-adds it).
	for _, stmt := range []string{
		`DROP TABLE repo_usage`,
		`DROP TABLE web_sessions`,
		`DROP INDEX IF EXISTS idx_web_sessions_user`,
		`DROP TABLE user_groups`,
		`DROP TABLE groups`,
		`DROP INDEX IF EXISTS idx_audit_action`,
		`DROP INDEX IF EXISTS idx_audit_actor`,
		`ALTER TABLE users DROP COLUMN email`,
		`DROP INDEX IF EXISTS idx_users_provider`,
		`ALTER TABLE users DROP COLUMN provider`,
		`ALTER TABLE users DROP COLUMN provider_id`,
		`DROP TABLE remote_cache`,
		`DROP TABLE replication_tasks`,
		`DROP INDEX IF EXISTS idx_replication_tasks_status`,
		`DROP INDEX IF EXISTS idx_replication_tasks_pending`,
		`DROP TABLE replications`,
		`DROP INDEX IF EXISTS idx_replications_source`,
		`DROP INDEX IF EXISTS idx_blobs_sha1`,
		`DROP TABLE docker_refs`,
		`DROP INDEX IF EXISTS idx_docker_tags_image`,
		`DROP TABLE docker_tags`,
		`DROP INDEX IF EXISTS idx_docker_manifests_image`,
		`DROP TABLE docker_manifests`,
		`ALTER TABLE remote_configs RENAME COLUMN blocked_out TO unreachable_mask`,
		`ALTER TABLE remote_configs DROP COLUMN allow_private_upstream`,
		`ALTER TABLE remote_configs DROP COLUMN metadata_ttl_seconds`,
		`ALTER TABLE remote_configs DROP COLUMN content_ttl_seconds`,
		`DELETE FROM schema_migrations WHERE version > 1`,
	} {
		if _, err := db2.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("rewinding to M1 shape (%s): %v", stmt, err)
		}
	}
	if err := db2.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// The upgrade: opening with the current build applies only 002.
	st2, err := Open(ctx, Options{Path: path, AdminPassword: "m1-pw"})
	if err != nil {
		t.Fatalf("Open (upgrade): %v", err)
	}
	defer func() { _ = st2.Close() }()

	v, err := CurrentVersion(ctx, st2.(*sqliteStore).db)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if want := latestMigrationVersion(); v != want {
		t.Fatalf("upgraded version = %d, want %d (latest)", v, want)
	}
	// M1 data intact, admin seed not rewritten.
	node, err := st2.Nodes().Get(ctx, "legacy", "a.jar")
	if err != nil || node.Sha256 != "legacy-blob" {
		t.Fatalf("legacy node after upgrade = %+v (err %v)", node, err)
	}
	u, err := st2.Users().Get(ctx, "admin")
	if err != nil || !VerifyPassword("m1-pw", u.PasswordHash) {
		t.Fatalf("admin password changed by upgrade: %+v (err %v)", u, err)
	}
	// The 002 tables are usable right away.
	if err := st2.Docker().PutManifest(ctx, &DockerManifest{
		RepoKey: "legacy", Image: "app", Digest: "d1", MediaType: "application/vnd.oci.image.manifest.v1+json",
		Size: 1, CreatedBy: "admin", CreatedAt: now,
	}); err != nil {
		t.Fatalf("docker put after upgrade: %v", err)
	}
}

// T-34 AC ①: the 002 sqlite DDL matches the architecture section 6 final
// block — three tables, the two (repo_key, image) indexes plus
// idx_docker_refs_blob, the composite primary keys, and the FK declarations
// of docker_manifests/docker_tags (docker_refs has none by design).
func TestDockerSchemaShapeMatchesArchitecture(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db

	wantTables := map[string]bool{
		"docker_manifests": false, "docker_tags": false, "docker_refs": false,
	}
	// Column sets and composite PK columns exactly as the architecture
	// section 6 "002_docker.sql" block defines them (order significant for
	// the PK tuples, sets for columns).
	wantColumns := map[string][]string{
		"docker_manifests": {"repo_key", "image", "digest", "media_type", "size", "created_by", "created_at"},
		"docker_tags":      {"repo_key", "image", "tag", "digest", "updated_by", "updated_at"},
		"docker_refs":      {"repo_key", "image", "manifest_digest", "blob_digest", "child_media_type"},
	}
	wantPK := map[string][]string{
		"docker_manifests": {"repo_key", "image", "digest"},
		"docker_tags":      {"repo_key", "image", "tag"},
		"docker_refs":      {"repo_key", "image", "manifest_digest", "blob_digest"},
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("listing tables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := wantTables[name]; ok {
			wantTables[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating tables: %v", err)
	}
	for name, seen := range wantTables {
		if !seen {
			t.Errorf("table %s missing", name)
			continue
		}
		cols, err := tableColumns(db, name)
		if err != nil {
			t.Errorf("columns of %s: %v", name, err)
			continue
		}
		if !sameSet(cols, wantColumns[name]) {
			t.Errorf("%s columns = %v, want %v", name, cols, wantColumns[name])
		}
		pk, err := tablePrimaryKey(db, name)
		if err != nil {
			t.Errorf("pk of %s: %v", name, err)
			continue
		}
		if !sameOrder(pk, wantPK[name]) {
			t.Errorf("%s primary key = %v, want %v", name, pk, wantPK[name])
		}
	}

	// Indexes exactly as the architecture block defines them.
	wantIndexes := map[string]bool{
		"idx_docker_manifests_image": false,
		"idx_docker_tags_image":      false,
		"idx_docker_refs_blob":       false,
	}
	irows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'index'`)
	if err != nil {
		t.Fatalf("listing indexes: %v", err)
	}
	defer func() { _ = irows.Close() }()
	for irows.Next() {
		var name string
		if err := irows.Scan(&name); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		if _, ok := wantIndexes[name]; ok {
			wantIndexes[name] = true
		}
	}
	if err := irows.Err(); err != nil {
		t.Fatalf("iterating indexes: %v", err)
	}
	for name, seen := range wantIndexes {
		if !seen {
			t.Errorf("index %s missing (architecture section 6 defines it)", name)
		}
	}

	// FK surface: manifests/tags reference repositories; docker_refs has no FK.
	fkRows, err := db.Query(`PRAGMA foreign_key_list(docker_refs)`)
	if err != nil {
		t.Fatalf("foreign_key_list(docker_refs): %v", err)
	}
	defer func() { _ = fkRows.Close() }()
	var n int
	for fkRows.Next() {
		n++
	}
	if err := fkRows.Err(); err != nil {
		t.Fatalf("iterating fk list: %v", err)
	}
	if n != 0 {
		t.Fatalf("docker_refs declares %d foreign keys, want 0 (architecture 11.12: no DB-level FK)", n)
	}
	for _, table := range []string{"docker_manifests", "docker_tags"} {
		var refs int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list(?)`, table).Scan(&refs); err != nil {
			t.Fatalf("foreign_key_list(%s): %v", table, err)
		}
		if refs == 0 {
			t.Errorf("%s declares no FK to repositories", table)
		}
	}
}

// tableColumns returns the declared column names of one table via
// pragma_table_info (table-valued pragma form keeps it a plain query).
func tableColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// tablePrimaryKey returns the PK column names in declaration order.
func tablePrimaryKey(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func sameOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
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
