-- 024_build_info.sql (sqlite dialect) — the build-info table family (M17
-- T-507, FR-152.1 / ADR-0045 decision 2 + Errata): builds, build_modules,
-- build_artifacts, build_dependencies, build_promotions and
-- build_properties — the normalized record plane of the build domain.
-- Build JSON is a record, not an artifact: nothing here enters
-- storage/nodes (ADR-0045 axis 2-A), and the original uploaded document is
-- archived in builds.payload so the GET face echoes exactly what arrived.
--
-- Run identity is the FOUR-TUPLE (build_name, build_number, started,
-- build_repo) — carried as the primary key itself (ADR-0045 Errata ④㋓:
-- same name and number with a different started is a different run, and
-- ?started= is the disambiguation key; the reference product pins the same
-- four as its UNIQUE index). build_repo is the authorization-domain
-- logical key, DEFAULT 'artifactory-build-info' (product schema DDL +
-- webhook.md section 3.4 sample + getPreferredBuildRepo fallback,
-- triple-sourced per ADR-0045 Errata ②): it requires NO repositories row
-- and no repository is ever auto-created (zero-silent-writes posture), and
-- a real repository of the same key never interferes (build data bypasses
-- nodes). started keeps the WIRE LITERAL (yyyy-MM-dd'T'HH:mm:ss.SSSZ) —
-- round-trip fidelity beats normalization; ordering projections order by
-- it lexicographically (the RFC3339-text argument, the audit cursor's
-- standing rule), which is chronological for zone-stable stamps.
--
-- build_modules.module_id is the Module ID field (T-512's consumption):
-- the merge key of the append face ("same id = same module, artifacts and
-- dependencies append") and the "<child-name>/<child-number>" reference
-- form of aggregate builds. Module properties stay in the payload archive
-- only (no module_props table — ADR-0045 decision 2 pins the six-table
-- family).
--
-- build_artifacts carries the nodes association as REAL foreign keys:
-- (repo_key, path) REFERENCES nodes(repo_key, path). NULL = record-only —
-- the artifact line without a resolvable checksum/node link never claims
-- one ("no checksum = record, no association", build-info.md section 2.2);
-- ON DELETE SET NULL keeps the historical row when the node leaves (the
-- build record outlives the artifact; history is append-only, a node
-- delete must not destroy it). wire fields kept normalized: name/type/
-- sha1/sha256/md5 (the wire `path` IS the association — its repo segment
-- splits into repo_key/path; originalDeploymentRepo stays in the payload).
-- seq preserves the wire array order (echo fidelity); dependency rows are
-- seq-keyed the same way. build_dependencies never touches nodes:
-- dependencies are not artifacts (sha1/md5 per inv-4 D1, no resolvability
-- requirement).
--
-- build_promotions is APPEND-ONLY history — the store exposes no update
-- path — and status is a FREE string: no closed set, deliberately no CHECK
-- (ADR-0045 Errata ②: staged/rolled-up/released are UI convention, not
-- protocol); the current status is the max-promoted_at row. dry_run keeps
-- the INTEGER 0/1 convention. comment closes the six-tuple
-- (status/timestamp/comment/repository/ciUser/user, section 2.4).
--
-- Dialect conventions of 001/009/018/021 (ADR-0007): statements inside
-- the SQLite/Postgres common subset, booleans as INTEGER 0/1, no
-- transaction statements in the body — the migrator wraps each migration
-- in one transaction. Idempotent: CREATE TABLE IF NOT EXISTS, the
-- post-011 convention the t212 rewind test replays under.
--
-- Indexes: builds(build_name) for the numbers face, builds(build_repo,
-- build_name) for the ACL visible-set walk, build_artifacts(repo_key,
-- path) for the artifact-reverse-lookup face (which builds pulled this
-- node — the Module ID/AQL include path), artifact/dependency checksum
-- indexes for the checksum-driven association and dependency-search faces
-- (the reference indexes sha1/md5 on artifacts; BinFlow's checksum-first
-- culture indexes sha256 instead of md5 — stored, unindexed).

CREATE TABLE IF NOT EXISTS builds (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,               -- wire literal, run identity element 3
	build_repo   TEXT NOT NULL DEFAULT 'artifactory-build-info',
	build_type   TEXT NOT NULL DEFAULT '',    -- wire `type`: MAVEN|GRADLE|ANT|IVY|GENERIC, '' allowed
	created_by   TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	updated_by   TEXT NOT NULL,
	updated_at   TEXT NOT NULL,
	payload      TEXT NOT NULL DEFAULT '',    -- archived original build info JSON
	PRIMARY KEY (build_name, build_number, started, build_repo)
);

CREATE TABLE IF NOT EXISTS build_modules (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	module_id    TEXT NOT NULL,               -- the Module ID field (merge key / reference form)
	module_type  TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (build_name, build_number, started, build_repo, module_id),
	FOREIGN KEY (build_name, build_number, started, build_repo)
		REFERENCES builds (build_name, build_number, started, build_repo) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS build_artifacts (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	module_id    TEXT NOT NULL,
	seq          INTEGER NOT NULL CHECK (seq >= 0),  -- wire array order
	name         TEXT NOT NULL DEFAULT '',
	type         TEXT NOT NULL DEFAULT '',
	sha1         TEXT NOT NULL DEFAULT '',
	sha256       TEXT NOT NULL DEFAULT '',
	md5          TEXT NOT NULL DEFAULT '',
	repo_key     TEXT,                        -- nodes association; NULL = record-only
	path         TEXT,
	PRIMARY KEY (build_name, build_number, started, build_repo, module_id, seq),
	FOREIGN KEY (build_name, build_number, started, build_repo, module_id)
		REFERENCES build_modules (build_name, build_number, started, build_repo, module_id) ON DELETE CASCADE,
	FOREIGN KEY (repo_key, path) REFERENCES nodes (repo_key, path) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS build_dependencies (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	module_id    TEXT NOT NULL,
	seq          INTEGER NOT NULL CHECK (seq >= 0),
	dep_id       TEXT NOT NULL,               -- wire `id`, whole coordinate (g:a:v:c et al)
	dep_type     TEXT NOT NULL DEFAULT '',
	scopes       TEXT NOT NULL DEFAULT '',    -- wire scopes[] joined with ','
	sha1         TEXT NOT NULL DEFAULT '',
	sha256       TEXT NOT NULL DEFAULT '',
	md5          TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (build_name, build_number, started, build_repo, module_id, seq),
	FOREIGN KEY (build_name, build_number, started, build_repo, module_id)
		REFERENCES build_modules (build_name, build_number, started, build_repo, module_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS build_promotions (
	id           TEXT PRIMARY KEY,            -- uuid minted by the writer
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	status       TEXT NOT NULL DEFAULT '',    -- FREE string, no closed set (ADR-0045 Errata ②)
	target_repo  TEXT NOT NULL DEFAULT '',
	ci_user      TEXT NOT NULL DEFAULT '',
	comment      TEXT NOT NULL DEFAULT '',    -- six-tuple member (section 2.4)
	dry_run      INTEGER NOT NULL DEFAULT 0 CHECK (dry_run IN (0,1)),
	params_json  TEXT NOT NULL DEFAULT '',    -- archived promotion request
	promoted_by  TEXT NOT NULL,
	promoted_at  TEXT NOT NULL,
	FOREIGN KEY (build_name, build_number, started, build_repo)
		REFERENCES builds (build_name, build_number, started, build_repo) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS build_properties (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	name         TEXT NOT NULL,
	value        TEXT NOT NULL DEFAULT '',    -- map semantics: one value per name
	PRIMARY KEY (build_name, build_number, started, build_repo, name),
	FOREIGN KEY (build_name, build_number, started, build_repo)
		REFERENCES builds (build_name, build_number, started, build_repo) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_builds_name ON builds (build_name);
CREATE INDEX IF NOT EXISTS idx_builds_repo_name ON builds (build_repo, build_name);
CREATE INDEX IF NOT EXISTS idx_build_artifacts_node ON build_artifacts (repo_key, path);
CREATE INDEX IF NOT EXISTS idx_build_artifacts_sha1 ON build_artifacts (sha1);
CREATE INDEX IF NOT EXISTS idx_build_artifacts_sha256 ON build_artifacts (sha256);
CREATE INDEX IF NOT EXISTS idx_build_dependencies_sha1 ON build_dependencies (sha1);
CREATE INDEX IF NOT EXISTS idx_build_dependencies_sha256 ON build_dependencies (sha256);
CREATE INDEX IF NOT EXISTS idx_build_promotions_build ON build_promotions (build_name, build_number, started, build_repo, promoted_at);
