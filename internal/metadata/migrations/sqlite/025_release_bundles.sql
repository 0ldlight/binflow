-- 025_release_bundles.sql (sqlite dialect) — the release-bundle table family
-- (M17 T-513, FR-153.1 / ADR-0046 decision 1 + Errata ①⑤): bundles and
-- bundle_items — the minimal-face record plane of the release-bundle domain
-- (Q2 exit ①: a versioned release RECORD, not a content copy — nothing here
-- enters storage/nodes, and no repository is ever auto-created for it, the
-- zero-silent-writes posture that also declines the reference's default
-- `release-bundles` storing repository).
--
-- Identity is the PAIR (bundle_name, bundle_version) — the conflict root the
-- reference product pins as UNIQUE(name, version, type); BinFlow keeps the
-- type dimension as a CODE constant (SOURCE — the single-instance record
-- form, architecture §26.2 "type 恒 SOURCE"), so the pair IS the uniqueness
-- key and the CHECK-level closed set shrinks to the two-state subset
-- (COMPLETE/INPROGRESS of the reference's four-value enum, release-bundle.md
-- §3.2's minimal-face recommendation).
--
-- signature is the PLACEHOLDER column (ADR-0046 Errata ⑤: the v1 product
-- DDL declares it NOT NULL; exit ① has no signing chain, so the column
-- carries the CONTENT DIGEST — sha256 over the manifest's item-identity
-- set — which is exactly the "same signature" comparison the conflict
-- tri-state's 200 arm needs, E6's 字面自定 ruling). description is the
-- BinFlow-native column the ADR skeleton keeps at zero cost (the reference
-- v1 wire has no description field; no face writes it in M17).
--
-- bundle_items is the NODES TIME-POINT SNAPSHOT: sha256/size are frozen at
-- resolution time, later artifact changes never propagate back (a bundle is
-- a release-moment record). '' sha256 = PENDING — the item's artifact is not
-- (yet) on this instance; the bundle stays INPROGRESS until every row
-- carries its snapshot, and the same-manifest re-create (the resume arm)
-- re-resolves only the pending rows. Deliberately NO foreign key into
-- nodes: the snapshot must outlive the artifact (history is a record, and
-- an artifact delete must not destroy the manifest row). id is a uuid the
-- writer mints; (bundle_name, bundle_version, repo_key, path) carries the
-- set semantics — the manifest is IMMUTABLE (no append face; a different
-- manifest at the same pair is the 409 conflict).
--
-- Dialect conventions of 001/018/021/024 (ADR-0007): statements inside the
-- SQLite/Postgres common subset, no transaction statements in the body —
-- the migrator wraps each migration in one transaction. Idempotent: CREATE
-- TABLE IF NOT EXISTS, the post-011 convention the t212 rewind test replays
-- under.
--
-- Indexes: bundle_items(bundle_name, bundle_version) for the segment read
-- (the ADR skeleton's named index), plus the UNIQUE identity index that
-- makes the set semantics a database fact rather than a writer promise.

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
