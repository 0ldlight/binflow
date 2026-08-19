// Package pypi implements the PyPI simple-repository protocol adapter
// (M3 FR-19, PEP 503/592/691 + the warehouse upload API).
//
// Wire surface (architecture section 5.4.3, PRD PE-01..PE-06):
//
//	/binflow/api/pypi/<repo>/                 POST upload (multipart), GET probe
//	/binflow/api/pypi/<repo>/simple/          repo-level project index (P2)
//	/binflow/api/pypi/<repo>/simple/<name>/   project page (PEP 503 HTML, PEP 691 JSON)
//	/binflow/api/pypi/<repo>/packages/<path>  file download (index href target)
//	/binflow/<repo>/<name>/<version>/<file>   the same nodes' second entrance
//
// The /binflow/api/pypi mount is httpapi's dispatch seam (T-63): it rewrites
// the URL onto the content plane, so this handler serves BOTH spellings with
// one code path and cannot tell them apart — by design (one node namespace,
// two entrances).
//
// Behavior sources (clean-room, ADR-0001): PEP 503/592/691 and the warehouse
// upload API are public specifications and win where they speak; the
// Artifactory calibration (api-version=2 head shape, 302 trailing-slash
// redirect, upload field set, original-name storage layout) comes from
// docs/reverse/maven-npm-pypi.md section 3 (high confidence). BinFlow
// deviations are deliberate and documented at each site:
//
//   - sha256-only index fragments (PRD Q3 ruling; no #md5= fallback),
//   - no private rel="internal|external" attributes (pip ignores them, PEP
//     503 does not define them),
//   - no hidden .pypi/ index directories — the index is regenerated from
//     node rows on every request (maven-npm-pypi.md section 4.5),
//   - duplicate filename uploads answer 400 (PRD interim ruling R8).
//
// N4 decision (T-63 review, carried into this ticket): a request spelled
// /binflow/api/pypi/** against a repository whose package_type is NOT pypi
// is REJECTED in principle (strict 404, the Artifactory posture) — a pypi
// URL must never mutate another protocol's namespace. The T-63 seam
// dispatches by the repo row's package type, so this package cannot observe
// such requests (it is only ever handed pypi rows); the enforcement point is
// the seam itself and stays with httpapi/T-63's contract (see
// TestT63APIProtocolMountMatrix's "dispatch follows the repository's package
// type" row). Flagged to the conductor for routing — the same one-line seam
// check serves npm (T-69) and pypi alike.
package pypi
