// The 026 module-ordinal migration's data-carrying upgrade (L023-2B): a
// database holding 024-shaped build segments (module_id keyed) rewinds
// under the pre-026 schema, carries real rows, then reopens — the
// migrator re-keys the three tables onto the ordinal and every module,
// artifact and dependency row survives with its parentage intact.

package metadata

import (
	"context"
	"path/filepath"
	"testing"
)

// TestBuildModuleOrdMigrationCarriesData walks the upgrade: seed 024-shaped
// rows (two runs, one with two modules and nested segments), rewind to the
// old schema, reopen, then read the segment back through ListModules.
func TestBuildModuleOrdMigrationCarriesData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "binflow.db")

	seedOld := func() {
		md, err := Open(ctx, Options{Path: path, AdminPassword: "pw"})
		if err != nil {
			t.Fatalf("Open (fresh): %v", err)
		}
		db := md.(*sqliteStore).db // the test package's own concrete type
		// Rewind to the 024 shape: drop the 026 tables and recreate the
		// module_id-keyed family with real rows (the t212 rewind pattern —
		// hand-built old shape, real data, the ledger row deleted).
		for _, stmt := range []string{
			`DROP TABLE IF EXISTS build_dependencies`,
			`DROP TABLE IF EXISTS build_artifacts`,
			`DROP TABLE IF EXISTS build_modules`,
			`CREATE TABLE build_modules (
				build_name TEXT NOT NULL, build_number TEXT NOT NULL,
				started TEXT NOT NULL, build_repo TEXT NOT NULL,
				module_id TEXT NOT NULL, module_type TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (build_name, build_number, started, build_repo, module_id),
				FOREIGN KEY (build_name, build_number, started, build_repo)
					REFERENCES builds (build_name, build_number, started, build_repo) ON DELETE CASCADE)`,
			`CREATE TABLE build_artifacts (
				build_name TEXT NOT NULL, build_number TEXT NOT NULL,
				started TEXT NOT NULL, build_repo TEXT NOT NULL,
				module_id TEXT NOT NULL, seq INTEGER NOT NULL,
				name TEXT NOT NULL DEFAULT '', type TEXT NOT NULL DEFAULT '',
				sha1 TEXT NOT NULL DEFAULT '', sha256 TEXT NOT NULL DEFAULT '', md5 TEXT NOT NULL DEFAULT '',
				repo_key TEXT, path TEXT,
				PRIMARY KEY (build_name, build_number, started, build_repo, module_id, seq),
				FOREIGN KEY (build_name, build_number, started, build_repo, module_id)
					REFERENCES build_modules (build_name, build_number, started, build_repo, module_id) ON DELETE CASCADE)`,
			`CREATE TABLE build_dependencies (
				build_name TEXT NOT NULL, build_number TEXT NOT NULL,
				started TEXT NOT NULL, build_repo TEXT NOT NULL,
				module_id TEXT NOT NULL, seq INTEGER NOT NULL,
				dep_id TEXT NOT NULL DEFAULT '', dep_type TEXT NOT NULL DEFAULT '',
				scopes TEXT NOT NULL DEFAULT '',
				sha1 TEXT NOT NULL DEFAULT '', sha256 TEXT NOT NULL DEFAULT '', md5 TEXT NOT NULL DEFAULT '',
				PRIMARY KEY (build_name, build_number, started, build_repo, module_id, seq),
				FOREIGN KEY (build_name, build_number, started, build_repo, module_id)
					REFERENCES build_modules (build_name, build_number, started, build_repo, module_id) ON DELETE CASCADE)`,
			// 028 (L024-5): the client-digest columns — dropped so the
			// migrator's re-apply of 028 lands cleanly (ALTER has no IF NOT
			// EXISTS in the common subset).
			`ALTER TABLE nodes DROP COLUMN client_md5`,
			`ALTER TABLE nodes DROP COLUMN client_sha1`,
			`ALTER TABLE nodes DROP COLUMN client_sha256`,
			`DELETE FROM schema_migrations WHERE version >= 26`,
		} {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("rewind stmt: %v\n%s", err, stmt)
			}
		}
		// Two runs of one build; the first carries two modules with nested
		// segments (an associated artifact row included).
		now := "2026-09-15T00:00:00Z"
		for _, stmt := range []string{
			`INSERT INTO builds (build_name, build_number, started, build_repo, build_type,
				created_by, created_at, updated_by, updated_at, payload)
				VALUES ('mig-app', '1', '2026-09-14T10:00:00.000+0000', 'artifactory-build-info', 'GENERIC',
				'ci', '` + now + `', 'ci', '` + now + `', '{}')`,
			`INSERT INTO build_modules VALUES ('mig-app', '1', '2026-09-14T10:00:00.000+0000', 'artifactory-build-info', 'mod-b', 'maven')`,
			`INSERT INTO build_modules VALUES ('mig-app', '1', '2026-09-14T10:00:00.000+0000', 'artifactory-build-info', 'mod-a', 'gradle')`,
			`INSERT INTO build_artifacts (build_name, build_number, started, build_repo, module_id, seq, name, type, sha1)
				VALUES ('mig-app', '1', '2026-09-14T10:00:00.000+0000', 'artifactory-build-info', 'mod-a', 0, 'a.jar', 'jar', 'aa')`,
			`INSERT INTO build_dependencies (build_name, build_number, started, build_repo, module_id, seq, dep_id, scopes)
				VALUES ('mig-app', '1', '2026-09-14T10:00:00.000+0000', 'artifactory-build-info', 'mod-b', 0, 'org:lib:1', 'compile')`,
			`INSERT INTO builds (build_name, build_number, started, build_repo,
				created_by, created_at, updated_by, updated_at, payload)
				VALUES ('mig-app', '2', '2026-09-15T10:00:00.000+0000', 'artifactory-build-info',
				'ci', '` + now + `', 'ci', '` + now + `', '{}')`,
			`INSERT INTO build_modules VALUES ('mig-app', '2', '2026-09-15T10:00:00.000+0000', 'artifactory-build-info', 'solo', 'generic')`,
		} {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("seed stmt: %v\n%s", err, stmt)
			}
		}
		if err := md.Close(); err != nil {
			t.Fatalf("close after rewind: %v", err)
		}
	}
	seedOld()

	// Reopen: the migrator applies 026 over the carrying database.
	md, err := Open(ctx, Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("Open (upgrade): %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	mods, err := md.Builds().ListModules(ctx, "mig-app", "1", "2026-09-14T10:00:00.000+0000", "")
	if err != nil {
		t.Fatalf("ListModules (run 1): %v", err)
	}
	// The old schema carries no insertion order, so 026 assigns ordinals
	// deterministically by module_id (the ROW_NUMBER's ORDER BY).
	if len(mods) != 2 || mods[0].ID != "mod-a" || mods[1].ID != "mod-b" {
		t.Fatalf("run 1 modules = %+v, want both rows (id order carried into ordinals)", mods)
	}
	if len(mods[0].Artifacts) != 1 || mods[0].Artifacts[0].Name != "a.jar" || mods[0].Artifacts[0].Sha1 != "aa" {
		t.Fatalf("mod-a artifacts = %+v, want the associated row carried", mods[0].Artifacts)
	}
	if len(mods[1].Dependencies) != 1 || mods[1].Dependencies[0].ID != "org:lib:1" || mods[1].Dependencies[0].Scopes != "compile" {
		t.Fatalf("mod-b dependencies = %+v, want the row carried", mods[1].Dependencies)
	}
	mods2, err := md.Builds().ListModules(ctx, "mig-app", "2", "2026-09-15T10:00:00.000+0000", "")
	if err != nil || len(mods2) != 1 || mods2[0].ID != "solo" {
		t.Fatalf("run 2 modules = %+v (err %v), want the solo row", mods2, err)
	}

	// The upgraded segment still accepts NEW duplicate-id writes (E5's law
	// over the migrated shape).
	if err := md.Builds().PutModules(ctx, "mig-app", "2", "2026-09-15T10:00:00.000+0000", "",
		[]*BuildModule{{ID: "solo", Type: "generic"}, {ID: "solo", Type: "generic"}}); err != nil {
		t.Fatalf("PutModules duplicates on upgraded schema: %v", err)
	}
	mods2, err = md.Builds().ListModules(ctx, "mig-app", "2", "2026-09-15T10:00:00.000+0000", "")
	if err != nil || len(mods2) != 2 || mods2[0].ID != "solo" || mods2[1].ID != "solo" {
		t.Fatalf("duplicate-id write = %+v (err %v), want two solo rows", mods2, err)
	}
}
