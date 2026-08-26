package cargo

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The Cargo metadata provider (the adapter SPI's per-protocol member,
// architecture section 5.4). Three consumers:
//
//   - Classify feeds the remote cache's dual TTL split (the M11 remote
//     ticket's engine hop): the .crate blobs and the .cargo metadata
//     sidecars are immutable per version (content class); the index/*
//     files are regenerable protocol documents (metadata class, short
//     TTL, revalidated).
//   - PackageName feeds the virtual metadata aggregation's skip rule (a
//     crate's identity — the crates/ directory name, the index file's
//     trailing segment).
//   - Versions is the SemVer ordering the M11 virtual best-version seam
//     consumes.
//
// UpstreamPath (the remote translation facet) is deliberately absent —
// the sparse-protocol upstream shapes (index.crates.io versus the CDN
// download host, config.original.json) are the remote ticket's own.

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
