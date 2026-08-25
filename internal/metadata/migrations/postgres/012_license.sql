-- 012_license.sql (postgres dialect) — the license row (M10, ADR-0032 /
-- architecture section 15.1.2).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). INTEGER
-- PRIMARY KEY with a CHECK pin is dialect-common; no sequences are needed
-- for a single-row table.

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
