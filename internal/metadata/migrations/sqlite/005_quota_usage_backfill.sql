-- 005_quota_usage_backfill.sql (sqlite dialect) — seed the quota accounting
-- rows from existing node state (M4, T-95 / GE-05, ADR-0015 decision 2).
--
-- The repo_usage table (004) is maintained IN THE SAME TRANSACTION as node
-- insert/delete by the T-95 service code. Databases upgraded from an M1~M3
-- baseline carry nodes but no usage rows, so the counter would forever
-- under-report (every future adjust only applies a delta). This migration
-- seeds one row per repository with the SUM of its node sizes — the logical
-- byte total of architecture section 4.6 (remote cache nodes included per
-- that section's wording; ongoing remote pull-through writes stay unmetered
-- per PRD section 7 Q2).
--
-- The timestamp is a sqlite-expression (migrations cannot take parameters):
-- strftime renders RFC3339 UTC (YYYY-MM-DDTHH:MM:SSZ), the column format of
-- every metadata table (ADR-0007). updated_at here is informational only.
--
-- ON CONFLICT DO NOTHING: a usage row can only pre-exist if the T-95 service
-- code already wrote it, and that row is by construction more current than
-- this snapshot. Conventions inherit from 001_init.sql: no transaction
-- statements in the file body (the migrator wraps each migration in one).
INSERT INTO repo_usage (repo_key, logical_bytes, updated_at)
SELECT repo_key, COALESCE(SUM(size), 0), strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM nodes
GROUP BY repo_key
ON CONFLICT (repo_key) DO NOTHING;
