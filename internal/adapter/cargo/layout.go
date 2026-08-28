package cargo

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// Wire-path grammar and the storage forms (doc.go's layout table).
//
// layout() is the single decode point (the nuget/goproxy posture):
// percent-decode first (the shared 400 defense), repository-key and
// path-shape validation, then the route split. Everything downstream sees
// decoded, storage-spelling paths.

const (
	// segIndex is the sparse-index namespace (config.json + pkgPath files).
	segIndex = "index"
	// fileConfig is the sparse entry document's name, inside index/.
	fileConfig = "config.json"
	// segV1 is the download plane's first literal (dl base = v1/crates).
	segV1 = "v1"
	// segCrates is the download plane's second literal.
	segCrates = "crates"
	// segDownload is the download plane's tail literal.
	segDownload = "download"
	// segAPI is the registry web API's prefix (api/v1/crates/…).
	segAPI = "api"
	// segNew is the publish target's literal.
	segNew = "new"
	// verbYank/verbUnyank are the yank family's tail literals.
	verbYank   = "yank"
	verbUnyank = "unyank"
	// gitFaces are the git-index protocol's request shapes (spec section
	// 1: sparse only — the git face answers the deprecation 404).
	gitInfoRefs = "info/refs"
	gitUploadPK = "git-upload-pack"

	// dirCrates is the storage directory of the package blobs.
	dirCrates = "crates"
	// dirMeta is the storage directory of the publish metadata sidecars.
	dirMeta = ".cargo"
	// suffixCrate is the package blob's file suffix.
	suffixCrate = ".crate"
	// suffixMetaJSON is the metadata sidecar's file suffix.
	suffixMetaJSON = ".json"

	// fileOriginalConfig is the remote cache's verbatim copy of the
	// UPSTREAM config.json (spec section 8 / S4: cached at the repository
	// root, preserved in its original form so the upstream dl/api bases
	// stay inspectable; BinFlow's own served config.json is synthesized
	// self-pointing either way).
	fileOriginalConfig = "config.original.json"
	// dirSearchCache is the remote cache's directory of upstream search
	// responses, keyed by the hex of the verbatim query string (the
	// marker-document posture: a wire document with no storage shape of
	// its own caches under a synthetic path the provider's UpstreamPath
	// facet maps back onto the query-carrying endpoint).
	dirSearchCache = dirMeta + "/search"

	// propName … are the .crate node's protocol properties (spec section 4).
	propName        = "crate.name"
	propVersion     = "crate.version"
	propDescription = "crate.description"
	propKeywords    = "crate.keywords"
	propCategories  = "crate.categories"
	propYanked      = "crate.yanked"

	// maxCrateNameLen is the crate-name ceiling (the crates.io restriction
	// set the spec adopts).
	maxCrateNameLen = 64
)

// routeKind enumerates the routable wire targets.
type routeKind int

const (
	kindUnknown     routeKind = iota
	kindRoot                  // "" — the repository probe
	kindConfig                // index/config.json
	kindIndexFile             // index/{pkgPath}
	kindDownload              // v1/crates/{name}/{version}/download
	kindPublish               // api/v1/crates/new (PUT)
	kindSearch                // api/v1/crates (GET, query)
	kindYank                  // api/v1/crates/{n}/{v}/yank (DELETE)
	kindUnyank                // api/v1/crates/{n}/{v}/unyank (PUT)
	kindGitFace               // info/refs | git-upload-pack (the deprecated face)
	kindBareContent           // none of the above: raw storage addressing
)

// route is one parsed repo-relative wire target in STORAGE spelling.
type route struct {
	kind    routeKind
	name    string // crate name, original case (index line / property rule)
	version string // version spelling as published
	pkgPath string // index/{pkgPath} path (index files)
	path    string // the bare-content storage path
}

// layout splits one /binflow-stripped request into repoKey plus the
// STORAGE-form repo-relative path (the shared adapter contract's decode
// order). Handler.Layout is the adapter-facing wrapper.
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
// repo-relative portion. The empty rest is legal (the repository-root
// probe). A trailing slash addresses a folder and stays for the bare face
// only; the protocol faces reject it in parseRoute.
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

// isControlRune reports C0 controls and DEL.
func isControlRune(r rune) bool { return r < 0x20 || r == 0x7f }

// parseRoute recognizes the routable shapes on a decoded repo-relative
// path. ok is false for every unrecognized spelling — which is also the
// owners/init answer (the unknown-path 404, spec section 11.2); the
// git-index shapes are carved out first so they can answer the
// deprecation wording instead of the plain 404.
func parseRoute(rel string) (route, bool) {
	if rel == "" {
		return route{kind: kindRoot}, true
	}
	switch rel {
	case gitInfoRefs, gitUploadPK:
		return route{kind: kindGitFace, path: rel}, true
	}
	plane, rest, _ := strings.Cut(rel, "/")
	switch plane {
	case segIndex:
		return parseIndexPlane(rest)
	case segV1:
		return parseDownloadPlane(rest)
	case segAPI:
		return parseAPIPlane(rest)
	default:
		// A path outside the protocol planes is bare storage addressing
		// (curl/debug reachability of the stored nodes).
		return route{kind: kindBareContent, path: rel}, true
	}
}

// parseIndexPlane parses index/{config.json | pkgPath}.
func parseIndexPlane(rest string) (route, bool) {
	if rest == "" {
		return route{}, false
	}
	if rest == fileConfig {
		return route{kind: kindConfig}, true
	}
	name, ok := indexPkgPathName(rest)
	if !ok {
		return route{}, false
	}
	return route{kind: kindIndexFile, pkgPath: rest, name: name}, true
}

// parseDownloadPlane parses v1/crates/{name}/{version}/download.
func parseDownloadPlane(rest string) (route, bool) {
	if !strings.HasPrefix(rest, segCrates+"/") {
		return route{}, false
	}
	tail := rest[len(segCrates)+1:]
	name, remainder, found := strings.Cut(tail, "/")
	if !found || !validCrateName(name) {
		return route{}, false
	}
	version, suffix, found := strings.Cut(remainder, "/")
	if !found || suffix != segDownload || version == "" {
		return route{}, false
	}
	if _, err := parseSemver(version); err != nil {
		return route{}, false
	}
	return route{kind: kindDownload, name: name, version: version}, true
}

// parseAPIPlane parses api/v1/crates[/new|/{n}/{v}/yank|/{n}/{v}/unyank].
func parseAPIPlane(rest string) (route, bool) {
	if rest != segV1 && !strings.HasPrefix(rest, segV1+"/") {
		return route{}, false
	}
	if rest == segV1 {
		return route{}, false
	}
	tail := rest[len(segV1)+1:]
	if tail != segCrates && !strings.HasPrefix(tail, segCrates+"/") {
		return route{}, false
	}
	if tail == segCrates {
		return route{kind: kindSearch}, true
	}
	rest = tail[len(segCrates)+1:]
	if rest == segNew {
		return route{kind: kindPublish}, true
	}
	// Remaining shapes: {name}/{version}/{yank|unyank}. The owners family
	// ({name}/owners[/user/{login}]) does not parse — the unknown 404.
	name, remainder, found := strings.Cut(rest, "/")
	if !found || !validCrateName(name) {
		return route{}, false
	}
	version, verb, found := strings.Cut(remainder, "/")
	if !found || version == "" {
		return route{}, false
	}
	if _, err := parseSemver(version); err != nil {
		return route{}, false
	}
	switch verb {
	case verbYank:
		return route{kind: kindYank, name: name, version: version}, true
	case verbUnyank:
		return route{kind: kindUnyank, name: name, version: version}, true
	default:
		return route{}, false
	}
}

// ---- crate names and the index path derivation (spec sections 3.2/3.3) ----

// validCrateName checks the crate-name restriction set the spec adopts
// (the crates.io limit): ASCII letter first, then letters/digits/'-'/'_',
// at most 64 characters.
func validCrateName(name string) bool {
	if name == "" || len(name) > maxCrateNameLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			// leading and body: always legal
		case i > 0 && (c >= '0' && c <= '9' || c == '-' || c == '_'):
			// body only: a crate name opens with a letter
		default:
			return false
		}
	}
	return true
}

// indexPath derives the index pkgPath of one crate name (spec section
// 3.2's four tiers). The FILE NAME is the lowercased name (the official
// note); the tier keys on the name's own length.
func indexPath(name string) string {
	lower := strings.ToLower(name)
	switch n := len(lower); n {
	case 1:
		return "1/" + lower
	case 2:
		return "2/" + lower
	case 3:
		return "3/" + lower[:1] + "/" + lower
	default:
		return lower[:2] + "/" + lower[2:4] + "/" + lower
	}
}

// indexPkgPathName validates one index/{pkgPath} spelling against the
// tier grammar and returns the (lowercased) crate name it addresses. ok
// is false for every non-canonical shape — the 404 family (official
// allows 404/410/451; BinFlow takes 404).
func indexPkgPathName(pkgPath string) (string, bool) {
	segs := strings.Split(pkgPath, "/")
	var name string
	switch len(segs) {
	case 2:
		if segs[0] == "1" && len(segs[1]) == 1 {
			name = segs[1]
		} else if segs[0] == "2" && len(segs[1]) == 2 {
			name = segs[1]
		} else {
			return "", false
		}
	case 3:
		if segs[0] == "3" && len(segs[1]) == 1 && len(segs[2]) == 3 {
			name = segs[2]
		} else if len(segs[0]) == 2 && len(segs[1]) == 2 && len(segs[2]) >= 4 {
			name = segs[2]
		} else {
			return "", false
		}
	default:
		return "", false
	}
	if !validCrateName(name) {
		return "", false
	}
	return name, true
}

// ---- storage addressing (spec section 4) ----

// cratePath is the .crate blob's storage path.
func cratePath(name, version string) string {
	return dirCrates + "/" + name + "/" + name + "-" + version + suffixCrate
}

// metaPath is the publish-metadata sidecar's storage path.
func metaPath(name, version string) string {
	return dirMeta + "/" + dirCrates + "/" + name + "/" + name + "-" + version + suffixMetaJSON
}

// crateDir is the storage directory holding one crate's versions.
func crateDir(name string) string { return dirCrates + "/" + name + "/" }

// splitCrateNode splits a stored .crate node path into (name, version);
// ok is false for every other shape (the search face and the index
// rewrite both feed on this).
func splitCrateNode(path string) (string, string, bool) {
	if !strings.HasPrefix(path, dirCrates+"/") {
		return "", "", false
	}
	rest := path[len(dirCrates)+1:]
	name, file, found := strings.Cut(rest, "/")
	if !found || !validCrateName(name) {
		return "", "", false
	}
	prefix := name + "-"
	if !strings.HasPrefix(file, prefix) || !strings.HasSuffix(file, suffixCrate) {
		return "", "", false
	}
	version := file[len(prefix) : len(file)-len(suffixCrate)]
	if version == "" {
		return "", "", false
	}
	if _, err := parseSemver(version); err != nil {
		return "", "", false
	}
	return name, version, true
}

// ---- the remote cache's synthetic paths (spec section 8 / S4) ----

// searchCachePath renders the storage path one upstream search response
// caches under: .cargo/search/<hex of the verbatim query>.json. The hex
// keeps the key path-legal and collision-free while staying a PURE,
// reversible encoding — the provider's UpstreamPath facet decodes it back
// into the query-carrying upstream endpoint.
func searchCachePath(rawQuery string) string {
	return dirSearchCache + "/" + hex.EncodeToString([]byte(rawQuery)) + suffixMetaJSON
}

// searchQueryOf decodes one search-cache storage path back into its
// verbatim query string; ok is false for every other shape.
func searchQueryOf(path string) (string, bool) {
	if !strings.HasPrefix(path, dirSearchCache+"/") {
		return "", false
	}
	name, ok := strings.CutSuffix(strings.TrimPrefix(path, dirSearchCache+"/"), suffixMetaJSON)
	if !ok || name == "" {
		return "", false
	}
	raw, err := hex.DecodeString(name)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// derivedPathCrate resolves the crate a server-derived storage path
// belongs to — the index family by its pkgPath grammar, the metadata
// sidecar family by its crate directory. ok is false for paths outside
// both families (the caller's index convergence simply skips them).
func derivedPathCrate(path string) (string, bool) {
	if rest, found := strings.CutPrefix(path, segIndex+"/"); found {
		if name, ok := indexPkgPathName(rest); ok {
			return name, true
		}
		return "", false
	}
	if rest, found := strings.CutPrefix(path, dirMeta+"/"+dirCrates+"/"); found {
		if name, _, ok := strings.Cut(rest, "/"); ok && validCrateName(name) {
			return name, true
		}
	}
	return "", false
}
