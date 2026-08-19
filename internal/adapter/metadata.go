package adapter

import (
	"fmt"
	"sort"
)

// MetadataKind is the content/metadata split of one repository-relative
// path (architecture section 5.4): the remote cache keys its two TTLs on it
// — content_ttl_seconds for immutable package files, metadata_ttl_seconds
// for regenerable protocol documents.
type MetadataKind string

const (
	// KindContent marks an immutable package file (a jar, an npm tarball, a
	// wheel): long cache TTL, and a checksum hit is never revalidated (the
	// immutability principle).
	KindContent MetadataKind = "content"
	// KindMetadata marks a regenerable protocol metadata document
	// (maven-metadata.xml, an npm packument, a PyPI simple index page):
	// short TTL, conditional revalidation on expiry.
	KindMetadata MetadataKind = "metadata"
)

// VersionComparator orders protocol version strings. It is the M4 virtual
// latest-resolution seam (architecture section 11.15); M3's consumer is the
// maven metadata calculator's version ordering (T-68) and the virtual
// metadata aggregation (T-72). Implementations are protocol-specific —
// Maven version ordering is not semver — and must be a total order over the
// protocol's legal version spellings.
type VersionComparator interface {
	// CompareVersions orders a against b: -1 when a < b, 0 when equal,
	// +1 when a > b.
	CompareVersions(a, b string) int
}

// MetadataProvider is the per-protocol metadata extraction SPI (architecture
// section 5.4; oss-structure section 6 insight 1: each protocol package
// ships the handler + layout + metadata-provider triple). One implementation
// lives in each protocol package and registers here; the SERVICE layer
// consumes the registry — the remote cache's TTL split (T-66) and virtual
// version ordering (M4+) — so protocol semantics never leak into
// internal/remote or internal/repo.
//
// Every method is a pure function of the repository-relative path (the
// spelling adapter.Layout produced): no request context, no store access,
// no side effects — implementations must stay safe for concurrent use.
//
// The registry is empty until the M3 protocol tickets register their
// providers (T-67/T-69/T-70): no registration, no consumer, zero behavior
// change (T-63 collects only the contract).
type MetadataProvider interface {
	// Protocol names the package type this provider parses ("maven", "npm",
	// "pypi"); it keys the registry and must equal the protocol Handler's
	// own Protocol().
	Protocol() string
	// Classify reports whether relPath addresses protocol metadata or
	// package content. The remote cache's two TTL columns key on this split;
	// a path outside the protocol's layout defaults to content (safe side:
	// long TTL on an immutable object is harmless, short TTL on one is not).
	Classify(relPath string) MetadataKind
	// PackageName resolves the package identity a path belongs to — the
	// groupId:artifactId pair for maven, the package name for npm, the
	// normalized project name for pypi. ok is false when the path is not a
	// package path under this protocol's layout; the virtual metadata
	// aggregation (T-72) skips such paths.
	PackageName(relPath string) (name string, ok bool)
	// Versions returns the protocol's version ordering, or nil when the
	// protocol defines none (generic). The M4 virtual best-version
	// resolution seam; nil means callers must fall back to first-hit
	// resolution (ADR-0013).
	Versions() VersionComparator
}

// RegisterMetadata adds p to the process-wide metadata-provider registry.
// A nil provider, an empty Protocol or a duplicate protocol panics: assembly
// bugs that must surface at startup, never at request time (the same ruling
// as Register).
func RegisterMetadata(p MetadataProvider) {
	if p == nil {
		panic("adapter: RegisterMetadata(nil provider)")
	}
	proto := p.Protocol()
	if proto == "" {
		panic("adapter: RegisterMetadata: provider has an empty Protocol")
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, dup := reg.byMeta[proto]; dup {
		panic(fmt.Sprintf("adapter: RegisterMetadata: duplicate protocol %q", proto))
	}
	reg.byMeta[proto] = p
}

// ForProtocol resolves the metadata provider of a package type. ok is false
// when the protocol registered no provider — the caller's default must be
// the generic posture (every path is content, first-hit resolution), which
// is what keeps this registry at zero behavior change until the M3 protocol
// tickets register their providers.
func ForProtocol(proto string) (p MetadataProvider, ok bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	p, ok = reg.byMeta[proto]
	return p, ok
}

// MetadataProtocols returns the registered provider protocols sorted
// (diagnostics, the same shape as Protocols).
func MetadataProtocols() []string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	out := make([]string, 0, len(reg.byMeta))
	for p := range reg.byMeta {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
