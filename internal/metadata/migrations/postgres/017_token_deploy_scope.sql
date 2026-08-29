-- 017_token_deploy_scope.sql (postgres dialect) — the narrow-scope column on
-- tokens (M12 T-349, FR-113.3 / ADR-0039 residual 1).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). Semantics,
-- column names and the fail-closed posture match the sqlite dialect file
-- one-to-one; see that file's header for the full contract.

ALTER TABLE tokens ADD COLUMN deploy_scope TEXT NOT NULL DEFAULT '';
