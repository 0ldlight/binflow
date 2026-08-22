-- 010_upload_sessions.sql (sqlite dialect) — local-filestore upload sessions (M6, T-209).
--
-- Contract: docs/design/architecture.md section 6, "010_upload_sessions.sql" block.
-- Conventions inherit from 001_init.sql (ADR-0007): RFC3339 UTC text timestamps,
-- no transaction statements in the file body — the migrator wraps each migration
-- in one transaction.

-- upload_sessions: one row per live local-filestore upload session. The disk
-- engine owns the row lifecycle (ADR-0006 decision 2, amended for DB persistence:
-- the server process can restart and ResumeSession re-materializes a session from
-- this row plus the data file on disk, re-hashing the partial bytes to rebuild the
-- digest chain — digest state is not serializable).
CREATE TABLE upload_sessions (
  id          TEXT PRIMARY KEY,       -- session uuid (engine-generated, version 4)
  state       TEXT NOT NULL DEFAULT '', -- opaque JSON owned by the engine (created_at/received); never parsed by the metadata layer
  created_at  TEXT NOT NULL,          -- RFC3339 UTC
  expires_at  TEXT NOT NULL           -- RFC3339 UTC; startup sweep deletes rows past this
);
CREATE INDEX idx_upload_sessions_expiry ON upload_sessions(expires_at);