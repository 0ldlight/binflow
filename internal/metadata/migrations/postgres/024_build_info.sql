-- 024_build_info.sql (postgres dialect) — the build-info table family (M17
-- T-507, FR-152.1 / ADR-0045 decision 2 + Errata).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). Semantics,
-- column names, keys, foreign keys and the NULL association rule match
-- the sqlite dialect file one-to-one; see that file's header for the full
-- contract (four-tuple run identity, build_repo logical key default,
-- append-only promotions with free-string status, ON DELETE SET NULL
-- nodes association).

CREATE TABLE IF NOT EXISTS builds (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL DEFAULT 'artifactory-build-info',
	build_type   TEXT NOT NULL DEFAULT '',
	created_by   TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	updated_by   TEXT NOT NULL,
	updated_at   TEXT NOT NULL,
	payload      TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (build_name, build_number, started, build_repo)
);

CREATE TABLE IF NOT EXISTS build_modules (
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	module_id    TEXT NOT NULL,
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
	seq          INTEGER NOT NULL CHECK (seq >= 0),
	name         TEXT NOT NULL DEFAULT '',
	type         TEXT NOT NULL DEFAULT '',
	sha1         TEXT NOT NULL DEFAULT '',
	sha256       TEXT NOT NULL DEFAULT '',
	md5          TEXT NOT NULL DEFAULT '',
	repo_key     TEXT,
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
	dep_id       TEXT NOT NULL,
	dep_type     TEXT NOT NULL DEFAULT '',
	scopes       TEXT NOT NULL DEFAULT '',
	sha1         TEXT NOT NULL DEFAULT '',
	sha256       TEXT NOT NULL DEFAULT '',
	md5          TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (build_name, build_number, started, build_repo, module_id, seq),
	FOREIGN KEY (build_name, build_number, started, build_repo, module_id)
		REFERENCES build_modules (build_name, build_number, started, build_repo, module_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS build_promotions (
	id           TEXT PRIMARY KEY,
	build_name   TEXT NOT NULL,
	build_number TEXT NOT NULL,
	started      TEXT NOT NULL,
	build_repo   TEXT NOT NULL,
	status       TEXT NOT NULL DEFAULT '',
	target_repo  TEXT NOT NULL DEFAULT '',
	ci_user      TEXT NOT NULL DEFAULT '',
	comment      TEXT NOT NULL DEFAULT '',
	dry_run      INTEGER NOT NULL DEFAULT 0 CHECK (dry_run IN (0,1)),
	params_json  TEXT NOT NULL DEFAULT '',
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
	value        TEXT NOT NULL DEFAULT '',
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
