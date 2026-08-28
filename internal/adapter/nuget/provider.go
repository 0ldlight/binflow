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
//     spellings the same bytes — the official immutability duty); the v2
//     markers and the whole .nuGetV3/ family are regenerable protocol
//     documents (metadata class, short TTL, revalidated — nuget.md
//     section 9.3's cache layout).
//   - PackageName feeds the virtual metadata aggregation's skip rule
//     (the leading id segment owns the aggregation identity).
//   - UpstreamPath is the OPTIONAL facet internal/remote consumes at the
//     upstream hop. The v3 document family rides the .nuGetV3/ markers:
//     the marker carries the upstream path VERBATIM (dynamically resolved
//     off the upstream service index at request time — the T-304 L4
//     ruling), so the facet is the identity strip. The canonical
//     flatcontainer spelling (the local layout's identity) keeps the
//     v3-flatcontainer join — the nuget.org-family default the v2 faces'
//     canonical probes and the service-level virtual resolution still
//     ride; the v2 markers translate onto the upstream v2 faces.

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
	// The .nuGetV3/ family: the cached upstream service index and every
	// registration document revalidate on the metadata TTL (nuget.md
	// section 9.3 — "复用 remote 仓缓存到期语义").
	if strings.HasPrefix(relPath, v3CacheDir+"/") {
		return adapter.KindMetadata
	}
	// Everything else on this layout is a regenerable document: the
	// versions index (the canonical identity path local repositories
	// serve by and the v2 canonical probes fetch).
	if strings.HasSuffix(relPath, "/"+fileIndex) {
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
// path (internal/remote's optional facet):
//
//   - .nuGetV3/<upstream-path> → <upstream-path> (the identity strip —
//     the marker was built from the dynamically resolved service-index
//     @id, nuget.md section 9.3);
//   - .nuget-v2/<hex>.xml → api/v2/<resource> and .nuget-v2/dl/<pkg> →
//     api/v2/package/<id>/<version> (sections 7.1/5.3);
//   - the canonical flatcontainer spelling joins the v3-flatcontainer
//     prefix (the nuget.org-family default — the identity the local
//     layout and the service-level member resolution share);
//   - unknown shapes pass through verbatim (the generic posture — no
//     provider facet, no rewriting).
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
	// The cached upstream service index translates onto the v3 feed path
	// (the xsd default v3FeedUrl's relative spelling — the ONE fixed join
	// of the dynamic family; every other resource resolves through this
	// document).
	if relPath == v3FeedMarker {
		return v3FeedUpstreamPath
	}
	// The v3 dynamic-marker family: the upstream path rides the marker.
	if rest, found := strings.CutPrefix(relPath, v3CacheDir+"/"); found && rest != "" {
		return rest
	}
	id, rest, found := strings.Cut(relPath, "/")
	if !found || !validPackageID(id) {
		return relPath
	}
	switch rest {
	case fileIndex:
		return v3FallbackFlatPath + "/" + id + "/" + fileIndex
	default:
		// Everything else under a valid package id is the per-version
		// file family (<id>/<version>/<id>.<version>.{nupkg,nupkg.sha512,nuspec}):
		// it joins identity onto the flatcontainer prefix. The engine
		// only requests paths this adapter addressed, so the shape is
		// already validated at the route layer.
		return v3FallbackFlatPath + "/" + relPath
	}
}
