package cargo

import (
	"fmt"
	"strconv"
	"strings"
)

// Strict SemVer 2.0 validation and precedence (semver.org; the cargo index
// "vers" grammar). Hand-rolled stdlib-only like the npm twin (ADR-0005) —
// same rules, restated locally so this package never reaches into a
// sibling adapter's internals.

// semver is one parsed cargo version.
type semver struct {
	major, minor, patch uint64
	pre                 string // prerelease without the leading '-', "" when none
	build               string // build metadata without '+', "" when none
}

// parseSemver parses the strict form major.minor.patch[-pre][+build].
func parseSemver(v string) (semver, error) {
	var s semver
	rest := v
	if i := strings.IndexByte(rest, '+'); i >= 0 {
		s.build = rest[i+1:]
		rest = rest[:i]
		if !validDotIdentifiers(s.build, true) {
			return s, fmt.Errorf("invalid build metadata")
		}
	}
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		s.pre = rest[i+1:]
		rest = rest[:i]
		if s.pre == "" || !validDotIdentifiers(s.pre, false) {
			return s, fmt.Errorf("invalid prerelease")
		}
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return s, fmt.Errorf("want major.minor.patch")
	}
	var err error
	if s.major, err = parseNumericIdentifier(parts[0]); err != nil {
		return s, err
	}
	if s.minor, err = parseNumericIdentifier(parts[1]); err != nil {
		return s, err
	}
	if s.patch, err = parseNumericIdentifier(parts[2]); err != nil {
		return s, err
	}
	return s, nil
}

// parseNumericIdentifier accepts only non-negative decimals WITHOUT leading
// zeros ("0" itself legal, "01" not — semver section 9).
func parseNumericIdentifier(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty numeric identifier")
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("numeric identifier %q has a leading zero", s)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("numeric identifier %q is not numeric", s)
		}
	}
	return strconv.ParseUint(s, 10, 64)
}

// validDotIdentifiers checks dot-separated prerelease/build identifiers:
// alphanumerics and hyphens; numeric prerelease identifiers additionally
// free of leading zeros (build metadata exempts them, semver section 10).
func validDotIdentifiers(s string, isBuild bool) bool {
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		numeric := true
		for i := 0; i < len(id); i++ {
			c := id[i]
			switch {
			case c >= '0' && c <= '9':
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
				numeric = false
			default:
				return false
			}
		}
		if numeric && !isBuild && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

// isNumericIdentifier reports whether id is all digits (ordering input).
func isNumericIdentifier(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

// compareSemver orders two version strings by semver precedence: -1 when
// a < b, 0 when equal (build metadata ignored — the index uniqueness rule),
// +1 when a > b. Invalid spellings order by raw string (a stable total
// order; validation is the publish chain's concern).
func compareSemver(a, b string) int {
	sa, errA := parseSemver(a)
	sb, errB := parseSemver(b)
	if errA != nil || errB != nil {
		return strings.Compare(a, b)
	}
	if c := compareUint(sa.major, sb.major); c != 0 {
		return c
	}
	if c := compareUint(sa.minor, sb.minor); c != 0 {
		return c
	}
	if c := compareUint(sa.patch, sb.patch); c != 0 {
		return c
	}
	// A version WITHOUT prerelease outranks the same version WITH one.
	switch {
	case sa.pre == "" && sb.pre == "":
		return 0
	case sa.pre == "":
		return 1
	case sb.pre == "":
		return -1
	}
	return comparePrerelease(sa.pre, sb.pre)
}

func compareUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// comparePrerelease implements semver section 11: identifiers compare
// pairwise (numeric < alphanumeric, numeric numerically, alphanumeric in
// ASCII order); a longer identifier list outranks its prefix.
func comparePrerelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		xn, yn := isNumericIdentifier(as[i]), isNumericIdentifier(bs[i])
		switch {
		case xn && yn:
			x, _ := strconv.ParseUint(as[i], 10, 64)
			y, _ := strconv.ParseUint(bs[i], 10, 64)
			if c := compareUint(x, y); c != 0 {
				return c
			}
		case xn:
			return -1 // numeric identifiers always have lower precedence
		case yn:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return compareInt(len(as), len(bs))
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// sameVersionIgnoringBuild reports whether two version spellings are the
// same version for the index uniqueness rule (official MUST: "the same
// version, ignoring build metadata, may not be listed twice"). Unparsable
// spellings fall back to plain string equality.
func sameVersionIgnoringBuild(a, b string) bool {
	sa, errA := parseSemver(a)
	sb, errB := parseSemver(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return sa.major == sb.major && sa.minor == sb.minor && sa.patch == sb.patch && sa.pre == sb.pre
}
