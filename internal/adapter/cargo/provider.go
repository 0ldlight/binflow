package cargo

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The Cargo metadata provider (the adapter SPI's per-protocol member,
// architecture section 5.4). Four consumers:
//
//   - Classify feeds the remote cache's dual TTL split (the T-316 remote
//     ticket's engine hop): the .crate blobs are immutable per version
//     (content class); the index/* files, the config.original.json copy
//     and the search-cache markers are regenerable protocol documents
//     (metadata class, short TTL, revalidated). The .cargo sidecars ride
//     the content class — immutable per version, never revalidated.
//   - PackageName feeds the virtual metadata aggregation's skip rule (a
//     crate's identity — the crates/ directory name, the index file's
//     trailing segment).
//   - Versions is the SemVer ordering the M11 virtual best-version seam
//     consumes.
//   - UpstreamPath is the OPTIONAL facet internal/remote consumes at the
//     upstream hop (T-316, the goproxy T-285 / conan T-312 posture): the
//     sparse wire grammar and the storage layout differ on two planes —
//     the .crate blobs store under crates/<n>/<n>-<v>.crate while the
//     upstream serves them at v1/crates/<n>/<v>/download, and the two
//     documents with no storage shape of their own (the upstream's own
//     config.json, cached verbatim as config.original.json; the search
//     responses, cached under their query-keyed markers) map back onto
//     the endpoints they stand for. Cache keys, landed nodes and the
//     singleflight slot all keep the STORAGE path — only the outbound
//     hop sees the wire spelling.

// provider is the Cargo MetadataProvider.
type provider struct{}

// RegisterMetadata enters the provider in the process-wide registry; the
// duplicate guard keeps repeated assembly (and test stacks that mount the
// handler more than once) from panicking (the maven.RegisterMetadata
// convention).
func RegisterMetadata() {
	if _, ok := adapter.ForProtocol(Protocol); ok {
		return
	}
	adapter.RegisterMetadata(provider{})
}

// Protocol implements adapter.MetadataProvider.
func (provider) Protocol() string { return Protocol }

// Classify implements adapter.MetadataProvider (see the type comment for
// the expirable set).
func (provider) Classify(relPath string) adapter.MetadataKind {
	if strings.HasPrefix(relPath, segIndex+"/") {
		return adapter.KindMetadata
	}
	switch relPath {
	case fileOriginalConfig:
		return adapter.KindMetadata
	}
	if strings.HasPrefix(relPath, dirSearchCache+"/") {
		return adapter.KindMetadata
	}
	// Everything else on this layout is immutable per version: the .crate
	// blob and the .cargo sidecar never change after publish.
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider: the crate identity a
// path belongs to — the crates/ directory name for the storage family,
// the trailing segment for the index family.
func (provider) PackageName(relPath string) (string, bool) {
	if strings.HasPrefix(relPath, dirCrates+"/") {
		name, _, ok := strings.Cut(relPath[len(dirCrates)+1:], "/")
		if ok && validCrateName(name) {
			return strings.ToLower(name), true
		}
		return "", false
	}
	if strings.HasPrefix(relPath, segIndex+"/") {
		if name, ok := indexPkgPathName(relPath[len(segIndex)+1:]); ok {
			return name, true
		}
	}
	return "", false
}

// Versions implements adapter.MetadataProvider: the cargo order is SemVer
// precedence over the index's vers spellings.
func (provider) Versions() adapter.VersionComparator { return comparator{} }

// comparator adapts compareSemver to adapter.VersionComparator.
type comparator struct{}

func (comparator) CompareVersions(a, b string) int { return compareSemver(a, b) }

// UpstreamPath maps one STORAGE-form repository path onto the UPSTREAM
// sparse wire path (internal/remote's optional facet):
//
//	crates/<n>/<n>-<v>.crate      → v1/crates/<n>/<v>/download
//	config.original.json          → index/config.json   (the upstream's own entry document)
//	.cargo/search/<hex>.json      → api/v1/crates?<decoded query>
//	index/{pkgPath}               → index/{pkgPath}     (identity)
//
// Unknown shapes pass through verbatim (the generic posture — no
// rewriting the engine cannot reason about). The mapping is a pure
// function of the path, so the query rides the marker's reversible hex
// encoding: every cache key stays a legal storage path while the outbound
// hop still addresses the query-carrying endpoint.
func (provider) UpstreamPath(relPath string) string {
	if name, version, ok := splitCrateNode(relPath); ok {
		return segV1 + "/" + segCrates + "/" + name + "/" + version + "/" + segDownload
	}
	if relPath == fileOriginalConfig {
		return segIndex + "/" + fileConfig
	}
	if q, ok := searchQueryOf(relPath); ok {
		return segAPI + "/" + segV1 + "/" + segCrates + "?" + q
	}
	return relPath
}
