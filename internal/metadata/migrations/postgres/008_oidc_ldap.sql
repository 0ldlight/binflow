-- 008_oidc_ldap.sql (postgres dialect) — OIDC/LDAP identity provider columns (M6, ADR-0020).
--
-- Contract: docs/design/architecture.md section 6, "008_oidc_ldap.sql" block.
-- PostgreSQL dialect: partial unique index for non-local provider uniqueness.

ALTER TABLE users ADD COLUMN provider TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN provider_id TEXT NOT NULL DEFAULT '';

-- PostgreSQL supports partial/conditional unique indexes.
-- Only non-local users (provider != 'local') enforce uniqueness on (provider, provider_id).
CREATE UNIQUE INDEX idx_users_provider ON users(provider, provider_id) WHERE provider != 'local';
