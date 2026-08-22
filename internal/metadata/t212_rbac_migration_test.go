// T-212 acceptance surface (migration 011, ADR-0026 decision 6): the role
// column's backfill and idempotency, the 100-user scale budget, the is_admin
// mirror on every write path, the can_manage bit's round trip, and snapshot
// fidelity — a backup artifact must carry both new columns verbatim.

package metadata_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// liveDB opens the store's database file through the raw driver (the same
// posture snapshot_test.go's openSnapshotRW uses for artifacts): the rewind
// statements need DDL access the public Store interface deliberately does not
// expose. WAL allows the second connection; the busy timeout covers contention.
func liveDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(15000)")
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// rewindToPreRBAC rolls a current-schema database back to the pre-011 shape:
// the two new columns leave together with their ledger row, so the next Open
// re-applies migration 011 exactly like a real M6-binary database upgrading
// to the M7 build.
func rewindToPreRBAC(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM schema_migrations WHERE version >= 11`,
		`ALTER TABLE users DROP COLUMN role`,
		`ALTER TABLE permission_principals DROP COLUMN can_manage`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("rewind (%q): %v", stmt, err)
		}
	}
}

// rawUsers runs one scalar query against the users table.
func rawUsers(t *testing.T, db *sql.DB, query string) map[string]string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var name, val string
		if err := rows.Scan(&name, &val); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[name] = val
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// TestRBACMigration011BackfillAndIdempotency: the upgrade path from a pre-011
// database (is_admin rows preserved), the backfill semantics, and that a
// second Open changes nothing.
func TestRBACMigration011BackfillAndIdempotency(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")

	// Build the pre-011 database: the current binary writes the M7 shape,
	// then the rewind undoes exactly the 011 delta. Two admins (the seed
	// plus one promoted row) and two plain users.
	st, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (pre shape): %v", err)
	}
	now := metadata.Now()
	for _, u := range []struct {
		name    string
		isAdmin bool
	}{
		{"boss", true}, {"worker", false}, {"onlooker", false},
	} {
		if err := st.Users().Create(ctx, &metadata.User{
			Username: u.name, PasswordHash: "x", IsAdmin: u.isAdmin, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("create %s: %v", u.name, err)
		}
	}
	db := liveDB(t, path)
	rewindToPreRBAC(t, db)
	// Post-rewind sanity: the columns are gone and the is_admin facts remain.
	if got := rawUsers(t, db, `SELECT username, CASE WHEN is_admin = 1 THEN 'admin' ELSE 'plain' END FROM users`); len(got) != 4 {
		t.Fatalf("users after rewind = %v, want 4 (seed admin, boss, worker, onlooker)", got)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The upgrade: 011 applies, the backfill maps is_admin onto role.
	st2, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (upgrade): %v", err)
	}
	defer func() { _ = st2.Close() }()
	db2 := liveDB(t, path)
	roles := rawUsers(t, db2, `SELECT username, role FROM users`)
	want := map[string]string{
		"admin": "admin", "boss": "admin", "worker": "user", "onlooker": "user",
	}
	for name, role := range want {
		if roles[name] != role {
			t.Errorf("role[%s] after backfill = %q, want %q", name, roles[name], role)
		}
	}
	// The mirror agrees with the backfill everywhere.
	mirror := rawUsers(t, db2, `SELECT username, CASE WHEN is_admin = 1 THEN 'admin' ELSE 'plain' END FROM users`)
	for name, role := range want {
		wantMirror := "plain"
		if role == "admin" {
			wantMirror = "admin"
		}
		if mirror[name] != wantMirror {
			t.Errorf("is_admin mirror[%s] = %q, want %q", name, mirror[name], wantMirror)
		}
	}
	// The manage bit landed with default 0.
	var managed int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM permission_principals WHERE can_manage != 0`).Scan(&managed); err != nil {
		t.Fatalf("can_manage probe: %v", err)
	}
	if managed != 0 {
		t.Fatalf("can_manage non-zero rows = %d, want 0 (default)", managed)
	}

	// Idempotency: a second Open re-runs nothing and changes no row.
	before := rawUsers(t, db2, `SELECT username, role FROM users`)
	st3, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (idempotency): %v", err)
	}
	defer func() { _ = st3.Close() }()
	db3 := liveDB(t, path)
	after := rawUsers(t, db3, `SELECT username, role FROM users`)
	if len(before) != len(after) {
		t.Fatalf("row count changed by reopen: %d -> %d", len(before), len(after))
	}
	for name, role := range before {
		if after[name] != role {
			t.Errorf("role[%s] changed by reopen: %q -> %q", name, role, after[name])
		}
	}
	var n int
	if err := db3.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 11`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema_migrations v11 rows = %d (%v), want exactly 1", n, err)
	}
}

// TestRBACMigration011HundredUsersUnderOneSecond: the AC budget — an existing
// database at the 100-user scale migrates in well under a second (the two
// ALTERs and one UPDATE are schema-shape work, independent of row count at
// this scale; the bound is asserted with an order of magnitude of headroom).
func TestRBACMigration011HundredUsersUnderOneSecond(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")

	st, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (pre shape): %v", err)
	}
	db := liveDB(t, path)
	rewindToPreRBAC(t, db)
	// Seed 100 users by raw SQL: no argon2, no store round trips — the
	// budget must measure migration 011, not password hashing.
	for i := 0; i < 100; i++ {
		isAdmin := 0
		if i%10 == 0 {
			isAdmin = 1
		}
		if _, err := db.Exec(`INSERT INTO users (username, password_hash, is_admin, enabled, created_at, updated_at, provider, provider_id)
			VALUES (?, 'x', ?, 1, '2026-08-23T00:00:00Z', '2026-08-23T00:00:00Z', 'local', '')`,
			"user-"+string(rune('a'+i/26))+string(rune('a'+i%26)), isAdmin); err != nil {
			t.Fatalf("seed user %d: %v", i, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	start := time.Now()
	st2, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (upgrade): %v", err)
	}
	defer func() { _ = st2.Close() }()
	if d := time.Since(start); d >= time.Second {
		t.Fatalf("migration 011 on a 100-user database took %s, want < 1s", d)
	}
	var admins, total int
	db2 := liveDB(t, path)
	if err := db2.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND role = 'admin'`).Scan(&admins); err != nil {
		t.Fatalf("admin count: %v", err)
	}
	if err := db2.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		t.Fatalf("total count: %v", err)
	}
	if admins != 10+1 || total != 101 { // 10 seeded admins + the seed admin row
		t.Fatalf("backfill at scale: admins=%d total=%d, want 11/101", admins, total)
	}
}

// TestSetRoleMirrorInvariant: SetRole is the one-statement role write — the
// is_admin mirror moves with it in every direction — and UpdateProfile (the
// boolean-shaped seam httpapi already calls) keeps the mirror while
// preserving a readonly_admin row's role on non-admin writes.
func TestSetRoleMirrorInvariant(t *testing.T) {
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{Path: filepath.Join(t.TempDir(), "binflow.db"), AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	users := st.Users()

	seed := func(name string) {
		t.Helper()
		now := metadata.Now()
		if err := users.Create(ctx, &metadata.User{
			Username: name, PasswordHash: "x", Enabled: true, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	check := func(name string, wantRole string, wantAdmin bool) {
		t.Helper()
		u, err := users.Get(ctx, name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if u.Role != wantRole || u.IsAdmin != wantAdmin {
			t.Fatalf("%s = (role %q, is_admin %v), want (%q, %v)", name, u.Role, u.IsAdmin, wantRole, wantAdmin)
		}
	}

	t.Run("create derives role from the admin flag", func(t *testing.T) {
		now := metadata.Now()
		if err := users.Create(ctx, &metadata.User{
			Username: "born-admin", PasswordHash: "x", IsAdmin: true, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		check("born-admin", "admin", true)
	})

	t.Run("SetRole moves the mirror both ways", func(t *testing.T) {
		seed("flip")
		if err := users.SetRole(ctx, "flip", metadata.RoleAdmin); err != nil {
			t.Fatalf("SetRole(admin): %v", err)
		}
		check("flip", "admin", true)
		if err := users.SetRole(ctx, "flip", metadata.RoleReadOnlyAdmin); err != nil {
			t.Fatalf("SetRole(readonly_admin): %v", err)
		}
		check("flip", "readonly_admin", false)
		if err := users.SetRole(ctx, "flip", metadata.RoleUser); err != nil {
			t.Fatalf("SetRole(user): %v", err)
		}
		check("flip", "user", false)
	})

	t.Run("SetRole unknown user", func(t *testing.T) {
		if err := users.SetRole(ctx, "ghost", metadata.RoleAdmin); err == nil {
			t.Fatal("SetRole on a missing user must fail")
		}
	})

	t.Run("UpdateProfile promotes and demotes through the mirror", func(t *testing.T) {
		seed("profile")
		if err := users.UpdateProfile(ctx, "profile", "a@b.c", true); err != nil {
			t.Fatalf("UpdateProfile(promote): %v", err)
		}
		check("profile", "admin", true)
		if err := users.UpdateProfile(ctx, "profile", "a@b.c", false); err != nil {
			t.Fatalf("UpdateProfile(demote): %v", err)
		}
		check("profile", "user", false)
	})

	t.Run("UpdateProfile keeps readonly_admin on a no-admin write", func(t *testing.T) {
		seed("auditor")
		if err := users.SetRole(ctx, "auditor", metadata.RoleReadOnlyAdmin); err != nil {
			t.Fatalf("SetRole: %v", err)
		}
		// The boolean seam cannot express readonly_admin; an email-only
		// partial update (the common console shape) must not silently
		// demote the auditor to user.
		if err := users.UpdateProfile(ctx, "auditor", "aud@b.c", false); err != nil {
			t.Fatalf("UpdateProfile(email-only): %v", err)
		}
		check("auditor", "readonly_admin", false)
	})
}

// TestPermissionCanManageRoundTrip: the manage bit survives the store's write
// and both read paths (GetTarget, PrincipalsFor).
func TestPermissionCanManageRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{Path: filepath.Join(t.TempDir(), "binflow.db"), AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	now := metadata.Now()
	if err := st.Permissions().PutTarget(ctx,
		&metadata.PermissionTarget{
			Name: "repo-admins", Repos: `["libs-release"]`, Includes: `[]`, Excludes: `[]`,
			CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{
			{TargetName: "repo-admins", Principal: "carol", PrincipalType: "user", CanManage: true},
			{TargetName: "repo-admins", Principal: "dave", PrincipalType: "user", CanRead: true, CanWrite: true},
		}); err != nil {
		t.Fatalf("PutTarget: %v", err)
	}

	_, rows, err := st.Permissions().GetTarget(ctx, "repo-admins")
	if err != nil {
		t.Fatalf("GetTarget: %v", err)
	}
	byName := map[string]*metadata.PermissionPrincipal{}
	for _, r := range rows {
		byName[r.Principal] = r
	}
	if !byName["carol"].CanManage || byName["carol"].CanRead || byName["carol"].CanWrite {
		t.Fatalf("carol = %+v, want manage-only", byName["carol"])
	}
	if byName["dave"].CanManage || !byName["dave"].CanRead {
		t.Fatalf("dave = %+v, want r/w without manage", byName["dave"])
	}

	forRepo, err := st.Permissions().PrincipalsFor(ctx, "libs-release")
	if err != nil {
		t.Fatalf("PrincipalsFor: %v", err)
	}
	managed := 0
	for _, r := range forRepo {
		if r.CanManage {
			managed++
		}
	}
	if managed != 1 {
		t.Fatalf("PrincipalsFor manage rows = %d, want 1", managed)
	}
}

// TestSnapshotPreservesRoleAndManage: export/import fidelity — the backup
// artifact carries users.role and permission_principals.can_manage verbatim
// (V-xx premise for restore: a restored readonly_admin is still one).
func TestSnapshotPreservesRoleAndManage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "binflow.db")
	st, err := metadata.Open(ctx, metadata.Options{Path: dbPath, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	now := metadata.Now()
	for _, u := range []struct {
		name, role string
	}{
		{"auditor", metadata.RoleReadOnlyAdmin}, {"boss", metadata.RoleAdmin}, {"worker", metadata.RoleUser},
	} {
		if err := st.Users().Create(ctx, &metadata.User{
			Username: u.name, PasswordHash: "x", Enabled: true,
			CreatedAt: now, UpdatedAt: now, Role: u.role,
		}); err != nil {
			t.Fatalf("create %s: %v", u.name, err)
		}
	}
	if err := st.Permissions().PutTarget(ctx,
		&metadata.PermissionTarget{
			Name: "keepers", Repos: `["libs-release"]`, Includes: `[]`, Excludes: `[]`,
			CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{{TargetName: "keepers", Principal: "carol", PrincipalType: "user", CanManage: true}}); err != nil {
		t.Fatalf("PutTarget: %v", err)
	}

	snap := filepath.Join(dir, "snapshot.db")
	vacuumSnapshotOf(t, dir, snap)
	if err := metadata.PurgeTransientFromSnapshot(ctx, snap); err != nil {
		t.Fatalf("PurgeTransientFromSnapshot: %v", err)
	}

	ro, err := sql.Open("sqlite", "file:"+snap+"?mode=ro")
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer func() { _ = ro.Close() }()
	roles := map[string]string{}
	rows, err := ro.Query(`SELECT username, role FROM users`)
	if err != nil {
		t.Fatalf("query roles: %v", err)
	}
	for rows.Next() {
		var name, role string
		if err := rows.Scan(&name, &role); err != nil {
			t.Fatalf("scan role: %v", err)
		}
		roles[name] = role
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("roles rows: %v", err)
	}
	for name, want := range map[string]string{"auditor": metadata.RoleReadOnlyAdmin, "boss": metadata.RoleAdmin, "worker": metadata.RoleUser, "admin": metadata.RoleAdmin} {
		if roles[name] != want {
			t.Errorf("snapshot role[%s] = %q, want %q", name, roles[name], want)
		}
	}
	var managed int
	if err := ro.QueryRow(`SELECT COUNT(*) FROM permission_principals WHERE can_manage = 1`).Scan(&managed); err != nil {
		t.Fatalf("query can_manage: %v", err)
	}
	if managed != 1 {
		t.Fatalf("snapshot can_manage rows = %d, want 1", managed)
	}
}
