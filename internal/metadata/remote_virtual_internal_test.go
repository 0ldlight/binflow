package metadata

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// T-62 AC ①: the 003 sqlite DDL matches the architecture section 6 final
// block — the remote_cache table (columns, PK, idx_remote_cache_expiry), the
// remote_configs widening (new columns with the defined defaults,
// unreachable_mask renamed away, the 001 columns still present) and the
// idx_blobs_sha1 seam.
func TestRemoteVirtualSchemaShapeMatchesArchitecture(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db

	// remote_cache columns and PK exactly as the architecture block defines.
	wantColumns := []string{
		"repo_key", "path", "etag", "last_modified", "fetched_at", "expires_at", "kind",
	}
	cols, err := tableColumns(db, "remote_cache")
	if err != nil {
		t.Fatalf("columns of remote_cache: %v", err)
	}
	if !sameSet(cols, wantColumns) {
		t.Fatalf("remote_cache columns = %v, want %v", cols, wantColumns)
	}
	pk, err := tablePrimaryKey(db, "remote_cache")
	if err != nil {
		t.Fatalf("pk of remote_cache: %v", err)
	}
	if !sameOrder(pk, []string{"repo_key", "path"}) {
		t.Fatalf("remote_cache primary key = %v, want [repo_key path]", pk)
	}
	// Column defaults exactly as the DDL defines them (pragma dflt_value
	// renders literals verbatim).
	wantDefaults := map[string]string{
		"etag":          "''",
		"last_modified": "''",
		"kind":          "'content'",
	}
	for col, want := range wantDefaults {
		got, err := columnDefault(db, "remote_cache", col)
		if err != nil {
			t.Fatalf("default of remote_cache.%s: %v", col, err)
		}
		if got != want {
			t.Errorf("remote_cache.%s default = %q, want %q", col, got, want)
		}
	}

	// remote_configs: widened columns present with the ADR-0012 defaults,
	// the renamed column in place, the legacy name gone, the 001 columns
	// untouched.
	wantConfigColumns := map[string]string{
		"cache_ttl_seconds":      "0",
		"content_ttl_seconds":    "86400",
		"metadata_ttl_seconds":   "600",
		"allow_private_upstream": "0",
		"blocked_out":            "0",
	}
	cols, err = tableColumns(db, "remote_configs")
	if err != nil {
		t.Fatalf("columns of remote_configs: %v", err)
	}
	have := map[string]bool{}
	for _, c := range cols {
		have[c] = true
	}
	for _, legacy := range []string{"repo_key", "url", "username", "password"} {
		if !have[legacy] {
			t.Errorf("remote_configs.%s lost by 003 (001 column must stay)", legacy)
		}
	}
	for col := range wantConfigColumns {
		if !have[col] {
			t.Errorf("remote_configs.%s missing after 003 (have %v)", col, cols)
		}
	}
	if have["unreachable_mask"] {
		t.Error("remote_configs.unreachable_mask still present; 003 must rename it to blocked_out")
	}
	for col, want := range wantConfigColumns {
		got, err := columnDefault(db, "remote_configs", col)
		if err != nil {
			t.Fatalf("default of remote_configs.%s: %v", col, err)
		}
		if got != want {
			t.Errorf("remote_configs.%s default = %q, want %q", col, got, want)
		}
	}

	// Indexes exactly as the architecture block (plus the T-73 seam) defines.
	wantIndexes := map[string]bool{
		"idx_remote_cache_expiry": false,
		"idx_blobs_sha1":          false,
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

	// remote_cache references repositories with ON DELETE CASCADE (the
	// repo-teardown semantics DeleteCacheByRepo backs up).
	var onDelete sql.NullString
	err = db.QueryRow(`SELECT on_delete FROM pragma_foreign_key_list('remote_cache')
		WHERE "table" = 'repositories'`).Scan(&onDelete)
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatal("remote_cache declares no FK to repositories")
	}
	if err != nil {
		t.Fatalf("foreign_key_list(remote_cache): %v", err)
	}
	if onDelete.String != "CASCADE" {
		t.Fatalf("remote_cache FK on_delete = %q, want CASCADE", onDelete.String)
	}
}

// columnDefault returns the declared default literal of one column, or
// sql.ErrNoRows when the column has none.
func columnDefault(db *sql.DB, table, column string) (string, error) {
	var dflt sql.NullString
	err := db.QueryRow(`SELECT dflt_value FROM pragma_table_info(?) WHERE name = ?`,
		table, column).Scan(&dflt)
	if err != nil {
		return "", err
	}
	if !dflt.Valid {
		return "", sql.ErrNoRows
	}
	return dflt.String, nil
}

// latestMigrationVersion reports the newest embedded migration version
// (versions are gapless 1..N, so the last loaded entry is the target every
// Open advances a database to).
func latestMigrationVersion() int {
	migs := loadMigrations(migrationsFS)
	return migs[len(migs)-1].version
}

// T-62 AC ①: the 003 migration is idempotent — a database that already sits
// at the latest version reopens without re-running it and without error.
func TestRemoteVirtualMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		st, err := Open(ctx, Options{Path: path, AdminPassword: "pw-003"})
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		v, err := CurrentVersion(ctx, st.(*sqliteStore).db)
		if err != nil {
			t.Fatalf("CurrentVersion #%d: %v", i+1, err)
		}
		if want := latestMigrationVersion(); v != want {
			t.Fatalf("Open #%d left version at %d, want %d (003 must not re-run)", i+1, v, want)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}

// buildLegacyDatabase emulates a database last written by an older build: it
// applies the first n migration bodies by hand and writes the matching
// schema_migrations ledger rows, so the current build's Open only has the
// remaining migrations to apply. seed runs inside the same raw connection
// for legacy data rows (the older build's own writes).
func buildLegacyDatabase(t *testing.T, path string, n int, seed func(*sql.DB)) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("creating ledger: %v", err)
	}
	migs := loadMigrations(migrationsFS)
	for i := 0; i < n; i++ {
		if _, err := db.ExecContext(ctx, migs[i].body); err != nil {
			t.Fatalf("applying %03d_%s: %v", migs[i].version, migs[i].name, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			migs[i].version, Now()); err != nil {
			t.Fatalf("recording %03d: %v", migs[i].version, err)
		}
	}
	if seed != nil {
		seed(db)
	}
}

// legacyAdminSeed inserts the admin row an older build would have seeded.
func legacyAdminSeed(t *testing.T, db *sql.DB, password string) {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hashing legacy admin password: %v", err)
	}
	now := Now()
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, is_admin, enabled, created_at, updated_at)
		VALUES ('admin', ?, 1, 1, ?, ?)`, hash, now, now); err != nil {
		t.Fatalf("seeding legacy admin: %v", err)
	}
}

// T-62 AC ①: upgrade path from an M1 database (only 001 applied, live data):
// opening with the current build applies 002+003 in place, the M1 data stays
// intact and the 003 surface is usable right away.
func TestRemoteVirtualUpgradeFromM1Database(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	buildLegacyDatabase(t, path, 1, func(db *sql.DB) {
		legacyAdminSeed(t, db, "m1-pw")
		now := Now()
		for _, stmt := range []string{
			`INSERT INTO repositories (repo_key, type, package_type, description, config, created_at, updated_at)
				VALUES ('legacy', 'local', 'generic', '', '{}', '` + now + `', '` + now + `')`,
			`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES ('legacy-blob', '', '', 7, '` + now + `')`,
			`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
				VALUES ('legacy', 'a.jar', 'legacy-blob', 7, 'application/java-archive', 'admin', '` + now + `', '` + now + `')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("seeding legacy data (%q): %v", stmt, err)
			}
		}
	})

	st, err := Open(ctx, Options{Path: path, AdminPassword: "ignored-by-upgrade"})
	if err != nil {
		t.Fatalf("Open (upgrade from M1): %v", err)
	}
	defer func() { _ = st.Close() }()

	v, err := CurrentVersion(ctx, st.(*sqliteStore).db)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if v < 3 || v != latestMigrationVersion() {
		t.Fatalf("upgraded version = %d, want %d (003 applied)", v, latestMigrationVersion())
	}
	node, err := st.Nodes().Get(ctx, "legacy", "a.jar")
	if err != nil || node.Sha256 != "legacy-blob" {
		t.Fatalf("legacy node after upgrade = %+v (err %v)", node, err)
	}
	u, err := st.Users().Get(ctx, "admin")
	if err != nil || !VerifyPassword("m1-pw", u.PasswordHash) {
		t.Fatalf("admin password changed by upgrade: %+v (err %v)", u, err)
	}
	// The widened remote_configs is usable: a config row created after the
	// upgrade carries the new columns.
	now := Now()
	if err := st.Repos().Create(ctx, &Repo{
		RepoKey: "legacy-remote", Type: "remote", PackageType: "maven",
		Description: "", Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create remote repo after upgrade: %v", err)
	}
	if err := st.Remote().CreateConfig(ctx, &RemoteConfig{
		RepoKey: "legacy-remote", URL: "https://repo.example.test/maven",
		ContentTTLSeconds: 7200, MetadataTTLSeconds: 300,
	}); err != nil {
		t.Fatalf("remote config put after upgrade: %v", err)
	}
	cfg, err := st.Remote().GetConfig(ctx, "legacy-remote")
	if err != nil {
		t.Fatalf("remote config get after upgrade: %v", err)
	}
	if cfg.ContentTTLSeconds != 7200 || cfg.MetadataTTLSeconds != 300 || cfg.BlockedOut {
		t.Fatalf("remote config roundtrip after upgrade = %+v", cfg)
	}
}

// T-62 AC ①: upgrade path from an M2 database (001+002 applied, live docker
// data): opening with the current build applies only 003 and leaves the M2
// surface untouched.
func TestRemoteVirtualUpgradeFromM2Database(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	buildLegacyDatabase(t, path, 2, func(db *sql.DB) {
		legacyAdminSeed(t, db, "m2-pw")
		now := Now()
		for _, stmt := range []string{
			`INSERT INTO repositories (repo_key, type, package_type, description, config, created_at, updated_at)
				VALUES ('docker-legacy', 'local', 'docker', '', '{}', '` + now + `', '` + now + `')`,
			`INSERT INTO docker_manifests (repo_key, image, digest, media_type, size, created_by, created_at)
				VALUES ('docker-legacy', 'app', 'd1', 'application/vnd.docker.distribution.manifest.v2+json', 42, 'ci', '` + now + `')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("seeding legacy data (%q): %v", stmt, err)
			}
		}
	})

	st, err := Open(ctx, Options{Path: path, AdminPassword: "m2-pw"})
	if err != nil {
		t.Fatalf("Open (upgrade from M2): %v", err)
	}
	defer func() { _ = st.Close() }()

	v, err := CurrentVersion(ctx, st.(*sqliteStore).db)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if v < 3 || v != latestMigrationVersion() {
		t.Fatalf("upgraded version = %d, want %d (only 003 was pending for an M2 database)", v, latestMigrationVersion())
	}
	if _, err := st.Docker().GetManifest(ctx, "docker-legacy", "app", "d1"); err != nil {
		t.Fatalf("M2 manifest lost by 003 upgrade: %v", err)
	}
	u, err := st.Users().Get(ctx, "admin")
	if err != nil || !VerifyPassword("m2-pw", u.PasswordHash) {
		t.Fatalf("admin password changed by upgrade: %+v (err %v)", u, err)
	}
	// The 003 surface works on the upgraded database.
	if err := st.Remote().PutCache(ctx, &RemoteCacheEntry{
		RepoKey: "docker-legacy", Path: "app/manifests/latest",
		ETag: `"abc"`, LastModified: Now(), FetchedAt: Now(),
		ExpiresAt: "2027-01-01T00:00:00Z", Kind: RemoteCacheKindContent,
	}); err != nil {
		t.Fatalf("remote cache put after upgrade: %v", err)
	}
}
