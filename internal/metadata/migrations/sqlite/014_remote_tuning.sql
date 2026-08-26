-- 014_remote_tuning.sql (sqlite dialect) — the smart remote effective-field
-- widening of remote_configs (M10 T-290, FR-90.2 / architecture 15.4).
--
-- Contract: three operational columns mirror the canonical repositories
-- .config JSON the same way content_ttl_seconds already mirrors
-- retrievalCachePeriodSecs (003): the JSON stays the echo/policy source a
-- GET hands back, the columns are what the fetcher reads at request time.
-- 0 means "unset" everywhere — the fetcher falls back to the legacy
-- socketTimeoutSecs JSON field and then the product defaults
-- (socketTimeoutMillis 15000 / metadataRetrievalTimeoutSecs 60, PRD v1.2 C4
-- per repo-semantics 7.1) — so pre-014 rows keep their exact behavior with
-- zero backfill.
--
--   socket_timeout_ms            millisecond-granularity upstream IO timeout
--                                (socketTimeoutMs / socketTimeoutMillis on
--                                the wire; the M3 socketTimeoutSecs field
--                                cannot spell sub-second values)
--   metadata_retrieval_timeout_secs  per-repository singleflight wait cap
--                                for metadata-class paths (the engine-level
--                                60s default becomes per-repository)
--   unused_cleanup_period_hours  unused-artifact cache cleanup period
--                                (P2 field-only landing: the column and the
--                                config face exist, the cleanup engine is
--                                M11 "replication hardening" — 0 = off, the
--                                repo-semantics 7.1 default)
--
-- enableTokenAuthentication / contentSynchronisation are deliberately NOT
-- here: PRD FR-90.2 rules them to M11 and the config plane refuses them
-- (400) rather than parking inert values.
--
-- Idempotency is the migrator's version ledger (ADR-0007 reopen-skips-
-- applied), the same guarantee 003's identical ALTER family relies on.

ALTER TABLE remote_configs ADD COLUMN socket_timeout_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE remote_configs ADD COLUMN metadata_retrieval_timeout_secs INTEGER NOT NULL DEFAULT 0;
ALTER TABLE remote_configs ADD COLUMN unused_cleanup_period_hours INTEGER NOT NULL DEFAULT 0;
