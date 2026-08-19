package maven

import "testing"

// TestVersionOrdering pins the Maven ordering properties the metadata
// calculator (T-68) and virtual aggregation (T-72) rely on: numeric dot
// segments, qualifier weights, snapshot-below-release, and the equality
// of trailing-zero spellings.
func TestVersionOrdering(t *testing.T) {
	// ordered lists: each entry must compare < the next.
	ordered := [][]string{
		{"1.0.0-alpha-1", "1.0.0-beta-1", "1.0.0-milestone-2", "1.0.0-rc1", "1.0.0-SNAPSHOT", "1.0.0", "1.0.0-sp1"},
		{"1.9", "1.10", "1.10.1", "2.0"},
		{"1.0-SNAPSHOT", "1.0"},
		{"1.0-a1", "1.0-b1", "1.0-m1", "1.0-cr1", "1.0", "1.0-zebra"},
		{"1.0", "1.1", "1.2.0-SNAPSHOT"},
		{"0.9", "1.0", "1.0.1"},
		{"1.0-20240819.101500-1", "1.0-20240819.101500-2", "1.0-20240820.101500-1"},
	}
	var c VersionComparator
	for _, list := range ordered {
		for i := 0; i+1 < len(list); i++ {
			a, b := list[i], list[i+1]
			if got := c.CompareVersions(a, b); got >= 0 {
				t.Errorf("CompareVersions(%q, %q) = %d, want < 0", a, b, got)
			}
			if got := c.CompareVersions(b, a); got <= 0 {
				t.Errorf("CompareVersions(%q, %q) = %d, want > 0", b, a, got)
			}
		}
	}

	equal := [][2]string{
		{"1.0", "1.0.0"},
		{"1.0", "1.0.0.0"},
		{"1.0", "1.0-ga"},
		{"1.0", "1.0-final"},
		{"1.0-rc1", "1.0-rc1"},
		{"1.0.0", "1.0.0-RELEASE"},
	}
	for _, pair := range equal {
		if got := c.CompareVersions(pair[0], pair[1]); got != 0 {
			t.Errorf("CompareVersions(%q, %q) = %d, want 0", pair[0], pair[1], got)
		}
	}
}

// TestVersionSorting exercises the latest/release derivation shape T-68
// performs: sort a mixed list and read off latest (last) and release (last
// non-SNAPSHOT). The position-wise comparison keeps 1.2.0-alpha-1 ABOVE
// 1.0.0 — the qualifier attaches to 1.2.0, exactly the Maven reading.
func TestVersionSorting(t *testing.T) {
	in := []string{
		"1.2.0-SNAPSHOT", "1.0.0", "0.9.0", "1.10.0", "1.0.0-SNAPSHOT", "1.2.0-alpha-1",
	}
	var c VersionComparator
	for i := 0; i < len(in); i++ {
		for j := i + 1; j < len(in); j++ {
			if c.CompareVersions(in[i], in[j]) > 0 {
				in[i], in[j] = in[j], in[i]
			}
		}
	}
	want := []string{"0.9.0", "1.0.0-SNAPSHOT", "1.0.0", "1.2.0-alpha-1", "1.2.0-SNAPSHOT", "1.10.0"}
	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("sorted[%d] = %q, want %q (full: %v)", i, in[i], want[i], in)
		}
	}
	if latest := in[len(in)-1]; latest != "1.10.0" {
		t.Errorf("latest = %q, want 1.10.0", latest)
	}
	release := ""
	for _, v := range in {
		if !hasSnapshotQualifier(v) {
			release = v
		}
	}
	if release != "1.10.0" {
		t.Errorf("release = %q, want 1.10.0", release)
	}
}

// hasSnapshotQualifier is the test-side snapshot spelling probe.
func hasSnapshotQualifier(v string) bool {
	for _, tok := range tokenizeVersion(v) {
		if tok.kind == tokQualifier && qualifierWeights[tok.qualifier()] == qualifierWeights["snapshot"] {
			return true
		}
	}
	return false
}
