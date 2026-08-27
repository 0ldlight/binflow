package conan

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// Wire-path grammar and the storage forms (doc.go's layout table).
//
// layout() is the single decode point (the cargo/nuget posture):
// percent-decode first (the shared 400 defense), repository-key and
// path-shape validation, then the route split. Everything downstream sees
// decoded, storage-spelling paths — including the v2 <ref> segments, whose
// case is preserved verbatim (a `Hello` package is a different package from
// `hello`; only the v1 files channel's coordinate ORDER differs, not its
// casing rule).

const (
	// segV2/segV1 are the protocol planes' first literals.
	segV2 = "v2"
	segV1 = "v1"
	// segConans is the v2 plane's second literal (v2/conans/…).
	segConans = "conans"
	// segUsers is the v1 handshake family's literal.
	segUsers = "users"
	// segFiles is the v1 direct channel's literal.
	segFiles = "files"
	// segPing is the capability probe's literal.
	segPing = "ping"
	// verbAuthenticate/verbCheckCreds are the handshake verbs (the
	// latter is a URL segment, not a secret — gosec's G101 misreads it).
	verbAuthenticate = "authenticate"
	verbCheckCreds   = "check_credentials" //nolint:gosec // G101: a wire literal, not a credential
	// segRevisions/segFilesTail/segPackages/segLatest/segSearch are the
	// shared segment literals of both planes.
	segRevisions = "revisions"
	segFilesTail = "files"
	segPackages  = "packages"
	segLatest    = "latest"
	segSearch    = "search"
	// verbDigest/verbDownloadURLs/verbUploadURLs/verbRemoveFiles/verbDelete
	// are the v1 data verbs.
	verbDigest       = "digest"
	verbDownloadURLs = "download_urls"
	verbUploadURLs   = "upload_urls"
	verbRemoveFiles  = "remove_files"
	verbDelete       = "delete"
	// dirExport is the recipe file tree's storage literal and
	// dirPackage the package tree's — SINGULAR, the spec section 4 layout
	// spelling (the v2 WIRE family uses the plural "packages"; the two
	// vocabularies never mix).
	dirExport       = "export"
	dirPackage      = "package"
	recipeIndexFile = "index.json"
	timestampFile   = ".timestamp"
	// revDefaultV1 is the v1 files channel's default revision segment.
	revDefaultV1 = "0"

	// maxRefSegLen bounds one <ref> segment (name/version/user/channel).
	maxRefSegLen = 128
	// maxRevLen bounds a revision or packageId segment.
	maxRevLen = 128
)

// routeKind enumerates the routable wire targets.
type routeKind int

const (
	// kindNotFoundFamily marks a shape inside a protocol plane that is not
	// one of the plane's endpoints (v2/conans/garbage) — the plane exists,
	// so the refusal is the conan 404 family, not the bare-storage fallthrough.
	kindNotFoundFamily     routeKind = iota
	kindV2Search                     // v2/conans/search
	kindV2RecipeDelete               // v2/conans/<ref> (DELETE)
	kindV2RefSearch                  // v2/conans/<ref>/search
	kindV2Latest                     // <ref>/latest
	kindV2Revisions                  // <ref>/revisions
	kindV2RevDelete                  // <ref>/revisions/<rRev> (DELETE)
	kindV2RevSearch                  // <ref>/revisions/<rRev>/search
	kindV2Files                      // <ref>/revisions/<rRev>/files[/<path>]
	kindV2PackagesDelete             // <ref>/revisions/<rRev>/packages (DELETE)
	kindV2PkgLatest                  // …/packages/<pid>/latest
	kindV2PkgRevisions               // …/packages/<pid>/revisions
	kindV2PkgRevDelete               // …/packages/<pid>/revisions/<pRev> (DELETE)
	kindV2PkgFiles                   // …/packages/<pid>/revisions/<pRev>/files[/<path>]
	kindV1Ping                       // v1/ping
	kindV1Authenticate               // v1/users/authenticate
	kindV1CheckCredentials           // v1/users/check_credentials
	kindV1Search                     // v1/conans/search
	kindV1RecipeSnapshot             // v1/conans/<ref> (GET/DELETE)
	kindV1RefSearch                  // v1/conans/<ref>/search
	kindV1Digest                     // v1/conans/<ref>/digest | …/packages/<pid>/digest
	kindV1DownloadURLs               // v1/conans/<ref>/download_urls | package arm
	kindV1UploadURLs                 // v1/conans/<ref>/upload_urls | package arm
	kindV1RemoveFiles                // v1/conans/<ref>/remove_files | package arm
	kindV1PkgSnapshot                // v1/conans/<ref>/packages/<pid> (GET)
	kindV1PackagesDelete             // v1/conans/<ref>/packages/delete (POST)
	kindV1Files                      // v1/files/<user>/<name>/<ver>/<c>/[0/]export|package/…
)

// ref is one parsed recipe coordinate — the four <ref> segments plus the
// coordinate-order storage root they map to.
type ref struct {
	name, version, user, channel string
}

// wireRef renders the v2 response spelling name/version@user/channel (the
// `_` placeholder rides verbatim, S12).
func (r ref) wireRef() string {
	return r.name + "/" + r.version + "@" + r.user + "/" + r.channel
}

// coordinateRoot renders the storage-order root (spec section 4):
// <user>/<name>/<version>/<channel>.
func (r ref) coordinateRoot() string {
	return r.user + "/" + r.name + "/" + r.version + "/" + r.channel
}

// route is one parsed repo-relative wire target in STORAGE spelling.
type route struct {
	kind routeKind
	ref  ref    // the coordinate, when the shape carries one
	rRev string // recipe revision, when addressed
	pRev string // package revision, when addressed
	pid  string // packageId, when addressed
	path string // the trailing file path (files endpoints / v1 channel)
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
// repo-relative portion (the cargo posture verbatim; the empty rest is the
// repository-root probe, which this adapter serves as the honest 404 —
// conan has no root document).
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
	if strings.Contains(rel, " ") {
		// Spec section 6.5: a path containing a space is the pinned 400.
		return fmt.Errorf("%w: Path is invalid (spaces are not allowed in conan paths)", adapter.ErrBadRequestPath)
	}
	if strings.HasSuffix(rel, "/") {
		// No conan endpoint addresses a folder; every trailing-slash
		// spelling is a malformed route, not a directory probe.
		return fmt.Errorf("%w: conan endpoints do not address folder paths (%q)", adapter.ErrBadRequestPath, rel)
	}
	body := rel
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
// path. ok is false for every shape outside the two protocol planes — the
// bare-storage fallthrough this adapter refuses with the conan 404 (the
// whole namespace belongs to the layout; there is no curl/debug raw face
// here because every path a client could address is one of the endpoints).
func parseRoute(rel string) (route, bool) {
	plane, rest, found := strings.Cut(rel, "/")
	if !found {
		return route{kind: kindNotFoundFamily}, true
	}
	switch plane {
	case segV2:
		return parseV2(rest)
	case segV1:
		return parseV1(rest)
	default:
		return route{}, false
	}
}

// parseV2 parses v2/conans/… (spec section 3.1's table, all seventeen
// endpoints incl. the HEAD arms) — plus the handshake family the REAL
// conan 2 client addresses under its own version prefix: conan 2.31 walks
// v2/users/authenticate (not the v1 spelling the spec section 2 table
// carries; serving BOTH prefixes is the reconciliation — the v1 legs stay
// for the conan 1.x clients and the spec's own text).
func parseV2(rest string) (route, bool) {
	if !strings.HasPrefix(rest, segConans+"/") {
		if rt, ok := parseHandshake(rest); ok {
			return rt, true
		}
		return route{kind: kindNotFoundFamily}, true
	}
	tail := rest[len(segConans)+1:]

	// v2/conans/search — one literal segment; a recipe NAMED "search" is
	// still reachable: its shapes all carry four more segments before any
	// verb, and the exact-match test below only fires on the lone word.
	if tail == segSearch {
		return route{kind: kindV2Search}, true
	}

	segs := strings.Split(tail, "/")
	if len(segs) < 4 {
		return route{kind: kindNotFoundFamily}, true
	}
	r, err := parseRef(segs[0], segs[1], segs[2], segs[3])
	if err != nil {
		return route{kind: kindNotFoundFamily}, true
	}
	rest = strings.Join(segs[4:], "/")

	if rest == "" {
		return route{kind: kindV2RecipeDelete, ref: r}, true
	}
	head, more, _ := strings.Cut(rest, "/")
	switch head {
	case segLatest:
		if more == "" {
			return route{kind: kindV2Latest, ref: r}, true
		}
	case segRevisions:
		return parseV2Revisions(r, more)
	case segSearch:
		if more == "" {
			return route{kind: kindV2RefSearch, ref: r}, true
		}
	}
	return route{kind: kindNotFoundFamily}, true
}

// parseV2Revisions parses the <ref>/revisions/… family.
func parseV2Revisions(r ref, more string) (route, bool) {
	if more == "" {
		return route{kind: kindV2Revisions, ref: r}, true
	}
	rrev, rest, _ := strings.Cut(more, "/")
	if !validRevision(rrev) {
		return route{kind: kindNotFoundFamily}, true
	}
	if rest == "" {
		return route{kind: kindV2RevDelete, ref: r, rRev: rrev}, true
	}
	head, tail, _ := strings.Cut(rest, "/")
	switch head {
	case segFilesTail:
		// …/files (the list) and …/files/<path> share the kind; an empty
		// tail is the listing, anything else the file target.
		return route{kind: kindV2Files, ref: r, rRev: rrev, path: tail}, true
	case segSearch:
		if tail == "" {
			return route{kind: kindV2RevSearch, ref: r, rRev: rrev}, true
		}
	case segPackages:
		return parseV2Packages(r, rrev, tail)
	}
	return route{kind: kindNotFoundFamily}, true
}

// parseV2Packages parses the …/packages[/<pid>[/…]] family.
func parseV2Packages(r ref, rrev, tail string) (route, bool) {
	if tail == "" {
		return route{kind: kindV2PackagesDelete, ref: r, rRev: rrev}, true
	}
	pid, rest, _ := strings.Cut(tail, "/")
	if !validPackageID(pid) {
		return route{kind: kindNotFoundFamily}, true
	}
	if rest == "" {
		return route{kind: kindNotFoundFamily}, true // …/packages/<pid> alone has no route
	}
	head, more, _ := strings.Cut(rest, "/")
	switch head {
	case segLatest:
		if more == "" {
			return route{kind: kindV2PkgLatest, ref: r, rRev: rrev, pid: pid}, true
		}
	case segRevisions:
		return parseV2PkgRevisions(r, rrev, pid, more)
	}
	return route{kind: kindNotFoundFamily}, true
}

// parseV2PkgRevisions parses …/packages/<pid>/revisions/….
func parseV2PkgRevisions(r ref, rrev, pid, more string) (route, bool) {
	if more == "" {
		return route{kind: kindV2PkgRevisions, ref: r, rRev: rrev, pid: pid}, true
	}
	prev, rest, _ := strings.Cut(more, "/")
	if !validRevision(prev) {
		return route{kind: kindNotFoundFamily}, true
	}
	if rest == "" {
		return route{kind: kindV2PkgRevDelete, ref: r, rRev: rrev, pid: pid, pRev: prev}, true
	}
	head, tail, _ := strings.Cut(rest, "/")
	if head == segFilesTail {
		return route{kind: kindV2PkgFiles, ref: r, rRev: rrev, pid: pid, pRev: prev, path: tail}, true
	}
	return route{kind: kindNotFoundFamily}, true
}

// parseV1 parses v1/… — the handshake family, the conans data family and
// the files channel (spec sections 2/3.2).
func parseV1(rest string) (route, bool) {
	head, tail, _ := strings.Cut(rest, "/")
	switch head {
	case segPing, segUsers:
		return parseHandshake(rest)
	case segConans:
		return parseV1Conans(tail)
	case segFiles:
		return parseV1Files(tail)
	}
	return route{kind: kindNotFoundFamily}, true
}

// parseHandshake parses the ping/users family shared by both version
// prefixes ({v1,v2}/ping, {v1,v2}/users/{authenticate,check_credentials}).
func parseHandshake(rest string) (route, bool) {
	head, tail, _ := strings.Cut(rest, "/")
	switch head {
	case segPing:
		if tail == "" {
			return route{kind: kindV1Ping}, true
		}
	case segUsers:
		verb, more, _ := strings.Cut(tail, "/")
		if more != "" {
			return route{kind: kindNotFoundFamily}, true
		}
		switch verb {
		case verbAuthenticate:
			return route{kind: kindV1Authenticate}, true
		case verbCheckCreds:
			return route{kind: kindV1CheckCredentials}, true
		}
	}
	return route{kind: kindNotFoundFamily}, true
}

// parseV1Conans parses v1/conans/….
func parseV1Conans(tail string) (route, bool) {
	if tail == segSearch {
		return route{kind: kindV1Search}, true
	}
	segs := strings.Split(tail, "/")
	if len(segs) < 4 {
		return route{kind: kindNotFoundFamily}, true
	}
	r, err := parseRef(segs[0], segs[1], segs[2], segs[3])
	if err != nil {
		return route{kind: kindNotFoundFamily}, true
	}
	rest := strings.Join(segs[4:], "/")
	if rest == "" {
		return route{kind: kindV1RecipeSnapshot, ref: r}, true
	}
	head, more, _ := strings.Cut(rest, "/")
	switch head {
	case segSearch, verbDigest, verbDownloadURLs, verbUploadURLs, verbRemoveFiles:
		if more == "" {
			switch head {
			case segSearch:
				return route{kind: kindV1RefSearch, ref: r}, true
			case verbDigest:
				return route{kind: kindV1Digest, ref: r}, true
			case verbDownloadURLs:
				return route{kind: kindV1DownloadURLs, ref: r}, true
			case verbUploadURLs:
				return route{kind: kindV1UploadURLs, ref: r}, true
			case verbRemoveFiles:
				return route{kind: kindV1RemoveFiles, ref: r}, true
			}
		}
	case segPackages:
		return parseV1Packages(r, more)
	}
	return route{kind: kindNotFoundFamily}, true
}

// parseV1Packages parses v1/conans/<ref>/packages/….
func parseV1Packages(r ref, tail string) (route, bool) {
	if tail == verbDelete {
		return route{kind: kindV1PackagesDelete, ref: r}, true
	}
	pid, rest, _ := strings.Cut(tail, "/")
	if !validPackageID(pid) {
		return route{kind: kindNotFoundFamily}, true
	}
	if rest == "" {
		return route{kind: kindV1PkgSnapshot, ref: r, pid: pid}, true
	}
	head, more, _ := strings.Cut(rest, "/")
	if more != "" {
		return route{kind: kindNotFoundFamily}, true
	}
	switch head {
	case verbDigest:
		return route{kind: kindV1Digest, ref: r, pid: pid}, true
	case verbDownloadURLs:
		return route{kind: kindV1DownloadURLs, ref: r, pid: pid}, true
	case verbUploadURLs:
		return route{kind: kindV1UploadURLs, ref: r, pid: pid}, true
	case verbRemoveFiles:
		return route{kind: kindV1RemoveFiles, ref: r, pid: pid}, true
	}
	return route{kind: kindNotFoundFamily}, true
}

// parseV1Files parses v1/files/<user>/<name>/<ver>/<channel>/[0/]export|package/…
// — the direct channel (spec section 3.2's last row). The optional segment
// is exactly the default revision `0`; the storage target is the 0 path
// either way. Package arms carry the pid; recipe arms go straight to
// export/.
func parseV1Files(tail string) (route, bool) {
	// tail = <user>/<name>/<version>/<channel>/[0/]export|package/…
	segs := strings.Split(tail, "/")
	if len(segs) < 5 {
		return route{kind: kindNotFoundFamily}, true
	}
	r, err := parseRef(segs[1], segs[2], segs[0], segs[3]) // name, version, user, channel
	if err != nil {
		return route{kind: kindNotFoundFamily}, true
	}
	i := 4
	if segs[i] == revDefaultV1 {
		i++
	}
	if i >= len(segs) {
		return route{kind: kindNotFoundFamily}, true
	}
	rt := route{kind: kindV1Files, ref: r}
	switch segs[i] {
	case dirExport:
		if i == len(segs)-1 {
			return route{kind: kindNotFoundFamily}, true // the export dir itself is not a file
		}
		rt.path = dirExport + "/" + strings.Join(segs[i+1:], "/")
	case dirPackage:
		if i >= len(segs)-2 {
			return route{kind: kindNotFoundFamily}, true
		}
		if !validPackageID(segs[i+1]) {
			return route{kind: kindNotFoundFamily}, true
		}
		rt.pid = segs[i+1]
		rt.path = dirPackage + "/" + segs[i+1] + "/" + strings.Join(segs[i+2:], "/")
	default:
		return route{kind: kindNotFoundFamily}, true
	}
	if !validChannelPath(rt.path) {
		return route{kind: kindNotFoundFamily}, true
	}
	return rt, true
}

// ---- segment validation ----

// parseRef validates one <ref>. The `_` placeholder is a legal literal
// value in every position (S12); an empty segment is not.
func parseRef(name, version, user, channel string) (ref, error) {
	for label, seg := range map[string]string{"name": name, "version": version, "user": user, "channel": channel} {
		if seg == "" {
			return ref{}, fmt.Errorf("empty ref segment %s", label)
		}
		if len(seg) > maxRefSegLen {
			return ref{}, fmt.Errorf("ref segment %s exceeds %d characters", label, maxRefSegLen)
		}
		if !validRefSegment(seg) {
			return ref{}, fmt.Errorf("ref segment %s %q has illegal characters", label, seg)
		}
	}
	return ref{name: name, version: version, user: user, channel: channel}, nil
}

// validRefSegment accepts the conan coordinate charset: letters, digits,
// `_`, `.`, `+`, `-` (the client's own restriction set; the server keeps
// the guard loose enough for every official spelling and tight enough to
// bar traversal).
func validRefSegment(seg string) bool {
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '.' || c == '+' || c == '-':
		default:
			return false
		}
	}
	return true
}

// validRevision accepts a revision spelling: hex up to 128 characters (the
// v2 sha256 64, the v1 md5 32, an scm commit 40 — plus the v1 default `0`).
// The server never recomputes the value (spec section 5); this is the
// path-safety charset, not a content check.
func validRevision(rev string) bool {
	return isHexRunes(rev) && len(rev) <= maxRevLen
}

// validPackageID accepts a packageId spelling: hex up to 128 characters
// (sha1 40 on conan 2, md5 32 on conan 1). Opaque to the server (spec
// section 5) — the charset bars traversal, nothing more.
func validPackageID(pid string) bool {
	return isHexRunes(pid) && len(pid) <= maxRevLen
}

// isHexRunes reports whether s is a non-empty hex string.
func isHexRunes(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// validChannelPath guards the v1 files channel's trailing path: non-empty,
// no dot segments (the earlier shared validation already ran, but the
// channel path was assembled from raw segments here — the belt to that
// suspenders).
func validChannelPath(path string) bool {
	if path == "" {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ---- storage addressing (spec section 4) ----

// recipeIndex is the recipe revision index node path.
func recipeIndex(root string) string { return root + "/" + recipeIndexFile }

// recipeTimestamp is the rRev's .timestamp node path.
func recipeTimestamp(root, rrev string) string { return root + "/" + rrev + "/" + timestampFile }

// recipeFile is one recipe file node path under export/.
func recipeFile(root, rrev, name string) string {
	return root + "/" + rrev + "/" + dirExport + "/" + name
}

// recipeExportPrefix lists the recipe file tree.
func recipeExportPrefix(root, rrev string) string {
	return root + "/" + rrev + "/" + dirExport + "/"
}

// pkgDir is the packageId's directory under one rRev.
func pkgDir(root, rrev, pid string) string {
	return root + "/" + rrev + "/" + dirPackage + "/" + pid
}

// pkgIndex is the package revision index node path.
func pkgIndex(root, rrev, pid string) string { return pkgDir(root, rrev, pid) + "/" + recipeIndexFile }

// pkgTimestamp is the pRev's .timestamp node path.
func pkgTimestamp(root, rrev, pid, prev string) string {
	return pkgDir(root, rrev, pid) + "/" + prev + "/" + timestampFile
}

// pkgFile is one package file node path (no export/ segment — conaninfo.txt
// and friends sit directly under the pRev, spec section 4).
func pkgFile(root, rrev, pid, prev, name string) string {
	return pkgDir(root, rrev, pid) + "/" + prev + "/" + name
}

// pkgFilePrefix lists the package file tree.
func pkgFilePrefix(root, rrev, pid, prev string) string {
	return pkgDir(root, rrev, pid) + "/" + prev + "/"
}
