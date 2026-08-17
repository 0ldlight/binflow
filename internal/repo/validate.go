package repo

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxRepoKeyLen: [a-z][a-z0-9-]{1,62} bounds the total length to 63 (PRD
// FR-3-AC4; the architecture DDL note's {1,31} was superseded by T-22).
const maxRepoKeyLen = 63

// maxNodePathLen bounds one artifact path; httpapi keeps its own request-line
// limits (FR-4-AC11, 512 chars), this is the metadata-side ceiling.
const maxNodePathLen = 512

// knownPackageTypes is the DDL enum of package_type (architecture section
// 6). Anything outside it is a plain ErrInvalidRepoType; future-but-valid
// combinations are rejected one step later with ErrRepoTypeNotSupported.
var knownPackageTypes = map[string]bool{
	PackageGeneric: true, "docker": true, "maven": true, "npm": true, "pypi": true,
}

// supportedPackageTypes is the M1 support matrix: which (type, packageType)
// pairs the service can actually serve. Everything valid-but-future is
// rejected with ErrRepoTypeNotSupported (message "supported from M3") —
// httpapi translates the shape (400), the semantics stay here.
var supportedPackageTypes = map[string]map[string]bool{
	TypeLocal:   {PackageGeneric: true},
	TypeRemote:  {},
	TypeVirtual: {},
}

// validateRepoKey checks one repository key against the charset rule and the
// ADR-0008 reserved segments.
func validateRepoKey(key string) error {
	if len(key) < 2 || len(key) > maxRepoKeyLen {
		return fmt.Errorf("%w %q: must match [a-z][a-z0-9-]{1,62}", ErrInvalidRepoKey, key)
	}
	if key[0] < 'a' || key[0] > 'z' {
		return fmt.Errorf("%w %q: must start with a lowercase letter", ErrInvalidRepoKey, key)
	}
	for i := 1; i < len(key); i++ {
		c := key[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return fmt.Errorf("%w %q: illegal character %q at offset %d", ErrInvalidRepoKey, key, c, i)
		}
	}
	if reservedRepoKeys[key] {
		return fmt.Errorf("%w: %q is a reserved routing segment (ADR-0008)", ErrReservedRepoKey, key)
	}
	return nil
}

// validateRepoType checks rclass and package type. A syntactically unknown
// value is ErrInvalidRepoType; a known-but-future combination (remote or
// virtual repositories, or any non-generic package) is
// ErrRepoTypeNotSupported with "supported from M3" wording so httpapi can
// surface the reason.
func validateRepoType(rclass, packageType string) error {
	if rclass != TypeLocal && rclass != TypeRemote && rclass != TypeVirtual {
		return fmt.Errorf("%w %q: must be one of local, remote, virtual", ErrInvalidRepoType, rclass)
	}
	if !knownPackageTypes[packageType] {
		return fmt.Errorf("%w %q: must be one of generic, docker, maven, npm, pypi",
			ErrInvalidRepoType, packageType)
	}
	if !supportedPackageTypes[rclass][packageType] {
		return fmt.Errorf("%w: %s %s repositories are supported from M3",
			ErrRepoTypeNotSupported, rclass, packageType)
	}
	return nil
}

// normalizeConfig validates the type-specific config blob and returns the
// canonical form ("{}" when empty).
func normalizeConfig(config string) (string, error) {
	if strings.TrimSpace(config) == "" {
		return "{}", nil
	}
	var probe any
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidRepoConfig, err)
	}
	return config, nil
}

// validateNodePath checks a repo-relative artifact path. Trailing slash is
// allowed (folder node). The rules mirror the adapter-side layout contract:
// no empty segments, no "." or ".." segments, no leading slash, bounded
// length. httpapi owns request-line normalization; this is the defense in
// depth so a bad path can never reach the metadata layer.
func validateNodePath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: path is empty", ErrInvalidPath)
	}
	if path == "/" {
		return fmt.Errorf("%w: the repository root is not a node", ErrInvalidPath)
	}
	if len(path) > maxNodePathLen {
		return fmt.Errorf("%w: %d chars exceeds the %d limit", ErrInvalidPath, len(path), maxNodePathLen)
	}
	if strings.Contains(path, "//") {
		return fmt.Errorf("%w %q: empty path segment", ErrInvalidPath, path)
	}
	if strings.Contains(path, "\\") {
		return fmt.Errorf("%w %q: backslash is not a path separator", ErrInvalidPath, path)
	}
	if path[0] == '/' {
		return fmt.Errorf("%w %q: leading slash", ErrInvalidPath, path)
	}
	trimmed := strings.TrimSuffix(path, "/")
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w %q: dot segment", ErrInvalidPath, path)
		}
		if seg == "" {
			return fmt.Errorf("%w %q: empty path segment", ErrInvalidPath, path)
		}
	}
	return nil
}

// isFolderNode reports whether the (already validated) path addresses a
// folder node.
func isFolderNode(path string) bool {
	return strings.HasSuffix(path, "/")
}

// parentPrefix returns the trailing-slash prefix of path's parent directory:
// "a/b" → "a/", "a/b/" → "a/", "a" → "". Deleting folders prunes empty
// parents by collecting exactly these prefixes.
func parentPrefix(path string) string {
	trimmed := strings.TrimSuffix(path, "/")
	if i := strings.LastIndexByte(trimmed, '/'); i >= 0 {
		return trimmed[:i+1]
	}
	return ""
}
