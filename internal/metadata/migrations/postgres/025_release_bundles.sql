-- 025_release_bundles.sql (postgres dialect) — the release-bundle table
-- family (M17 T-513, FR-153.1 / ADR-0046 decision 1 + Errata ①⑤).
--
-- Mirrors the sqlite dialect file 025 one-to-one (ADR-0007 lockstep); see
-- that file's header for the full contract (identity pair, signature-as-
-- content-digest placeholder, the nodes time-point snapshot semantics and
-- the deliberately absent nodes FK). The statements are inside the
-- SQLite/Postgres common subset, so they carry over verbatim; only this
-- header differs. Idempotent: CREATE TABLE IF NOT EXISTS / CREATE INDEX IF
-- NOT EXISTS, the post-011 convention.

CREATE TABLE IF NOT EXISTS bundles (
	bundle_name    TEXT NOT NULL,
	bundle_version TEXT NOT NULL,
	state          TEXT NOT NULL CHECK (state IN ('COMPLETE', 'INPROGRESS')),
	signature      TEXT NOT NULL DEFAULT '',   -- content-digest placeholder (no signing chain in M17)
	description    TEXT NOT NULL DEFAULT '',   -- BinFlow-native column, zero-cost (ADR-0046 decision 1)
	created_by     TEXT NOT NULL,
	created_at     TEXT NOT NULL,
	updated_by     TEXT NOT NULL,
	updated_at     TEXT NOT NULL,
	PRIMARY KEY (bundle_name, bundle_version)
);

CREATE TABLE IF NOT EXISTS bundle_items (
	id             TEXT PRIMARY KEY,           -- uuid minted by the writer
	bundle_name    TEXT NOT NULL,
	bundle_version TEXT NOT NULL,
	repo_key       TEXT NOT NULL,              -- the manifest row's repository
	path           TEXT NOT NULL,              -- the node path inside it
	sha256         TEXT NOT NULL DEFAULT '',   -- '' = pending (no snapshot yet)
	size           INTEGER NOT NULL DEFAULT 0, -- 0 while pending
	added_at       TEXT NOT NULL DEFAULT '',
	added_by       TEXT NOT NULL DEFAULT '',
	FOREIGN KEY (bundle_name, bundle_version)
		REFERENCES bundles (bundle_name, bundle_version) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_bundle_items_bundle ON bundle_items (bundle_name, bundle_version);
CREATE UNIQUE INDEX IF NOT EXISTS idx_bundle_items_identity
	ON bundle_items (bundle_name, bundle_version, repo_key, path);
