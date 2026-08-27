// Package helm serves the CLASSIC Helm chart repository protocol (HTTP API
// v1 — index.yaml plus tgz downloads; helm.md sections 1-5) on LOCAL
// repositories. The remote pull-through and the virtual aggregation are
// their own M11 tickets; HelmOCI (the registry-v2 face) reuses the docker
// adapter and never touches this package (HL-3).
//
// # Mount form (HL-1, ADR-0034)
//
// The main face is the CONTENT plane: /binflow/<repoKey>/index.yaml,
// /binflow/<repoKey>/<chart>.tgz. The Artifactory-compatible alias
// /binflow/api/helm/<repoKey>/<path> rewrites onto the content plane
// through httpapi's apiProtocolMounts and is READ-ONLY there (GET/HEAD
// serve, every write verb answers 405) — uploads always address the
// content plane, exactly the curl -T posture JFrog documents.
//
// # Wire surface (helm.md section 2, local column)
//
//	Method  Path                    Behavior
//	GET     index.yaml              the stored repo-root node, text/yaml
//	HEAD    index.yaml              200/404 on node existence
//	GET     <path>.tgz|.tar.gz      stream + X-Checksum-* family
//	GET     <path>.tgz.prov         the provenance file, plain node
//	PUT     <path>.tgz              upload: parse, chart.* props, reindex
//	PUT     <path>.tgz.prov         plain-file landing (S14: never indexed)
//	DELETE  <path>.tgz              delete + index-entry removal
//	POST    api/helm/{key}/reindex  management plane (dispatchAPI family,
//	                               httpapi/helm.go — NOT this handler)
//
// _external/ and _transitive/ are the remote/virtual families: on a local
// repository they answer 400 (helm.md section 2).
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
// # Storage layout (helm.md section 3)
//
//	<repoKey>/index.yaml            server-generated (client PUTs refused)
//	<repoKey>/<chart>-<version>.tgz the chart (any subdirectory legal)
//	<repoKey>/<chart>-<version>.tgz.prov  provenance, plain file
//
// The .tgz judgment is the EXTENSION .tgz (a .tar.gz body lands as a plain
// file and never triggers the indexer — helm.md section 3's rule).
package helm
