package cargo

import "testing"

// SemVer 2.0 validation table (semver.org sections 9/10; the spec
// section 5.3 rule: `vers` must be legal SemVer 2.0).
func TestParseSemver(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"0.0.0", true},
		{"0.1.0", true},
		{"1.2.3", true},
		{"1.2.3-alpha.1", true},
		{"1.2.3-alpha.1+build.5", true},
		{"1.2.3+build", true},
		{"1.2.3-0", true},
		{"1.2.3-01a", true}, // alphanumeric prerelease identifier may carry leading zero

		{"", false},
		{"1.2", false},
		{"1.2.3.4", false},
		{"01.2.3", false}, // leading-zero numeric identifier
		{"1.02.3", false},
		{"1.2.03", false},
		{"1.2.3-", false},
		{"1.2.3-alpha..1", false},
		{"1.2.3-!", false},
		{"v1.2.3", false},
		{"1.2.3+", false}, // empty build metadata
	}
	for _, tc := range cases {
		_, err := parseSemver(tc.v)
		if (err == nil) != tc.want {
			t.Errorf("parseSemver(%q) err = %v, want ok=%v", tc.v, err, tc.want)
		}
	}
}

// SemVer precedence table (semver.org section 11 — the index order and
// the search max-version ride on this).
func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.0", "2.0.0", -1},
		{"2.1.0", "2.0.9", 1},
		{"1.0.0-alpha", "1.0.0", -1},      // prerelease ranks below release
		{"1.0.0-alpha", "1.0.0-beta", -1}, // alphanumeric ASCII order
		{"1.0.0-alpha.1", "1.0.0-alpha", 1},
		{"1.0.0-alpha.beta", "1.0.0-beta", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0+b1", "1.0.0+b2", 0}, // build metadata ignored in precedence
		{"10.0.0", "9.0.0", 1},      // numeric, not lexicographic
	}
	for _, tc := range cases {
		if got := compareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// sameVersionIgnoringBuild table (the index uniqueness rule, CG-3's
// input: build metadata does not distinguish versions).
func TestSameVersionIgnoringBuild(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.0", "1.0.0", true},
		{"1.0.0", "1.0.0+build", true},
		{"1.0.0+b1", "1.0.0+b2", true},
		{"1.0.0", "1.0.1", false},
		{"1.0.0", "1.0.0-alpha", false},
	}
	for _, tc := range cases {
		if got := sameVersionIgnoringBuild(tc.a, tc.b); got != tc.want {
			t.Errorf("sameVersionIgnoringBuild(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
