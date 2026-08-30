// Package conan is the Conan adapter (M11/T-308+T-312, PRD FR-96): the v2
// revision-aware protocol in full plus the complete v1 data plane on LOCAL
// repositories, the REMOTE pull-through face (remote.go) and the VIRTUAL
// aggregation face (virtual.go).
//
// # Behavior basis
//
// docs/reverse/conan.md (T-284) is the contract, with the rulings folded in
// (reports/agents/tl-fr91-ac3.md section 2 + the BOARD user rulings):
//
//   - CN-1 (final, 2026-08-26 20:55): the v1 plane is FULL — all seventeen
//     endpoints (handshake three + the thirteen data rows + the files
//     channel), not the narrowed three-endpoint subset;
//   - TL-2: X-Conan-Server-Version reports 0.20.0 on every response;
//     X-Conan-Server-Capabilities lists what the repository class serves
//     (local: complex_search,checksum_deploy,revisions,matrix_params;
//     remote/virtual append only_v2);
//   - TL-3: .timestamp is first-write-wins, never overwritten, no
//     threshold; latest ordering is always index.json's time field;
//   - TL-1: the absolute URLs upload_urls/download_urls/digest cite take
//     Options.BaseURL (server.base_url), falling back to the request's
//     scheme+host (X-Forwarded-Proto honored — the npm/nuget posture);
//   - S12: the `_` placeholder user/channel segments are stored literally
//     and matched literally;
//   - S6: the v1 DATA plane is local-only — a remote or virtual repository
//     answers 400 with the pinned wording. The handshake trio
//     (ping/authenticate/check_credentials, both version prefixes) stays
//     class-independent: it is how a client discovers the only_v2
//     capability those classes advertise (spec section 2's table applied
//     to the remote/virtual rows).
//
// # The three classes (spec section 7)
//
// LOCAL serves both planes in full (v2.go/v1.go). REMOTE serves v2 reads
// through repo.Service's pull-through engine — the upstream hop translates
// the storage grammar onto the v2 wire grammar through the provider's
// UpstreamPath facet (provider.go), the index documents fetch as their
// isomorphic revisions bodies, and the two document faces without a
// storage shape of their own (the files listing, the packageId search)
// cache under the layout's marker paths (.files.json/.search.json) and
// serve verbatim; writes answer the BinFlow-wide read-only 405 (remote
// search is not proxied — the T-287 query-in-URL constraint). VIRTUAL
// aggregates: revisions merge time-descending with first-seen dedupe,
// file listings and packageId rows union, file bodies resolve
// first-found through svc.Get, search answers the local members' stored
// facts, PUT routes onto the configured defaultDeploymentRepo (the index
// read-modify-write is member-targeted so a routed write never copies the
// other members' chains into the target), and DELETE answers the
// no-propagation 405.
//
// # Wire (after the /binflow prefix strip httpapi performs)
//
//	v2/conans/search?q=…                                  recipe search
//	v2/conans/<ref>/latest                                latest rRev
//	v2/conans/<ref>/revisions                             rRev list (time desc)
//	v2/conans/<ref>/revisions/<rRev>/files                recipe file list
//	v2/conans/<ref>/revisions/<rRev>/files/<path>         recipe file GET/HEAD/PUT
//	v2/conans/<ref>/revisions/<rRev>/search?q=            packageId metadata
//	v2/conans/<ref>/revisions/<rRev>/packages             DELETE all binaries
//	v2/conans/<ref>/revisions/<rRev>/packages/<pid>/latest           latest pRev
//	v2/conans/<ref>/revisions/<rRev>/packages/<pid>/revisions        pRev list
//	v2/conans/<ref>/revisions/<rRev>/packages/<pid>/revisions/<pRev>/files…
//	v2/conans/<ref>/search?q=                             packageId metadata
//	DELETE v2/conans/<ref>                                whole recipe
//	DELETE v2/conans/<ref>/revisions/<rRev>               one rRev
//
//	v1/ping                                               capability probe
//	v1/users/authenticate                                 Basic -> token body
//	v1/users/check_credentials                            credential probe
//	v1/conans/search?q=…                                  recipe search
//	v1/conans/<ref>                                       snapshot / DELETE
//	v1/conans/<ref>/search?q=                             packageId metadata
//	v1/conans/<ref>/digest                                manifest URL
//	v1/conans/<ref>/download_urls                         file URL map
//	v1/conans/<ref>/upload_urls                           PUT URL map (POST)
//	v1/conans/<ref>/remove_files                          file removal (POST)
//	v1/conans/<ref>/packages/delete                       batch pid removal
//	v1/conans/<ref>/packages/<pid>…                       the package arms
//	v1/files/<user>/<name>/<ver>/<channel>/[0/]export|package/…  direct channel
//
// `<ref>` is `{name}/{version}/{user}/{channel}` — the v2 wire order; the
// storage layout and the v1 files channel use the coordinate order
// `<user>/<name>/<version>/<channel>` (spec section 4).
//
// # Storage layout (spec section 4; blob addressing still the filestore's)
//
//	<user>/<name>/<version>/<channel>/index.json          recipe revision index
//	<user>/<name>/<version>/<channel>/<rRev>/.timestamp   first-write marker
//	<user>/<name>/<version>/<channel>/<rRev>/.files.json  files-listing marker (remote)
//	<user>/<name>/<version>/<channel>/<rRev>/.search.json pid-metadata marker (remote)
//	<user>/<name>/<version>/<channel>/<rRev>/export/…     recipe files
//	<user>/<name>/<version>/<channel>/<rRev>/package/<pid>/index.json
//	<user>/<name>/<version>/<channel>/<rRev>/package/<pid>/<pRev>/.timestamp
//	<user>/<name>/<version>/<channel>/<rRev>/package/<pid>/<pRev>/…
//
// The two remote-hop markers exist only in REMOTE repositories (the local
// plane serves both faces from its own facts); they ride the metadata TTL
// class and map back onto their wire endpoints at the upstream hop.
//
// The v1 files channel addresses the same trees through the default
// revision segment `0` (getExportPathDefaultRevision's shape): a v1 PUT
// registers `0` in the indexes so the index-layer latest resolution the v1
// endpoints ride on stays truthful.
//
// index.json is the revisions endpoint's own response body, isomorphic and
// time-descending (S2); .timestamp is the revision's birth mark (TL-3) and
// never appears in a file listing or snapshot.
//
// # Management face
//
// POST /binflow/api/conan/reindex and POST /binflow/api/conan/{repoPath}/reindex
// are served by ManagementHandler (ADR-0034's dispatchAPI family posture).
// The router wire-up is two dispatchAPI cases owned by the assembly; this
// package ships the handler itself so the business body lives with the
// protocol (reindex.go).
//
// # Boot sweep (T-371 / ADR-0042)
//
// SweepV1FilesLayout is the startup seam the assembly calls before the HTTP
// listener goes up: it re-homes the D-F2 legacy trees (v1 files-channel
// package files the pre-T-371 channelFileName bug landed double-spelled
// onto <coordinateRoot>/0/package/<pid>/0/package/<pid>/<tail>) onto the
// spec section 4 layout, through repo.Service's RewriteSubtreePrefix
// primitive. Predicate-consuming and idempotent: a clean instance scans
// nothing, a swept one reports moved=0 on the next boot (sweep.go).
package conan
