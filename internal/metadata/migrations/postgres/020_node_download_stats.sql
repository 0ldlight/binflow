-- 020_node_download_stats.sql (postgres dialect) — the per-node download
-- statistics widening of nodes (M16 T-438, FR-146.2 / ADR-0044 K69,
-- architecture section 25.5).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). Semantics,
-- column names and the zero-defaults rule match the sqlite dialect file
-- one-to-one; see that file's header for the full contract.

ALTER TABLE nodes ADD COLUMN download_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN last_downloaded_at TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN last_downloaded_by TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN remote_download_count INTEGER NOT NULL DEFAULT 0;
