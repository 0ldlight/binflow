package helm

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The Helm metadata provider (the adapter SPI's per-protocol member,
// architecture section 5.4). Consumers:
//
//   - Classify feeds the remote cache's dual TTL split (the M11 remote
//     ticket's engine hop): the repo-root index.yaml is the regenerable
//     protocol document (metadata class); every .tgz/.prov is immutable
//     per version (content class).
//   - PackageName is the identity a path belongs to. Chart identity is
//     METADATA-carried (Chart.yaml), not path-carried — the path form is
//     the conventional <name>-<version>.tgz, so the best-effort arm
//     splits on the shortest suffix that parses as SemVer and answers
//     the prefix; a path that does not resolve answers ok=false (the
//     virtual ticket reads chart.* node properties for the exact view).
//   - Versions is the SemVer ordering the virtual best-version seam
//     consumes (the same comparator the index sort uses).
//
// UpstreamPath is deliberately absent — the remote translation
// (chartsBaseUrl fallback chain, _external/_transitive) is the remote
// ticket's own.

// provider is the Helm MetadataProvider.
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

// Classify implements adapter.MetadataProvider.
func (provider) Classify(relPath string) adapter.MetadataKind {
	if relPath == fileIndex || strings.HasSuffix(relPath, "/"+fileIndex) {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider (the best-effort
// filename split; see the type comment).
func (provider) PackageName(relPath string) (string, bool) {
	base := relPath
	if i := strings.LastIndexByte(relPath, '/'); i >= 0 {
		base = relPath[i+1:]
	}
	if !strings.HasSuffix(base, suffixTgz) {
		return "", false
	}
	stem := strings.TrimSuffix(base, suffixTgz)
	// The shortest valid-SemVer suffix wins: "my-chart-1.0.0" splits at
	// the last dash whose tail parses, keeping dashed names whole.
	for i := 0; i < len(stem); i++ {
		if stem[i] != '-' {
			continue
		}
		if _, err := parseSemver(stem[i+1:]); err == nil {
			return stem[:i], true
		}
	}
	return "", false
}

// Versions implements adapter.MetadataProvider.
func (provider) Versions() adapter.VersionComparator { return comparator{} }

// comparator adapts compareSemver to adapter.VersionComparator.
type comparator struct{}

func (comparator) CompareVersions(a, b string) int { return compareSemver(a, b) }
