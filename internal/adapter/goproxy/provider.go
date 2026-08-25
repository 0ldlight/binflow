package goproxy

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// provider is the GOPROXY metadata provider (the adapter SPI's per-protocol
// triple member, architecture section 5.4). Three consumers:
//
//   - Classify feeds the remote cache's dual TTL split. The expirable set
//     is the reverse spec's section 3.2: .info, the .versionList marker and
//     the .latest marker participate in cache refresh; .mod/.zip are
//     immutable content (the official same-version-same-bytes duty) and
//     keep the long content TTL.
//   - PackageName feeds the virtual metadata aggregation's skip rule.
//   - UpstreamPath is the OPTIONAL facet internal/remote consumes at the
//     upstream hop: the storage path is decoded, the wire path is escaped,
//     and the two list markers differ from their upstream documents. This
//     is the §3.1 three-state rule's third leg (wire <-> storage <->
//     upstream) made mechanical.
type provider struct{}

// RegisterMetadata enters the provider in the process-wide registry; the
// duplicate guard keeps repeated assembly (and test stacks that mount the
// handler more than once) from panicking — the maven.RegisterMetadata
// convention.
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
	if strings.HasSuffix(relPath, ".info") ||
		strings.HasSuffix(relPath, ".versionList") ||
		strings.HasSuffix(relPath, ".latest") {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider: the decoded module path
// owns the aggregation identity (the two cache markers included).
func (provider) PackageName(relPath string) (string, bool) {
	if t, ok := parseTarget(relPath); ok {
		return t.module, true
	}
	for _, marker := range []string{"/@v/.versionList", "@latest.latest"} {
		if m := strings.TrimSuffix(relPath, marker); m != relPath && validModule(m) {
			return m, true
		}
	}
	return "", false
}

// Versions implements adapter.MetadataProvider: the @latest candidate
// order is a total order over the protocol's version spellings.
func (provider) Versions() adapter.VersionComparator {
	return comparator{}
}

// comparator adapts CompareVersions to adapter.VersionComparator.
type comparator struct{}

func (comparator) CompareVersions(a, b string) int { return CompareVersions(a, b) }

// UpstreamPath maps one STORAGE-form repository path onto the UPSTREAM wire
// path (internal/remote's optional facet): re-escape the module and version
// elements, and translate the two internal cache markers back onto the
// document endpoints they stand for. Unknown shapes pass through verbatim
// (the generic posture — no provider facet, no rewriting).
func (provider) UpstreamPath(relPath string) string {
	if t, ok := parseTarget(relPath); ok {
		return t.wirePath()
	}
	if m := strings.TrimSuffix(relPath, "/@v/.versionList"); m != relPath && validModule(m) {
		return escapePath(m) + suffixList
	}
	if m := strings.TrimSuffix(relPath, "@latest.latest"); m != relPath && validModule(m) {
		return escapePath(m) + suffixLatest
	}
	return relPath
}
