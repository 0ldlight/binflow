-- 022_backups.sql (postgres dialect) — the backup entity payload table (M16
-- T-450, FR-150.3 / ADR-0044 decisions 2 and 5).
--
-- Mirrors the sqlite dialect file 022 one-to-one (ADR-0007 lockstep); see
-- that file's header for the full contract (the schedules-ledger split and
-- the deliberate absence of the reference's unused descriptor fields).
-- Booleans stay INTEGER 0/1 — the shipped postgres convention (009/021/024)
-- and what BackupStore's int64 0/1 binding (substores_backups.go) writes.
-- Idempotent: CREATE TABLE IF NOT EXISTS, the post-011 convention.

CREATE TABLE IF NOT EXISTS backups (
	key         TEXT NOT NULL PRIMARY KEY,  -- the backup key; equals the schedules row key (domain='backup')
	enabled     INTEGER NOT NULL DEFAULT 1,
	export_dir  TEXT NOT NULL DEFAULT '',   -- server path for the backup artifacts (one timestamped subdir per fire)
	created_at  TEXT NOT NULL,
	created_by  TEXT NOT NULL,
	updated_at  TEXT NOT NULL,
	updated_by  TEXT NOT NULL
);
