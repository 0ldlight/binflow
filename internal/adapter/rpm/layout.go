package rpm

// Wire-path grammar (the helm/cargo posture): layout() is the single decode
// point — percent-decode first, repository-key and path-shape validation,
// then the route split. Everything downstream sees decoded, storage-spelling
// paths.
//
// The content plane is a PURE STORAGE-PATH protocol (rpm.md section 1): dnf
// GETs repo-relative file paths verbatim (.rpm, repodata/repomd.xml, the
// digest-prefixed indexes). The route kinds below only separate the faces
// with protocol-specific verbs; everything unrecognized is raw storage
// addressing.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

const (
	// dirRepodata is the metadata directory every yum client walks first.
	dirRepodata = "repodata"
	// fileRepomd is the index-of-indexes document.
	fileRepomd = "repodata/repomd.xml"
	// suffixRpm marks a package artifact (the ONLY extension that fires
	// the property parser and the recompute trigger).
	suffixRpm = ".rpm"
	// tmpPrefix is the reindex staging prefix (rpm.md section 2.3 — the
	// atomic-swap scratch directory; excluded from recompute candidates
	// and refused to client writes).
	tmpPrefix = "_tmp_"
)

// checksumSuffixes are the client-checksum sidecar spellings the content
// plane refuses to serve (rpm.md section 3.1: 404 with the pinned wording,
// never a fetch).
var checksumSuffixes = []string{".sha256", ".sha1", ".md5", ".sha512"}

// routeKind enumerates the routable wire targets.
type routeKind int

const (
	kindUnknown  routeKind = iota
	kindRoot               // "" — the repository probe
	kindRpm                // <path>.rpm — the artifact face
	kindSidecar            // <path>.rpm.<checksum> — always 404
	kindRepomd             // repodata/repomd.xml[.asc|.key]
	kindIndex              // repodata/<digest>-<index>.xml.gz — server-generated
	kindGroup              // repodata/<groupFileName>.xml — the user-uploaded comps spelling
	kindRepodata           // anything else under repodata/
	kindTmp                // _tmp_... — the reindex staging area
	kindBare               // none of the above: raw storage addressing
)

// route is one parsed repo-relative wire target in STORAGE spelling.
type route struct {
	kind routeKind
	path string
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
	// The key segment carries the same security floor as the path body: a
	// dot-segment key would address the parent in any downstream path join,
	// a backslash or control character has no business in a routing segment.
	switch key {
	case ".", "..":
		return "", "", fmt.Errorf("%w: dot segment %q in the repository key", adapter.ErrBadRequestPath, key)
	}
	if strings.ContainsAny(key, "\\\r\n\t") || strings.ContainsFunc(key, isControlRune) {
		return "", "", fmt.Errorf("%w: illegal character in repository key %q", adapter.ErrBadRequestPath, key)
	}
	if err := validateRelPath(rest); err != nil {
		return "", "", err
	}
	return key, rest, nil
}

// validateRelPath enforces the shared artifact-path rules on the decoded
// repo-relative portion (the helm/adapter-root spelling of the contract:
// no empty/dot segments, no backslash, no controls, bounded length).
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
// path. The _tmp_ prefix wins over every suffix match (the staging area is
// the reindex engine's, never an artifact face); a "repodata" path SEGMENT
// wins next — with yumRootDepth > 0 the metadata directory sits under the
// release subtree, not at the repository root (rpm.md section 1). Everything
// unrecognized is bare storage addressing.
func parseRoute(rel string) route {
	switch {
	case rel == "":
		return route{kind: kindRoot}
	case isTmpPath(rel):
		return route{kind: kindTmp, path: rel}
	}
	if _, tail, ok := splitRepodata(rel); ok {
		return route{kind: repodataKind(tail), path: rel}
	}
	switch {
	case isRpmPath(rel):
		return route{kind: kindRpm, path: rel}
	case isSidecarPath(rel):
		return route{kind: kindSidecar, path: rel}
	default:
		return route{kind: kindBare, path: rel}
	}
}

// splitRepodata finds the FIRST "repodata" path segment and returns the
// subtree root plus the member tail. ok is false when no such segment
// exists. The reserved reading keeps one metadata directory per subtree —
// the layout every yum client addresses.
func splitRepodata(rel string) (root, tail string, ok bool) {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		if s != dirRepodata {
			continue
		}
		if i > 0 {
			root = strings.Join(segs[:i], "/")
		}
		return root, strings.Join(segs[i+1:], "/"), true
	}
	return "", "", false
}

// repodataKind refines one repodata member tail: the server-generated index
// family (repomd three-piece, digest-prefixed indexes, the legacy sqlite)
// versus the un-prefixed group-file spelling versus anything else.
func repodataKind(tail string) routeKind {
	switch {
	case tail == "":
		return kindRepodata
	case tail == "repomd.xml", tail == "repomd.xml.asc", tail == "repomd.xml.key":
		return kindRepomd
	case isDigestPrefixed(tail) && (strings.HasSuffix(tail, ".xml") ||
		strings.HasSuffix(tail, ".xml.gz") || strings.HasSuffix(tail, ".yaml.gz")):
		return kindIndex
	case strings.HasSuffix(tail, ".sqlite.bz2"):
		// The legacy sqlite metadata: generated family (cleared on every
		// recompute, rpm.md section 2.2; never writable here).
		return kindIndex
	case strings.HasSuffix(tail, ".xml"):
		return kindGroup // the un-prefixed comps spelling (PUT allowed)
	default:
		return kindRepodata
	}
}

// isDigestPrefixed reports a <64-hex>- filename head.
func isDigestPrefixed(base string) bool {
	if len(base) < 65 || base[64] != '-' {
		return false
	}
	for i := 0; i < 64; i++ {
		c := base[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// isRpmPath reports a STORAGE path ending in .rpm.
func isRpmPath(p string) bool { return strings.HasSuffix(p, suffixRpm) }

// isSidecarPath reports a .rpm.<checksum-suffix> spelling.
func isSidecarPath(p string) bool {
	for _, s := range checksumSuffixes {
		if strings.HasSuffix(p, suffixRpm+s) {
			return true
		}
	}
	return false
}

// isTmpPath reports the reindex staging prefix: any path whose FIRST
// segment is a `_tmp_`-prefixed directory name (the spec's
// `_tmp_<nanoTime><hash>` spelling, rpm.md section 2.3).
func isTmpPath(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return strings.HasPrefix(first, tmpPrefix)
}

// yumRootOf derives the recompute root of one storage path under a
// repository configured with the given yumRootDepth: the first depth
// segments of the path's directory (rpm.md section 4.1 rule 3). ok is
// false when the directory is shallower than depth (the section's depth
// filter — such an upload never triggers the recompute). depth 0 (the
// default) is always the repository root "".
func yumRootOf(path string, depth int) (string, bool) {
	if depth <= 0 {
		return "", true
	}
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "", false // no directory at all: shallower than any depth ≥ 1
	}
	segs := strings.Split(path[:i], "/")
	if len(segs) < depth {
		return "", false
	}
	return strings.Join(segs[:depth], "/"), true
}
