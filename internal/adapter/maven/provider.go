package maven

// MetadataProvider registration (T-63's registry seam, T-67 AC②): the
// maven slice of the content/metadata split the remote cache and the
// virtual aggregation key on.

import (
	"github.com/lzwzzy/binflow/internal/adapter"
)

// metadataProvider is the maven MetadataProvider: every method is a pure
// function of the repository-relative path (the registry's contract — no
// request context, no store access, safe for concurrent use).
type metadataProvider struct{}

// Protocol implements adapter.MetadataProvider.
func (metadataProvider) Protocol() string { return Protocol }

// Classify implements adapter.MetadataProvider: maven-metadata.xml (and
// its plugin-group variant, plus their checksum sidecars) is the
// regenerable metadata family — the remote cache's short TTL; every other
// layout-conformant path is immutable package content. A path outside the
// layout defaults to content, the safe side of the split (a long TTL on an
// immutable object is harmless; the reverse is not).
func (metadataProvider) Classify(relPath string) adapter.MetadataKind {
	l, err := Parse(relPath)
	if err != nil {
		return adapter.KindContent
	}
	switch l.Kind {
	case KindMetadata:
		return adapter.KindMetadata
	case KindSidecar:
		// A checksum sidecar of a METADATA document is regenerated with
		// it (T-68 recomputes both); a sidecar of an artifact is bound to
		// immutable content.
		if l.TargetKind == KindMetadata {
			return adapter.KindMetadata
		}
		return adapter.KindContent
	default:
		return adapter.KindContent
	}
}

// PackageName implements adapter.MetadataProvider: the groupId:artifactId
// identity of the path. Version-level paths (and sidecars) collapse onto
// the same pair as the module root, which is what the virtual metadata
// aggregation (T-72) merges on.
//
// The metadata-document ambiguity: a bare metadata path cannot be told
// module-level from version-level without content (the version directory
// spelling would have to be recognized as one). The module-level reading —
// last directory is the artifactId — is the one merged in practice
// (version-level snapshot metadata always sits one directory below a
// module-level spelling of the same GAV), so it is the chosen heuristic;
// T-68/T-72 refine with content when they need the level.
func (metadataProvider) PackageName(relPath string) (string, bool) {
	l, err := Parse(relPath)
	if err != nil {
		return "", false
	}
	if l.OrgPath == "" || l.Module == "" {
		return "", false
	}
	return l.OrgPath + ":" + l.Module, true
}

// Versions implements adapter.MetadataProvider: the Maven ordering
// (maven is the protocol version ordering actually exists for in M3 —
// npm's semver slice is T-69's call, generic has none).
func (metadataProvider) Versions() adapter.VersionComparator { return VersionComparator{} }

// provider is the singleton registered in the process-wide registry.
var provider metadataProvider

// RegisterMetadata registers the maven provider with the adapter registry
// (cmd assembly calls it once at startup; the idempotence guard keeps test
// harnesses that assemble per-case from panicking on the duplicate).
func RegisterMetadata() {
	if _, ok := adapter.ForProtocol(Protocol); ok {
		return
	}
	adapter.RegisterMetadata(provider)
}

// GAVOf exposes the layout's groupId:artifactId spelling for callers
// working with raw paths (log contexts, tests).
func GAVOf(relPath string) (string, bool) {
	return provider.PackageName(relPath)
}

// IsMetadataPath reports whether relPath addresses the metadata family —
// the consumer-side one-liner remote/virtual code can reuse without
// parsing the full layout.
func IsMetadataPath(relPath string) bool {
	return provider.Classify(relPath) == adapter.KindMetadata
}
