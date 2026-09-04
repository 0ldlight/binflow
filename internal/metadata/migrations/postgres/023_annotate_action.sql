-- 023_annotate_action.sql (postgres dialect) — the 'a' (annotate) action bit
-- (M16 T-444, FR-146.1 / ADR-0044 K68 / architecture section 25.6).
-- The statements are inside the SQLite/Postgres common subset, so the
-- dialect files carry them verbatim (ADR-0007 lockstep rule; booleans stay
-- INTEGER 0/1 per the 001 convention this column family already uses).
-- Semantics and the zero-privilege backfill: see the sqlite dialect file.

ALTER TABLE permission_principals ADD COLUMN can_annotate INTEGER NOT NULL DEFAULT 0;

UPDATE permission_principals SET can_annotate = 1 WHERE can_write = 1;
