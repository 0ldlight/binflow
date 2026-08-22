-- 010_upload_sessions.sql (postgres dialect) — local-filestore upload sessions (M6, T-209).
--
-- Contract: docs/design/architecture.md section 6. PostgreSQL dialect: TEXT
-- primary key (the session id is an engine-generated uuid, not a sequence).

CREATE TABLE upload_sessions (
  id          TEXT PRIMARY KEY,
  state       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  expires_at  TEXT NOT NULL
);
CREATE INDEX idx_upload_sessions_expiry ON upload_sessions(expires_at);
