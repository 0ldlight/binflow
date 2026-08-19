package npm

import (
	"fmt"
	"strings"
)

// Package-name rules (npm registry naming rules, NFR-S18 charset ruling):
//
//   - unscoped: 1..214 chars, limited charset [A-Za-z0-9._~-], no leading
//     "." or "_" (npm rejects those for new packages), never "." or "..";
//   - scoped: "@<scope>/<name>" with both halves non-empty and the same
//     per-half rules; exactly one '/'.
//
// Path traversal dies earlier (the layout's dot-segment defense, judged on
// the DECODED form) — these rules are the semantic layer on top: a name that
// survives decoding but is not a legal npm name (empty scope, double slash,
// charset violation) is a 400, not a silent store.
const (
	maxNameLen   = 214
	nameCharset  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._~-"
	scopeMark    = '@'
	scopeSep     = "/"
	tarballDir   = "-/"
	tarballExt   = ".tgz"
	tarballExt2  = ".tar.gz"
	packumentSeg = "packument.json"
)

// validatePackageName checks one package name spelling. The NFR-S18 finite
// charset is enforced char-by-char; the scope structure structurally.
func validatePackageName(name string) error {
	if name == "" {
		return fmt.Errorf("npm package name is empty")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("npm package name %q exceeds %d characters", name, maxNameLen)
	}
	if strings.HasPrefix(name, string(scopeMark)) {
		scope, pkg, found := strings.Cut(name, scopeSep)
		if !found || scope == "@" || pkg == "" || strings.Contains(pkg, scopeSep) {
			return fmt.Errorf("invalid scoped npm package name %q", name)
		}
		if err := validateNamePart(scope[1:]); err != nil {
			return err
		}
		return validateNamePart(pkg)
	}
	if strings.Contains(name, scopeSep) {
		return fmt.Errorf("invalid npm package name %q: unexpected '/'", name)
	}
	return validateNamePart(name)
}

// validateNamePart checks one unscoped half.
func validateNamePart(part string) error {
	if part == "" {
		return fmt.Errorf("npm package name part is empty")
	}
	if part == "." || part == ".." {
		return fmt.Errorf("npm package name part %q is a dot segment", part)
	}
	if part[0] == '.' || part[0] == '_' {
		return fmt.Errorf("npm package name part %q must not start with '.' or '_'", part)
	}
	for i := 0; i < len(part); i++ {
		if !strings.ContainsRune(nameCharset, rune(part[i])) {
			return fmt.Errorf("npm package name %q contains illegal character %q", part, string(part[i]))
		}
	}
	return nil
}

// tarballPath is the storage path of one version's tarball (C8 ruling):
// <name>/-/<name>-<version>.tgz, scoped @<scope>/<name>/-/@<scope>/<name>-<version>.tgz.
func tarballPath(name, version string) string {
	return name + "/" + tarballDir + name + "-" + version + tarballExt
}

// packumentPath is the storage path of the package document node
// (architecture section 5.4.2).
func packumentPath(name string) string {
	return name + "/" + packumentSeg
}

// isTarballFilename reports whether file carries a tarball extension. npm
// only ever publishes .tgz; Artifactory also routes .tar.gz reads.
func isTarballFilename(file string) bool {
	return strings.HasSuffix(file, tarballExt) || strings.HasSuffix(file, tarballExt2)
}
