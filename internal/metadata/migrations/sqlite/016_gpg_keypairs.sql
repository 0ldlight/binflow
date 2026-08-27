-- 016_gpg_keypairs.sql (sqlite dialect) — the instance-level GPG signing
-- keypair table (M11 T-319, ADR-0038 decision 2 / FR-97+98 prerequisite).
--
-- Contract: one row per key pair, keyed by the Artifactory wire identifier
-- (KeyPairInput.pairName — the same spelling the REST family addresses a
-- pair by). public_key is the armored public-key block verbatim (a public
-- artifact served back by the GET family and by the repo-keyed public-key
-- endpoint); private_key_enc and passphrase_enc are ALWAYS 'enc:v1:'
-- (AES-256-GCM under BINFLOW_REMOTE_CREDENTIALS_KEY, the instance master
-- key) — the armored private-key block as imported/generated is sealed
-- WHOLE (its own S2K passphrase protection stays inside the sealed blob;
-- the master key is the independent second line against offline cracking
-- of a weak passphrase, ADR-0038 decision 2). Plaintext key material never
-- lands in this table, and neither sealed column is ever echoed by any
-- read path: the private key and the passphrase never leave the store.
--
-- pair_type is the closed set 'GPG' (M11 scope: debian/rpm repository
-- metadata signing; the RSA PEM family serves Alpine-style index signing
-- and is out of scope). algorithm is the material summary (RSA-4096,
-- Ed25519, ...) carried for display. Repository association is NOT a
-- column here: repositories reference pairs forward through their own
-- config blob (keyPairName field, ADR-0038 decision 5 — the repo owns the
-- signing intent), so this table needs no back-references and the DELETE
-- guard scans the repositories' configs.
--
-- Posture: with no master key configured, every write path refuses before
-- reaching this table; existing rows without a master key fail the boot
-- (ADR-0038 decision 2, the instance static-secret family posture).
--
-- created_at/updated_at/updated_by carry the audit-provenance projections
-- (RFC3339 UTC text / principal name; timestamps follow the 001
-- conventions). Idempotent: CREATE TABLE IF NOT EXISTS, matching the
-- migrator's reopen-skips-applied guarantee (ADR-0007).

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
