-- 011_rbac_role.sql (sqlite dialect) — closed-set role + manage action (M7, ADR-0026).
--
-- Contract: docs/design/architecture.md section 3.4a (migration 011 draft) and
-- ADR-0026 decision 6. Conventions inherit from 001_init.sql (ADR-0007):
-- booleans as INTEGER 0/1, statements inside the SQLite/Postgres common subset,
-- and no transaction statements in the file body — the migrator wraps each
-- migration in one transaction.

-- users.role: the closed-set system role ('admin' | 'readonly_admin' | 'user').
-- The closed set lives in code (auth.Role*, ADR-0026 decision 1); the column
-- stores the spelling verbatim and unknown values normalize to 'user' at the
-- auth boundary. DEFAULT 'user' is the pre-RBAC default role.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';

-- Backfill: every existing admin keeps being one. is_admin stays behind as a
-- compatibility mirror (role = 'admin' <=> is_admin = 1, maintained in the
-- SAME statement by every write path; removal planned M8, ADR-0026 decision 6).
UPDATE users SET role = 'admin' WHERE is_admin = 1;

-- permission_principals.can_manage: the 'm' (manage) action bit — repo-scoped
-- admin for the targets listing the repo (ADR-0026 decision 3). m matches on
-- repos[] only; includes/excludes are path-plane concepts and never apply to
-- it (docs/reverse/auth-model.md section 4: manage is an ACE action without a
-- path subdomain).
ALTER TABLE permission_principals ADD COLUMN can_manage INTEGER NOT NULL DEFAULT 0;
