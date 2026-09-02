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

// supportedPackageTypes is the support matrix (FR-15, T-64): all three
// repository classes × {generic, maven, npm, pypi} since M3, docker on
// LOCAL since M2, on REMOTE since T-392 (M14 FR-129 — the /v2 pull-through
// rides the family-shared remote data chain helmoci opened in T-363, the
// K54 "shared seam, marginal cost zero" ruling), and on VIRTUAL since
// T-431 (M15 PRD Q6's ruling): the aggregated READ plane T-365 built for
// helmoci is family-shared exactly like the remote chain, so a docker
// virtual walks its members through the same four read use cases — the
// helmoci precedent's semantics, one matrix cell. The matrix therefore
// currently refuses nothing among the static five; the refusal arm below
// stays because supportedPackageTypes remains the single source of truth —
// a future cell ruling empties (or here, re-fills) a cell without
// re-plumbing the refusal, and httpapi's 400 translation of
// ErrRepoTypeNotSupported still covers the shape.
var supportedPackageTypes = map[string]map[string]bool{
	TypeLocal:   {PackageGeneric: true, PackageDocker: true, PackageMaven: true, PackageNpm: true, PackagePypi: true},
	TypeRemote:  {PackageGeneric: true, PackageDocker: true, PackageMaven: true, PackageNpm: true, PackagePypi: true},
	TypeVirtual: {PackageGeneric: true, PackageDocker: true, PackageMaven: true, PackageNpm: true, PackagePypi: true},
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

// validateRclass checks the repository-class closed set (the piece of
// validateRepoType the T-283 dynamic overlay reuses — a registry-known
// package type still demands one of the three classes).
func validateRclass(rclass string) error {
	if rclass != TypeLocal && rclass != TypeRemote && rclass != TypeVirtual {
		return fmt.Errorf("%w %q: must be one of local, remote, virtual", ErrInvalidRepoType, rclass)
	}
	return nil
}

// errClassNotSupported is the class-matrix refusal — shared by the static
// path and the dynamic overlay so the two can never drift on the wording.
// Its history: FR-15-AC7 originally refused both remote and virtual docker;
// T-392/FR-129 opened remote onto the T-363 remote seam, and T-431 (M15 Q6)
// opened virtual onto the T-365 aggregated read plane — the matrix holds no
// refused cell today, so this arm is the seam a future ruling re-fills, not
// a live refusal.
func errClassNotSupported(rclass, packageType string) error {
	return fmt.Errorf("%w: %s %s repositories are not supported",
		ErrRepoTypeNotSupported, rclass, packageType)
}

// validateRepoType checks rclass and package type. A syntactically unknown
// value is ErrInvalidRepoType; a matrix-refused combination (none among the
// static five since T-431 opened virtual docker — see
// supportedPackageTypes) is ErrRepoTypeNotSupported with the
// errClassNotSupported wording so httpapi can surface the reason.
func validateRepoType(rclass, packageType string) error {
	if err := validateRclass(rclass); err != nil {
		return err
	}
	if !knownPackageTypes[packageType] {
		return fmt.Errorf("%w %q: must be one of generic, docker, maven, npm, pypi",
			ErrInvalidRepoType, packageType)
	}
	if !supportedPackageTypes[rclass][packageType] {
		return errClassNotSupported(rclass, packageType)
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

// ancestorDirs enumerates the trailing-slash folder paths of every ancestor
// directory of path, outermost first, EXCLUDING path itself — a folder
// target is written by putNode's own folder arm, never as its own ancestor
// (ADR-0016: "a/b/c/" materializes "a/" and "a/b/", then writes "a/b/c/").
// Root-level paths ("f", "a/") have no ancestors and answer nil.
func ancestorDirs(path string) []string {
	segs := strings.Split(strings.TrimSuffix(path, "/"), "/")
	var dirs []string
	for i := 1; i < len(segs); i++ {
		dirs = append(dirs, strings.Join(segs[:i], "/")+"/")
	}
	return dirs
}
