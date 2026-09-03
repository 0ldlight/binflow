-- 021_schedules.sql (sqlite dialect) — the unified cron schedules ledger
-- (M16 T-446, FR-150.1 / ADR-0044 decisions 2 and 3): one row per scheduled
-- full-type job, keyed by (domain, key).
--
-- domain is the closed set of consuming planes ADR-0044 decision 1 pins
-- (maintenance | backup | replication); a new domain is an incremental
-- registration (a CHECK list edit in a later migration), never a schema
-- change — the ADR-0041 event-closed-set discipline lifted onto scheduling.
-- key is the in-domain entity key: the maintenance job slot name
-- (gc | cleanup-unused-cache | cleanup-virtual), the backup key, or the
-- replication config id.
--
-- "No row = not scheduled" is the single-state semantics (ADR-0044 decision
-- 2): clearing a cronExp or deleting the entity removes the row in the same
-- transaction — there is deliberately NO "row that never fires" shape. No
-- rows are preseeded (zero-silent-writes posture; Artifactory's factory
-- backup-daily/backup-weekly defaults are a registered intentional
-- difference, architecture sections 10/25.7).
--
-- next_run_at is RFC3339 UTC; '' means unscheduled (the disabled state —
-- a disabled row keeps its cron_expr so re-enabling recomputes from now).
-- last_status is the closed set ('', ok, failed). Dialect conventions of
-- 001/009/018 (ADR-0007): RFC3339 UTC text timestamps, booleans as INTEGER
-- 0/1, lexicographic time comparison (the (enabled, next_run_at) index is
-- the tick query's only predicate — the webhook deliveries queue shape).
-- Idempotent: CREATE TABLE IF NOT EXISTS, matching the post-011 convention
-- the t212 rewind test replays under.

CREATE TABLE IF NOT EXISTS schedules (
	domain      TEXT NOT NULL CHECK (domain IN ('maintenance','backup','replication')),
	key         TEXT NOT NULL,               -- in-domain entity key (see above)
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
