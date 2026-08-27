// Package deb serves the Debian package type (M11 T-310): the apt
// repository protocol over pure storage paths plus the debPUT upload
// chain with automatic index calculation.
//
// The content plane (debian.md section 1) is a pure storage-path
// protocol — apt GETs repo-relative files verbatim (dists/<dist>/Release,
// pool/... .debs); there is no API mount of its own. The upload face is
// debPUT (section 3): a .deb/.dsc lands at any path with its
// <dist>/<component>/<arch> coordinates arriving as matrix parameters,
// the coordinates register as deb.*/dsc.* node properties, and every
// write triggers the automatic (asynchronous) recompute of the affected
// distribution's Packages/Sources indexes, By-Hash copies and Release.
//
// Board rulings this ticket implements (FR-97.1, milestone-11 section
// 97.1): TL-4 — the forced architecture families default ON
// (i386,amd64; empty Packages files are still generated per component);
// DB-2 — a .deb PUT without the full coordinate triple answers 400
// (Artifactory silently stores it instead; the BinFlow tightening the
// board confirmed); DB-3 — direct client writes into the generated
// index family under dists/ answer 403 (the automatic repository's
// indexes are the debPUT chain's output, never a client's input); DB-1 —
// unsigned mode (the instance keypair system is K-1; stale signature
// files are swept instead of written).
//
// LOCAL repositories carry the automatic pipeline above (T-310); the
// REMOTE class is the pull-through mirror and the VIRTUAL class the
// per-request stanza aggregation (T-314, remote.go / virtual.go).
package deb
