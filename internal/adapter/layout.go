package adapter

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
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
//
// Since M10 (T-286, architecture section 15.3.1) the decode chain also
// runs SplitMatrixParams: a paired ";k=v" trailing sequence is peeled off
// the path (the M1 "no matrix parameters" reservation's redemption). The
// properties are VALIDATED here — a k=v-shaped suffix with an illegal key
// is a 400 on every verb — but Layout itself discards them: reads use the
// stripped path for addressing only, while the PUT family switches to
// ResolveContent so the properties reach repo.PutOptions.
func Layout(r *http.Request) (string, string, error) {
	key, rel, _, err := ResolveContent(r)
	return key, rel, err
}

// ResolveContent is Layout's full form: the path-normalization single
// point with the peeled matrix parameters surfaced as deploy properties
// (architecture section 15.3.1's calculateRepoPath equivalent). The PUT
// family of every content adapter consumes it — ServeHTTP resolves once,
// boxes the properties into the request context (WithDeployProps, the
// WithPrincipal twin) and the put handlers hand them to
// repo.PutOptions.Properties.
func ResolveContent(r *http.Request) (string, string, DeployProps, error) {
	if r == nil || r.URL == nil {
		return "", "", nil, fmt.Errorf("%w: empty request URL", ErrBadRequestPath)
	}
	raw := r.URL.EscapedPath()
	if raw == "" {
		raw = r.URL.Path
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: malformed percent-encoding in %q: %w", ErrBadRequestPath, raw, err)
	}
	clean, matrix := SplitMatrixParams(decoded)
	props, err := ParseMatrixProps(matrix)
	if err != nil {
		return "", "", nil, err
	}
	key, rel, err := splitRepoPath(clean)
	if err != nil {
		return "", "", nil, err
	}
	return key, rel, props, nil
}

// splitRepoPath splits a decoded path into (repoKey, relPath) and applies
// the generic layout rules:
//
//   - no query strings; matrix parameters are peeled upstream
//     (SplitMatrixParams, M10) — a non-paired ';' stays an ordinary path
//     character (the M1 legacy semantics, decision 11.39);
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
	if strings.ContainsFunc(rel, isControlByte) {
		// NUL/CR/LF/tab/DEL and every other control byte are rejected before
		// the segment walk: this layer is the one place raw client input
		// becomes a stored path, and later renderers (T-15 listings, HTML)
		// must never inherit the job of sanitizing it (T-13 review m2).
		return fmt.Errorf("%w: control characters are not allowed in artifact paths", ErrBadRequestPath)
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

// isControlByte reports whether r is a control character (C0 range, DEL).
func isControlByte(r rune) bool { return r < 0x20 || r == 0x7f }

// ---- matrix parameters (M10 T-286, architecture section 15.3.1) ----

// SplitMatrixParams is the single point of the matrix-parameter peel: it
// takes the DECODED content path (repo key included — the whole path is the
// stripping domain, inv-3 section 3.1) and returns the cleaned path plus
// the raw matrix region ("" when there is none).
//
// Grammar (BinFlow's own, backward compatibility first): everything from
// the FIRST ';' is a matrix region iff every ';'-separated segment of it
// contains '=' (the ";k=v(;k2=v2)*" shape). A region that matches is peeled
// wholesale; a region that does not — "file;name.jar", "x;y;z.txt", a ';'
// in a folder segment — leaves the path byte-identical to M1~M9 (the
// legacy fallback, decision 11.39: five seeded ';' fixtures keep their
// literal reachability forever). Key/value legality is ParseMatrixProps'
// question and only runs on a region that matched, so a legacy path can
// never 400 here.
//
// A trailing slash survives the peel ("dir/;k=v" -> "dir/"), matching the
// folder-addressing rule of validateRelPath.
func SplitMatrixParams(decoded string) (clean, matrix string) {
	i := strings.IndexByte(decoded, ';')
	if i < 0 {
		return decoded, ""
	}
	region := decoded[i:]
	for _, seg := range strings.Split(strings.TrimPrefix(region, ";"), ";") {
		if !strings.Contains(seg, "=") {
			// Not the k=v grammar: M1 literal-path semantics (the legacy
			// compatibility fallback — never an error).
			return decoded, ""
		}
	}
	return decoded[:i], region
}

// ParseMatrixProps validates and parses the matrix region SplitMatrixParams
// peeled off: each ";k=v" segment contributes one value to key k (repeated
// keys accumulate, the multi-value rule), and the closed property rules
// (metadata.ValidateProp*) guard charset, sizes and cardinality. A
// violation wraps ErrBadRequestPath — the 400 the deploy plane answers
// (Artifactory's illegal-key posture, rest-api.md section 1.3).
func ParseMatrixProps(matrix string) (DeployProps, error) {
	if matrix == "" {
		return nil, nil
	}
	props := DeployProps{}
	for _, seg := range strings.Split(strings.TrimPrefix(matrix, ";"), ";") {
		key, value, found := strings.Cut(seg, "=")
		if !found {
			// Unreachable through SplitMatrixParams (the region matched the
			// k=v shape); kept defensive so the two halves may also be used
			// apart without a silent no-op.
			return nil, fmt.Errorf("%w: matrix parameter %q carries no '='", ErrBadRequestPath, seg)
		}
		if err := metadata.ValidatePropKey(key); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadRequestPath, err)
		}
		if err := metadata.ValidatePropValue(key, value); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadRequestPath, err)
		}
		props[key] = append(props[key], value)
	}
	if err := metadata.ValidatePropSet(props); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadRequestPath, err)
	}
	return props, nil
}

// NormalizeRelPath is the exported one-liner other protocol adapters (M2+
// docker subpaths, M3 maven) can reuse for their own relPath validation
// once they have peeled off their protocol-specific segments.
func NormalizeRelPath(rel string) error { return validateRelPath(rel) }
