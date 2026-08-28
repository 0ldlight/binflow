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

	// The rest of the v2 route literals (nuget.md section 2, the 18-endpoint
	// table; every function import also routes without its parens — the
	// DE-noted ignoreParens regex).
	v2Search      = "Search"
	v2Packages    = "Packages"
	v2GetUpdates  = "GetUpdates"
	v2Batch       = "$batch"
	v2Count       = "$count"
	v2Download    = "Download"
	v2PackagePref = "package" // the official client's /package/ prefix (T-287)
	v2IDProp      = "Id"      // the single projection the wire carries

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
	kindV2ServiceDoc               // v2 base root: GET service document / PUT publish (nuget.md section 2 #1/#17)
	kindV2Metadata                 // v2 $metadata
	kindV2Search                   // v2 Search() ±$count
	kindV2FindPackages             // v2 FindPackagesById() ±$count
	kindV2Packages                 // v2 Packages(...) family: feed / single entry / $count / Id projection
	kindV2GetUpdates               // v2 GetUpdates() ±$count
	kindV2Batch                    // v2 $batch (POST)
	kindV2Download                 // v2 Download/{id}/{version}
	kindV2BareNupkg                // v2 {file}.nupkg (any depth, last segment)
	kindV2Push                     // v2 PUT <path prefix> / DELETE <id>/<version>
	kindBareContent                // no plane: raw storage addressing
)

// route is one parsed repo-relative wire target in STORAGE spelling.
type route struct {
	kind    routeKind
	id      string // package id key, lowercase (flatcontainer family)
	version string // version key, normalized lowercase where applicable
	file    string // packageFile: "nupkg" | "sha512" | "nuspec"; page: the file name
	path    string // the bare-content storage path / marker storage path / v2 path argument

	// The v2 OData family's modifiers (nuget.md section 2).
	count        bool   // the /$count suffix
	entryID      string // Packages(Id='x',...) — decoded, verbatim casing
	entryVersion string // Packages(Id='x',Version='y') — decoded, verbatim
	project      string // the /Id projection tail
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

// parseV2 parses the v2 face: nuget.md section 2's 18-endpoint table. The
// base root (empty rest — with or without the trailing slash) is the service
// document / publish root (#1/#17); the function imports route with or
// without their empty parens (the DE-noted ignoreParens posture); every
// other spelling is the bare-download shape when its last segment ends in
// .nupkg (#15), else the publish path form (#18 PUT / #16 DELETE — the
// DELETE arm takes the first two segments as id/version and ignores the
// rest).
func parseV2(rest string, found bool) (route, bool) {
	if !found || rest == "" {
		// v2 or v2/ — the service document (GET) and the publish root (PUT).
		return route{kind: kindV2ServiceDoc}, true
	}
	// The official GetUpdates spelling carries a "/" between the resource
	// and the query string ("GetUpdates()/?packageIds=…"); a trailing slash
	// is never meaningful in the v2 grammar — drop it.
	rest = strings.TrimSuffix(rest, "/")
	seg, tail, more := strings.Cut(rest, "/")
	// The function imports route bare AND with empty parens (ignoreParens);
	// fold the empty-paren spelling onto the bare name before the switch.
	name := strings.TrimSuffix(seg, "()")
	switch name {
	case fileMetadata:
		if more || seg != name {
			return route{}, false
		}
		return route{kind: kindV2Metadata}, true
	case v2Batch:
		if more || seg != name {
			return route{}, false
		}
		return route{kind: kindV2Batch}, true
	case v2Search:
		return parseV2Function(kindV2Search, tail, more)
	case v2GetUpdates:
		return parseV2Function(kindV2GetUpdates, tail, more)
	case findPackagesByID:
		return parseV2Function(kindV2FindPackages, tail, more)
	default:
		if strings.HasPrefix(seg, v2Packages+"(") && strings.HasSuffix(seg, ")") || seg == v2Packages {
			return parseV2Packages(seg, tail, more)
		}
		if name == v2Download && seg == name {
			if !more || tail == "" {
				return route{}, false
			}
			id, version, hasVer := strings.Cut(tail, "/")
			if !validPackageID(id) || !hasVer || version == "" || strings.Contains(version, "/") {
				return route{}, false
			}
			norm, ok := normalizeNuGetVersion(version)
			if !ok {
				return route{}, false
			}
			return route{kind: kindV2Download, id: lowerASCII(id), version: norm}, true
		}
		// The bare .nupkg download shape (#15): any depth, the LAST segment
		// ends in .nupkg, the path addresses storage verbatim.
		if last := lastSegment(rest); last != "" && strings.HasSuffix(last, suffixNupkg) {
			return route{kind: kindV2BareNupkg, path: rest}, true
		}
		// The publish path form (#18): <path prefix> (the official client's
		// /package/ literal strips first); the DELETE arm (#16) takes the
		// first two segments as id/version. The prefix may be any shape —
		// validation is the publish/delete arms' (the identity always comes
		// from the package's own nuspec).
		prefix := rest
		if strings.HasPrefix(prefix, v2PackagePref+"/") {
			prefix = strings.TrimPrefix(prefix, v2PackagePref+"/")
		}
		return route{kind: kindV2Push, path: prefix}, true
	}
}

// parseV2Function parses the shared shape of the argument-less function
// imports (Search/GetUpdates/FindPackagesById): bare or empty-paren head,
// optional /$count tail.
func parseV2Function(kind routeKind, tail string, more bool) (route, bool) {
	if !more {
		return route{kind: kind}, true
	}
	if tail != v2Count {
		return route{}, false
	}
	return route{kind: kind, count: true}, true
}

// parseV2Packages parses the Packages(...) family: the bare literal and the
// empty parens are the full feed (#7/#10); an argument list addresses one
// entry (#8); the /Id tail projects the Id property (#9). Anything else
// after the resource (other projections, deeper paths) is the 404 family.
func parseV2Packages(seg, tail string, more bool) (route, bool) {
	args := ""
	switch {
	case seg == v2Packages:
	case strings.HasPrefix(seg, v2Packages+"(") && strings.HasSuffix(seg, ")"):
		args = seg[len(v2Packages)+1 : len(seg)-1]
	default:
		return route{}, false
	}
	rt := route{kind: kindV2Packages}
	if args != "" {
		// Id='x' | Id='x',Version='y' (whitespace-tolerant; the names fold
		// case-insensitively — nuget.md section 4's CaseInsensitiveMap).
		for _, pair := range strings.Split(args, ",") {
			name, value, ok := strings.Cut(pair, "=")
			if !ok {
				return route{}, false
			}
			name = trimSpaceASCII(name)
			value = trimSpaceASCII(value)
			if len(value) < 2 || value[0] != '\'' || value[len(value)-1] != '\'' {
				return route{}, false
			}
			value = strings.ReplaceAll(value[1:len(value)-1], "''", "'")
			switch lowerASCII(name) {
			case "id":
				rt.entryID = value
			case "version":
				rt.entryVersion = value
			default:
				return route{}, false
			}
		}
		if rt.entryID == "" {
			return route{}, false
		}
	}
	if !more {
		return rt, true
	}
	switch tail {
	case v2Count:
		if rt.entryID != "" {
			return route{}, false // $count addresses the feed, never one entry
		}
		rt.count = true
		return rt, true
	case v2IDProp:
		if rt.entryID == "" {
			return route{}, false
		}
		rt.project = v2IDProp
		return rt, true
	default:
		return route{}, false
	}
}

// lastSegment returns the final path segment ("" for an empty path).
func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
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
