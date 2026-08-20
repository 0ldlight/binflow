-- 004_console_governance.sql (sqlite dialect) — console session, groups and
-- governance schema (M4, ADR-0014/0015 + the T-108 errata).
--
-- Contract: docs/design/architecture.md section 6, "004_console_governance.sql"
-- block (final DDL — the four tables, users.email and every index must match
-- it; only comments are allowed to differ). Two column-level additions come
-- from the PRD as registered by T-108 (R2): users.email (FR-27-AC8) and the
-- audit query indexes (GE-01/NFR-P17).
-- Conventions inherit from 001_init.sql (ADR-0007): RFC3339 UTC text
-- timestamps, booleans as INTEGER 0/1, statements inside the SQLite/Postgres
-- common subset, and no transaction statements in the file body — the
-- migrator wraps each migration in one transaction and nesting one is an
-- error (T-10 review M9).

-- users widening: email (PRD FR-27-AC8). The M1 put/post validation chain is
-- unchanged (blank stays valid at the schema level; the 400-on-blank rule is
-- a service-layer concern), GET round-trips the value (T-97 SE-05/06).
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';

-- Audit query indexes (PRD GE-01: the full-parameter query face; idx_audit_time
-- and idx_audit_repo exist since 001). The composite (column, time) shape
-- serves both the equality filter and the time-ordered keyset pagination.
CREATE INDEX idx_audit_actor ON audit_events(actor, time);
CREATE INDEX idx_audit_action ON audit_events(action, time);

CREATE TABLE groups (                     -- user groups (the consumer of permission_principals rows with principal_type='group' — the M1 reserved column pays off, no DDL change there)
  id          INTEGER PRIMARY KEY,        -- sqlite: ROWID alias; postgres: SERIAL (dialect-local, ADR-0007 note)
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE user_groups (                -- membership (table name aligned with PRD section 0; T-108 errata, draft name was group_members)
  group_id  INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  username  TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  PRIMARY KEY (group_id, username)
);
-- Authorizer consumption: at authentication time a JOIN resolves the groups
-- into Principal.Groups (architecture 3.4); permission_principals group rows
-- then match the group list in Can — no DDL change (the principal_type column
-- has existed since 001). Admin surface: group CRUD lives at the compatibility
-- endpoint /api/security/groups (SE-01~04); membership is maintained through
-- the groups[] field of PUT/POST /api/security/users/{name} (unknown group ->
-- 400); deleting a group referenced by a permission target -> 409 (PRD K3).

CREATE TABLE web_sessions (               -- browser sessions (ADR-0014 decision 2 + errata 3; storage follows the tokens rule: only sha256(plaintext) is persisted)
  id_hash      TEXT PRIMARY KEY,          -- sha256(session id plaintext); the plaintext only ever travels in the binflow_session cookie
  username     TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  created_at   TEXT NOT NULL,
  expires_at   TEXT NOT NULL,             -- absolute TTL: console.session_ttl_hours default 24 (a seconds override key exists for test granularity); sliding refresh is capped by this bound
  last_used_at TEXT NOT NULL DEFAULT '',
  revoked_at   TEXT NOT NULL DEFAULT ''   -- logout = revoke; expired/revoked rows are cleaned by the startup sweep (same pattern as the sessions/ directory, architecture 11 item 18)
);
CREATE INDEX idx_web_sessions_user ON web_sessions(username);

CREATE TABLE repo_usage (                 -- quota accounting (ADR-0015 decision 2): maintained in the same transaction as node insert/delete
  repo_key      TEXT PRIMARY KEY REFERENCES repositories(repo_key) ON DELETE CASCADE,
  logical_bytes INTEGER NOT NULL DEFAULT 0,
  updated_at    TEXT NOT NULL
);
