package nuget

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// Wire-path grammar and the storage forms (doc.go's layout table).
//
// layout() is the single decode point: percent-decode first (the shared
// 400 defense), repository-key and path-shape validation, then the plane
// split. Everything downstream sees decoded, lowercase-keyed storage
// spellings.

const (
	// planeV3/planeV2 are the protocol plane segments (inside the
	// repository namespace, after the router's api-mount rewrite).
	planeV3 = "v3"
	planeV2 = "v2"

	// segFlat/segRegistration/segQuery are the v3 sub-route literals.
	segFlat         = "flatcontainer"
	segRegistration = "registration"
	segQuery        = "query"

	// fileIndex is the versions-document and service-index file name.
	fileIndex = "index.json"
	// fileMetadata is the v2 EDMX document's literal ($ is a legal path
	// byte for the OData face only).
	fileMetadata = "$metadata"
	// findPackagesByID is the v2 feed literal, with or without the empty
	// parens (the client spellings both occur). The VALUE keeps the
	// protocol's own casing ("FindPackagesById" — the official OData
	// import spelling), so the const name follows Go, the literal follows
	// the wire.
	findPackagesByID = "FindPackagesById"

	// sidecar suffixes.
	suffixNupkg   = ".nupkg"
	suffixSha512  = ".nupkg.sha512"
	suffixNuspec  = ".nuspec"
	pageMarkerSeg = ".page"
	regMarkerName = ".registration"
)

// routeKind enumerates the routable wire targets.
type routeKind int

const (
	kindUnknown          routeKind = iota
	kindServiceIndex               // v3 index.json
	kindVersions                   // v3 flatcontainer/<id>/index.json
	kindPackageFile                // v3 flatcontainer/<id>/<ver>/<id>.<ver>.{nupkg,nupkg.sha512,nuspec}
	kindPush                       // v3 flatcontainer/<id>/<ver> (PUT/DELETE)
	kindPushDirect                 // v3 flatcontainer itself (PUT, identity from the nuspec)
	kindRegistration               // v3 registration/<id>/index.json
	kindRegistrationPage           // v3 registration/<id>/page/<file>
	kindSearch                     // v3 query
	kindV2Feed                     // v2 FindPackagesById()
	kindV2Metadata                 // v2 $metadata
	kindV2Push                     // v2 PUT <id>/<version>
	kindBareContent                // no plane: raw storage addressing
)

// route is one parsed repo-relative wire target in STORAGE spelling.
type route struct {
	kind    routeKind
	id      string // package id key, lowercase (flatcontainer family)
	version string // version key, normalized lowercase where applicable
	file    string // packageFile: "nupkg" | "sha512" | "nuspec"; page: the file name
	path    string // the bare-content storage path / marker storage path
}

// layout splits one /binflow-stripped request into repoKey plus the
// STORAGE-form repo-relative path, running the decode/validation order the
// shared adapter contract fixes (percent-decode, key validation, segment
// walk). Handler.Layout is the adapter-facing wrapper.
func layout(r *http.Request) (string, string, error) {
	if r == nil || r.URL == nil {
		return "", "", fmt.Errorf("%w: empty request URL", adapter.ErrBadRequestPath)
	}
	raw := r.URL.EscapedPath()
	if raw == "" {
		raw = r.URL.Path
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", "", fmt.Errorf("%w: malformed percent-encoding in %q: %w", adapter.ErrBadRequestPath, raw, err)
	}
	decoded = strings.TrimPrefix(decoded, "/")
	if decoded == "" {
		return "", "", fmt.Errorf("%w: path is empty (no repository key)", adapter.ErrBadRequestPath)
	}
	key, rest, _ := strings.Cut(decoded, "/")
	if key == "" {
		return "", "", fmt.Errorf("%w: empty repository key", adapter.ErrBadRequestPath)
	}
	if len(key) > adapter.MaxRepoKeyLen {
		return "", "", fmt.Errorf("%w: repository key longer than %d characters", adapter.ErrBadRequestPath, adapter.MaxRepoKeyLen)
	}
	if adapter.IsReservedSegment(key) {
		return "", "", fmt.Errorf("%w: %q is a reserved routing segment", adapter.ErrBadRequestPath, key)
	}
	if err := validateRelPath(rest); err != nil {
		return "", "", err
	}
	return key, rest, nil
}

// validateRelPath enforces the shared artifact-path rules on the decoded
// repo-relative portion. The empty rest is legal (the repository root
// probe). A trailing slash addresses a folder and is preserved for the
// bare-content face only; the protocol faces reject it in parseRoute.
func validateRelPath(rel string) error {
	if rel == "" {
		return nil
	}
	if len(rel) > adapter.MaxRelPathLen {
		return fmt.Errorf("%w: artifact path of %d characters exceeds the %d limit",
			adapter.ErrBadRequestPath, len(rel), adapter.MaxRelPathLen)
	}
	if strings.Contains(rel, "\\") {
		return fmt.Errorf("%w: backslash is not a path separator", adapter.ErrBadRequestPath)
	}
	if strings.ContainsFunc(rel, isControlRune) {
		return fmt.Errorf("%w: control characters are not allowed in artifact paths", adapter.ErrBadRequestPath)
	}
	body := strings.TrimSuffix(rel, "/")
	if body == "" {
		return fmt.Errorf("%w: empty artifact path segments in %q", adapter.ErrBadRequestPath, rel)
	}
	for _, seg := range strings.Split(body, "/") {
		switch seg {
		case "":
			return fmt.Errorf("%w: empty path segment in %q (double slash?)", adapter.ErrBadRequestPath, rel)
		case ".", "..":
			return fmt.Errorf("%w: dot segment %q in %q escapes or dilutes the repository root",
				adapter.ErrBadRequestPath, seg, rel)
		}
	}
	return nil
}

// parseRoute recognizes the routable shapes on a decoded repo-relative
// path. ok is false for every unrecognized spelling (the 404 family).
//
// Case rules: the plane and sub-route literals are case-sensitive (the
// official protocol's route spellings are); package ids and versions are
// lowercased into their storage keys (the flatcontainer contract — the
// official storage keys are the lowercased id and normalized-lowercase
// version).
func parseRoute(rel string) (route, bool) {
	plane, rest, found := strings.Cut(rel, "/")
	switch plane {
	case planeV3:
		return parseV3(rest, found)
	case planeV2:
		return parseV2(rest, found)
	default:
		// A path without a plane segment is bare storage addressing
		// (curl/debug reachability of the stored nodes).
		return route{kind: kindBareContent, path: rel}, rel != ""
	}
}

// parseV3 parses the v3 face. found is false for the bare "/<repo>/v3"
// probe (404 — the index lives at v3/index.json).
func parseV3(rest string, found bool) (route, bool) {
	if !found || rest == "" {
		return route{}, false
	}
	seg, tail, more := strings.Cut(rest, "/")
	switch seg {
	case fileIndex:
		if more {
			return route{}, false
		}
		return route{kind: kindServiceIndex}, true
	case segQuery:
		if more {
			return route{}, false
		}
		return route{kind: kindSearch}, true
	case segFlat:
		if tail == "" {
			// The modern dotnet client (8.x verified live, T-287) PUTs the
			// package DIRECTLY to the PackagePublish @id — no id/version in
			// the URL, the identity comes from the embedded nuspec.
			return route{kind: kindPushDirect}, true
		}
		return parseFlatContainer(tail, more)
	case segRegistration:
		return parseRegistration(tail, more)
	default:
		return route{}, false
	}
}

// parseFlatContainer parses flatcontainer/<…>.
func parseFlatContainer(tail string, more bool) (route, bool) {
	if !more || tail == "" {
		return route{}, false
	}
	id, rest, found := strings.Cut(tail, "/")
	if !validPackageID(id) {
		return route{}, false
	}
	id = lowerASCII(id)
	if !found || rest == "" {
		return route{}, false
	}
	// versions document: flatcontainer/<id>/index.json
	if rest == fileIndex {
		return route{kind: kindVersions, id: id, path: id + "/" + fileIndex}, true
	}
	version, fileSeg, hasFile := strings.Cut(rest, "/")
	norm, ok := normalizeNuGetVersion(version)
	if !ok {
		return route{}, false
	}
	if !hasFile || fileSeg == "" {
		// push/unlist target: flatcontainer/<id>/<version>
		return route{kind: kindPush, id: id, version: norm}, true
	}
	if strings.Contains(fileSeg, "/") {
		return route{}, false
	}
	switch fileSeg {
	case id + "." + norm + suffixNupkg:
		return route{kind: kindPackageFile, id: id, version: norm, file: "nupkg"}, true
	case id + "." + norm + suffixSha512:
		return route{kind: kindPackageFile, id: id, version: norm, file: "sha512"}, true
	case id + "." + norm + suffixNuspec:
		return route{kind: kindPackageFile, id: id, version: norm, file: "nuspec"}, true
	default:
		return route{}, false
	}
}

// parseRegistration parses registration/<…>.
func parseRegistration(tail string, more bool) (route, bool) {
	if !more || tail == "" {
		return route{}, false
	}
	id, rest, found := strings.Cut(tail, "/")
	if !validPackageID(id) {
		return route{}, false
	}
	id = lowerASCII(id)
	if !found || rest == "" {
		return route{}, false
	}
	if rest == fileIndex {
		return route{kind: kindRegistration, id: id, path: regMarker(id)}, true
	}
	pageSeg, file, isPage := strings.Cut(rest, "/")
	if pageSeg != pageMarkerSeg || !isPage || file == "" {
		return route{}, false
	}
	// Page files are the upstream pagination slugs (lo/hi version pairs);
	// the segment walk has already rejected dot/empty segments, and the
	// charset is bounded by the same rules.
	if len(file) > 128 || strings.ContainsFunc(file, isControlRune) {
		return route{}, false
	}
	return route{kind: kindRegistrationPage, id: id, file: file, path: pageMarker(id, file)}, true
}

// parseV2 parses the v2 face.
func parseV2(rest string, found bool) (route, bool) {
	if !found || rest == "" {
		return route{}, false
	}
	seg, tail, more := strings.Cut(rest, "/")
	switch {
	case seg == fileMetadata && !more:
		return route{kind: kindV2Metadata}, true
	case strings.HasPrefix(seg, findPackagesByID):
		// With or without the empty parens; anything else after the literal
		// (a real argument list) is not the minimal face.
		suffix := strings.TrimPrefix(seg, findPackagesByID)
		if suffix != "" && suffix != "()" {
			return route{}, false
		}
		if more {
			// FindPackagesById()/$count is deliberately absent (the K28
			// ruling: only the feed itself ships; $count waits for T-280).
			return route{}, false
		}
		return route{kind: kindV2Feed}, true
	default:
		// v2 push: <id>/<version> (the Artifactory publish shape); the
		// official client's /package/<id>/<version> spelling strips its
		// literal prefix first.
		if seg == "package" {
			if !more {
				return route{}, false
			}
			seg, tail, more = strings.Cut(tail, "/")
		}
		id, version, hasVer := seg, tail, more
		if !validPackageID(id) || !hasVer || version == "" || strings.Contains(version, "/") {
			return route{}, false
		}
		norm, ok := normalizeNuGetVersion(version)
		if !ok {
			return route{}, false
		}
		return route{kind: kindV2Push, id: lowerASCII(id), version: norm}, true
	}
}

// pkgRef is the canonical lowercase (id, version) storage key pair.
type pkgRef struct {
	id      string
	version string
}

func (p pkgRef) dir() string    { return p.id + "/" + p.version }
func (p pkgRef) nupkg() string  { return p.dir() + "/" + p.id + "." + p.version + suffixNupkg }
func (p pkgRef) sha512() string { return p.dir() + "/" + p.id + "." + p.version + suffixSha512 }
func (p pkgRef) nuspec() string { return p.dir() + "/" + p.id + "." + p.version + suffixNuspec }

// versionsPath is the flatcontainer versions document's storage path (the
// identity mapping: remote caches live exactly where the wire asks).
func versionsPath(id string) string { return id + "/" + fileIndex }

// regMarker is the remote registration index's internal cache path.
func regMarker(id string) string { return id + "/" + regMarkerName }

// pageMarker is a remote registration page's cache path.
func pageMarker(id, file string) string { return id + "/" + pageMarkerSeg + "/" + file }

// versionOfNupkgNode extracts the version key of one stored nupkg node,
// verifying the whole flatcontainer spelling (the file name must be
// <id>.<version>.nupkg inside the <id>/<version>/ directory — a foreign
// file in the tree is not a package version).
func versionOfNupkgNode(path, id string) (string, bool) {
	prefix := id + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := path[len(prefix):]
	version, file, found := strings.Cut(rest, "/")
	if !found || version == "" {
		return "", false
	}
	if file != id+"."+version+suffixNupkg {
		return "", false
	}
	if _, ok := normalizeNuGetVersion(version); !ok {
		return "", false
	}
	return version, true
}
