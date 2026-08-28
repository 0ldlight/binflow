// Package cargo is the Rust crates adapter (M11/T-294 + T-316, PRD
// FR-88/FR-100): the sparse HTTP index protocol plus the crates.io
// registry web API, LOCAL repositories in full and REMOTE repositories as
// a sparse pull-through proxy (remote.go). The virtual aggregation is
// T-318's and answers the honest 404 until it lands.
//
// # Behavior basis
//
// docs/reverse/cargo.md (T-284) is the contract, with the rulings folded
// in (reports/agents/tl-fr91-ac3.md section 3, T-304's CG-2 anchor table):
//
//   - CG-2 (final, T-316): the publish FAILURE face is Artifactory's
//     dual track — processing failures answer 200 + warnings.other
//     carrying "Failed to publish with error '…'" strings (NEVER a
//     top-level errors key: cargo 1.98 reads its presence as failure),
//     the permission family keeps 401 (anonymous) / 403 (named) + the
//     errors[] envelope, and a length-prefix defect answers 500 (the
//     uncaught-RuntimeException family's degraded declaration);
//   - D-3: no conflict arm — a duplicate version the principal may delete
//     is OVERWRITTEN (the service's repo-semantics section 3 gate), one
//     they may not is the 401/403 refusal; the index keeps one row per
//     version ignoring build metadata (newest spelling wins);
//   - D-5: bare writes under index/** and .cargo/** are ACCEPTED and
//     followed by the index convergence rewrite (cksum reconciliation by
//     recalculation, not by refusal); the synthesized config.json stays
//     unwritable;
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
// The remote cache adds two derived shapes (spec section 8 / S4):
//
//	config.original.json                       the upstream config.json, verbatim
//	.cargo/search/<hex-of-query>.json          one cached upstream search response
//
// The index file is REGENERABLE (rewritten whole on publish/yank/unyank
// and after every bare write under the derived families — the convergence
// the CargoMetadataInterceptor chain stands for on the reference, D-5).
package cargo
