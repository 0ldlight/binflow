package helm

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// Wire-path grammar (the cargo/nuget posture): layout() is the single
// decode point — percent-decode first, repository-key and path-shape
// validation, then the route split. Everything downstream sees decoded,
// storage-spelling paths.

const (
	// fileIndex is the repo-root chart index (the classic protocol's one
	// required document).
	fileIndex = "index.yaml"
	// suffixTgz marks a chart archive (the ONLY extension that fires the
	// indexer; .tar.gz is a plain file, helm.md section 3).
	suffixTgz = ".tgz"
	// suffixTarGz is served on GET (the route regex family) but never
	// indexed.
	suffixTarGz = ".tar.gz"
	// suffixProv marks a provenance file (S14: plain storage, never in the
	// index; helm fetches it as <tgz-url>.prov for --verify).
	suffixProv = ".prov"
	// segExternal/segTransitive are the remote-family proxy prefixes — on
	// a local repository they answer the 400 refusal (helm.md section 2).
	segExternal   = "_external"
	segTransitive = "_transitive"

	// dirIndexMeta is the virtual-repository cache root (.index — the
	// classic-protocol constant; the virtual ticket consumes it, this
	// package only reserves the spelling against bare-content addressing).
	dirIndexMeta = ".index"
)

// routeKind enumerates the routable wire targets.
type routeKind int

const (
	kindUnknown     routeKind = iota
	kindRoot                  // "" — the repository probe
	kindIndex                 // index.yaml (repo root)
	kindChart                 // <path>.tgz — the upload/index face
	kindTarGz                 // <path>.tar.gz — served, never indexed
	kindProv                  // <path>.prov — provenance sidecar
	kindExternal              // _external/... — remote family, local 400
	kindTransitive            // _transitive/... — remote family, local 400
	kindBareContent           // none of the above: raw storage addressing
)

// route is one parsed repo-relative wire target in STORAGE spelling.
type route struct {
	kind routeKind
	path string // the storage path (identical to rel for every kind)
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
// path. The two remote-family PREFIXES win over every suffix match (an
// _external/https/.../x.tgz URL is the proxy family's, never a chart at
// a path that happens to start with the prefix — the segments are
// reserved). Everything unrecognized is bare storage addressing.
func parseRoute(rel string) route {
	switch {
	case rel == "":
		return route{kind: kindRoot}
	case rel == fileIndex:
		return route{kind: kindIndex, path: rel}
	case rel == segExternal || strings.HasPrefix(rel, segExternal+"/"):
		return route{kind: kindExternal, path: rel}
	case rel == segTransitive || strings.HasPrefix(rel, segTransitive+"/"):
		return route{kind: kindTransitive, path: rel}
	case strings.HasSuffix(rel, suffixProv):
		return route{kind: kindProv, path: rel}
	case strings.HasSuffix(rel, suffixTarGz):
		return route{kind: kindTarGz, path: rel}
	case strings.HasSuffix(rel, suffixTgz):
		return route{kind: kindChart, path: rel}
	default:
		return route{kind: kindBareContent, path: rel}
	}
}

// isChartPath reports whether a STORAGE path is a chart archive node (the
// indexer's membership test over List results).
func isChartPath(p string) bool {
	return strings.HasSuffix(p, suffixTgz) && !strings.HasSuffix(p, suffixProv)
}

// chartBaseName is the final path segment.
func chartBaseName(p string) string { return path.Base(p) }
