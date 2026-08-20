package metadata

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// T-115 (T-97 architecture review NB1): the 006 username index on
// user_groups. GroupsOfUser is the authentication-time membership join —
// fillGroups runs it on every authenticated request (both the authenticator
// and the token path enrich Principal.Groups through it, architecture 3.4)
// — and until 006 the table had only the group_id-leading primary key, so
// no username-shaped access path existed. The statement below is the exact
// SQL substores_console.go ships (only whitespace differs; the planner sees
// the same query).
const groupsOfUserPlanStmt = `SELECT g.id, g.name, g.description, g.created_at, g.updated_at
	FROM groups g JOIN user_groups ug ON ug.group_id = g.id
	WHERE ug.username = ? ORDER BY g.name`

// planOf runs EXPLAIN QUERY PLAN over query and returns the detail column,
// so tests can pin the access path instead of trusting intuition.
func planOf(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plans []string
	for rows.Next() {
		var id, parent, notu int
		var detail string
		if err := rows.Scan(&id, &parent, &notu, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		plans = append(plans, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating plan rows: %v", err)
	}
	if len(plans) == 0 {
		t.Fatal("empty query plan")
	}
	return plans
}

func planContains(plans []string, substr string) bool {
	for _, p := range plans {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}

// AC: the fresh schema carries idx_user_groups_username over exactly the
// username column (the section 6 004-block errata line, shipped by 006),
// and the hot-path join consumes it — SEARCH, never a bare table SCAN.
func TestUserGroupsUsernameIndexShapeAndPlan(t *testing.T) {
	st := openTest(t)
	db := st.(*sqliteStore).db

	var sqlText string
	if err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_user_groups_username'`,
	).Scan(&sqlText); err != nil {
		t.Fatalf("index missing from schema: %v (architecture section 6 004-block errata defines it)", err)
	}
	for _, want := range []string{"CREATE INDEX idx_user_groups_username", "ON user_groups(username)"} {
		if !strings.Contains(sqlText, want) {
			t.Errorf("index DDL = %q, want it to contain %q", sqlText, want)
		}
	}
	// Exactly one index of that name — a duplicated CREATE would have failed
	// the Open above, but the count pins the shape for future migrations.
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_user_groups_username'`,
	).Scan(&count); err != nil || count != 1 {
		t.Errorf("idx_user_groups_username count = %d (err %v), want 1", count, err)
	}
	// Leading column is username (an index the planner can only use
	// left-to-right — the whole point of 006).
	rows, err := db.Query(`SELECT name FROM pragma_index_info('idx_user_groups_username')`)
	if err != nil {
		t.Fatalf("pragma_index_info: %v", err)
	}
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan index column: %v", err)
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating index columns: %v", err)
	}
	_ = rows.Close()
	if len(cols) != 1 || cols[0] != "username" {
		t.Errorf("idx_user_groups_username columns = %v, want [username]", cols)
	}

	// SQLite renders the table alias (ug), not the table name, in plan
	// details — the shipped statement aliases the join the same way.
	plans := planOf(t, db, groupsOfUserPlanStmt, "admin")
	t.Logf("fresh-schema plan: %v", plans)
	if !planContains(plans, "SEARCH ug USING INDEX idx_user_groups_username") {
		t.Fatalf("GroupsOfUser plan %v does not seek through idx_user_groups_username", plans)
	}
	if planContains(plans, "SCAN ug") {
		t.Fatalf("GroupsOfUser plan %v still full-scans user_groups (alias ug)", plans)
	}
}

// AC: the upgrade story, in the T-95 005-test shape — a database last
// written by the pre-006 build (001..005 applied, live memberships) is
// opened by the current build: 006 applies in place, the plan flips from
// the unindexed SCAN to the indexed SEARCH, the membership data survives
// untouched, and a second reopen re-runs nothing.
func TestUserGroupsIndexMigrationUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "binflow.db")

	const ts = "2026-08-19T00:00:00Z"
	seed := func(db *sql.DB) {
		for _, stmt := range []string{
			`INSERT INTO users (username, password_hash, is_admin, enabled, created_at, updated_at)
				VALUES ('alice', 'x', 0, 1, '` + ts + `', '` + ts + `'),
				       ('bob',   'x', 0, 1, '` + ts + `', '` + ts + `')`,
			`INSERT INTO groups (name, description, created_at, updated_at)
				VALUES ('devs', '', '` + ts + `', '` + ts + `'),
				       ('qa',   '', '` + ts + `', '` + ts + `')`,
			`INSERT INTO user_groups (group_id, username) VALUES (1, 'alice'), (2, 'alice'), (2, 'bob')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("seeding pre-006 data (%q): %v", stmt, err)
			}
		}
	}
	buildLegacyDatabase(t, path, 5, seed) // the pre-006 build: 001..005 applied

	// BEFORE: no username index exists, so the membership join has no
	// user-shaped access path — the pre-006 plan is pinned here as the
	// recorded regression baseline.
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open raw (pre-006): %v", err)
	}
	before := planOf(t, raw, groupsOfUserPlanStmt, "alice")
	t.Logf("pre-006 plan: %v", before)
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}
	if planContains(before, "idx_user_groups_username") {
		t.Fatalf("pre-006 plan %v already uses the index — the BEFORE baseline is stale", before)
	}
	if !planContains(before, "SCAN ug") {
		t.Fatalf("pre-006 plan %v does not full-scan user_groups (alias ug) — update the baseline to the observed shape", before)
	}

	// Upgrade: Open applies 006 (and only 006 — 001..005 are ledgered).
	st, err := Open(ctx, Options{Path: path, AdminPassword: "pw-006"})
	if err != nil {
		t.Fatalf("Open (upgrade): %v", err)
	}
	defer func() { _ = st.Close() }()
	v, err := CurrentVersion(ctx, st.(*sqliteStore).db)
	if err != nil || v != latestMigrationVersion() {
		t.Fatalf("upgraded version = %d (err %v), want %d", v, err, latestMigrationVersion())
	}

	// AFTER: the hot path seeks through the new index.
	after := planOf(t, st.(*sqliteStore).db, groupsOfUserPlanStmt, "alice")
	t.Logf("post-006 plan: %v", after)
	if !planContains(after, "SEARCH ug USING INDEX idx_user_groups_username") {
		t.Fatalf("post-006 plan %v does not seek through idx_user_groups_username", after)
	}
	if planContains(after, "SCAN ug") {
		t.Fatalf("post-006 plan %v still full-scans user_groups (alias ug)", after)
	}

	// The live membership data rode through the upgrade untouched.
	for user, want := range map[string][]string{
		"alice": {"devs", "qa"},
		"bob":   {"qa"},
	} {
		got, err := st.Groups().GroupsOfUser(ctx, user)
		if err != nil {
			t.Fatalf("GroupsOfUser(%s) after upgrade: %v", user, err)
		}
		if len(got) != len(want) {
			t.Fatalf("GroupsOfUser(%s) after upgrade = %d groups, want %d", user, len(got), len(want))
		}
		for i, g := range got {
			if g.Name != want[i] {
				t.Errorf("GroupsOfUser(%s)[%d] = %q, want %q", user, i, g.Name, want[i])
			}
		}
	}

	// Reopen: the ledger skips 006 — a re-run CREATE INDEX would error
	// ("index already exists"), so a clean second Open IS the idempotency
	// proof (same posture as the 004/005 idempotency tests).
	st2, err := Open(ctx, Options{Path: path, AdminPassword: "pw-006"})
	if err != nil {
		t.Fatalf("reopen (idempotency): %v", err)
	}
	defer func() { _ = st2.Close() }()
	if v, _ := CurrentVersion(ctx, st2.(*sqliteStore).db); v != latestMigrationVersion() {
		t.Fatalf("version after reopen = %d, want %d (006 must not re-run)", v, latestMigrationVersion())
	}
	if g, err := st2.Groups().GroupsOfUser(ctx, "alice"); err != nil || len(g) != 2 {
		t.Fatalf("GroupsOfUser(alice) after reopen = %d groups (err %v), want 2 — nothing re-ran or doubled", len(g), err)
	}
}
