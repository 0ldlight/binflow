-- 026_build_module_ord.sql (sqlite dialect) — module identity becomes the
-- ORDINAL, not the id (L023-2B / build-info.md §11.4-E5: append is a LIST
-- CONCATENATION — same-id modules are never merged, never deduplicated,
-- duplicate entries coexist as separate rows each holding its own
-- artifacts and dependencies).
--
-- The 024 family keyed build_modules by (coords, module_id) and the child
-- segments by (coords, module_id, seq) — the merge-by-id law that L023-1's
-- live probe falsified (E5: two appended mod-a rows echo as TWO entries).
-- This migration re-keys the module segment on the wire ordinal:
--   build_modules      PK (coords, ord)            — module_id stays a column
--   build_artifacts    PK (coords, module_ord, seq) — FK (coords, module_ord)
--   build_dependencies PK (coords, module_ord, seq) — FK (coords, module_ord)
-- The run header, promotions and properties tables are untouched.
--
-- Conventions inherit from 001/024 (ADR-0007): statements inside the
-- SQLite/Postgres common subset, booleans as INTEGER 0/1, no transaction
-- statements in the body — the migrator wraps each migration in one
-- transaction. The rebuild is version-gated (applied versions are skipped);
-- ROW_NUMBER() partitions the pre-existing unique ids into ordinals and the
-- children join back 1:1 (the old key's uniqueness is the join's guarantee).
-- Indexes on the dropped children are recreated after the rename.

CREATE TABLE IF NOT EXISTS build_modules_ord (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	ord          INTEGER NOT NULL CHECK (ord >= 0),  -- wire array position
	module_id    TEXT NOT NULL DEFAULT '',           -- wire `id`; duplicates legal (E5)
	module_type  TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (build_name, build_number, started, build_repo, ord),
	FOREIGN KEY (build_name, build_number, started, build_repo)
		REFERENCES builds (build_name, build_number, started, build_repo) ON DELETE CASCADE
);

INSERT INTO build_modules_ord (build_name, build_number, started, build_repo, ord, module_id, module_type)
	SELECT build_name, build_number, started, build_repo,
		ROW_NUMBER() OVER (PARTITION BY build_name, build_number, started, build_repo ORDER BY module_id) - 1,
		module_id, module_type
	FROM build_modules;

CREATE TABLE IF NOT EXISTS build_artifacts_ord (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	module_ord   INTEGER NOT NULL,
	module_id    TEXT NOT NULL DEFAULT '',           -- denormalized echo of the parent row
	seq          INTEGER NOT NULL CHECK (seq >= 0),  -- wire array order inside the module
	name         TEXT NOT NULL DEFAULT '',
	type         TEXT NOT NULL DEFAULT '',
	sha1         TEXT NOT NULL DEFAULT '',
	sha256       TEXT NOT NULL DEFAULT '',
	md5          TEXT NOT NULL DEFAULT '',
	repo_key     TEXT,                        -- nodes association; NULL = record-only
	path         TEXT,
	PRIMARY KEY (build_name, build_number, started, build_repo, module_ord, seq),
	FOREIGN KEY (build_name, build_number, started, build_repo, module_ord)
		REFERENCES build_modules_ord (build_name, build_number, started, build_repo, ord) ON DELETE CASCADE,
	FOREIGN KEY (repo_key, path) REFERENCES nodes (repo_key, path) ON DELETE SET NULL
);

INSERT INTO build_artifacts_ord (build_name, build_number, started, build_repo, module_ord, module_id,
		seq, name, type, sha1, sha256, md5, repo_key, path)
	SELECT a.build_name, a.build_number, a.started, a.build_repo, m.ord, a.module_id,
		a.seq, a.name, a.type, a.sha1, a.sha256, a.md5, a.repo_key, a.path
	FROM build_artifacts a
	JOIN build_modules_ord m ON a.build_name = m.build_name AND a.build_number = m.build_number
		AND a.started = m.started AND a.build_repo = m.build_repo AND a.module_id = m.module_id;

CREATE TABLE IF NOT EXISTS build_dependencies_ord (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	module_ord   INTEGER NOT NULL,
	module_id    TEXT NOT NULL DEFAULT '',
	seq          INTEGER NOT NULL CHECK (seq >= 0),
	dep_id       TEXT NOT NULL DEFAULT '',           -- wire `id`, whole coordinate (g:a:v:c et al)
	dep_type     TEXT NOT NULL DEFAULT '',
	scopes       TEXT NOT NULL DEFAULT '',           -- wire scopes[] joined with ','
	sha1         TEXT NOT NULL DEFAULT '',
	sha256       TEXT NOT NULL DEFAULT '',
	md5          TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (build_name, build_number, started, build_repo, module_ord, seq),
	FOREIGN KEY (build_name, build_number, started, build_repo, module_ord)
		REFERENCES build_modules_ord (build_name, build_number, started, build_repo, ord) ON DELETE CASCADE
);

INSERT INTO build_dependencies_ord (build_name, build_number, started, build_repo, module_ord, module_id,
		seq, dep_id, dep_type, scopes, sha1, sha256, md5)
	SELECT d.build_name, d.build_number, d.started, d.build_repo, m.ord, d.module_id,
		d.seq, d.dep_id, d.dep_type, d.scopes, d.sha1, d.sha256, d.md5
	FROM build_dependencies d
	JOIN build_modules_ord m ON d.build_name = m.build_name AND d.build_number = m.build_number
		AND d.started = m.started AND d.build_repo = m.build_repo AND d.module_id = m.module_id;

DROP TABLE IF EXISTS build_artifacts;
DROP TABLE IF EXISTS build_dependencies;
DROP TABLE IF EXISTS build_modules;
ALTER TABLE build_modules_ord RENAME TO build_modules;
ALTER TABLE build_artifacts_ord RENAME TO build_artifacts;
ALTER TABLE build_dependencies_ord RENAME TO build_dependencies;

CREATE INDEX IF NOT EXISTS idx_build_artifacts_node ON build_artifacts (repo_key, path);
CREATE INDEX IF NOT EXISTS idx_build_artifacts_sha1 ON build_artifacts (sha1);
CREATE INDEX IF NOT EXISTS idx_build_artifacts_sha256 ON build_artifacts (sha256);
CREATE INDEX IF NOT EXISTS idx_build_dependencies_sha1 ON build_dependencies (sha1);
CREATE INDEX IF NOT EXISTS idx_build_dependencies_sha256 ON build_dependencies (sha256);
