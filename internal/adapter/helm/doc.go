// Package helm serves the CLASSIC Helm chart repository protocol (HTTP API
// v1 — index.yaml plus tgz downloads; helm.md sections 1-7) on all three
// repository classes: LOCAL (T-309), the REMOTE pull-through and the
// VIRTUAL aggregation (T-313). HelmOCI (the registry-v2 face) reuses the
// docker adapter and never touches this package (HL-3).
//
// # Mount form (HL-1, ADR-0034)
//
// The main face is the CONTENT plane: /binflow/<repoKey>/index.yaml,
// /binflow/<repoKey>/<chart>.tgz. The Artifactory-compatible alias
// /binflow/api/helm/<repoKey>/<path> rewrites onto the content plane
// through httpapi's apiProtocolMounts and is READ-ONLY there (GET/HEAD
// serve, every write verb answers 405) — uploads always address the
// content plane, exactly the curl -T posture JFrog documents. The alias
// serves every class's read faces; the aggregated index's download URLs
// always cite the content plane (R-2's revision — the two faces must not
// disagree inside one index).
//
// # Wire surface (helm.md section 2)
//
//	Method  Path                    Behavior
//	GET     index.yaml              local: the stored repo-root node;
//	                                remote: the pull-through engine's
//	                                cached upstream index (metadata TTL);
//	                                virtual: the aggregated, rewritten
//	                                index (virtual.go)
//	HEAD    index.yaml              local/remote: node existence; virtual:
//	                                the membership probe (200 with members,
//	                                404 without — no aggregation runs)
//	GET     <path>.tgz|.tar.gz      stream + X-Checksum-* family on every
//	                                class (svc.Get: the local node read,
//	                                the remote pull-through, the virtual
//	                                first-hit member resolution)
//	GET     <path>.tgz.prov         the provenance file, plain node
//	PUT     <path>.tgz              upload: parse, chart.* props, reindex
//	                                (a virtual write routes onto its
//	                                deployment member inside the service —
//	                                the policy and index steps follow the
//	                                landing member)
//	PUT     <path>.tgz.prov         plain-file landing (S14: never indexed)
//	DELETE  <path>.tgz              local: delete + index-entry removal;
//	                                remote: the RE-06 cache drop; virtual:
//	                                the C5 405 (deletes never propagate)
//	GET     _external/<proto>/<url> the external-dependency proxy (remote
//	                                and virtual only): fetch
//	                                <proto>://<url> through the guarded
//	                                outbound client, allow-list gated
//	GET     _transitive/<proto>/<url> the upstream's OWN _external face —
//	                                a path-joined upstream fetch that
//	                                rides the pull-through engine (cached)
//	POST    api/helm/{key}/reindex  management plane (dispatchAPI family,
//	                               httpapi/helm.go — NOT this handler)
//
// _external and _transitive answer the pinned 400 on a LOCAL repository
// (helm.md section 2); their wire path is the absolute URL with "://"
// folded onto "/" (scheme "://" host "/" path → scheme "/" host "/" path).
//
// # Index chain (helm.md section 4)
//
// A landed .tgz fires the synchronous recompute: read the repo-root
// index.yaml (or create the skeleton), replace the same name+version
// entry, rewrite the whole file. Entries sort SemVer DESCENDING (newest
// first — helm install's no-version pick); digest is the tgz sha256 as
// bare hex; urls are RELATIVE by default (HL-2: BinFlow supports helm 3+
// only, where relative urls are the native form — the absolute mode's
// config seat exists in Options but no config key exposes it).
//
// # Remote repository (helm.md section 6, remote.go)
//
// The index and the path-aligned chart downloads are the shared FR-20
// pull-through verbatim (negative cache, dual TTL, stale downgrade; the
// metadata provider classifies the repo-root index as regenerable). The
// upstream fetch base is the repository URL — the path-aligned mirror
// rule; a divergent chartsBaseUrl is a config-schema seat this ticket
// deliberately leaves out (registered in the ticket report). The
// _external face fetches absolute URLs through the same guarded client
// family internal/remote ships (per-hop screening, redirect re-checks,
// the repository's timeout and private-upstream exemption) — nothing is
// landed in the cache namespace on that face (the engine's land path is
// the only cache writer; an absolute-URL fetch seam would be an
// internal/remote extension).
//
// # Virtual repository (helm.md section 7, virtual.go)
//
// The aggregated index walks the two-bucket member order, reads every
// member's index (a local member's stored node, a remote member's
// upstream index through the FR-20 chain), merges per (name, version)
// FIRST-WINS by member order (S13) and rewrites every urls[0] (S8): the
// charts-base-aligned member path, the upstream's own _external face onto
// _transitive, the allow-list-hit external URL onto the folded _external
// proxy path, the allow-list miss kept verbatim, oci:// entries kept
// verbatim (D-5 re-evaluated with T-342: the helmoci package type serves
// the /v2 plane on LOCAL repositories only — no OCI face stands behind
// this virtual's oci:// entries, so the passthrough stays the honest
// rewrite). The aggregation
// computes PER REQUEST (the pypi/npm/maven virtual posture — no on-disk
// .index cache; member changes and member chart uploads are visible to
// the next request, and the S7 cache-invalidation step is vacuous). The
// .index storage spelling stays reserved against client writes.
//
// # Storage layout (helm.md section 3)
//
//	<repoKey>/index.yaml            server-generated (client PUTs refused)
//	<repoKey>/<chart>-<version>.tgz the chart (any subdirectory legal)
//	<repoKey>/<chart>-<version>.tgz.prov  provenance, plain file
//	<remoteKey>/…                   the pull-through cache (the engine's
//	                                own landing paths)
//
// The .tgz judgment is the EXTENSION .tgz (a .tar.gz body lands as a plain
// file and never triggers the indexer — helm.md section 3's rule).
package helm
