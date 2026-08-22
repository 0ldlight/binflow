-- 011_rbac_role.sql (postgres dialect) — closed-set role + manage action (M7, ADR-0026).
--
-- Contract: docs/design/architecture.md section 3.4a (migration 011 draft).
-- The statements are inside the SQLite/Postgres common subset, so the dialect
-- files carry them verbatim (ADR-0007 lockstep rule; booleans stay INTEGER 0/1
-- per the 001 convention this column family already uses).

ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
UPDATE users SET role = 'admin' WHERE is_admin = 1;
ALTER TABLE permission_principals ADD COLUMN can_manage INTEGER NOT NULL DEFAULT 0;
