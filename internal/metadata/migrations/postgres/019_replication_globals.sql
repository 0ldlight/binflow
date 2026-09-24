-- 019_replication_globals.sql (postgres dialect) — the global
-- blockPush/blockPull emergency-brake row (M15 T-422, FR-138.3 /
-- replication.md §9.1-B/§9.2-B — the blocksystemreplication counterpart).
--
-- Mirrors the sqlite dialect file 019 one-to-one (ADR-0007 lockstep); see
-- that file's header for the full contract. INTEGER PRIMARY KEY with a
-- CHECK pin is dialect-common (the same single-row shape as 012_license);
-- booleans stay INTEGER 0/1 per the shipped postgres convention
-- (009/021/024). Idempotent: CREATE TABLE IF NOT EXISTS, the post-011
-- convention.

CREATE TABLE IF NOT EXISTS replication_globals (
    id         INTEGER PRIMARY KEY CHECK (id = 1),  -- the single row
    block_push INTEGER NOT NULL DEFAULT 0,          -- 1 = push replication blocked
    block_pull INTEGER NOT NULL DEFAULT 0,          -- 1 = pull replication blocked
    updated_at TEXT NOT NULL,                       -- RFC3339 UTC, last persist
    updated_by TEXT NOT NULL DEFAULT ''             -- actor of the last REST flip ('' = boot seed)
);
