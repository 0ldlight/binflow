-- 008_oidc_ldap.sql (sqlite dialect) — OIDC/LDAP identity provider columns (M6, ADR-0020).
--
-- Contract: docs/design/architecture.md section 6, "008_oidc_ldap.sql" block.
-- Conventions inherit from 001_init.sql (ADR-0007): RFC3339 UTC text timestamps,
-- booleans as INTEGER 0/1, statements inside the SQLite/Postgres common subset
-- (no AUTOINCREMENT/RETURNING), and no transaction statements in the file body —
-- the migrator wraps each migration in one transaction.

-- provider: the identity provider that owns this user row.
-- 'local' (default) = local password/token user, unchanged semantics.
-- 'oidc'  = OIDC-authenticated user (password_hash empty, no local password).
-- 'ldap'  = LDAP-authenticated user (password_hash empty, no local password).
ALTER TABLE users ADD COLUMN provider TEXT NOT NULL DEFAULT 'local';

-- provider_id: the stable identifier assigned by the identity provider.
-- OIDC: the 'sub' claim from the ID Token.
-- LDAP:  the user's DN (distinguished name).
-- Must be empty for local users (default).
ALTER TABLE users ADD COLUMN provider_id TEXT NOT NULL DEFAULT '';

-- Unique index on (provider, provider_id) for non-local users.
-- SQLite does not support partial/conditional indexes (WHERE provider != 'local'),
-- so the UNIQUE constraint covers all rows. For local users, provider_id is always
-- '' (empty string), which means two local users with empty provider_id would collide
-- if the index were a simple UNIQUE. The index is therefore a plain non-unique index
-- in SQLite; uniqueness for non-local users is enforced in the service layer.
-- The index still accelerates the OIDC/LDAP login lookup (provider + provider_id).
CREATE INDEX idx_users_provider ON users(provider, provider_id);
