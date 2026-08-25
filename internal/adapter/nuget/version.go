package nuget

import (
	"strings"
	"unicode"
)

// NuGet version grammar and ordering (the official NuGet versioning spec:
// major.minor.patch[.revision][-prerelease][+build metadata]).
//
// The adapter's use of the ordering is narrow and honest: sorting versions
// ascending for registrations/feeds and picking the "latest stable" flag
// for v2 entries and search. The implementation below is a faithful
// simplification of the spec's comparison rules (numeric fields compared
// numerically, missing revision = 0; a release sorts above any prerelease;
// prerelease identifiers compare numeric<alphanumeric, numeric
// numerically, alphanumeric ordinal-cased; build metadata ignored for
// ordering).

// nugetVersion is one parsed version spelling.
type nugetVersion struct {
	core       [4]uint64 // major, minor, patch, revision (0 when absent)
	hasRev     bool      // the fourth field was spelled
	prerelease []string  // dot-separated prerelease identifiers ("" when none)
	hasPre     bool
	build      string // +metadata, ordering-irrelevant, kept for rendering
}

// parseNuGetVersion parses one version spelling. ok is false for anything
// that is not the legal grammar (empty identifiers, non-numeric cores,
// empty prerelease identifiers, whitespace).
func parseNuGetVersion(s string) (nugetVersion, bool) {
	var v nugetVersion
	if s == "" {
		return v, false
	}
	// Build metadata peels first (never participates in ordering).
	if i := strings.IndexByte(s, '+'); i >= 0 {
		v.build = s[i+1:]
		s = s[:i]
		if s == "" {
			return v, false
		}
	}
	// Prerelease peels after the core.
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre := s[i+1:]
		s = s[:i]
		v.hasPre = true
		for _, id := range strings.Split(pre, ".") {
			if id == "" || !validPrereleaseIdent(id) {
				return nugetVersion{}, false
			}
			// A numeric identifier must not carry leading zeros ("01" is
			// the spec's own example of an illegal spelling).
			if isNumericIdent(id) && len(id) > 1 && id[0] == '0' {
				return nugetVersion{}, false
			}
			v.prerelease = append(v.prerelease, id)
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return nugetVersion{}, false
	}
	for i, p := range parts {
		n, ok := parseUint(p)
		if !ok {
			return nugetVersion{}, false
		}
		v.core[i] = n
		if i == 3 {
			v.hasRev = true
		}
	}
	return v, true
}

// validPrereleaseIdent: the spec's identifier charset (letters, digits,
// hyphens; numeric identifiers further constrained at the call site).
func validPrereleaseIdent(id string) bool {
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

func isNumericIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseUint is strconv.ParseUint for the version grammar's own bounds
// (no signs, no underscores, 64-bit range).
func parseUint(s string) (uint64, bool) {
	if s == "" || len(s) > 19 {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, true
}

// compareNuGetVersions is the spec's total order: core fields numerically
// (missing revision = 0), then release > prerelease, then prerelease
// identifiers pairwise (numeric < alphanumeric; numeric numerically;
// alphanumeric ordinal, ASCII upper before lower); a shorter prerelease
// list sorts below its own prefix extension ("1.0.0-alpha" < "1.0.0-alpha.1").
// Build metadata is ignored, so equal-order versions return 0.
func compareNuGetVersions(a, b string) int {
	va, oka := parseNuGetVersion(a)
	vb, okb := parseNuGetVersion(b)
	if !oka || !okb {
		// Unparsable spellings never reach here through validated paths;
		// the fallback keeps the order total anyway (lexicographic).
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	}
	for i := 0; i < 4; i++ {
		if va.core[i] != vb.core[i] {
			if va.core[i] < vb.core[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case !va.hasPre && !vb.hasPre:
		return 0
	case !va.hasPre:
		return 1 // release above prerelease
	case !vb.hasPre:
		return -1
	}
	for i := 0; i < len(va.prerelease) && i < len(vb.prerelease); i++ {
		x, y := va.prerelease[i], vb.prerelease[i]
		xn, yn := isNumericIdent(x), isNumericIdent(y)
		switch {
		case xn && yn:
			if x != y {
				if len(x) < len(y) || (len(x) == len(y) && x < y) {
					return -1
				}
				return 1
			}
		case xn:
			return -1 // numeric identifiers sort below alphanumeric
		case yn:
			return 1
		default:
			if x != y {
				if compareOrdinalIdent(x, y) < 0 {
					return -1
				}
				return 1
			}
		}
	}
	switch {
	case len(va.prerelease) < len(vb.prerelease):
		return -1
	case len(va.prerelease) > len(vb.prerelease):
		return 1
	}
	return 0
}

// compareOrdinalIdent: the spec compares ASCII ordinals with upper before
// lower ("1.0.0-Beta" < "1.0.0-beta") — exactly Go's string comparison.
func compareOrdinalIdent(a, b string) int { return strings.Compare(a, b) }

// normalizeNuGetVersion renders the canonical spelling (the official
// normalization: missing minor/patch filled to three fields — "1.2"
// becomes "1.2.0"; a spelled fourth field kept — "1.2.3.4" stays;
// leading zeros stripped; prerelease and build metadata lowercased).
// This is the flatcontainer / registration "lower version" key the
// official protocol keys its storage on. ok is false for an unparsable
// spelling.
func normalizeNuGetVersion(s string) (string, bool) {
	v, ok := parseNuGetVersion(s)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString(u64toa(v.core[0]))
	for i := 1; i < 3; i++ {
		b.WriteByte('.')
		b.WriteString(u64toa(v.core[i]))
	}
	if v.hasRev {
		b.WriteByte('.')
		b.WriteString(u64toa(v.core[3]))
	}
	if v.hasPre {
		b.WriteByte('-')
		for i, id := range v.prerelease {
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(strings.ToLower(id))
		}
	}
	if v.build != "" {
		b.WriteByte('+')
		b.WriteString(strings.ToLower(v.build))
	}
	return b.String(), true
}

// isPrereleaseVersion reports the -prerelease mark.
func isPrereleaseVersion(s string) bool {
	v, ok := parseNuGetVersion(s)
	return ok && v.hasPre
}

// u64toa is strconv.FormatUint(_, 10) without the import.
func u64toa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// validPackageID applies the official package ID grammar: non-empty,
// starts and ends with a non-dot non-hyphen character, letters/digits/
// hyphens/underscores/dots inside, bounded length (100 is the gallery's
// documented ceiling).
func validPackageID(id string) bool {
	if id == "" || len(id) > 100 {
		return false
	}
	if !isIDCorner(id[0]) || !isIDCorner(id[len(id)-1]) {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func isIDCorner(c byte) bool {
	return c == '-' || c == '_' || isAlnumASCII(c)
}

func isAlnumASCII(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// lowerASCII is strings.ToLower for the ASCII-only identifiers this
// package handles (avoids unicode-folding surprises in path keys).
func lowerASCII(s string) string {
	hasUpper := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return s
	}
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// isControlRune guards the path grammar (the shared defense's C0/DEL set).
func isControlRune(r rune) bool { return r < 0x20 || r == 0x7f }

// trimSpaceASCII is strings.TrimSpace for the header/query surfaces.
func trimSpaceASCII(s string) string { return strings.TrimFunc(s, unicode.IsSpace) }
