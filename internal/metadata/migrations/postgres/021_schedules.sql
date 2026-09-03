-- 021_schedules.sql (postgres dialect) — the unified cron schedules ledger
-- (M16 T-446, FR-150.1 / ADR-0044 decisions 2 and 3).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). Semantics,
-- column names and the closed sets match the sqlite dialect file
-- one-to-one; see that file's header for the full contract.

CREATE TABLE IF NOT EXISTS schedules (
	domain      TEXT NOT NULL CHECK (domain IN ('maintenance','backup','replication')),
	key         TEXT NOT NULL,               -- in-domain entity key (see the sqlite file)
	cron_expr   TEXT NOT NULL,
	enabled     INTEGER NOT NULL DEFAULT 1,
	next_run_at TEXT NOT NULL,               -- RFC3339 UTC; '' = unscheduled (disabled)
	last_run_at TEXT NOT NULL DEFAULT '',
	last_status TEXT NOT NULL DEFAULT '' CHECK (last_status IN ('','ok','failed')),
	last_error  TEXT NOT NULL DEFAULT '',    -- failure summary, truncated by the writer
	created_at  TEXT NOT NULL,
	created_by  TEXT NOT NULL,
	updated_at  TEXT NOT NULL,
	updated_by  TEXT NOT NULL,
	PRIMARY KEY (domain, key)
);

CREATE INDEX IF NOT EXISTS idx_schedules_due ON schedules (enabled, next_run_at);
