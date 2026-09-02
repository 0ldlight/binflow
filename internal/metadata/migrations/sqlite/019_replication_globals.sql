-- 019_replication_globals.sql (sqlite dialect) — the global blockPush/
-- blockPull emergency-brake row (M15 T-422, FR-138.3 / replication.md
-- §9.1-B/§9.2-B — the blocksystemreplication counterpart).
--
-- ONE row, id pinned to 1 by the CHECK constraint: the state is a pair of
-- instance-wide direction flags, not a collection (Artifactory's central
-- descriptor carries the same two booleans, §9.2-B-2 写入即持久). Every
-- REST flip rewrites the row; boot adopts it when present and otherwise
-- seeds it once from the binflow.yaml replication.block_push/block_pull
-- keys (the K31 dual-source posture: database-managed from the first boot).
--
-- Dialect conventions of 001/009 (ADR-0007): RFC3339 UTC text timestamps,
-- booleans as INTEGER 0/1, no RETURNING. Idempotent: CREATE TABLE IF NOT
-- EXISTS, matching the post-011 convention the t212 rewind test replays
-- under.

CREATE TABLE IF NOT EXISTS replication_globals (
    id         INTEGER PRIMARY KEY CHECK (id = 1),  -- the single row
    block_push INTEGER NOT NULL DEFAULT 0,          -- 1 = push replication blocked
    block_pull INTEGER NOT NULL DEFAULT 0,          -- 1 = pull replication blocked
    updated_at TEXT NOT NULL,                       -- RFC3339 UTC, last persist
    updated_by TEXT NOT NULL DEFAULT ''             -- actor of the last REST flip ('' = boot seed)
);
