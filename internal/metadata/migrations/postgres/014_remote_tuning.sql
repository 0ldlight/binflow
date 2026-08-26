-- 014_remote_tuning.sql (postgres dialect) — the smart remote effective-field
-- widening of remote_configs (M10 T-290, FR-90.2 / architecture 15.4).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). Semantics,
-- column names and the 0-means-unset rule match the sqlite dialect file
-- one-to-one; see that file's header for the full contract.

ALTER TABLE remote_configs ADD COLUMN socket_timeout_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE remote_configs ADD COLUMN metadata_retrieval_timeout_secs INTEGER NOT NULL DEFAULT 0;
ALTER TABLE remote_configs ADD COLUMN unused_cleanup_period_hours INTEGER NOT NULL DEFAULT 0;
