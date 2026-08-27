package conan

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The Conan metadata provider (the adapter SPI's per-protocol member, the
// cargo posture). Four consumers:
//
//   - Classify feeds the remote cache's dual TTL split (the T-312 remote
//     ticket's engine hop): the export/package file trees are immutable
//     per revision (content class); the index.json, .timestamp and the two
//     remote document markers (.files.json/.search.json) are regenerable
//     documents (metadata class, short TTL, revalidated).
//   - PackageName feeds the virtual metadata aggregation's skip rule (a
//     ref's identity — the coordinate root directory).
//   - Versions is the revision chain's own ordering surface; conan has no
//     SemVer ordering server-side beyond the index's time ordering, so the
//     comparator is the identity (the time-descending index IS the
//     ordering — the virtual merge sorts by the index's own time rule).
//   - UpstreamPath is the OPTIONAL facet internal/remote consumes at the
//     upstream hop (T-312, the goproxy T-285 posture): the storage layout
//     (<user>/<name>/<version>/<channel>/…) and the v2 wire grammar
//     (v2/conans/<name>/<version>/<user>/<channel>/…) differ in segment
//     order AND in endpoint vocabulary, and the two listing/search
//     documents have no wire-named storage path at all. Cache keys, landed
//     nodes and the singleflight slot all keep the STORAGE path — only the
//     outbound hop sees the wire spelling.

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
	// The trailing segment is the file itself: an index document, a birth
	// marker or one of the remote hop's cached documents anywhere in the
	// tree is regenerable metadata (reindex rebuilds the first two from
	// the stored facts; the markers re-fetch from the upstream).
	switch base := relPath[strings.LastIndexByte(relPath, '/')+1:]; base {
	case recipeIndexFile, timestampFile, filesListFile, refSearchFile:
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

// UpstreamPath maps one STORAGE-form repository path onto the UPSTREAM v2
// wire path (internal/remote's optional facet, the goproxy posture): the
// coordinate order swaps (storage <user>/<name>/<version>/<channel> versus
// wire <name>/<version>/<user>/<channel>), the two index documents become
// their revisions endpoints, and the two markers become the listing/search
// endpoints they stand for. Unknown shapes pass through verbatim (the
// generic posture — no rewriting the engine cannot reason about).
func (provider) UpstreamPath(relPath string) string {
	segs := strings.Split(relPath, "/")
	if len(segs) < 5 {
		return relPath
	}
	rf, err := parseRef(segs[1], segs[2], segs[0], segs[3]) // storage order
	if err != nil {
		return relPath
	}
	base := segV2 + "/" + segConans + "/" + rf.name + "/" + rf.version + "/" + rf.user + "/" + rf.channel
	rest := segs[4:]
	switch rest[0] {
	case recipeIndexFile:
		if len(rest) == 1 {
			return base + "/" + segRevisions // the isomorphic revisions body IS the index document
		}
	case timestampFile:
		// No upstream endpoint: the marker never crosses the hop (the
		// remote arms never address it; the identity keeps a stray read
		// honest).
		return relPath
	}
	if !validRevision(rest[0]) {
		return relPath
	}
	rrev := rest[0]
	// <root>/<rRev>/.files.json | .search.json | export/…
	if len(rest) == 2 {
		switch rest[1] {
		case filesListFile:
			return base + "/" + segRevisions + "/" + rrev + "/" + segFilesTail
		case refSearchFile:
			return base + "/" + segRevisions + "/" + rrev + "/" + segSearch
		}
	}
	if len(rest) >= 2 && rest[1] == dirExport {
		return base + "/" + segRevisions + "/" + rrev + "/" + segFilesTail + "/" + strings.Join(rest[2:], "/")
	}
	// <root>/<rRev>/package/<pid>/…
	if len(rest) >= 4 && rest[1] == dirPackage && validPackageID(rest[2]) {
		pid := rest[2]
		pkgBase := base + "/" + segRevisions + "/" + rrev + "/" + segPackages + "/" + pid + "/" + segRevisions
		switch tail := rest[3:]; {
		case len(tail) == 1 && tail[0] == recipeIndexFile:
			return pkgBase
		case len(tail) >= 2 && validRevision(tail[0]):
			prev := tail[0]
			if len(tail) == 2 && tail[1] == filesListFile {
				return pkgBase + "/" + prev + "/" + segFilesTail
			}
			return pkgBase + "/" + prev + "/" + segFilesTail + "/" + strings.Join(tail[1:], "/")
		}
	}
	return relPath
}
