-- 023_annotate_action.sql (sqlite dialect) — the 'a' (annotate) action bit
-- (M16 T-444, FR-146.1 / ADR-0044 K68 / architecture section 25.6): the
-- property-write half of the write verb's split.
--
-- Semantics: can_annotate is the property-write permission (the M10
-- ?properties family's PUT/DELETE gate). can_write is NOT renamed and keeps
-- its internal code 'w' — the wire word renewal (write -> deploy-cache on
-- /api/v1/permissions) is a presentation split only; deploy/cache stay one
-- merged column, isomorphic to the reference's single Deploy/Cache column.
--
-- Zero-privilege equivalence migration (ADR-0044 K68 point 3): the
-- pre-split property-write gate was `w`, so every existing can_write=1 row
-- is backfilled can_annotate=1 — the effective permission matrix over
-- (principal x repo x path x action) is bit-for-bit identical before and
-- after the migration (NFR-S77). Newly written targets may grant
-- deploy-cache without annotate (the split's new expressive power, the
-- reference's five-column parity).
--
-- Reverse migration (ADR-0044 K68 point 4): drop the column (or keep it
-- with zero consumers) and revert the wire word — can_write is untouched
-- by the whole cycle, so no data is lost in either direction.
--
-- Conventions inherit from 001_init.sql (ADR-0007): booleans as INTEGER
-- 0/1, statements inside the SQLite/Postgres common subset, no transaction
-- statements in the file body — the migrator wraps each migration in one
-- transaction.

ALTER TABLE permission_principals ADD COLUMN can_annotate INTEGER NOT NULL DEFAULT 0;

UPDATE permission_principals SET can_annotate = 1 WHERE can_write = 1;
