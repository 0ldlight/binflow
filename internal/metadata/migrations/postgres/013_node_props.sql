-- 013_node_props.sql (postgres dialect) — the artifact properties table
-- (M10 T-286, FR-89 / ADR-0033 / architecture section 15.3.2).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). The composite
-- foreign key rides nodes' existing (repo_key, path) primary key; both
-- dialects accept the table-level FOREIGN KEY form.

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
