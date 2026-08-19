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
// 6). Anything outside it is a plain ErrInvalidRepoType; valid-but-unserved
// combinations are rejected one step later with ErrRepoTypeNotSupported.
var knownPackageTypes = map[string]bool{
	PackageGeneric: true, PackageDocker: true, PackageMaven: true, PackageNpm: true, PackagePypi: true,
}

// supportedPackageTypes is the M3 support matrix (FR-15, T-64): all three
// repository classes × {generic, maven, npm, pypi}, docker on LOCAL only
// (FR-15-AC7 / PRD Q4: remote and virtual docker stay out of M3 — the
// registry proxy/aggregation semantics are unverified spec ground,
// docker-registry.md section 9; re-evaluation is M4). The one rejected
// combination answers ErrRepoTypeNotSupported with "not supported in M3"
// wording; httpapi translates the shape (400), the semantics stay here.
var supportedPackageTypes = map[string]map[string]bool{
	TypeLocal:   {PackageGeneric: true, PackageDocker: true, PackageMaven: true, PackageNpm: true, PackagePypi: true},
	TypeRemote:  {PackageGeneric: true, PackageMaven: true, PackageNpm: true, PackagePypi: true},
	TypeVirtual: {PackageGeneric: true, PackageMaven: true, PackageNpm: true, PackagePypi: true},
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
// value is ErrInvalidRepoType; the M3 matrix leaves exactly one
// valid-but-unserved combination — docker on remote or virtual — which is
// ErrRepoTypeNotSupported with "not supported in M3" wording (FR-15-AC7) so
// httpapi can surface the reason.
func validateRepoType(rclass, packageType string) error {
	if rclass != TypeLocal && rclass != TypeRemote && rclass != TypeVirtual {
		return fmt.Errorf("%w %q: must be one of local, remote, virtual", ErrInvalidRepoType, rclass)
	}
	if !knownPackageTypes[packageType] {
		return fmt.Errorf("%w %q: must be one of generic, docker, maven, npm, pypi",
			ErrInvalidRepoType, packageType)
	}
	if !supportedPackageTypes[rclass][packageType] {
		return fmt.Errorf("%w: %s %s repositories are not supported in M3 (docker is local-only; PRD Q4)",
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

// ---- docker shapes (architecture section 5.3 / 6 layout) ----

// hexDigits bounds validateDigest's character walk.
var hexDigits = map[byte]bool{
	'0': true, '1': true, '2': true, '3': true, '4': true, '5': true, '6': true, '7': true,
	'8': true, '9': true, 'a': true, 'b': true, 'c': true, 'd': true, 'e': true, 'f': true,
}

// validateDigest checks the bare-hex sha256 shape the docker plane keys on:
// exactly 64 lowercase hex characters (architecture section 5.3 ruling 2 —
// M2 serves sha256 only; adapters strip the "sha256:" prefix at the edge, so
// an uppercase or prefixed value reaching here is an adapter bug or a forged
// internal call and is refused).
func validateDigest(digest string) error {
	if len(digest) != 64 {
		return fmt.Errorf("%w %q: must be 64 lowercase hex characters (bare sha256, no algorithm prefix)",
			ErrInvalidDigest, digest)
	}
	for i := 0; i < len(digest); i++ {
		if !hexDigits[digest[i]] {
			return fmt.Errorf("%w %q: illegal character %q at offset %d (lowercase hex only)",
				ErrInvalidDigest, digest, digest[i], i)
		}
	}
	return nil
}

// validateTag checks the docker tag charset the schema comment carries
// ([a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}) — 128 characters at most. docker_tags
// has no DB-level constraint for it, so this layer is the rule's only home.
func validateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("%w: tag is empty", ErrInvalidTag)
	}
	if len(tag) > 128 {
		return fmt.Errorf("%w %q: %d characters exceeds the 128 limit", ErrInvalidTag, tag, len(tag))
	}
	c := tag[0]
	isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
	if !isAlnum && c != '_' {
		return fmt.Errorf("%w %q: first character must be [a-zA-Z0-9_]", ErrInvalidTag, tag)
	}
	for i := 1; i < len(tag); i++ {
		c := tag[i]
		isAlnum = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlnum && c != '_' && c != '.' && c != '-' {
			return fmt.Errorf("%w %q: illegal character %q at offset %d", ErrInvalidTag, tag, c, i)
		}
	}
	return nil
}

// dockerImageManifestPath is the node path of one manifest under the docker
// layout convention (architecture section 6, 002_docker comment):
// "<image>/manifests/<digest-hex>".
func dockerImageManifestPath(image, digest string) string {
	return image + "/manifests/" + digest
}

// parentPrefix returns the trailing-slash prefix of path's parent directory:
// "a/b" → "a/", "a/b/" → "a/", "a" → "". Deleting folders prunes empty
// parents by collecting exactly these prefixes.
//
// NOTE: the trailing slash is the *storage* spelling of a folder row. It must
// never be handed to NodeStore.ListByPrefix/DeleteByPrefix as-is: metadata's
// likePrefix builds the subtree arm as prefix+"/%", so "d/" would become
// "d//%" — a pattern no path can match (double slashes are rejected by
// validateNodePath). Strip the slash first (see T-12 review B1/B2).
func parentPrefix(path string) string {
	trimmed := strings.TrimSuffix(path, "/")
	if i := strings.LastIndexByte(trimmed, '/'); i >= 0 {
		return trimmed[:i+1]
	}
	return ""
}
