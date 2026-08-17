-- 001_init.sql (sqlite dialect) — initial BinFlow metadata schema.
--
-- Contract: docs/design/architecture.md section 6 (M1 revision, including the
-- T-22 write-back: permissions use the named-target two-table form of PRD
-- E-24, and repo keys allow [a-z][a-z0-9-]{1,62}).
--
-- Conventions (ADR-0007): timestamps are RFC3339 UTC text; booleans are
-- INTEGER 0/1; statements stay inside the SQLite/Postgres common subset (no
-- AUTOINCREMENT, no RETURNING). The schema_migrations bookkeeping table is
-- created by the migrator itself (see internal/metadata/migrate.go), not by
-- this file.

CREATE TABLE repositories (
  repo_key  TEXT PRIMARY KEY,              -- [a-z][a-z0-9-]{1,62} (PRD FR-3-AC4); reserved words api/v2 are rejected by the service layer (ADR-0008 routing)
  type      TEXT NOT NULL,                 -- 'local' | 'remote' | 'virtual'
  package_type TEXT NOT NULL,              -- 'generic' | 'docker' | 'maven' | 'npm' | 'pypi'
  description TEXT NOT NULL DEFAULT '',
  config    TEXT NOT NULL DEFAULT '{}',    -- JSON: type-specific config (remote.url etc, [M3])
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX idx_repositories_type ON repositories(type, package_type);

-- Remote proxy config [M3]; table created in M1 but unused (evolving both
-- dialects in lockstep is cheaper than a later structural migration).
CREATE TABLE remote_configs (
  repo_key   TEXT PRIMARY KEY REFERENCES repositories(repo_key) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  username   TEXT NOT NULL DEFAULT '',
  password   TEXT NOT NULL DEFAULT '',     -- encrypted storage [M3: key scheme TBD]
  cache_ttl_seconds INTEGER NOT NULL DEFAULT 0,
  unreachable_mask BOOLEAN NOT NULL DEFAULT 0
);

CREATE TABLE blobs (
  sha256     TEXT PRIMARY KEY,             -- hex lowercase, 64 chars
  sha1       TEXT NOT NULL DEFAULT '',
  md5        TEXT NOT NULL DEFAULT '',
  size       INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE nodes (
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  path      TEXT NOT NULL,                 -- repo-relative path, '/' separated, no leading '/'
  sha256    TEXT NOT NULL REFERENCES blobs(sha256),
  size      INTEGER NOT NULL,
  mime      TEXT NOT NULL DEFAULT 'application/octet-stream',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (repo_key, path)
);
CREATE INDEX idx_nodes_blob ON nodes(sha256);   -- GC anti-join & delete-blob precheck

CREATE TABLE users (
  username        TEXT PRIMARY KEY,
  password_hash   TEXT NOT NULL,           -- argon2id encoded string (PHC format)
  is_admin        INTEGER NOT NULL DEFAULT 0,
  enabled         INTEGER NOT NULL DEFAULT 1,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);

CREATE TABLE tokens (
  id           INTEGER PRIMARY KEY,        -- sqlite: ROWID alias; postgres: SERIAL (dialect-local, ADR-0007 note)
  username     TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  token_sha256 TEXT NOT NULL UNIQUE,       -- sha256(plaintext); plaintext is shown once at issue time (NFR-S2)
  expires_at   TEXT NOT NULL,              -- RFC3339; '9999-12-31T00:00:00Z' means never expires
  created_at   TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_tokens_user ON tokens(username);

-- Permission model: named permission targets (PRD E-24, M1 final form after
-- the T-22 architecture write-back).
CREATE TABLE permission_targets (
  name       TEXT PRIMARY KEY,             -- target name, unique (e.g. 'ci-out-rw')
  repos      TEXT NOT NULL DEFAULT '[]',   -- JSON array: applicable repo keys ('*' unused, explicit list)
  includes   TEXT NOT NULL DEFAULT '[]',   -- JSON array: includePatterns, '**'/'*' two-level wildcards
  excludes   TEXT NOT NULL DEFAULT '[]',   -- JSON array: excludePatterns; an exclude hit wins over include
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE permission_principals (       -- target x principal x actions (users; groups [M4])
  id          INTEGER PRIMARY KEY,
  target_name TEXT NOT NULL REFERENCES permission_targets(name) ON DELETE CASCADE,
  principal   TEXT NOT NULL,               -- username (groups distinguished by principal_type [M4])
  principal_type TEXT NOT NULL DEFAULT 'user',
  can_read    INTEGER NOT NULL DEFAULT 0,  -- actions in read|write|delete; write covers upload, not delete
  can_write   INTEGER NOT NULL DEFAULT 0,
  can_delete  INTEGER NOT NULL DEFAULT 0,
  UNIQUE (target_name, principal, principal_type)
);
-- Differences vs the abandoned flat permission rows (architecture section 6):
-- (1) targets carry a name so /binflow/api/v1/permissions can CRUD them and
--     dropping a target drops its grants wholesale;
-- (2) many repos plus include/exclude pattern pairs replace the single
--     include-only path_prefix;
-- (3) principals live in their own table, so groups later only add rows.

CREATE TABLE audit_events (
  id         INTEGER PRIMARY KEY,
  time       TEXT NOT NULL,
  actor      TEXT NOT NULL,
  action     TEXT NOT NULL,                -- deploy|delete|download|login.success|login.failed|repo.create|...
  repo_key   TEXT NOT NULL DEFAULT '',
  path       TEXT NOT NULL DEFAULT '',
  detail     TEXT NOT NULL DEFAULT '{}'    -- JSON
);
CREATE INDEX idx_audit_time ON audit_events(time);
CREATE INDEX idx_audit_repo ON audit_events(repo_key, time);

-- Virtual repository resolution order [M3]
CREATE TABLE virtual_members (
  virtual_repo TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  member_repo  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  position     INTEGER NOT NULL,
  PRIMARY KEY (virtual_repo, member_repo)
);
