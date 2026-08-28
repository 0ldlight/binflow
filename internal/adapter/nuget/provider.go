package nuget

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The NuGet metadata provider (the adapter SPI's per-protocol triple
// member, architecture section 5.4). Three consumers:
//
//   - Classify feeds the remote cache's dual TTL split: the nupkg and its
//     per-version sidecars are immutable content (the same version
//     spellings the same bytes — the official immutability duty); the
//     versions document and the registration markers are regenerable
//     protocol documents (metadata class, short TTL, revalidated).
//   - PackageName feeds the virtual metadata aggregation's skip rule
//     (the leading id segment owns the aggregation identity).
//   - UpstreamPath is the OPTIONAL facet internal/remote consumes at the
//     upstream hop: BinFlow's storage layout IS the official
//     flatcontainer layout, so package files map identity onto the
//     upstream flatcontainer prefix, and the two internal registration
//     markers translate back onto the document endpoints they stand for.
//
// The upstream prefixes are nuget.org's CURRENT service-index spellings
// (probed live, August 2026: PackageBaseAddress =
// https://api.nuget.org/v3-flatcontainer/, RegistrationsBaseUrl/3.6.0 =
// https://api.nuget.org/v3/registration5-gz-semver2/). nuget.org has
// re-spelled these over the years (registration3, registration5-…); a
// repository whose upstream uses a different spelling configures the
// base URL that makes the join right, and the constants below are the
// one place to update when the public index moves again. T-287 ruling,
// registered for the spec ticket.

const (
	// upstreamFlatPrefix is the flatcontainer path prefix under the
	// configured upstream base.
	upstreamFlatPrefix = "v3-flatcontainer"
	// upstreamRegistrationPrefix is the registration path prefix under
	// the configured upstream base.
	upstreamRegistrationPrefix = "v3/registration5-gz-semver2"
)

// provider is the NuGet MetadataProvider.
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
	if strings.HasSuffix(relPath, suffixNupkg) {
		return adapter.KindContent
	}
	// The per-version sidecars are as immutable as the package itself
	// (the .sha512 IS the package's digest; the .nuspec is the package's
	// own manifest) — content TTL, never revalidated.
	if strings.HasSuffix(relPath, suffixSha512) || strings.HasSuffix(relPath, suffixNuspec) {
		return adapter.KindContent
	}
	// The v2 upstream-response markers are regenerable protocol documents
	// (search feeds revalidate on the metadata TTL; the alternative-download
	// marker ends in .nupkg and stays content above).
	if strings.HasPrefix(relPath, v2CacheDir+"/") {
		return adapter.KindMetadata
	}
	// Everything else on this layout is a regenerable document: the
	// versions index, the registration marker and the page markers.
	if strings.HasSuffix(relPath, "/"+fileIndex) ||
		strings.HasSuffix(relPath, "/"+regMarkerName) ||
		strings.Contains(relPath, "/"+pageMarkerSeg+"/") {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider: the leading id segment
// owns the aggregation identity (markers included).
func (provider) PackageName(relPath string) (string, bool) {
	id, _, found := strings.Cut(relPath, "/")
	if !found || !validPackageID(id) {
		return "", false
	}
	return lowerASCII(id), true
}

// Versions implements adapter.MetadataProvider: the NuGet order is a
// total order over the protocol's version spellings.
func (provider) Versions() adapter.VersionComparator { return comparator{} }

// comparator adapts compareNuGetVersions to adapter.VersionComparator.
type comparator struct{}

func (comparator) CompareVersions(a, b string) int { return compareNuGetVersions(a, b) }

// UpstreamPath maps one STORAGE-form repository path onto the UPSTREAM
// path (internal/remote's optional facet): package files join onto the
// flatcontainer prefix, the registration markers translate onto their
// document endpoints, the v2 markers (nuget.md sections 5.3/7.1) translate
// onto the upstream v2 faces (the feed context path api/v2 — the xsd
// default — for the search family, api/v2/package for the alternative
// download), and unknown shapes pass through verbatim (the generic
// posture — no provider facet, no rewriting).
func (provider) UpstreamPath(relPath string) string {
	// The v2 alternative-download marker first (it carries no package id
	// segment at the front, so the flat walk below never sees it).
	if _, found := strings.CutPrefix(relPath, v2CacheDir+"/dl/"); found {
		if id, version, ok := v2DownloadCacheOf(relPath); ok {
			// Section 5.3: non-smart upstream sources get the version's
			// SemVer2 build metadata stripped.
			version, _, _ = strings.Cut(version, "+")
			return v2DownloadContextPath + "/" + id + "/" + version
		}
		return relPath
	}
	if resource, ok := v2ResourceOfCachePath(relPath); ok {
		return v2FeedContextPath + "/" + resource
	}
	id, rest, found := strings.Cut(relPath, "/")
	if !found || !validPackageID(id) {
		return relPath
	}
	switch {
	case rest == fileIndex:
		return upstreamFlatPrefix + "/" + id + "/" + fileIndex
	case rest == regMarkerName:
		return upstreamRegistrationPrefix + "/" + id + "/" + fileIndex
	case strings.HasPrefix(rest, pageMarkerSeg+"/"):
		return upstreamRegistrationPrefix + "/" + id + "/" + rest
	default:
		// Everything else under a valid package id is the per-version
		// file family (<id>/<version>/<id>.<version>.{nupkg,nupkg.sha512,nuspec}):
		// it joins identity onto the flatcontainer prefix. The engine
		// only requests paths this adapter addressed, so the shape is
		// already validated at the route layer.
		return upstreamFlatPrefix + "/" + relPath
	}
}
