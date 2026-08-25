-- 012_license.sql (sqlite dialect) — the license row (M10, ADR-0032 /
-- architecture section 15.1.2).
--
-- Contract: single-license model. id is CHECK-pinned to 1: exactly one
-- license per instance, replaced atomically by PutLicense (delete + insert
-- in one transaction — the D7 contract that a failed install leaves the
-- previous license in force). The Artifactory multi-license additive shape
-- (HA three-node licenses) belongs to the HA addon and would widen this
-- table with its own ADR; M10 plants no speculative seam.
--
-- doc stores the document text verbatim (the re-verification fact source:
-- startup and the daily ticker re-run the signature chain over this exact
-- text); the other columns are derived query/render projections. expires_at
-- is NULL for a perpetual (community-tier) license; timestamps are RFC3339
-- UTC text per the 001 conventions. Idempotent: CREATE TABLE IF NOT EXISTS,
-- matching the migrator's reopen-skips-applied guarantee (ADR-0007).

CREATE TABLE IF NOT EXISTS licenses (
  id           INTEGER PRIMARY KEY CHECK (id = 1),
  license_id   TEXT NOT NULL,
  tier         TEXT NOT NULL,
  licensee     TEXT NOT NULL,
  doc          TEXT NOT NULL,
  issued_at    TEXT NOT NULL,
  not_before   TEXT NOT NULL,
  expires_at   TEXT,
  installed_at TEXT NOT NULL
);
