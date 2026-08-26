-- 015_auth_configs.sql (sqlite dialect) — the authentication configuration
-- descriptor table (M11 T-305, ADR-0035 decision 2 / FR-92).
--
-- Contract: section-granularity integral replacement. section is the closed
-- set of the three protocol planes ('ldap', 'oidc', 'saml' — BinFlow
-- internal names; the wire spellings map at the REST layer). doc is the
-- section's JSON text under the write path's strict schema (unknown keys
-- refused, the config package decodeRaw posture); secrets inside doc are
-- ALWAYS 'enc:v1:'-sealed (AES-256-GCM under BINFLOW_REMOTE_CREDENTIALS_KEY,
-- the instance master key) — plaintext secrets never land in this table.
-- Every PUT replaces the whole row (PutLicense's replace-in-one-statement
-- semantics); there is no field-level merge and no delete (disablement is
-- the enabled flag inside the doc, immediate through the same write path).
--
-- updated_at/updated_by carry the audit-provenance projections of the last
-- writer (RFC3339 UTC text / principal name); timestamps follow the 001
-- conventions. Idempotent: CREATE TABLE IF NOT EXISTS, matching the
-- migrator's reopen-skips-applied guarantee (ADR-0007).

CREATE TABLE IF NOT EXISTS auth_configs (
  section     TEXT PRIMARY KEY,
  doc         TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  updated_by  TEXT NOT NULL
);
