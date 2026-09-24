-- 005_quota_usage_backfill.sql (postgres dialect) — seed the quota accounting
-- rows from existing node state (M4, T-95 / GE-05, ADR-0015 decision 2).
--
-- Mirrors the sqlite dialect file 005 one-to-one (ADR-0007 lockstep); see
-- that file's header for the full contract. The only dialect deltas are the
-- RFC3339 UTC timestamp expression (to_char(now() AT TIME ZONE 'utc', ...)
-- replacing strftime) — ON CONFLICT (repo_key) DO NOTHING is native
-- postgres syntax carried over verbatim.
--
-- ON CONFLICT DO NOTHING: a usage row can only pre-exist if the T-95 service
-- code already wrote it, and that row is by construction more current than
-- this snapshot. updated_at here is informational only.
INSERT INTO repo_usage (repo_key, logical_bytes, updated_at)
SELECT repo_key, COALESCE(SUM(size), 0), to_char(now() AT TIME ZONE 'utc', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
FROM nodes
GROUP BY repo_key
ON CONFLICT (repo_key) DO NOTHING;
