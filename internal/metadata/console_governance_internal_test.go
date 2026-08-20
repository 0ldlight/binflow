package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// T-90 AC ①: the 004 sqlite DDL matches the architecture section 6 final
// block — four tables, the users.email widening, the two audit query indexes
// and the web_sessions user index, the composite primary keys and the FK
// declarations.
func TestConsoleGovernanceSchemaShapeMatchesArchitecture(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db

	wantColumns := map[string][]string{
		"groups":       {"id", "name", "description", "created_at", "updated_at"},
		"user_groups":  {"group_id", "username"},
		"web_sessions": {"id_hash", "username", "created_at", "expires_at", "last_used_at", "revoked_at"},
		"repo_usage":   {"repo_key", "logical_bytes", "updated_at"},
	}
	wantPK := map[string][]string{
		"groups":       {"id"},
		"user_groups":  {"group_id", "username"},
		"web_sessions": {"id_hash"},
		"repo_usage":   {"repo_key"},
	}
	for table := range wantColumns {
		cols, err := tableColumns(db, table)
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		if !sameSet(cols, wantColumns[table]) {
			t.Errorf("%s columns = %v, want %v", table, cols, wantColumns[table])
		}
		pk, err := tablePrimaryKey(db, table)
		if err != nil {
			t.Errorf("pk of %s: %v", table, err)
			continue
		}
		if !sameOrder(pk, wantPK[table]) {
			t.Errorf("%s primary key = %v, want %v", table, pk, wantPK[table])
		}
	}

	// users.email widening: column present, NOT NULL, default '' (legacy
	// rows upgrade to empty, not NULL).
	emailDefault, err := columnDefault(db, "users", "email")
	if err != nil {
		t.Fatalf("users.email default: %v (column missing?)", err)
	}
	if emailDefault != "''" {
		t.Fatalf("users.email default = %q, want \"''\"", emailDefault)
	}

	// groups.name uniqueness (the 409/400 semantics of SE-01 ride on it).
	var nameUnique int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('groups') WHERE "unique" = 1 AND origin = 'u'`).Scan(&nameUnique); err != nil {
		t.Fatalf("groups unique indexes: %v", err)
	}
	if nameUnique == 0 {
		t.Error("groups.name has no UNIQUE constraint")
	}

	// Indexes exactly as the architecture block defines them.
	wantIndexes := []string{
		"idx_audit_actor", "idx_audit_action", "idx_web_sessions_user",
	}
	have := map[string]bool{}
	irows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'index'`)
	if err != nil {
		t.Fatalf("listing indexes: %v", err)
	}
	for irows.Next() {
		var name string
		if err := irows.Scan(&name); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		have[name] = true
	}
	if err := irows.Err(); err != nil {
		t.Fatalf("iterating indexes: %v", err)
	}
	_ = irows.Close()
	for _, w := range wantIndexes {
		if !have[w] {
			t.Errorf("index %s missing (architecture section 6 004 block defines it)", w)
		}
	}

	// FK surface: user_groups -> groups/users, web_sessions -> users,
	// repo_usage -> repositories, all ON DELETE CASCADE.
	fkWant := map[string]int{
		"user_groups": 2, "web_sessions": 1, "repo_usage": 1,
	}
	for table, want := range fkWant {
		var refs int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_list(?)`, table).Scan(&refs); err != nil {
			t.Fatalf("foreign_key_list(%s): %v", table, err)
		}
		if refs != want {
			t.Errorf("%s declares %d foreign keys, want %d", table, refs, want)
		}
	}
}

// T-90 AC ①: the 004 migration is idempotent — a database already at the
// latest version reopens without re-running anything and without error.
func TestConsoleGovernanceMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		st, err := Open(ctx, Options{Path: path, AdminPassword: "pw-004"})
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		v, err := CurrentVersion(ctx, st.(*sqliteStore).db)
		if err != nil {
			t.Fatalf("CurrentVersion #%d: %v", i+1, err)
		}
		if want := latestMigrationVersion(); v != want {
			t.Fatalf("Open #%d left version at %d, want %d (004 must not re-run)", i+1, v, want)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}

// T-90 AC ①: upgrade paths from every historical build (M1, M2, M3 —
// 001/002/003 applied with live data): opening with the current build
// applies the pending migrations in place, keeps the legacy rows and the
// admin seed, backfills users.email with ” and leaves the 004 surface
// usable right away.
func TestConsoleGovernanceUpgradeFromHistoricalShapes(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		migs     int // migrations the "older build" had applied
		password string
		seed     func(t *testing.T, db *sql.DB)
	}{
		{
			name:     "M1 database (001 only)",
			migs:     1,
			password: "m1-pw",
			seed: func(t *testing.T, db *sql.DB) {
				now := Now()
				for _, stmt := range []string{
					`INSERT INTO repositories (repo_key, type, package_type, description, config, created_at, updated_at)
						VALUES ('legacy', 'local', 'generic', '', '{}', '` + now + `', '` + now + `')`,
					`INSERT INTO blobs (sha256, sha1, md5, size, created_at) VALUES ('legacy-blob', '', '', 7, '` + now + `')`,
					`INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
						VALUES ('legacy', 'a.jar', 'legacy-blob', 7, 'application/java-archive', 'admin', '` + now + `', '` + now + `')`,
				} {
					if _, err := db.Exec(stmt); err != nil {
						t.Fatalf("seeding M1 data (%q): %v", stmt, err)
					}
				}
			},
		},
		{
			name:     "M2 database (001+002, docker data)",
			migs:     2,
			password: "m2-pw",
			seed: func(t *testing.T, db *sql.DB) {
				now := Now()
				for _, stmt := range []string{
					`INSERT INTO repositories (repo_key, type, package_type, description, config, created_at, updated_at)
						VALUES ('docker-legacy', 'local', 'docker', '', '{}', '` + now + `', '` + now + `')`,
					`INSERT INTO docker_manifests (repo_key, image, digest, media_type, size, created_by, created_at)
						VALUES ('docker-legacy', 'app', 'd1', 'application/vnd.docker.distribution.manifest.v2+json', 42, 'ci', '` + now + `')`,
				} {
					if _, err := db.Exec(stmt); err != nil {
						t.Fatalf("seeding M2 data (%q): %v", stmt, err)
					}
				}
			},
		},
		{
			name:     "M3 database (001+002+003, remote data)",
			migs:     3,
			password: "m3-pw",
			seed: func(t *testing.T, db *sql.DB) {
				now := Now()
				for _, stmt := range []string{
					`INSERT INTO repositories (repo_key, type, package_type, description, config, created_at, updated_at)
						VALUES ('remote-legacy', 'remote', 'maven', '', '{}', '` + now + `', '` + now + `')`,
					`INSERT INTO remote_configs (repo_key, url, username, password, cache_ttl_seconds,
						content_ttl_seconds, metadata_ttl_seconds, allow_private_upstream, blocked_out)
						VALUES ('remote-legacy', 'https://repo.example.test/maven', '', '', 0, 7200, 300, 0, 0)`,
				} {
					if _, err := db.Exec(stmt); err != nil {
						t.Fatalf("seeding M3 data (%q): %v", stmt, err)
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "binflow.db")
			buildLegacyDatabase(t, path, tt.migs, func(db *sql.DB) {
				legacyAdminSeed(t, db, tt.password)
				tt.seed(t, db)
			})

			st, err := Open(ctx, Options{Path: path, AdminPassword: "ignored-by-upgrade"})
			if err != nil {
				t.Fatalf("Open (upgrade): %v", err)
			}
			defer func() { _ = st.Close() }()

			v, err := CurrentVersion(ctx, st.(*sqliteStore).db)
			if err != nil {
				t.Fatalf("CurrentVersion: %v", err)
			}
			if v != latestMigrationVersion() {
				t.Fatalf("upgraded version = %d, want %d", v, latestMigrationVersion())
			}
			// Legacy data intact, admin seed not rewritten.
			u, err := st.Users().Get(ctx, "admin")
			if err != nil || !VerifyPassword(tt.password, u.PasswordHash) {
				t.Fatalf("admin password changed by upgrade: %+v (err %v)", u, err)
			}
			// users.email backfilled to '' on the legacy admin row.
			if u.Email != "" {
				t.Fatalf("legacy user email = %q, want '' (column backfill)", u.Email)
			}
			// The 004 surface is usable right away.
			now := Now()
			if err := st.Groups().Create(ctx, &Group{
				Name: "devs", Description: "upgraded", CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("group create after upgrade: %v", err)
			}
			if err := st.Groups().SetUserGroups(ctx, "admin", []string{"devs"}); err != nil {
				t.Fatalf("membership after upgrade: %v", err)
			}
			got, err := st.Groups().GroupsOfUser(ctx, "admin")
			if err != nil || len(got) != 1 || got[0].Name != "devs" {
				t.Fatalf("groups-of-user after upgrade = %+v (err %v)", got, err)
			}
			if err := st.WebSessions().Create(ctx, &WebSession{
				IDHash: "upgrade-session-hash", Username: "admin",
				CreatedAt: now, ExpiresAt: "2027-01-01T00:00:00Z",
			}); err != nil {
				t.Fatalf("web session create after upgrade: %v", err)
			}
			if _, err := st.WebSessions().GetBySHA256(ctx, "upgrade-session-hash"); err != nil {
				t.Fatalf("web session get after upgrade: %v", err)
			}
			if _, err := st.Audits().Query(ctx, AuditQuery{Actor: "admin", Limit: 5}); err != nil {
				t.Fatalf("audit query after upgrade: %v", err)
			}
		})
	}
}

// T-90 AC ②: the audit query plans never degrade to a full table scan —
// over a 10k-row audit_events table every filter shape is served by the 001
// (repo_key,time)/time or the 004 (actor,time)/(action,time) composite
// indexes (NFR-P17; the 100k-row timing budget belongs to T-105).
func TestAuditQueryPlanNoFullTableScan(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db
	ctx := context.Background()

	// 10k rows across several actors/actions/repos and a time spread, in one
	// transaction: a bulk insert as fast as the store allows.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	const total = 10000
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO audit_events (time, actor, action, repo_key, path, detail) VALUES (?, ?, ?, ?, '', '{}')`)
	if err != nil {
		t.Fatalf("prepare seed: %v", err)
	}
	for i := 0; i < total; i++ {
		time := fmt.Sprintf("2026-08-%02dT%02d:%02d:00Z", 1+i%19, i%24, i%60)
		if _, err := stmt.ExecContext(ctx, time,
			fmt.Sprintf("actor-%d", i%10), // 10 actors
			[]string{"deploy", "delete", "download", "login.success"}[i%4],
			fmt.Sprintf("repo-%d", i%5), // 5 repos
		); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	// The exact SQL auditStore.Query builds, reproduced per filter shape so
	// the plan assertion targets the shipped statement text.
	const selectCols = `SELECT id, time, actor, action, repo_key, path, detail FROM audit_events`
	const cursorPredicate = `(time < ? OR (time = ? AND id < ?))`
	tests := []struct {
		name string
		sql  string
		args []any
	}{
		{"actor", selectCols + ` WHERE actor = ? ORDER BY time DESC, id DESC LIMIT ?`, []any{"actor-3", 100}},
		{"actor+since+until+cursor", selectCols + ` WHERE actor = ? AND time >= ? AND time < ? AND ` + cursorPredicate + ` ORDER BY time DESC, id DESC LIMIT ?`,
			[]any{"actor-3", "2026-08-05T00:00:00Z", "2026-08-15T00:00:00Z", "2026-08-10T00:00:00Z", "2026-08-10T00:00:00Z", int64(5000), 100}},
		{"action", selectCols + ` WHERE action = ? ORDER BY time DESC, id DESC LIMIT ?`, []any{"delete", 100}},
		{"action+since", selectCols + ` WHERE action = ? AND time >= ? ORDER BY time DESC, id DESC LIMIT ?`, []any{"delete", "2026-08-10T00:00:00Z", 100}},
		{"repo", selectCols + ` WHERE repo_key = ? ORDER BY time DESC, id DESC LIMIT ?`, []any{"repo-2", 100}},
		{"repo+until", selectCols + ` WHERE repo_key = ? AND time < ? ORDER BY time DESC, id DESC LIMIT ?`, []any{"repo-2", "2026-08-15T00:00:00Z", 100}},
		{"actor+action", selectCols + ` WHERE actor = ? AND action = ? ORDER BY time DESC, id DESC LIMIT ?`, []any{"actor-3", "delete", 100}},
		{"since only", selectCols + ` WHERE time >= ? ORDER BY time DESC, id DESC LIMIT ?`, []any{"2026-08-18T00:00:00Z", 100}},
		{"until only", selectCols + ` WHERE time < ? ORDER BY time DESC, id DESC LIMIT ?`, []any{"2026-08-02T00:00:00Z", 100}},
		{"no filter", selectCols + ` ORDER BY time DESC, id DESC LIMIT ?`, []any{100}},
		{"no filter + cursor", selectCols + ` WHERE ` + cursorPredicate + ` ORDER BY time DESC, id DESC LIMIT ?`,
			[]any{"2026-08-10T00:00:00Z", "2026-08-10T00:00:00Z", int64(5000), 100}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+tt.sql, tt.args...)
			if err != nil {
				t.Fatalf("EXPLAIN: %v", err)
			}
			var plans []string
			for rows.Next() {
				var id, parent, notu int
				var detail string
				if err := rows.Scan(&id, &parent, &notu, &detail); err != nil {
					t.Fatalf("scan plan: %v", err)
				}
				plans = append(plans, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("plans: %v", err)
			}
			_ = rows.Close()
			if len(plans) == 0 {
				t.Fatal("empty query plan")
			}
			for _, p := range plans {
				if !strings.Contains(p, "audit_events") {
					continue // USE TEMP B-TREE rows etc.
				}
				// A bare "SCAN audit_events" is a full table scan; index-driven
				// access (SEARCH ... USING INDEX, or the index-ordered SCAN
				// over idx_audit_time that serves unfiltered pages) is the
				// accepted shape.
				if strings.Contains(p, "SCAN audit_events") && !strings.Contains(p, "USING INDEX") {
					t.Fatalf("full table scan in plan %q (shape %q)", p, tt.name)
				}
				if !strings.Contains(p, "USING INDEX") {
					t.Fatalf("plan %q does not use an index (shape %q)", p, tt.name)
				}
			}
		})
	}

	// The shipped Query itself runs green over the seeded table and keeps
	// the cursor walk exact at the 10k scale (spot check: full walk count).
	var seen int
	cursor := ""
	for {
		page, err := st.Audits().Query(ctx, AuditQuery{Limit: 1000, Cursor: cursor})
		if err != nil {
			t.Fatalf("Query page: %v", err)
		}
		seen += len(page)
		if len(page) == 0 {
			break
		}
		last := page[len(page)-1]
		cursor = last.Time + auditCursorSeparator + fmt.Sprintf("%d", last.ID)
	}
	if seen != total {
		t.Fatalf("cursor walk saw %d events, want %d (no skips, no repeats)", seen, total)
	}
}
