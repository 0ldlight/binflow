// Package npm is the npm registry HTTP API adapter (M3, FR-18), mounted at
// /binflow/api/npm/<repoKey>/** through the T-63 api-mount seam — which
// rewrites those addresses onto the content plane, so the SAME handler also
// answers /binflow/<repoKey>/** with identical request shapes (one node
// namespace, two entrances; architecture section 5.4.2).
//
// Behavior provenance (clean-room, ADR-0001): the npm registry HTTP API is
// publicly specified ([NPM-API] docs — RESOURCES.md / package-metadata.md);
// the Artifactory-specific deltas (ten-step publish validation chain,
// dist-tags wording, the -rev fake success, ETag = packument sha1) come from
// docs/reverse/maven-npm-pypi.md section 2 (high confidence) and the M3 PRD
// v1.1/v1.2 rulings that calibrated it (Q7: duplicate publish 403, E-01
// envelope; R8: integrity mismatch enforced at 400 by BinFlow decision).
//
// Storage posture (architecture section 5.4.2, spec section 4.5): there is NO
// hidden .npm directory — the packument is an ordinary node at
// <pkg>/packument.json, tarballs land at <name>/-/<name>-<version>.tgz
// (scoped: @<scope>/<name>/-/@<scope>/<name>-<version>.tgz), and every index
// fact is derived from those nodes plus the blobs ledger.
package npm
