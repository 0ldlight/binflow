-- 015_auth_configs.sql (postgres dialect) — the authentication configuration
-- descriptor table (M11 T-305, ADR-0035 decision 2 / FR-92).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). Semantics,
-- column names and the section-closed-set rule match the sqlite dialect
-- file one-to-one; see that file's header for the full contract.

CREATE TABLE IF NOT EXISTS auth_configs (
  section     TEXT PRIMARY KEY,
  doc         TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  updated_by  TEXT NOT NULL
);
