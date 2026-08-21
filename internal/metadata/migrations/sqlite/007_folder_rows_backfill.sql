-- 007_folder_rows_backfill.sql (sqlite dialect) — materialize the folder row
-- of every ancestor directory of every stored node (M5, T-128 / FR-44,
-- ADR-0016 decision 4).
--
-- Databases written before ADR-0016 carry file rows whose parent directories
-- never got a folder row (putNode materialized nothing), so implicit
-- directories 404 on the storage plane and pruneEmptyParents has no rows to
-- prune. The service write path now maintains the invariant
-- (repo.materializeAncestors); this migration backfills history once.
--
-- Step 1 — sentinel blob row FIRST, but only when there is anything to
-- backfill: nodes.sha256 references blobs(sha256), so a folder row cannot
-- land before the marker row exists (the FK is enforced per connection;
-- this ordering is the entire point of the migration). The EXISTS guard
-- repeats the walk's seed predicate, so a database with no ancestor to
-- materialize gains NOTHING — fresh databases keep the pre-007 shape (no
-- marker row until the first folder write seeds it), and Blobs().Count /
-- FilterUnreferenced baselines stay put. The row is byte-identical to
-- ensureFolderLedger's (size 0, empty sha1/md5, RFC3339 created_at);
-- INSERT OR IGNORE keeps a marker that a pre-existing explicit mkdir
-- already seeded untouched.
--
-- Step 2 — recursive walk: every node row contributes its created_at to
-- each of its ancestor directories (seed: first segment + the rest;
-- step: consume one more segment). created_at/updated_at of a backfilled
-- folder row take the YOUNGEST descendant's created_at (MAX) — folder
-- mtime follows its descendants, the same direction the write path's
-- idempotent refresh uses — which makes the value a pure function of the
-- stored rows: deterministic across replays. created_by='' — historical
-- materialization has no attributable actor (runtime rows attribute the
-- triggering writer; the backfill has none).
--
-- A folder row already present from an explicit mkdir is its own correct
-- shape and stays untouched (INSERT OR IGNORE); its path still contributes
-- ancestors for the levels above it. Idempotent by construction: a replay
-- of the whole body inserts nothing. Conventions inherit from 001_init.sql:
-- no transaction statements in the file body (the migrator wraps each
-- migration in one).

INSERT OR IGNORE INTO blobs (sha256, sha1, md5, size, created_at)
SELECT '0000000000000000000000000000000000000000000000000000000000000000', '', '', 0,
       strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE EXISTS (SELECT 1 FROM nodes
              WHERE instr(path, '/') > 0 AND instr(path, '/') < length(path));

WITH RECURSIVE walk(repo_key, dir, rest, stamp) AS (
  SELECT repo_key,
         substr(path, 1, instr(path, '/')),
         substr(path, instr(path, '/') + 1),
         created_at
  FROM nodes
  WHERE instr(path, '/') > 0 AND instr(path, '/') < length(path)
  UNION ALL
  SELECT repo_key,
         dir || substr(rest, 1, instr(rest, '/')),
         substr(rest, instr(rest, '/') + 1),
         stamp
  FROM walk
  WHERE instr(rest, '/') > 0
)
INSERT OR IGNORE INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
SELECT repo_key, dir,
       '0000000000000000000000000000000000000000000000000000000000000000',
       0, 'application/octet-stream', '', MAX(stamp), MAX(stamp)
FROM walk
GROUP BY repo_key, dir;
