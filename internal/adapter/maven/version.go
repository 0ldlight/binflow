package maven

import (
	"strconv"
	"strings"
)

// VersionComparator implements Maven version ordering for the metadata
// registry (architecture section 5.4; the consumer is T-68's metadata
// calculator sorting <versions>, and M4's virtual best-version seam).
//
// Maven version ordering is NOT semver; this is a clean-room subset of the
// maven-artifact ComparableVersion rules (no spec text was copied — the
// behavior table below is what Maven's own documentation and the wild GAV
// population exercise):
//
//   - Versions tokenize on '.' and '-' into integers, known qualifiers and
//     unknown strings.
//   - Integer tokens compare numerically (1.10 > 1.9); trailing zero
//     tokens equal a missing token (1.0 == 1.0.0).
//   - Qualifier weights, oldest to newest:
//     alpha(a) < beta(b) < milestone(m) < rc(cr) < snapshot < release
//     ("" /ga/final) < sp < any unknown qualifier (lexicographic among
//     themselves, case-insensitive).
//   - A numeric token at a position where the other version has none is
//     greater than the missing side, but equal when the number is zero.
//   - Consequently 1.0-SNAPSHOT < 1.0, 1.0-rc1 < 1.0, 1.0 < 1.0-sp1, and
//     the timestamped snapshot file spelling
//     (1.0-20240101.123456-1, all-numeric tokens) sorts above 1.0 — the
//     version DIRECTORY spellings the calculator sorts never use the
//     timestamped form, so that property is inert there.
type VersionComparator struct{}

// CompareVersions orders a against b: -1, 0, +1.
func (VersionComparator) CompareVersions(a, b string) int {
	x, y := tokenizeVersion(a), tokenizeVersion(b)
	n := len(x)
	if len(y) > n {
		n = len(y)
	}
	for i := 0; i < n; i++ {
		var ta, tb versionToken
		if i < len(x) {
			ta = x[i]
		} else {
			ta = versionToken{kind: tokMissing}
		}
		if i < len(y) {
			tb = y[i]
		} else {
			tb = versionToken{kind: tokMissing}
		}
		if c := ta.compare(tb); c != 0 {
			return c
		}
	}
	return 0
}

// token kinds.
type tokKind int

const (
	tokMissing tokKind = iota
	tokInt
	tokQualifier
)

// versionToken is one parsed version component.
type versionToken struct {
	kind tokKind
	num  int64
	str  string
}

// qualifierWeights orders the known qualifiers (see the comparator doc).
var qualifierWeights = map[string]int{
	"alpha": 1, "a": 1,
	"beta": 2, "b": 2,
	"milestone": 3, "m": 3,
	"rc": 4, "cr": 4,
	"snapshot": 5,
	"":         6, "ga": 6, "final": 6, "release": 6,
	"sp": 7,
}

// constUnknownWeight sorts after every known qualifier.
const constUnknownWeight = 8

// compare orders two tokens per the rules above.
func (t versionToken) compare(o versionToken) int {
	// Missing-vs-missing equal; missing acts as int 0 / qualifier release.
	switch {
	case t.kind == tokMissing && o.kind == tokMissing:
		return 0
	case t.kind == tokInt && o.kind == tokInt:
		switch {
		case t.num < o.num:
			return -1
		case t.num > o.num:
			return 1
		}
		return 0
	case t.kind == tokInt && o.kind == tokMissing:
		if t.num == 0 {
			return 0 // trailing zero equals the absent token
		}
		return 1
	case t.kind == tokMissing && o.kind == tokInt:
		if o.num == 0 {
			return 0
		}
		return -1
	case t.kind == tokInt && o.kind == tokQualifier:
		return 1 // a numeric token outranks a qualifier label
	case t.kind == tokQualifier && o.kind == tokInt:
		return -1
	}
	// qualifier vs qualifier (missing counts as the release qualifier)
	wa, wb := qualifierWeight(t.qualifier()), qualifierWeight(o.qualifier())
	if wa != wb {
		if wa < wb {
			return -1
		}
		return 1
	}
	if wa == constUnknownWeight && t.str != o.str {
		return strings.Compare(strings.ToLower(t.str), strings.ToLower(o.str))
	}
	return 0
}

// qualifierWeight maps a qualifier spelling onto its ordering weight;
// every spelling outside the known table is an unknown qualifier
// (constUnknownWeight), never the map's zero value.
func qualifierWeight(s string) int {
	if w, ok := qualifierWeights[s]; ok {
		return w
	}
	return constUnknownWeight
}

// qualifier renders the token's qualifier weight key; tokMissing is the
// release weight.
func (t versionToken) qualifier() string {
	if t.kind == tokQualifier {
		return strings.ToLower(t.str)
	}
	return ""
}

// tokenizeVersion splits a version into comparison tokens. Empty input
// tokenizes to nothing (equal to any other empty/zero spelling).
func tokenizeVersion(v string) []versionToken {
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	tokens := make([]versionToken, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if n, err := strconv.ParseInt(f, 10, 64); err == nil {
			tokens = append(tokens, versionToken{kind: tokInt, num: n})
			continue
		}
		// Mixed spellings ("rc1", "alpha-1" already split, "1ga"):
		// split leading letters from a trailing number so rc1/alpha2 rank
		// by label then number — the Maven spelling habit for numbered
		// qualifiers.
		head, tail := splitAlphaNum(f)
		if head != "" {
			tokens = append(tokens, versionToken{kind: tokQualifier, str: head})
		}
		if tail != "" {
			if n, err := strconv.ParseInt(tail, 10, 64); err == nil {
				tokens = append(tokens, versionToken{kind: tokInt, num: n})
			} else {
				tokens = append(tokens, versionToken{kind: tokQualifier, str: tail})
			}
		}
	}
	return tokens
}

// splitAlphaNum splits a mixed token into its leading letter run and the
// remainder ("rc1" -> "rc", "1"; "x" -> "x", "").
func splitAlphaNum(s string) (head, tail string) {
	i := 0
	for i < len(s) {
		c := s[i]
		isLetter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !isLetter {
			break
		}
		i++
	}
	return s[:i], s[i:]
}
