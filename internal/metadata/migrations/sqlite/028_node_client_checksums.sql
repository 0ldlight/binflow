-- 028_node_client_checksums.sql (sqlite dialect) — the client-declared
-- digests on the node row (L024-5, diff L4): badChecksum's DB-side
-- client-vs-server comparison needs the digest the deploying client
-- DECLARED (X-Checksum-Md5/Sha1/Sha256), which until now lived only in the
-- upload verification and never persisted. '' = undeclared (the reference's
-- own "missing client is bad" arm). Conventions inherit from 001
-- (ADR-0007): plain column adds, defaults backfill every existing row as
-- undeclared.

ALTER TABLE nodes ADD COLUMN client_md5 TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN client_sha1 TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN client_sha256 TEXT NOT NULL DEFAULT '';
