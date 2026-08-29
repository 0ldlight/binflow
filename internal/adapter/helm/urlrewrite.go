package helm

// The virtual index's URL rewriting (helm.md section 7.3 / S8) and the
// external-dependency path algebra (section 6 / S10). Everything here is a
// pure function over strings — the aggregation (virtual.go) feeds it one
// entry's urls[0] plus the member's context and serves whatever returns.

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// urlPrefixOCI marks an OCI-referencing entry (an upstream index may carry
// them for charts whose home is an OCI registry).
const urlPrefixOCI = "oci://"

// defaultExternalPatterns is the Patterns Allow List's product default
// (helm.md section 6: Ant-style, default "**"). It is a code constant
// because BinFlow's remote config schema carries no helm-specific seat
// yet; Options.ExternalPatterns is the plug point for the day it does.
var defaultExternalPatterns = []string{"**"}

// externalPatterns resolves the effective allow list.
func externalPatterns(override []string) []string {
	if len(override) == 0 {
		return defaultExternalPatterns
	}
	return override
}

// externalAllowed reports whether the URL matches any allow-list pattern.
func externalAllowed(patterns []string, rawURL string) bool {
	for _, p := range patterns {
		if antMatch(p, rawURL) {
			return true
		}
	}
	return false
}

// antMatch is the Ant-style pattern matcher the allow list speaks:
// "**" spans path separators, "*" and "?" one segment's characters.
func antMatch(pattern, s string) bool {
	return antMatchSegs(strings.Split(pattern, "/"), strings.Split(s, "/"))
}

// antMatchSegs matches already-split path segments.
func antMatchSegs(pat, segs []string) bool {
	switch {
	case len(pat) == 0:
		return len(segs) == 0
	case pat[0] == "**":
		// "**" matches nothing (drop both) or at least one segment (drop
		// the segment, keep the wildcard).
		return antMatchSegs(pat[1:], segs) ||
			(len(segs) > 0 && antMatchSegs(pat, segs[1:]))
	case len(segs) == 0:
		return false
	case antMatchSegment(pat[0], segs[0]):
		return antMatchSegs(pat[1:], segs[1:])
	default:
		return false
	}
}

// antMatchSegment matches one "*" / "?" pattern against one segment.
func antMatchSegment(pat, s string) bool {
	// Iterative single-char wildcard walk (the classic two-pointer glob).
	star, mark := -1, 0
	pi, si := 0, 0
	for si < len(s) {
		switch {
		case pi < len(pat) && (pat[pi] == '?' || pat[pi] == s[si]):
			pi++
			si++
		case pi < len(pat) && pat[pi] == '*':
			star = pi
			mark = si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}

// errExternalPath shapes the malformed-proxy-path refusal (400).
type errExternalPath struct{ msg string }

func (e *errExternalPath) Error() string { return e.msg }

// parseExternalPath decodes one _external or _transitive wire path into the
// absolute target URL: "<prefix>/<scheme>/<host>/<path...>" re-inflates to
// "<scheme>://<host>/<path...>" — the wire form is the absolute URL with
// "://" folded onto "/" (helm.md section 7.3). Everything after the scheme
// segment rides verbatim, so a query string carried in the rewritten URL
// survives the round trip.
func parseExternalPath(rel, prefix string) (string, error) {
	rest, ok := strings.CutPrefix(rel, prefix+"/")
	if !ok || rest == "" {
		return "", &errExternalPath{msg: fmt.Sprintf(
			"malformed %s path %q: expected %s/<protocol>/<host>/<path>", prefix, rel, prefix)}
	}
	scheme, tail, _ := strings.Cut(rest, "/")
	if scheme != "http" && scheme != "https" {
		return "", &errExternalPath{msg: fmt.Sprintf(
			"malformed %s path %q: the external dependency protocol must be http or https", prefix, rel)}
	}
	host, _, _ := strings.Cut(tail, "/")
	if host == "" || strings.ContainsAny(host, "@\\") {
		return "", &errExternalPath{msg: fmt.Sprintf(
			"malformed %s path %q: no usable upstream host in the folded URL", prefix, rel)}
	}
	target := scheme + "://" + tail
	if _, err := url.Parse(target); err != nil {
		return "", &errExternalPath{msg: fmt.Sprintf(
			"malformed %s path %q: %v", prefix, rel, err)}
	}
	return target, nil
}

// foldExternalURL renders one absolute URL's folded proxy path (the
// rewrite target of the allow-list branch): scheme "://" collapses onto
// "/". ok is false for shapes this face does not proxy (non-http(s)
// schemes, hostless URLs, unparsable strings) — the caller then keeps the
// original URL so the client goes direct.
func foldExternalURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	tail := strings.TrimPrefix(raw, u.Scheme+"://")
	return segExternal + "/" + u.Scheme + "/" + tail, true
}

// memberCtx is one virtual member's rewriting context (the chart's base
// the entry URLs are recognized against).
type memberCtx struct {
	key        string
	typ        string // repo.TypeLocal | repo.TypeRemote
	chartsBase string // the remote member's upstream URL (chartsBaseUrl fallback: the repo URL itself)
}

// rewriteVirtualURL computes one entry's rewritten urls[0] (section 7.3's
// branch order, relative-urls mode — HL-2's default):
//
//   - a REMOTE member's entry under the member's charts base collapses to
//     the member-relative path (the client re-fetches it through the
//     virtual, resolution walks the member order, the member's engine
//     fetches upstream base + path — the path-aligned mirror rule of
//     section 6); an entry under the upstream's OWN _external/ prefix
//     becomes _transitive/<...> (the upstream is itself an
//     Artifactory-like server);
//   - an allow-list hit outside the charts base becomes the folded
//     _external/<scheme>/<host>/<path> proxy path;
//   - an allow-list miss keeps the original URL (the client goes direct);
//   - a LOCAL member's entry is its in-repo path already (BinFlow
//     generates relative urls); an absolute URL naming the member's own
//     content plane collapses back to that path, anything else runs the
//     external branches;
//   - oci:// entries stay verbatim (the T-313 D-5 posture, re-evaluated
//     with T-342: the helmoci package type now serves the /v2 plane, but
//     only LOCAL repositories — there is no OCI pull-through or virtual
//     aggregation behind this index, so a rewritten oci:// URL would point
//     at charts BinFlow cannot serve; the verbatim entry at least names a
//     working upstream. helm.preserve.oci.urls stays meaningless until an
//     oci://-aware remote/virtual lands — registered in both ticket
//     reports).
func rewriteVirtualURL(raw string, mc memberCtx, baseURL string, patterns []string) string {
	if raw == "" || strings.HasPrefix(raw, urlPrefixOCI) {
		return raw
	}
	u, err := url.Parse(raw)
	absolute := err == nil && u.Scheme != "" && u.Host != ""
	if !absolute {
		// A relative URL is the member-relative path on both classes
		// (BinFlow local indexes are relative by construction; an upstream
		// index with relative urls resolves against its own base, which IS
		// the charts base).
		return raw
	}
	switch mc.typ {
	case repo.TypeRemote:
		if rest, ok := stripChartsBase(mc.chartsBase, raw); ok {
			if rest == "" {
				return raw // the URL IS the base: no path to serve
			}
			if tail, isExternal := strings.CutPrefix(rest, segExternal+"/"); isExternal {
				return segTransitive + "/" + tail
			}
			return rest
		}
	case repo.TypeLocal:
		if p, ok := stripContentPlane(baseURL, mc.key, u); ok {
			return p
		}
	}
	// The external branch: allow-list hit folds onto the proxy path, a
	// miss keeps the original URL.
	if !externalAllowed(patterns, raw) {
		return raw
	}
	if folded, ok := foldExternalURL(raw); ok {
		return folded
	}
	return raw
}

// stripChartsBase strips the member's charts base off one absolute URL,
// returning the member-relative path ("" when the URL IS the base — ok
// still true, the caller keeps the original). A prefix match must end on
// a path boundary ("https://h/charts" does not claim
// "https://h/charts2/x.tgz").
func stripChartsBase(base, raw string) (string, bool) {
	if base == "" || !strings.HasPrefix(raw, base) {
		return "", false
	}
	rest := raw[len(base):]
	if rest != "" && !strings.HasPrefix(rest, "/") {
		return "", false // a longer spelling ("charts2") is a different base
	}
	return strings.TrimLeft(rest, "/"), true
}

// stripContentPlane strips "<baseURL>/binflow/<memberKey>/" off one
// absolute URL naming a local member's own content plane, returning the
// member-relative path (the recognition arm for foreign absolute-urls
// indexes migrated in).
func stripContentPlane(baseURL, memberKey string, u *url.URL) (string, bool) {
	if baseURL == "" {
		return "", false
	}
	b, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Host != b.Host || u.Scheme != b.Scheme {
		return "", false
	}
	prefix := "/binflow/" + memberKey + "/"
	if !strings.HasPrefix(u.Path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(u.Path, prefix), true
}
