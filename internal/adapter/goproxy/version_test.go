package goproxy

import "testing"

// TestValidPutVersion: the PUT grammar gate — canonical releases (with
// prerelease), +incompatible and the three pseudo-version shapes pass;
// branch names, leading zeros and bare majors fail (goproxy.md section
// 5.1).
func TestValidPutVersion(t *testing.T) {
	valid := []string{
		"v1.0.0", "v0.0.0", "v10.20.30", "v1.0.0-beta", "v1.0.0-beta.1",
		"v1.0.0-alpha.beta", "v2.3.4+incompatible",
		"v0.0.0-20180102030405-0123456789ab",       // pseudo base
		"v1.2.3-pre.0.20180102030405-0123456789ab", // pseudo pre-release
		"v1.2.4-0.20180102030405-0123456789ab",     // pseudo patch+1
		"v1.0.0+incompatible",
		"v1.0.0-20180102030405", // a digit-run prerelease is legal canonical grammar
	}
	for _, v := range valid {
		if !validPutVersion(v) {
			t.Errorf("validPutVersion(%q) = false, want true", v)
		}
	}
	invalid := []string{
		"1.0.0", "v1.0", "v1", "v01.0.0", "v1.0.00", "main", "master",
		"v1.0.0-", "v1..0", "v1.0.0+compat", "vv1.0.0",
	}
	for _, v := range invalid {
		if validPutVersion(v) {
			t.Errorf("validPutVersion(%q) = true, want false", v)
		}
	}
}

// TestCheckMajorConsistency: the /vN and gopkg.in .vN module-path rules.
func TestCheckMajorConsistency(t *testing.T) {
	ok := []struct{ module, version string }{
		{"example.com/m", "v1.0.0"},
		{"example.com/m/v2", "v2.0.0"},
		{"example.com/m/v2", "v2.5.0+incompatible"},
		{"example.com/m/v3/sub", "v1.0.0"}, // the suffix must be the LAST element
		{"gopkg.in/yaml.v2", "v2.4.0"},
	}
	for _, c := range ok {
		if err := checkMajorConsistency(c.module, c.version); err != nil {
			t.Errorf("checkMajorConsistency(%q, %q) = %v, want nil", c.module, c.version, err)
		}
	}
	bad := []struct{ module, version string }{
		{"example.com/m/v2", "v1.0.0"},
		{"example.com/m/v3", "v1.0.0"},
		{"gopkg.in/yaml.v3", "v2.4.0"},
	}
	for _, c := range bad {
		if err := checkMajorConsistency(c.module, c.version); err == nil {
			t.Errorf("checkMajorConsistency(%q, %q) = nil, want error", c.module, c.version)
		}
	}
}

// TestCompareVersions: the @latest candidate order — release beats
// pre-release beats pseudo; semver within a class; pseudo by commit time.
func TestCompareVersions(t *testing.T) {
	greater := []struct{ hi, lo string }{
		{"v1.0.0", "v1.0.0-beta"},
		{"v2.0.0", "v1.9.9"},
		{"v1.10.0", "v1.9.0"},
		{"v1.0.0-rc.2", "v1.0.0-rc.1"},
		{"v1.0.0-beta.2", "v1.0.0-beta"},
		{"v1.0.0-10", "v1.0.0-2"},                        // numeric identifiers compare numerically
		{"v1.0.0-alpha", "v1.0.0-1"},                     // alphanumeric beats numeric
		{"v1.0.0", "v0.0.0-20180102030405-0123456789ab"}, // release beats pseudo
		{"v1.0.0-beta", "v1.0.0-0.20180102030405-0123456789ab"},
		{"v0.0.0-20190102030405-0123456789ab", "v0.0.0-20180102030405-0123456789ab"}, // newer commit
		{"v1.0.0+incompatible", "v0.9.0"},                                            // +incompatible ignored by precedence
	}
	for _, c := range greater {
		if got := CompareVersions(c.hi, c.lo); got <= 0 {
			t.Errorf("CompareVersions(%q, %q) = %d, want > 0", c.hi, c.lo, got)
		}
		if got := CompareVersions(c.lo, c.hi); got >= 0 {
			t.Errorf("CompareVersions(%q, %q) = %d, want < 0", c.lo, c.hi, got)
		}
	}
	if got := CompareVersions("v1.0.0", "v1.0.0"); got != 0 {
		t.Errorf("equal versions compare %d", got)
	}
}

// TestPickLatest: the global winner across mixed classes.
func TestPickLatest(t *testing.T) {
	best, ok := pickLatest([]string{
		"v0.0.0-20180102030405-0123456789ab",
		"v1.0.0-beta.1",
		"v0.9.0",
		"v1.0.0-alpha",
	})
	if !ok || best != "v0.9.0" {
		t.Errorf("pickLatest = (%q, %v), want (v0.9.0, true)", best, ok)
	}
	best, ok = pickLatest([]string{
		"v0.0.0-20190102030405-0123456789ab",
		"v0.0.0-20180102030405-0123456789ab",
	})
	if !ok || best != "v0.0.0-20190102030405-0123456789ab" {
		t.Errorf("pickLatest pseudo = (%q, %v), want the newer commit", best, ok)
	}
	if _, ok := pickLatest(nil); ok {
		t.Error("pickLatest(nil) ok, want false")
	}
}

// TestModuleDirectiveOf: the .mod identity line parser (quoted and bare
// paths, comments, leading noise).
func TestModuleDirectiveOf(t *testing.T) {
	cases := []struct {
		body string
		want string
		ok   bool
	}{
		{body: "module example.com/m\n", want: "example.com/m", ok: true},
		{body: "module \"example.com/m\"\n", want: "example.com/m", ok: true},
		{body: "// comment\nmodule example.com/m\n\ngo 1.21\n", want: "example.com/m", ok: true},
		{body: "module example.com/m // trailing\n", want: "example.com/m", ok: true},
		{body: "go 1.21\n", ok: false},
		{body: "module\n", ok: false},
		{body: "module two words\n", ok: false},
		{body: "", ok: false},
	}
	for _, c := range cases {
		got, ok := moduleDirectiveOf([]byte(c.body))
		if ok != c.ok || got != c.want {
			t.Errorf("moduleDirectiveOf(%q) = (%q, %v), want (%q, %v)", c.body, got, ok, c.want, c.ok)
		}
	}
}
