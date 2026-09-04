// T-444 acceptance surface (FR-146.1, ADR-0044 K68 point 3/4/5,
// architecture section 25.6): migration 023's dry-run mapping report, the
// backfill's bidirectional set equality, the zero-privilege equivalence of
// the effective permission matrix across the migration (NFR-S77), the
// reopen idempotency, and the documented rollback round-trip.

package metadata_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// rewindToPreAnnotate rolls a current-schema database back to the pre-023
// shape (a T-438-build database): the annotate column leaves with its
// ledger row, every other column stays — the rewind pattern of
// rewindToPreRBAC, one version later.
func rewindToPreAnnotate(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM schema_migrations WHERE version >= 23`,
		`ALTER TABLE permission_principals DROP COLUMN can_annotate`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("rewind (%q): %v", stmt, err)
		}
	}
}

// seedLegacyGrants writes the legacy-shaped rows the migration upgrades:
// five principals over one path-scoped target (plus a group row), covering
// every write-bit shape the backfill must follow.
func seedLegacyGrants(t *testing.T, st metadata.Store) {
	t.Helper()
	ctx := context.Background()
	now := metadata.Now()
	mk := func(l ...string) string {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	rows := []*metadata.PermissionPrincipal{
		{TargetName: "legacy", Principal: "alice", PrincipalType: "user", CanRead: true, CanWrite: true, CanDelete: true},
		{TargetName: "legacy", Principal: "bob", PrincipalType: "user", CanRead: true},
		{TargetName: "legacy", Principal: "carol", PrincipalType: "user", CanManage: true},
		{TargetName: "legacy", Principal: "dave", PrincipalType: "user", CanWrite: true},
		{TargetName: "legacy", Principal: "devs", PrincipalType: "group", CanRead: true, CanWrite: true},
	}
	if err := st.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
		Name: "legacy", Repos: mk("libs-release"),
		Includes: mk("ci/**"), Excludes: mk("ci/secret.key"),
		CreatedAt: now, UpdatedAt: now,
	}, rows); err != nil {
		t.Fatalf("PutTarget: %v", err)
	}
}

// effectiveMatrix snapshots the authorization answers of every probe
// principal over the probe grid, through the real auth.Service over the
// store — the machine form of K68 point 5's "principal × repo × path ×
// action" matrix.
func effectiveMatrix(t *testing.T, st metadata.Store) map[string]bool {
	t.Helper()
	ctx := context.Background()
	svc := auth.NewFromStore(st, false)
	principals := map[string]*auth.Principal{
		"alice":       {Name: "alice"},
		"bob":         {Name: "bob"},
		"carol":       {Name: "carol"},
		"dave":        {Name: "dave"},
		"eve":         {Name: "eve"},                             // no row anywhere
		"devs-member": {Name: "frank", Groups: []string{"devs"}}, // the SE-07 union arm
	}
	actions := []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete, auth.ActionManage, auth.ActionAnnotate}
	out := map[string]bool{}
	for pname, p := range principals {
		for _, repo := range []string{"libs-release", "other-repo"} {
			for _, path := range []string{"ci/y.bin", "ci/secret.key", "out/z.bin", ""} {
				for _, act := range actions {
					out[pname+"|"+repo+"|"+path+"|"+act] = svc.Can(ctx, p, repo, path, act)
				}
			}
		}
	}
	return out
}

// TestAnnotateMigration023DryRunBackfillAndZeroEscalation: the full K68
// point 3/4/5 cycle — dry-run mapping table (rate 100%, annotate column ≡
// write column), the upgrade's backfill (bidirectional set equality), and
// the pre/post effective-matrix diff of zero (zero privilege escalation
// AND zero regression: the property-write face after the split is exactly
// the write face before it, and every other action is untouched).
func TestAnnotateMigration023DryRunBackfillAndZeroEscalation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")

	// Seed with the current binary (the column exists, all rows a=0 — the
	// legacy semantics: no property-write bit anywhere, the §15.3.3
	// as-built property gate is `w`), then capture the pre-migration
	// matrix BEFORE the rewind models the legacy database.
	st, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (seed): %v", err)
	}
	seedLegacyGrants(t, st)
	pre := effectiveMatrix(t, st)
	if err := st.Close(); err != nil {
		t.Fatalf("close (seed): %v", err)
	}

	// Rewind to the pre-023 shape; the DRY RUN reads that database.
	db := liveDB(t, path)
	rewindToPreAnnotate(t, db)
	dry, err := metadata.AnnotateMappingDryRun(ctx, db)
	if err != nil {
		t.Fatalf("AnnotateMappingDryRun: %v", err)
	}
	if dry.TotalRows != 5 || len(dry.Rows) != 5 {
		t.Fatalf("dry-run rows = %d (len %d), want 5", dry.TotalRows, len(dry.Rows))
	}
	if dry.MappingRatePercent != 100 {
		t.Fatalf("dry-run mapping rate = %v%%, want 100", dry.MappingRatePercent)
	}
	if dry.WriteRows != 3 { // alice, dave, devs
		t.Fatalf("dry-run write rows = %d, want 3", dry.WriteRows)
	}
	for _, r := range dry.Rows {
		if r.AnnotateBackfill != r.DeployCache {
			t.Fatalf("row %s %s: annotate backfill %v != deploy-cache %v (the backfill law)",
				r.TargetName, r.Principal, r.AnnotateBackfill, r.DeployCache)
		}
	}
	// The post-apply check against a pre-023 schema is an honest error
	// (no-such-column), not a silent pass.
	if _, err := metadata.AnnotateBackfillVerify(ctx, db); err == nil {
		t.Fatal("AnnotateBackfillVerify on a pre-023 database must fail (column absent)")
	}

	// The upgrade: reopening applies 023 and backfills.
	st2, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (upgrade): %v", err)
	}
	defer func() { _ = st2.Close() }()
	db2 := liveDB(t, path)
	var v int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 23`).Scan(&v); err != nil || v != 1 {
		t.Fatalf("schema_migrations v23 rows = %d (%v), want 1", v, err)
	}
	diff, err := metadata.AnnotateBackfillVerify(ctx, db2)
	if err != nil {
		t.Fatalf("AnnotateBackfillVerify: %v", err)
	}
	if diff.WriteWithoutAnnotate != 0 || diff.AnnotateWithoutWrite != 0 {
		t.Fatalf("backfill set != write set: %+v (disagreements %v)", diff, diff.Disagreements)
	}
	if diff.WriteRows != 3 || diff.AnnotateRows != 3 {
		t.Fatalf("post-apply sets: write=%d annotate=%d, want 3/3", diff.WriteRows, diff.AnnotateRows)
	}
	// The dry run recomputes identically over the upgraded database (the
	// mapping is a pure function of can_write).
	again, err := metadata.AnnotateMappingDryRun(ctx, db2)
	if err != nil || again.TotalRows != dry.TotalRows || again.WriteRows != dry.WriteRows {
		t.Fatalf("dry-run over upgraded db = %+v (%v), want the same table as pre-run", again, err)
	}

	// Zero-privilege equivalence, half one: the legacy verbs' cells are
	// bit-for-bit identical pre and post (r/w/d/m — the migration touches
	// no column they read; the pre model's closed set has no a).
	post := effectiveMatrix(t, st2)
	if len(pre) != len(post) {
		t.Fatalf("matrix size changed: %d -> %d", len(pre), len(post))
	}
	for _, act := range []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete, auth.ActionManage} {
		for _, p := range []string{"alice", "bob", "carol", "dave", "eve", "devs-member"} {
			for _, repo := range []string{"libs-release", "other-repo"} {
				for _, pth := range []string{"ci/y.bin", "ci/secret.key", "out/z.bin", ""} {
					key := p + "|" + repo + "|" + pth + "|" + act
					if pre[key] != post[key] {
						t.Errorf("matrix drift at %s: %v -> %v (legacy verb changed by the migration)",
							key, pre[key], post[key])
					}
				}
			}
		}
	}
	// Half two — the one sanctioned substitution: the old property-write
	// gate (w) maps onto the new annotate face. post(a) == pre(w) everywhere.
	for _, p := range []string{"alice", "bob", "carol", "dave", "eve", "devs-member"} {
		for _, repo := range []string{"libs-release", "other-repo"} {
			for _, pth := range []string{"ci/y.bin", "ci/secret.key", "out/z.bin", ""} {
				w := pre[p+"|"+repo+"|"+pth+"|"+auth.ActionWrite]
				a := post[p+"|"+repo+"|"+pth+"|"+auth.ActionAnnotate]
				if a != w {
					t.Errorf("annotate face drift %s %s/%s: a=%v, pre w=%v (the split must preserve the property-write face)", p, repo, pth, a, w)
				}
			}
		}
	}

	// Idempotency: a third Open re-runs nothing and moves no bit.
	st3, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (idempotency): %v", err)
	}
	defer func() { _ = st3.Close() }()
	db3 := liveDB(t, path)
	if err := db3.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 23`).Scan(&v); err != nil || v != 1 {
		t.Fatalf("schema_migrations v23 rows after reopen = %d (%v), want 1", v, err)
	}
	diff3, err := metadata.AnnotateBackfillVerify(ctx, db3)
	if err != nil || diff3.WriteWithoutAnnotate != 0 || diff3.AnnotateWithoutWrite != 0 {
		t.Fatalf("backfill after reopen = %+v (%v), want the same clean sets", diff3, err)
	}
}

// TestAnnotateMigration023RollbackRoundTrip: the documented reverse
// migration (drop the column with its ledger row) is non-destructive —
// can_write survives it verbatim, and re-opening re-applies 023 to the
// same backfill. K68 point 4's "reversible" evidence.
func TestAnnotateMigration023RollbackRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "binflow.db")

	st, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seedLegacyGrants(t, st)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db := liveDB(t, path)
	rewindToPreAnnotate(t, db) // THE ROLLBACK (the wire word flips back in code)
	var writes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM permission_principals WHERE can_write = 1`).Scan(&writes); err != nil || writes != 3 {
		t.Fatalf("can_write rows after rollback = %d (%v), want 3 (untouched)", writes, err)
	}

	st2, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (re-apply): %v", err)
	}
	defer func() { _ = st2.Close() }()
	db2 := liveDB(t, path)
	diff, err := metadata.AnnotateBackfillVerify(ctx, db2)
	if err != nil {
		t.Fatalf("AnnotateBackfillVerify (re-apply): %v", err)
	}
	if diff.WriteRows != 3 || diff.AnnotateRows != 3 ||
		diff.WriteWithoutAnnotate != 0 || diff.AnnotateWithoutWrite != 0 {
		t.Fatalf("re-applied backfill = %+v, want the same clean 3/3 sets", diff)
	}
}
