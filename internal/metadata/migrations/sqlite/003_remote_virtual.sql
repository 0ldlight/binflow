-- 003_remote_virtual.sql (sqlite dialect) — remote proxy + virtual domain
-- schema (M3, ADR-0012/0013).
--
-- Contract: docs/design/architecture.md section 6, "003_remote_virtual.sql"
-- block (final DDL — the remote_cache table, the remote_configs widening and
-- every index must match it; only comments are allowed to differ).
-- Conventions inherit from 001_init.sql (ADR-0007): RFC3339 UTC text
-- timestamps, statements inside the SQLite/Postgres common subset, and no
-- transaction statements in the file body — the migrator wraps each
-- migration in one transaction and nesting one is an error (T-10 review M9).

-- remote_configs widening (the 001 placeholder table becomes the M3 remote
-- model). cache_ttl_seconds stays as declared in 001: it is superseded by the
-- dual TTL below and is intentionally not surfaced by RemoteStore. The DDL
-- defaults below are schema-level fallbacks only: the product-level defaults
-- (retrievalCachePeriodSecs 7200 / missedRetrievalCachePeriodSecs 1800,
-- PRD v1.2 C4 per the ADR-0012 T-79 errata) are applied by the service layer
-- at repo-creation time (T-64).
ALTER TABLE remote_configs ADD COLUMN content_ttl_seconds INTEGER NOT NULL DEFAULT 86400;  -- artifact TTL (long; checksum hits are never revalidated)
ALTER TABLE remote_configs ADD COLUMN metadata_ttl_seconds INTEGER NOT NULL DEFAULT 600;   -- metadata TTL (short: maven-metadata / packument / simple pages)
ALTER TABLE remote_configs ADD COLUMN allow_private_upstream INTEGER NOT NULL DEFAULT 0;   -- SSRF chain exemption, enforced at request time (explicit, admin-set, audited; ADR-0012)
ALTER TABLE remote_configs RENAME COLUMN unreachable_mask TO blocked_out;                  -- manual mask (ADR-0012 decision 2)
-- password column semantics change (no DDL): plaintext -> 'enc:v1:<b64(nonce+ciphertext)>'
-- AES-256-GCM under env BINFLOW_REMOTE_CREDENTIALS_KEY (ADR-0012). No stock
-- rows exist to migrate — M1/M2 never wrote remote_configs — so there is
-- nothing to re-encrypt here; the rows-without-key startup fail-fast lands
-- with the crypto chain (T-66).

CREATE TABLE remote_cache (               -- cache validator metadata (per repo+path; nodes gains no column, the local artifact surface stays untouched)
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  path      TEXT NOT NULL,
  etag      TEXT NOT NULL DEFAULT '',
  last_modified TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL,               -- RFC3339
  expires_at TEXT NOT NULL,
  kind      TEXT NOT NULL DEFAULT 'content', -- 'content' | 'metadata' (TTL split, ADR-0012)
  PRIMARY KEY (repo_key, path)
);
CREATE INDEX idx_remote_cache_expiry ON remote_cache(expires_at);  -- periodic sweep candidates (nice-to-have in M3, not required)

-- virtual_members (001 already created): the position column semantics are
-- upgraded to 'declaration order within the bucket' (ADR-0013 as amended by
-- the T-79 linkage record / PRD C3: resolution is two-bucket — the
-- priorityResolution-marked members first, then the rest — and position
-- orders members inside each bucket; local is no longer unconditionally
-- first) — no DDL change, comment and documentation semantics only.

-- sha1 fast-path seam for T-73 (X-Checksum-Deploy carrying only
-- X-Checksum-Sha1): blobs ledger lookup by secondary checksum.
CREATE INDEX idx_blobs_sha1 ON blobs(sha1);

-- npm/PyPI need no tables of their own: the npm packument is a node
-- (<pkg>/packument.json, architecture 5.4.2); pypi simple pages are
-- generated on demand (5.4.3); maven maven-metadata.xml is generated on
-- demand (5.4.1). All three protocols share nodes+blobs.
