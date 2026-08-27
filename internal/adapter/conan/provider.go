package conan

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The Conan metadata provider (the adapter SPI's per-protocol member, the
// cargo posture). Three consumers:
//
//   - Classify feeds the remote cache's dual TTL split (the T-312 remote
//     ticket's engine hop): the export/package file trees are immutable
//     per revision (content class); the index.json and .timestamp nodes
//     are regenerable index documents (metadata class, short TTL,
//     revalidated).
//   - PackageName feeds the virtual metadata aggregation's skip rule (a
//     ref's identity — the coordinate root directory).
//   - Versions is the revision chain's own ordering surface; conan has no
//     SemVer ordering server-side beyond the index's time ordering, so the
//     comparator is the identity (the time-descending index IS the
//     ordering; the virtual best-version seam lands with T-312 and may
//     pin its own rule then).
//
// UpstreamPath (the remote translation facet) is deliberately absent —
// the upstream URL grammar differences (v2/conans spellings versus the
// v1 files channel) are the remote ticket's own.

// provider is the Conan MetadataProvider.
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
	// The trailing segment is the file itself: an index document or a
	// birth marker anywhere in the tree is regenerable metadata (reindex
	// rebuilds both from the stored facts).
	switch base := relPath[strings.LastIndexByte(relPath, '/')+1:]; base {
	case recipeIndexFile, timestampFile:
		return adapter.KindMetadata
	}
	// Everything else on this layout is immutable per revision: the
	// export/ recipe files and the package trees never change after their
	// revision's first write.
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider: the ref identity a
// path belongs to — the coordinate root directory (<user>/<name> is NOT
// enough: every version and channel is its own aggregation unit on this
// protocol, matching the index.json granularity the virtual merge will
// consume).
func (provider) PackageName(relPath string) (string, bool) {
	segs := strings.Split(relPath, "/")
	if len(segs) < 4 {
		return "", false
	}
	rf, err := parseRef(segs[1], segs[2], segs[0], segs[3]) // storage order
	if err != nil {
		return "", false
	}
	return rf.coordinateRoot(), true
}

// Versions implements adapter.MetadataProvider: the identity order (see
// the type comment — the index's time ordering is the protocol's own
// truth, and no SemVer rule applies server-side).
func (provider) Versions() adapter.VersionComparator { return comparator{} }

// comparator is the identity VersionComparator.
type comparator struct{}

func (comparator) CompareVersions(a, b string) int {
	switch {
	case a == b:
		return 0
	case a < b:
		return -1
	default:
		return 1
	}
}
