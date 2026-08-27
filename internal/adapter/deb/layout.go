package deb

// Wire-path grammar (the helm/rpm posture): layout() is the single decode
// point — percent-decode first, matrix-parameter peel second (the debPUT
// coordinates ride the ";k=v" tail, the generic plane's shared grammar),
// repository-key and path-shape validation, then the route split.
// Everything downstream sees decoded, storage-spelling paths plus the
// peeled coordinate properties.
//
// The content plane is a PURE STORAGE-PATH protocol (debian.md section 1):
// apt GETs repo-relative paths verbatim. The route kinds below separate
// only the faces with protocol-specific verbs — the debPUT family (.deb /
// .dsc) and the server-generated index family under dists/ (DB-3's 403
// domain); everything unrecognized is raw storage addressing.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
)

const (
	// dirDists roots the index tree every apt client walks first.
	dirDists = "dists"
	// dirByHash is the By-Hash history directory inside an index
	// directory (debian.md section 5).
	dirByHash = "by-hash"
	// suffixDeb marks a binary package artifact.
	suffixDeb = ".deb"
	// suffixDsc marks a source package descriptor.
	suffixDsc = ".dsc"
	// fileInRelease is the inline-signed Release spelling (unsigned mode
	// still sweeps it — a stale signature must not survive).
	fileInRelease = "InRelease"
)

// indexNamePrefixes are the generated-file name prefixes of the DB-3
// refusal: any segment under dists/ named Release*, InRelease, Packages*
// or Sources* — or the by-hash segment and everything beneath it — is the
// index engine's output, never a client write target.
var indexNamePrefixes = []string{"Release", "Packages", "Sources"}

// routeKind enumerates the routable wire targets.
type routeKind int

const (
	kindUnknown routeKind = iota
	kindRoot              // "" — the repository probe
	kindDeb               // <path>.deb — the debPUT binary face
	kindDsc               // <path>.dsc — the debPUT source face
	kindIndex             // dists/**/{Release*,InRelease,Packages*,Sources*,by-hash/**}
	kindDists             // anything else under dists/ — plain storage
	kindBare              // none of the above: raw storage addressing
)

// route is one parsed repo-relative wire target in STORAGE spelling.
type route struct {
	kind routeKind
	path string
}

// layout splits one /binflow-stripped request into repoKey plus the
// STORAGE-form repo-relative path and the peeled matrix properties (the
// debPUT coordinates arrive as ";deb.distribution=..." tail parameters —
// the shared SplitMatrixParams grammar, so a path without a paired k=v
// tail keeps M1 literal-path semantics).
func layout(r *http.Request) (string, string, adapter.DeployProps, error) {
	if r == nil || r.URL == nil {
		return "", "", nil, fmt.Errorf("%w: empty request URL", adapter.ErrBadRequestPath)
	}
	raw := r.URL.EscapedPath()
	if raw == "" {
		raw = r.URL.Path
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: malformed percent-encoding in %q: %w", adapter.ErrBadRequestPath, raw, err)
	}
	decoded = strings.TrimPrefix(decoded, "/")
	if decoded == "" {
		return "", "", nil, fmt.Errorf("%w: path is empty (no repository key)", adapter.ErrBadRequestPath)
	}
	clean, matrix := adapter.SplitMatrixParams(decoded)
	props, err := adapter.ParseMatrixProps(matrix)
	if err != nil {
		return "", "", nil, err
	}
	key, rest, _ := strings.Cut(clean, "/")
	if key == "" {
		return "", "", nil, fmt.Errorf("%w: empty repository key", adapter.ErrBadRequestPath)
	}
	if len(key) > adapter.MaxRepoKeyLen {
		return "", "", nil, fmt.Errorf("%w: repository key longer than %d characters", adapter.ErrBadRequestPath, adapter.MaxRepoKeyLen)
	}
	if adapter.IsReservedSegment(key) {
		return "", "", nil, fmt.Errorf("%w: %q is a reserved routing segment", adapter.ErrBadRequestPath, key)
	}
	// The key segment carries the same security floor as the path body (the
	// rpm spelling of the contract).
	switch key {
	case ".", "..":
		return "", "", nil, fmt.Errorf("%w: dot segment %q in the repository key", adapter.ErrBadRequestPath, key)
	}
	if strings.ContainsAny(key, "\\\r\n\t") || strings.ContainsFunc(key, isControlRune) {
		return "", "", nil, fmt.Errorf("%w: illegal character in repository key %q", adapter.ErrBadRequestPath, key)
	}
	if err := validateRelPath(rest); err != nil {
		return "", "", nil, err
	}
	return key, rest, props, nil
}

// validateRelPath enforces the shared artifact-path rules on the decoded
// repo-relative portion (adapter.NormalizeRelPath — no empty/dot segments,
// no backslash, no controls, bounded length). The empty path is the
// repository-root probe and passes (the kindRoot face).
func validateRelPath(rel string) error {
	if rel == "" {
		return nil
	}
	return adapter.NormalizeRelPath(rel)
}

// isControlRune reports C0 controls and DEL.
func isControlRune(r rune) bool { return r < 0x20 || r == 0x7f }

// parseRoute recognizes the routable shapes on a decoded repo-relative
// path. The dists/ index family wins first (DB-3's domain is judged
// before the artifact suffixes — a path the engine owns is never an
// upload face), then the debPUT suffixes, then the dists/ subtree, and
// everything unrecognized is bare storage addressing.
func parseRoute(rel string) route {
	switch {
	case rel == "":
		return route{kind: kindRoot}
	case isIndexPath(rel):
		return route{kind: kindIndex, path: rel}
	case strings.HasSuffix(rel, suffixDeb):
		return route{kind: kindDeb, path: rel}
	case strings.HasSuffix(rel, suffixDsc):
		return route{kind: kindDsc, path: rel}
	case underDists(rel):
		return route{kind: kindDists, path: rel}
	default:
		return route{kind: kindBare, path: rel}
	}
}

// underDists reports whether the path's first segment is dists/.
func underDists(rel string) bool {
	first, _, _ := strings.Cut(rel, "/")
	return first == dirDists
}

// isIndexPath decides the DB-3 domain (debian.md section 3 step 6, PRD
// 97.1): dists/**/{Release*,InRelease,Packages*,Sources*,by-hash/**}.
// Any SEGMENT at or below dists/ matching the family — not just the
// basename — marks the path generated: the by-hash history tree sits
// beside the indexes it mirrors, and a Release.gpg companion is as much
// the engine's output as the Release it signs.
func isIndexPath(rel string) bool {
	if !underDists(rel) {
		return false
	}
	segs := strings.Split(rel, "/")
	for _, seg := range segs[1:] {
		if seg == dirByHash || seg == fileInRelease {
			return true
		}
		for _, p := range indexNamePrefixes {
			if strings.HasPrefix(seg, p) {
				return true
			}
		}
	}
	return false
}

// ---- debPUT coordinates (debian.md sections 2.3 / 3.1) ----

// The coordinate property keys: deb.* for binary packages, dsc.* for
// source descriptors (dsc.architecture is server-forced to "source").
const (
	PropDebDistribution = "deb.distribution"
	PropDebComponent    = "deb.component"
	PropDebArchitecture = "deb.architecture"
	PropDscDistribution = "dsc.distribution"
	PropDscComponent    = "dsc.component"
	PropDscArchitecture = "dsc.architecture"

	// archSource is the Sources index's architecture spelling (the
	// source/ directory under a component).
	archSource = "source"
)

// pseudoArches are the coordinate values that never name a binary-<arch>
// index family of their own for the Release Architectures line
// (debian.md section 4.1: filter any/all) — an "all" package still lands
// in binary-all, it just does not join the arch list.
var pseudoArches = map[string]bool{"all": true, "any": true}

// coordinates is one upload's registration set: the cartesian product of
// the three axes lands the file in every named index context.
type coordinates struct {
	distributions []string
	components    []string
	architectures []string
}

// debCoordinates peels the deb.* axes off the request's matrix set.
func debCoordinates(props adapter.DeployProps) coordinates {
	return coordinates{
		distributions: props[PropDebDistribution],
		components:    props[PropDebComponent],
		architectures: props[PropDebArchitecture],
	}
}

// dscCoordinates peels the dsc.* axes; the architecture axis is the
// server's, never the client's (section 2.3: dsc.architecture is forced
// to source).
func dscCoordinates(props adapter.DeployProps) coordinates {
	return coordinates{
		distributions: props[PropDscDistribution],
		components:    props[PropDscComponent],
		architectures: []string{archSource},
	}
}

// complete reports whether every axis is present (DB-2's validation
// input: a .deb without the full triple is refused, a .dsc without its
// distribution/component pair likewise).
func (c coordinates) complete() bool {
	return len(c.distributions) > 0 && len(c.components) > 0 && len(c.architectures) > 0
}

// props renders the coordinates back as the node's registration property
// set (the index engine's source of truth — the engine never sees the
// request, only the stored properties).
func (c coordinates) props(prefix string) map[string][]string {
	return map[string][]string{
		prefix + ".distribution": c.distributions,
		prefix + ".component":    c.components,
		prefix + ".architecture": c.architectures,
	}
}

// validate checks every token against the coordinate charset: these
// values become path segments under dists/, so the floor is the node-path
// family (no traversal, no empties, no controls) plus the Debian token
// alphabet. Distributions additionally admit '/' — Debian suite names
// like wheezy/updates nest (the official dists/ layout honors it).
func (c coordinates) validate() error {
	for _, d := range c.distributions {
		if err := validateToken(d, true); err != nil {
			return fmt.Errorf("deb.distribution %q: %w", d, err)
		}
	}
	for _, comp := range c.components {
		if err := validateToken(comp, false); err != nil {
			return fmt.Errorf("deb.component %q: %w", comp, err)
		}
	}
	for _, a := range c.architectures {
		if err := validateToken(a, false); err != nil {
			return fmt.Errorf("deb.architecture %q: %w", a, err)
		}
	}
	return nil
}

// errBadCoordinate wraps every coordinate-token refusal (the 400 family).
var errBadCoordinate = fmt.Errorf("invalid debian coordinate")

// validateToken checks one distribution/component/architecture token:
// 1..64 chars, starts alphanumeric, continues with the Debian token
// alphabet [A-Za-z0-9+._-] (plus '/' when slashes are legal — the
// distribution axis), and never forms a dot-only or empty path segment.
func validateToken(tok string, slashes bool) error {
	if tok == "" || len(tok) > 64 {
		return fmt.Errorf("%w: %q must be 1..64 characters", errBadCoordinate, tok)
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '+', c == '.', c == '_', c == '-':
		case c == '/' && slashes:
		default:
			return fmt.Errorf("%w: %q carries illegal character %q", errBadCoordinate, tok, c)
		}
	}
	for _, seg := range strings.Split(tok, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q has an empty or dot path segment", errBadCoordinate, tok)
		}
	}
	return nil
}
