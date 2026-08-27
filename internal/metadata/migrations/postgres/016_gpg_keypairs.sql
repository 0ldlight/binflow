-- 016_gpg_keypairs.sql (postgres dialect) — the instance-level GPG signing
-- keypair table (M11 T-319, ADR-0038 decision 2 / FR-97+98 prerequisite).
--
-- Statements are inside the SQLite/Postgres common subset, so this file
-- carries them verbatim (ADR-0007 lockstep rule; the postgres migrator is
-- not embedded yet — see ../migrations/postgres/README.md). Semantics,
-- column names and the sealing posture match the sqlite dialect file
-- one-to-one; see that file's header for the full contract.

CREATE TABLE IF NOT EXISTS gpg_keypairs (
  pair_name      TEXT PRIMARY KEY,
  pair_type      TEXT NOT NULL,
  alias          TEXT NOT NULL,
  public_key     TEXT NOT NULL,
  private_key_enc TEXT NOT NULL,
  passphrase_enc TEXT NOT NULL,
  algorithm      TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  updated_by     TEXT NOT NULL
);
