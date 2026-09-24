-- 007_folder_rows_backfill.sql (postgres dialect) — materialize the folder
-- row of every ancestor directory of every stored node (M5, T-128 / FR-44,
-- ADR-0016 decision 4).
--
-- Mirrors the sqlite dialect file 007 one-to-one (ADR-0007 lockstep); see
-- that file's header for the full contract. Dialect deltas per the postgres
-- README porting notes: ON CONFLICT DO NOTHING replaces INSERT OR IGNORE,
-- strpos(path, '/') replaces instr(path, '/'), substr/length/MAX are
-- unchanged, and the RFC3339 UTC timestamp expression is to_char(now() AT
-- TIME ZONE 'utc', ...) replacing strftime. The recursive CTE ports verbatim
-- (WITH RECURSIVE ... INSERT INTO ... SELECT is native postgres).
--
-- Step 1 — sentinel blob row FIRST, but only when there is anything to
-- backfill: nodes.sha256 references blobs(sha256), so a folder row cannot
-- land before the marker row exists. The EXISTS guard repeats the walk's
-- seed predicate, so a database with no ancestor to materialize gains
-- NOTHING, and the row is byte-identical to ensureFolderLedger's (size 0,
-- empty sha1/md5, RFC3339 created_at); ON CONFLICT DO NOTHING keeps a
-- marker that a pre-existing explicit mkdir already seeded untouched.

INSERT INTO blobs (sha256, sha1, md5, size, created_at)
SELECT '0000000000000000000000000000000000000000000000000000000000000000', '', '', 0,
       to_char(now() AT TIME ZONE 'utc', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
WHERE EXISTS (SELECT 1 FROM nodes
              WHERE strpos(path, '/') > 0 AND strpos(path, '/') < length(path))
ON CONFLICT DO NOTHING;

-- Step 2 — recursive walk: every node row contributes its created_at to
-- each of its ancestor directories (seed: first segment + the rest; step:
-- consume one more segment). created_at/updated_at of a backfilled folder
-- row take the YOUNGEST descendant's created_at (MAX) — deterministic
-- across replays — and created_by='' (historical materialization has no
-- attributable actor). A folder row already present from an explicit mkdir
-- is its own correct shape and stays untouched; its path still contributes
-- ancestors for the levels above it.

WITH RECURSIVE walk(repo_key, dir, rest, stamp) AS (
  SELECT repo_key,
         substr(path, 1, strpos(path, '/')),
         substr(path, strpos(path, '/') + 1),
         created_at
  FROM nodes
  WHERE strpos(path, '/') > 0 AND strpos(path, '/') < length(path)
  UNION ALL
  SELECT repo_key,
         dir || substr(rest, 1, strpos(rest, '/')),
         substr(rest, strpos(rest, '/') + 1),
         stamp
  FROM walk
  WHERE strpos(rest, '/') > 0
)
INSERT INTO nodes (repo_key, path, sha256, size, mime, created_by, created_at, updated_at)
SELECT repo_key, dir,
       '0000000000000000000000000000000000000000000000000000000000000000',
       0, 'application/octet-stream', '', MAX(stamp), MAX(stamp)
FROM walk
GROUP BY repo_key, dir
ON CONFLICT DO NOTHING;
