package adapter

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Layout implements the generic layout (architecture section 5.1): the
// first path segment is the repository key, the remainder is the
// repo-relative artifact path, stored verbatim (Artifactory generic
// semantics — no layout mapping).
//
// Decoding responsibility lives HERE, at this layer's entry (T-12 review
// carried it into the ticket): the raw request path is percent-decoded
// before any segment analysis, so an encoded traversal attempt (%2e%2e)
// is judged on its decoded form and can never slip past the dot-segment
// defense into repo.validateNodePath (FR-4-AC10/NFR-S4).
//
// The request path handed in must already have the /binflow prefix
// stripped (httpapi's job). Both r.URL.Path (pre-decoded by net/http) and
// r.URL.EscapedPath() are consulted: decoding EscapedPath ourselves makes
// the normalization independent of which form the router passed through.
func Layout(r *http.Request) (string, string, error) {
	if r == nil || r.URL == nil {
		return "", "", fmt.Errorf("%w: empty request URL", ErrBadRequestPath)
	}
	raw := r.URL.EscapedPath()
	if raw == "" {
		raw = r.URL.Path
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", "", fmt.Errorf("%w: malformed percent-encoding in %q: %w", ErrBadRequestPath, raw, err)
	}
	return splitRepoPath(decoded)
}

// splitRepoPath splits a decoded path into (repoKey, relPath) and applies
// the generic layout rules:
//
//   - no query strings, no matrix parameters (BinFlow M1 semantic; ';'
//     stays an ordinary path character);
//   - repository key: non-empty, no '/', bounded by MaxRepoKeyLen;
//   - artifact path: no "." or ".." segments anywhere (decoded!), no empty
//     segments (double slashes), no leading '/', at most MaxRelPathLen
//     characters; a single trailing '/' is kept (folder addressing);
//   - path.Clean is deliberately NOT applied silently: "a/./b" and
//     "a/../b" are rejected, not rewritten — a rewrite would let clients
//     address a different node than the URL they signed checksums for.
func splitRepoPath(decoded string) (string, string, error) {
	decoded = strings.TrimPrefix(decoded, "/")
	if decoded == "" {
		return "", "", fmt.Errorf("%w: path is empty (no repository key)", ErrBadRequestPath)
	}
	key, rest, found := strings.Cut(decoded, "/")
	if !found {
		// A bare repository root: content paths always need at least one
		// artifact segment (a trailing slash alone is an empty segment).
		return "", "", fmt.Errorf("%w: %q has no artifact path after the repository key", ErrBadRequestPath, key)
	}
	if key == "" {
		return "", "", fmt.Errorf("%w: empty repository key", ErrBadRequestPath)
	}
	if len(key) > MaxRepoKeyLen {
		return "", "", fmt.Errorf("%w: repository key longer than %d characters", ErrBadRequestPath, MaxRepoKeyLen)
	}
	if IsReservedSegment(key) {
		return "", "", fmt.Errorf("%w: %q is a reserved routing segment", ErrBadRequestPath, key)
	}
	if err := validateRelPath(rest); err != nil {
		return "", "", err
	}
	return key, rest, nil
}

// validateRelPath enforces the artifact-path rules on the decoded,
// repo-relative portion.
func validateRelPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("%w: empty artifact path", ErrBadRequestPath)
	}
	if len(rel) > MaxRelPathLen {
		return fmt.Errorf("%w: artifact path of %d characters exceeds the %d limit", ErrBadRequestPath, len(rel), MaxRelPathLen)
	}
	if strings.Contains(rel, "\\") {
		return fmt.Errorf("%w: backslash is not a path separator", ErrBadRequestPath)
	}
	// A trailing slash addresses a folder and is preserved; everything else
	// must be a real segment.
	body := strings.TrimSuffix(rel, "/")
	if body == "" {
		return fmt.Errorf("%w: empty artifact path segments in %q", ErrBadRequestPath, rel)
	}
	for _, seg := range strings.Split(body, "/") {
		switch seg {
		case "":
			return fmt.Errorf("%w: empty path segment in %q (double slash?)", ErrBadRequestPath, rel)
		case ".", "..":
			return fmt.Errorf("%w: dot segment %q in %q escapes or dilutes the repository root", ErrBadRequestPath, seg, rel)
		}
	}
	return nil
}

// NormalizeRelPath is the exported one-liner other protocol adapters (M2+
// docker subpaths, M3 maven) can reuse for their own relPath validation
// once they have peeled off their protocol-specific segments.
func NormalizeRelPath(rel string) error { return validateRelPath(rel) }
