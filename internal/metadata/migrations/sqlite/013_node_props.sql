-- 013_node_props.sql (sqlite dialect) — the artifact properties table
-- (M10 T-286, FR-89 / ADR-0033 / architecture section 15.3.2).
--
-- Contract: the nodes association table (option A of section 15.3.2's
-- candidate comparison — the Artifactory-isomorphic shape, inv-4 B6).
-- Multi-value is multi-row: a property is a SET of values, each value one
-- row under the same (repo_key, path, name) key head, so the set semantics
-- never need a JSON column and M11+'s property search (search/props, AQL,
-- cleanup policies, replication property sync) gets an indexable dimension.
-- The nodes rows themselves are untouched (hot rows stay thin).
--
-- FKs: repo_key cascades repository teardown; the composite (repo_key,
-- path) FK rides nodes' existing PRIMARY KEY and cascades every node
-- delete — NodeStore.Delete/DeleteByPrefix clear the property rows in the
-- same statement, the "node 删除同事务清 node_props" guarantee of section
-- 15.3.2 by construction rather than by service-layer bookkeeping.
-- Inserting properties for a node that does not exist fails on the FK
-- (the store refuses orphan annotations).
--
-- Limits (per node <=64 keys, per key <=32 values) are enforced by the
-- service layer (repo.ValidatePropSet) — the metadata bomb guard FR-89.3.
--
-- Idempotent: CREATE TABLE IF NOT EXISTS, matching the migrator's
-- reopen-skips-applied guarantee (ADR-0007).

CREATE TABLE IF NOT EXISTS node_props (
  repo_key TEXT NOT NULL,
  path     TEXT NOT NULL,                    -- repo-relative, '/' separated, no leading '/'
  name     TEXT NOT NULL,                    -- key, [A-Za-z][A-Za-z0-9_.-]{0,63}
  value    TEXT NOT NULL,                    -- one value of the key's set
  PRIMARY KEY (repo_key, path, name, value),
  FOREIGN KEY (repo_key) REFERENCES repositories(repo_key) ON DELETE CASCADE,
  FOREIGN KEY (repo_key, path) REFERENCES nodes(repo_key, path) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_node_props_name ON node_props(name, value);
