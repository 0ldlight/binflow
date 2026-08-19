package npm

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// provider is the npm MetadataProvider (architecture section 5.4): the
// packument/metadata split the remote cache keys its two TTLs on, and the
// package identity the virtual aggregation (T-72) groups by. Pure functions
// of the repository-relative path, safe for concurrent use.
type provider struct{}

// Compile-time contract pins.
var (
	_ adapter.MetadataProvider = provider{}
	_ adapter.Handler          = (*Handler)(nil)
)

// Protocol implements adapter.MetadataProvider: the same literal Register
// keyed the handler under (T-63 review N2 — one registration point).
func (provider) Protocol() string { return Protocol }

// Classify implements adapter.MetadataProvider: a packument node is
// regenerable protocol METADATA (short TTL, conditional revalidation);
// everything else — tarballs — is immutable CONTENT. A path outside the npm
// layout defaults to content (the safe side, metadata.go's contract).
func (provider) Classify(relPath string) adapter.MetadataKind {
	return classifyRelPath(relPath)
}

// classifyRelPath is the exported-behavior core (shared with the handler's
// own routing decisions): the packument segment ends the metadata family.
func classifyRelPath(relPath string) adapter.MetadataKind {
	if strings.HasSuffix(relPath, "/"+packumentSeg) {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider: the package identity a
// path belongs to ("" when the path is not a package path under the npm
// layout). Only routes that address the package's STORAGE namespace count —
// the packument node, the tarballs, the revision addresses; pure request
// routes (the /-/ service family, version reads) are not storage paths and
// report ok=false so the virtual aggregation skips them.
func (provider) PackageName(relPath string) (string, bool) {
	// The packument node itself: <pkg>/packument.json (it parses as a
	// "version read" tail, so it is peeled before the route table).
	if rest, ok := strings.CutSuffix(relPath, "/"+packumentSeg); ok && rest != "" {
		return rest, true
	}
	rt, ok := parseRoute(relPath)
	if !ok || rt.name == "" {
		return "", false
	}
	switch rt.kind {
	case routePackument, routePackageRev, routeTarball, routeTarballRev:
		return rt.name, true
	default:
		return "", false
	}
}

// Versions implements adapter.MetadataProvider: npm orders by semver.
func (provider) Versions() adapter.VersionComparator { return semverComparator{} }

// semverComparator adapts compareSemver onto the VersionComparator seam.
type semverComparator struct{}

// CompareVersions implements adapter.VersionComparator: semver precedence,
// build metadata ignored; invalid spellings fall back to lexical order so
// the comparator stays a total order over its input (the contract's
// requirement — validation is the publish chain's business).
func (semverComparator) CompareVersions(a, b string) int { return compareSemver(a, b) }
