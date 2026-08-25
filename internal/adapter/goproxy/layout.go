package goproxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// The wire shapes after the repository key (goproxy.md section 2): every
// route is <module>/@v/<something>. The storage form of the version-file
// trio is the DECODED path itself (<decoded-module>/@v/<decoded-version>.<ext>),
// so Layout hands ServeHTTP a path that doubles as the node path.
const (
	// segVersionMarker is the literal "@v" directory of the GOPROXY layout.
	segVersionMarker = "/@v/"
	// suffixList and suffixLatest are the two pseudo-file routes.
	suffixList   = "/@v/list"
	suffixLatest = "/@latest"
)

// targetKind enumerates the routable wire shapes.
type targetKind int

const (
	kindUnknown targetKind = iota
	kindFile               // <module>/@v/<version>.{info,mod,zip}
	kindList               // <module>/@v/list
	kindLatest             // <module>/@latest
)

// target is one parsed repo-relative wire target in its STORAGE spelling
// (module and version already case-decoded).
type target struct {
	module  string
	version string
	ext     string // "info" | "mod" | "zip" for kindFile
	kind    targetKind
}

// parseTarget recognizes the three routable shapes on a decoded
// repo-relative path. ok is false for everything else (the sumdb/* family,
// bare module paths, stray files) — those are unknown paths and answer 404
// (goproxy.md section 2: sumdb is deliberately not implemented in M10).
func parseTarget(rel string) (target, bool) {
	if m := strings.TrimSuffix(rel, suffixList); m != rel {
		return target{module: m, kind: kindList}, validModule(m)
	}
	if m := strings.TrimSuffix(rel, suffixLatest); m != rel {
		return target{module: m, kind: kindLatest}, validModule(m)
	}
	i := strings.LastIndex(rel, segVersionMarker)
	if i < 0 {
		return target{}, false
	}
	module, file := rel[:i], rel[i+len(segVersionMarker):]
	for _, ext := range []string{"zip", "mod", "info"} {
		suffix := "." + ext
		if strings.HasSuffix(file, suffix) && len(file) > len(suffix) {
			t := target{module: module, version: strings.TrimSuffix(file, suffix), ext: ext, kind: kindFile}
			return t, validModule(module)
		}
	}
	return target{}, false
}

// validModule applies the structural floor to a decoded module path: at
// least one element, no empty elements. The full module-path charset is the
// client's business (the go command refuses anything it cannot import);
// PUT additionally enforces the version/major consistency rules in
// version.go.
func validModule(module string) bool {
	if module == "" || strings.HasSuffix(module, "/") || strings.HasPrefix(module, "/") {
		return false
	}
	for _, seg := range strings.Split(module, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// nodePath renders the storage path of a version file.
func (t target) nodePath() string {
	if t.kind != kindFile {
		return ""
	}
	return t.module + segVersionMarker + t.version + "." + t.ext
}

// wirePath renders the wire (re-escaped) spelling of this target — the
// Location header of a PUT, and the upstream path when proxying.
func (t target) wirePath() string {
	switch t.kind {
	case kindList:
		return escapePath(t.module) + suffixList
	case kindLatest:
		return escapePath(t.module) + suffixLatest
	case kindFile:
		return escapePath(t.module) + segVersionMarker + escapeElement(t.version) + "." + t.ext
	default:
		return ""
	}
}

// versionListMarker is the internal cache path of a remote repository's
// upstream @v/list copy (goproxy.md section 3.2: the reverse-engineered
// marker spelling; the wire "list" and the storage ".versionList" differ on
// purpose so the marker can never be addressed as module content).
func versionListMarker(module string) string {
	return module + "/@v/.versionList"
}

// latestMarker is the internal cache path of a remote repository's upstream
// @latest copy — the reverse-engineered spelling WITHOUT a '/' before
// "@latest" (goproxy.md section 3.2's noted shape quirk, kept verbatim).
func latestMarker(module string) string {
	return module + "@latest.latest"
}

// layout splits one /binflow-stripped request into repoKey plus the
// STORAGE-form repo-relative path. Decoding happens here in the mandated
// order — percent-decode first (a malformed escape dies with the shared 400
// defense), then segment validation, then the !lower case decoding — so
// ServeHTTP and every service call downstream only ever see decoded paths.
// Handler.Layout is the adapter-facing wrapper.
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
	// GOPROXY has no folder-shaped routes (unlike pypi's index pages): the
	// empty rest IS legal (the repository-root probe), everything else must
	// be a file-shaped path.
	storage, err := unescapePath(rest)
	if err != nil {
		return "", "", err
	}
	return key, storage, nil
}

// validateRelPath enforces the shared artifact-path rules on the decoded
// repo-relative portion: no dot segments, no empty segments, no
// backslashes, no control bytes, no trailing slash, bounded length.
func validateRelPath(rel string) error {
	if rel == "" {
		return nil // the repository root probe
	}
	if len(rel) > adapter.MaxRelPathLen {
		return fmt.Errorf("%w: artifact path of %d characters exceeds the %d limit",
			adapter.ErrBadRequestPath, len(rel), adapter.MaxRelPathLen)
	}
	if strings.HasSuffix(rel, "/") {
		return fmt.Errorf("%w: %q: GOPROXY paths address files, not folders", adapter.ErrBadRequestPath, rel)
	}
	if strings.Contains(rel, "\\") {
		return fmt.Errorf("%w: backslash is not a path separator", adapter.ErrBadRequestPath)
	}
	if strings.ContainsFunc(rel, isControlByte) {
		return fmt.Errorf("%w: control characters are not allowed in artifact paths", adapter.ErrBadRequestPath)
	}
	for _, seg := range strings.Split(rel, "/") {
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

// isControlByte reports whether r is a control character (C0 range, DEL).
func isControlByte(r rune) bool { return r < 0x20 || r == 0x7f }
