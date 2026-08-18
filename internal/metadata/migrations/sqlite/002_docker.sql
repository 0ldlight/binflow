-- 002_docker.sql (sqlite dialect) — docker domain tables (M2, ADR-0010).
--
-- Contract: docs/design/architecture.md section 6, "002_docker.sql" block
-- (final DDL — column set, primary keys, indexes and FK declarations must
-- match it; only comments are allowed to differ). Conventions inherit from
-- 001_init.sql (ADR-0007): RFC3339 UTC text timestamps, statements inside
-- the SQLite/Postgres common subset, and no transaction statements in the
-- file body — the migrator wraps each migration in one transaction and
-- nesting one is an error (T-10 review M9).
--
-- docker artifact node layout convention (no new table; nodes is reused):
--   manifest blob node path = "<image>/manifests/<digest-hex>";
--   layer/config blob node path = "<image>/blobs/<digest-hex>". image is the
--   repository-relative name (name minus the repo key first segment, may
--   contain '/'). Digest addressing hits the nodes primary key directly, no
--   JOIN needed.

CREATE TABLE docker_manifests (           -- manifest metadata (the manifest body itself is a plain blob/node)
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  image     TEXT NOT NULL,                -- repository-relative image name (without the repo key first segment)
  digest    TEXT NOT NULL,                -- bare hex sha256 (same keyspace as blobs.sha256)
  media_type TEXT NOT NULL,               -- one of the architecture 5.3 media types (validation lives in the service layer)
  size      INTEGER NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY (repo_key, image, digest)
);
CREATE INDEX idx_docker_manifests_image ON docker_manifests(repo_key, image);

CREATE TABLE docker_tags (                -- tag -> digest pointer (mutable, repointable)
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  image     TEXT NOT NULL,
  tag       TEXT NOT NULL,                -- 'latest' etc.; charset per spec ([a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}), enforced by the service layer
  digest    TEXT NOT NULL,                -- -> docker_manifests.digest (logical FK only: a cross-table FK covering partial columns of a composite primary key is not declared, integrity is the service layer's job)
  updated_by TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (repo_key, image, tag)
);
CREATE INDEX idx_docker_tags_image ON docker_tags(repo_key, image);

CREATE TABLE docker_refs (                -- manifest <-> blob reference ledger (config/layers; second source of GC reference facts)
  repo_key TEXT NOT NULL,                 -- no DB-level FK: consistency contract in doc.go (architecture 11.12)
  image     TEXT NOT NULL,
  manifest_digest TEXT NOT NULL,          -- referencing side
  blob_digest     TEXT NOT NULL,          -- referenced side (config or layer; child_media_type records the role)
  child_media_type TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (repo_key, image, manifest_digest, blob_digest)
);
CREATE INDEX idx_docker_refs_blob ON docker_refs(blob_digest);  -- pre-delete-blob reference check (same role as idx_nodes_blob)
-- GC impact (architecture 4.4 addendum): the mark-phase reference set grows
-- from "SELECT DISTINCT sha256 FROM nodes" to additionally UNION "SELECT
-- DISTINCT blob_digest FROM docker_refs" — layers/config referenced by a
-- manifest must not be reclaimed even when they carry no node row of their
-- own. docker_manifests/docker_tags rows follow the nodes cascade semantics
-- maintained by the service layer.
