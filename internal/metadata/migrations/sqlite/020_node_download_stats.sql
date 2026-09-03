-- 020_node_download_stats.sql (sqlite dialect) — the per-node download
-- statistics widening of nodes (M16 T-438, FR-146.2 / ADR-0044 K69,
-- architecture section 25.5; the DDL comments there are the contract).
--
--   download_count         per-node download counter (three-arm criterion:
--                          direct / via virtual — the count lands on the
--                          MEMBER's row / remote-cache serving)
--   last_downloaded_at     RFC3339 UTC of the latest download, '' = never
--   last_downloaded_by     principal name of the latest downloader,
--                          'anonymous' for unauthenticated access, '' = never
--   remote_download_count  downloads this REMOTE repository served as the
--                          serving point (local rows keep a structural 0) —
--                          BinFlow's own cache-serving metric, deliberately
--                          NOT Artifactory's smart-remote pull-back
--                          remote_downloads (that family has no source here,
--                          docs/reverse/aql.md section 14.1)
--
-- Folder rows stay at zero by construction: the counting UPDATE excludes the
-- shared folder-marker blob, so no caller needs its own folder guard.
--
-- The four columns are the SINGLE counting channel of the download plane
-- (ADR-0044 K69 decision 1's single-source contract): the ?stats wire face,
-- the batch-3 field family and the FR-148 usage domain all read them, and
-- nothing else counts. Zero backfill: pre-020 rows mean "never downloaded",
-- which is exactly what the defaults say.
--
-- Idempotency is the migrator's version ledger (ADR-0007 reopen-skips-
-- applied), the same guarantee 014's ALTER family relies on.

ALTER TABLE nodes ADD COLUMN download_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN last_downloaded_at TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN last_downloaded_by TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN remote_download_count INTEGER NOT NULL DEFAULT 0;
