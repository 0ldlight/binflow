// Package cargo is the Rust crates adapter (M11/T-294, PRD FR-88): the
// sparse HTTP index protocol plus the crates.io registry web API, LOCAL
// repositories in full. Remote pull-through and virtual aggregation are
// separate M11 tickets (the spec's S4/S5 arms) and answer the honest 404
// here until they land.
//
// # Behavior basis
//
// docs/reverse/cargo.md (T-284) is the contract, with the TL rulings
// folded in (reports/agents/tl-fr91-ac3.md section 3):
//
//   - CG-2: every publish failure answers 4xx/5xx + the errors[] envelope —
//     the Artifactory 200+errors dual form is NOT implemented (malformed
//     framing is the client's 400, a server IO failure the 500);
//   - CG-3: a duplicate name+version (build metadata ignored) answers 409;
//   - TL-1: config.json's dl/api absolutes take server.base_url, falling
//     back to the request's scheme+host (the npm/nuget injection posture);
//   - TL-6: yank/unyank of an unknown crate or version answers 404 +
//     envelope.
//
// # Layout (one namespace, the sparse protocol's own spellings)
//
// Wire (after the /binflow prefix strip httpapi performs):
//
//	/binflow/<repo>/                                    root probe (200 empty)
//	/binflow/<repo>/index/config.json                   sparse entry config
//	/binflow/<repo>/index/{pkgPath}                     index file (NDJSON)
//	/binflow/<repo>/v1/crates/<name>/<version>/download .crate download
//	/binflow/<repo>/api/v1/crates/new                   PUT publish
//	/binflow/<repo>/api/v1/crates?q=…&per_page=…        search
//	/binflow/<repo>/api/v1/crates/<n>/<v>/yank          DELETE yank
//	/binflow/<repo>/api/v1/crates/<n>/<v>/unyank        PUT unyank
//
// The owners family and the git-index face (info/refs, git-upload-pack)
// are explicit non-goals: owners answers the unknown-path 404 (spec
// section 11.2), the git face answers 404 with the deprecation wording
// (spec section 1 — BinFlow implements sparse only).
//
// Storage (repo-relative; spec section 4):
//
//	crates/<name>/<name>-<version>.crate       the package blob (content)
//	.cargo/crates/<name>/<name>-<version>.json publish metadata verbatim
//	index/{1,2,3/c,ab/cd}/<name>               index file, one line/version
//
// The index file is REGENERABLE (rewritten whole on publish/yank/unyank);
// client PUTs onto index/** and .cargo/** are refused (the DB-3 posture:
// a hand-written index line could break the cksum == measured-sha256
// reconciliation the download contract rides on).
package cargo
