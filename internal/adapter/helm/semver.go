package helm

import (
	"fmt"
	"strconv"
	"strings"
)

// SemVer 2.0 (semver.org) parse and precedence — the ordering helm.md
// section 5.1 pins for index entries (descending, string-compare fallback
// for unparsable spellings) and the enforce-layout version check. Written
// against the spec text; the ecosystem's Go spellings live in siblings,
// but each protocol package owns its own copy by convention (cargo's is
// not importable across adapters).

type semver struct {
	major, minor, patch uint64
	pre                 string
}

// parseSemver validates one strict SemVer 2.0 spelling.
func parseSemver(v string) (semver, error) {
	var s semver
	rest := v
	if i := strings.IndexByte(rest, '+'); i >= 0 {
		if !validDotIdentifiers(rest[i+1:]) {
			return s, fmt.Errorf("invalid build metadata")
		}
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		s.pre = rest[i+1:]
		rest = rest[:i]
		if s.pre == "" || !validDotIdentifiers(s.pre) {
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

// parseNumericIdentifier accepts non-negative decimals without leading
// zeros ("0" legal, "01" not — semver section 9).
func parseNumericIdentifier(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty numeric identifier")
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("leading zero in numeric identifier %q", s)
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("numeric identifier %q: %w", s, err)
	}
	return n, nil
}

// validDotIdentifiers checks one dot-separated identifier list: non-empty
// overall, every identifier non-empty, charset [0-9A-Za-z-].
func validDotIdentifiers(s string) bool {
	if s == "" {
		return false
	}
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		for i := 0; i < len(id); i++ {
			c := id[i]
			switch {
			case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
			default:
				return false
			}
		}
	}
	return true
}

// compareSemver orders by semver precedence: -1 when a < b, 0 when equal
// (build metadata ignored), +1 when a > b. Invalid spellings order by raw
// string — a stable total order; validation is the upload chain's concern.
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
		x, y := as[i], bs[i]
		xn, xerr := strconv.ParseUint(x, 10, 64)
		yn, yerr := strconv.ParseUint(y, 10, 64)
		switch {
		case xerr == nil && yerr == nil:
			if c := compareUint(xn, yn); c != 0 {
				return c
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		case x != y:
			return strings.Compare(x, y)
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}
