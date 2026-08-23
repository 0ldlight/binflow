-- 009_replication.sql (postgres dialect) — replication/federation schema (M6, ADR-0021).
--
-- Contract: docs/design/architecture.md section 6, "009_replication.sql" block.
-- PostgreSQL dialect: SERIAL for auto-increment columns, standard SQL.

CREATE TABLE replications (
  id                         SERIAL PRIMARY KEY,
  name                       TEXT NOT NULL UNIQUE,
  source_repo                TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  target_url                 TEXT NOT NULL,
  target_repo                TEXT NOT NULL,
  target_username            TEXT NOT NULL DEFAULT '',
  target_password_enc        TEXT NOT NULL DEFAULT '',
  max_bandwidth_bytes_per_sec INTEGER NOT NULL DEFAULT 0,
  max_items_per_push         INTEGER NOT NULL DEFAULT 1000,
  enabled                    INTEGER NOT NULL DEFAULT 1,
  created_at                 TEXT NOT NULL,
  updated_at                 TEXT NOT NULL
);
CREATE INDEX idx_replications_source ON replications(source_repo);

CREATE TABLE replication_tasks (
  id              SERIAL PRIMARY KEY,
  replication_id  INTEGER NOT NULL REFERENCES replications(id) ON DELETE CASCADE,
  blob_sha256     TEXT NOT NULL,
  node_path       TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending',
  attempts        INTEGER NOT NULL DEFAULT 0,
  last_error      TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL,
  completed_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_replication_tasks_status ON replication_tasks(replication_id, status);
CREATE INDEX idx_replication_tasks_pending ON replication_tasks(status, created_at);
