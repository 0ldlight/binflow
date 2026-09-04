-- 022_backups.sql (sqlite dialect) — the backup entity payload table (M16
-- T-450, FR-150.3 / ADR-0044 decisions 2 and 5, axis-5-A): one row per
-- scheduled backup configuration. The CRON half of the entity lives in the
-- 021 schedules ledger (domain='backup', key = this table's key) — this
-- table carries only the payload the export carrier consumes at fire time
-- (the enabled bit and the server path the artifacts land under).
--
-- Artifactory's backup descriptor fields that have no BinFlow carrier
-- (repository subsets — BinFlow's export is the whole-instance snapshot —
-- plus incremental, retention-period rotation, zip archival and the
-- mail-on-error flag) are deliberately absent rather than stored dead
-- (the no-fabrication posture; registered as a C-layer difference in the
-- T-450 report). No rows are preseeded (the 021 zero-silent-writes
-- posture; Artifactory's factory backup-daily/backup-weekly defaults are
-- an intentional difference, architecture sections 10/25.7).
--
-- Dialect conventions of 001/021 (ADR-0007): RFC3339 UTC text timestamps,
-- booleans as INTEGER 0/1. Idempotent: CREATE TABLE IF NOT EXISTS, matching
-- the post-011 convention the t212 rewind test replays under.

CREATE TABLE IF NOT EXISTS backups (
	key         TEXT NOT NULL PRIMARY KEY,  -- the backup key; equals the schedules row key (domain='backup')
	enabled     INTEGER NOT NULL DEFAULT 1,
	export_dir  TEXT NOT NULL DEFAULT '',   -- server path for the backup artifacts (one timestamped subdir per fire)
	created_at  TEXT NOT NULL,
	created_by  TEXT NOT NULL,
	updated_at  TEXT NOT NULL,
	updated_by  TEXT NOT NULL
);
