-- 027_build_artifact_wire.sql (sqlite dialect) — the build artifact
-- echo/collection wire keys (L023-2F, diff report D3/D4): wire_path keeps
-- the document's own `path` value verbatim (a relative path like
-- "x/y/a.jar" is echo data, NOT the nodes association), and
-- original_repo keeps `originalDeploymentRepo` — the manifest channel's
-- repo key, the promote collection's primary address
-- (originalDeploymentRepo + path resolve the node directly).
--
-- The nodes association columns (repo_key, path) keep their meaning; the
-- new columns are the wire-fidelity pair the GET face echoes and the
-- manifest channel reads. Conventions inherit from 001/024 (ADR-0007).

ALTER TABLE build_artifacts ADD COLUMN wire_path TEXT NOT NULL DEFAULT '';
ALTER TABLE build_artifacts ADD COLUMN original_repo TEXT NOT NULL DEFAULT '';
