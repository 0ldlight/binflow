package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// The state closed set — the minimal-face subset of the reference's
// four-value enum (release-bundle.md §3.2: FAILED/INPROGRESS/COMPLETE/
// CLOSE_INPROGRESS; ADR-0046 Errata ⑤ keeps COMPLETE/INPROGRESS, the
// C-layer cut logged in both). COMPLETE = every manifest row carries its
// nodes snapshot; INPROGRESS = at least one pending row (the artifact is
// not on this instance yet — the honest record-domain analog of the
// official async artifact copy, whose completion event belongs to the
// face-out Distribution plane).
const (
	StateComplete   = "COMPLETE"
	StateInProgress = "INPROGRESS"
)

// BundleTypeSource is the type dimension's only value (the reference's
// SOURCE/TARGET split is a two-instance topology: the source instance
// records what it published, the target instance records what it received.
// BinFlow's single instance keeps SOURCE records only — architecture
// §26.2's "type 恒 SOURCE" ruling, so the dimension is a code constant,
// never a column).
const BundleTypeSource = "SOURCE"

// Limits (release-bundle.md §2.1, high confidence, plus the wire-hygiene
// floors this domain owns): name first character alphanumeric, the rest
// alphanumeric or -_: (≤255); version ≤255 with no SemVer enforcement;
// the manifest carries the reference's recommended scale as a generous
// hard ceiling (the v1 recommendation is ≤3,000 artifacts per bundle —
// a recommendation, so the ceiling sits well above it and exists only to
// turn an absurd document into an honest 400).
const (
	maxBundleNameLength    = 255
	maxBundleVersionLength = 255
	maxManifestItems       = 10000
)

// ErrInvalidBundle marks a malformed create request (the 400 family's
// service face): a bad name/version, a bad manifest row, a duplicate
// identity, or an over-cap manifest. A well-formed request addressing a
// denied or conflicting pair is ErrForbidden / ErrBundleConflict, never
// this.
var ErrInvalidBundle = errors.New("bundle: invalid release bundle request")

// ValidateBundleName enforces the official naming rule: the first
// character is a letter or digit, every following character is
// alphanumeric or one of -_: (≤255 bytes). The name is also the Any
// Distribution channel's PATH element and a path segment of the family's
// URIs, so '/' and control characters are refused everywhere (wire
// hygiene; the official charset already excludes them — the control sweep
// is the belt to its braces).
func ValidateBundleName(name string) error {
	if name == "" {
		return fmt.Errorf("bundle name is empty: %w", ErrInvalidBundle)
	}
	if len(name) > maxBundleNameLength {
		return fmt.Errorf("bundle name exceeds %d bytes: %w", maxBundleNameLength, ErrInvalidBundle)
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			// Alphanumeric everywhere.
		case i > 0 && (r == '-' || r == '_' || r == ':'):
			// The three separators, never first.
		default:
			return fmt.Errorf("bundle name %q may carry alphanumerics and -_: only (first character alphanumeric): %w",
				name, ErrInvalidBundle)
		}
	}
	return nil
}

// ValidateBundleVersion enforces the version floor: non-empty, ≤255 bytes,
// control-free, no '/' (a path segment of the family's URIs; the official
// law imposes no SemVer format).
func ValidateBundleVersion(version string) error {
	if version == "" {
		return fmt.Errorf("bundle version is empty: %w", ErrInvalidBundle)
	}
	if len(version) > maxBundleVersionLength {
		return fmt.Errorf("bundle version exceeds %d bytes: %w", maxBundleVersionLength, ErrInvalidBundle)
	}
	if strings.ContainsRune(version, '/') || hasControl(version) {
		return fmt.Errorf("bundle version %q contains '/' or control characters: %w", version, ErrInvalidBundle)
	}
	return nil
}

// validateManifestRow enforces one manifest row's floor: a non-empty,
// control-free repository key that is NOT a preset wildcard bucket (a
// bucket literal can never resolve to a node — refusing it here keeps the
// pseudo-key channel out of the manifest), and a non-empty path without
// leading '/', '//' collapse, '..' segments or control characters (the
// traversal hygiene every path-parameter face owes).
func validateManifestRow(repoKey, path string) error {
	if repoKey == "" {
		return fmt.Errorf("manifest row repository is empty: %w", ErrInvalidBundle)
	}
	if hasControl(repoKey) || strings.ContainsRune(repoKey, '/') {
		return fmt.Errorf("manifest row repository %q contains '/' or control characters: %w", repoKey, ErrInvalidBundle)
	}
	if auth.IsWildcardBucket(repoKey) {
		return fmt.Errorf("manifest row repository %q is a preset wildcard bucket, not a repository: %w",
			repoKey, ErrInvalidBundle)
	}
	if path == "" {
		return fmt.Errorf("manifest row path is empty: %w", ErrInvalidBundle)
	}
	if hasControl(path) || strings.HasPrefix(path, "/") {
		return fmt.Errorf("manifest row path %q is not a relative artifact path: %w", path, ErrInvalidBundle)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("manifest row path %q carries an empty, '.' or '..' segment: %w", path, ErrInvalidBundle)
		}
	}
	return nil
}

// hasControl reports whether s contains any Unicode control character (C0
// including NUL/newline/carriage return, and DEL); invalid UTF-8 counts
// as control too — a malformed byte never passes the wire floor.
func hasControl(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ManifestItem is one row of the explicit manifest (the create face's
// body form — the official AQL-assembly channel degraded to the
// explicit-list subset, soft-seam ⑥). Repo and Path are the identity the
// digest hashes; Sha256 is an OPTIONAL pin — when carried and the live
// node disagrees, the create refuses (a wrong pin must not be silently
// snapshotted).
type ManifestItem struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
}

// manifestDigest is the signature placeholder (ADR-0046 Errata ② E6: no
// signing chain exists, so "same signature" is CONTENT-DIGEST equality —
// sha256 over the manifest's item-identity set). The digest covers ONLY
// the identities (repo, path) — never the resolution state — so a resume
// of the same manifest after artifacts landed digests identically, while
// any identity change (add, drop, re-spelling) is a different digest and
// therefore the 409 arm. Sorted lines of "repo/path" joined by '\n': repo
// keys cannot contain '/' (the key charset) and no validated string can
// contain a control character, so the encoding is unambiguous.
func manifestDigest(items []*ManifestItem) string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.Repo+"/"+it.Path)
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// digestOfSnapshotRows re-derives the digest from STORED item rows — the
// resume arm's integrity check that the stored identity set really is the
// request's (the digest comparison already implies it; this is the
// belt-and-braces readback, same inputs same function).
func digestOfSnapshotRows(rows []*metadata.BundleItem) string {
	items := make([]*ManifestItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, &ManifestItem{Repo: r.RepoKey, Path: r.Path})
	}
	return manifestDigest(items)
}
