-- 009_replication.sql (sqlite dialect) — replication/federation schema (M6, ADR-0021).
--
-- Contract: docs/design/architecture.md section 6, "009_replication.sql" block.
-- Conventions inherit from 001_init.sql (ADR-0007): RFC3339 UTC text timestamps,
-- booleans as INTEGER 0/1, statements inside the SQLite/Postgres common subset
-- (no AUTOINCREMENT/RETURNING), and no transaction statements in the file body —
-- the migrator wraps each migration in one transaction.

-- replications: one row per replication target (push replication config).
-- A replication defines a source repo → target BinFlow instance mapping.
CREATE TABLE replications (
  id                         INTEGER PRIMARY KEY,
  name                       TEXT NOT NULL UNIQUE,       -- human-readable name
  source_repo                TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  target_url                 TEXT NOT NULL,              -- target BinFlow instance base URL (e.g. https://remote.example.com)
  target_repo                TEXT NOT NULL,              -- target repo key on the remote instance
  target_username            TEXT NOT NULL DEFAULT '',
  target_password_enc        TEXT NOT NULL DEFAULT '',   -- AES-256-GCM encrypted, same format as remote_configs.password (enc:v1:<b64>)
  max_bandwidth_bytes_per_sec INTEGER NOT NULL DEFAULT 0, -- 0 = unlimited
  max_items_per_push         INTEGER NOT NULL DEFAULT 1000, -- per-trigger item limit
  enabled                    INTEGER NOT NULL DEFAULT 1,
  created_at                 TEXT NOT NULL,
  updated_at                 TEXT NOT NULL
);
CREATE INDEX idx_replications_source ON replications(source_repo);

-- replication_tasks: individual replication job records (push).
-- Each row tracks one blob replication attempt.
CREATE TABLE replication_tasks (
  id              INTEGER PRIMARY KEY,
  replication_id  INTEGER NOT NULL REFERENCES replications(id) ON DELETE CASCADE,
  blob_sha256     TEXT NOT NULL,              -- hex sha256 of the blob being replicated
  node_path       TEXT NOT NULL,              -- repo-relative path of the node
  status          TEXT NOT NULL DEFAULT 'pending', -- pending|in_progress|success|failed|skipped
  attempts        INTEGER NOT NULL DEFAULT 0,
  last_error      TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL,
  completed_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_replication_tasks_status ON replication_tasks(replication_id, status);
CREATE INDEX idx_replication_tasks_pending ON replication_tasks(status, created_at);  -- scheduler scan