package npm

import "testing"

// TestSemverValidity: strict node-semver acceptance, including the npm
// registry's leading-zero rule.
func TestSemverValidity(t *testing.T) {
	valid := []string{
		"0.0.0", "1.0.0", "1.2.3", "10.20.30", "1.0.0-alpha", "1.0.0-alpha.1",
		"1.0.0-0.3.7", "1.0.0-x.7.z.92", "1.0.0-alpha+001", "1.0.0+build.1",
		"1.0.0-beta+exp.sha.5114f85", "0.0.4", "1.1.0-rc.1",
	}
	for _, v := range valid {
		if _, err := parseSemver(v); err != nil {
			t.Errorf("parseSemver(%q) = %v, want valid", v, err)
		}
	}
	invalid := []string{
		"", "1", "1.0", "1.0.0.0", "01.0.0", "1.02.3", "1.0.03", "v1.0.0",
		"1.0.0-", "1.0.0-", "1.0.0-a..b", "1.0.0-01", "1.0.0-α",
		"1.0.0+", "=1.0.0", "1.0.x", "latest",
	}
	for _, v := range invalid {
		if _, err := parseSemver(v); err == nil {
			t.Errorf("parseSemver(%q) accepted, want invalid", v)
		}
	}
}

// TestSemverOrder: precedence per semver section 11 (build metadata ignored,
// numeric < alphanumeric, no-prerelease > prerelease, longer wins ties).
func TestSemverOrder(t *testing.T) {
	ordered := []string{
		"0.0.1", "0.1.0", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta",
		"1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0",
		"1.0.1", "1.1.0", "2.0.0", "10.0.0",
	}
	for i := 0; i+1 < len(ordered); i++ {
		a, b := ordered[i], ordered[i+1]
		if compareSemver(a, b) >= 0 {
			t.Errorf("compareSemver(%s, %s) = %d, want <0", a, b, compareSemver(a, b))
		}
		if compareSemver(b, a) <= 0 {
			t.Errorf("compareSemver(%s, %s) = %d, want >0", b, a, compareSemver(b, a))
		}
	}
	if compareSemver("1.0.0+build.1", "1.0.0+other") != 0 {
		t.Errorf("build metadata must not affect precedence")
	}
	if compareSemver("1.0.0", "1.0.0") != 0 {
		t.Errorf("equal versions must compare 0")
	}
}

// TestLatestVersion picks the semver-greatest key.
func TestLatestVersion(t *testing.T) {
	got := latestVersion(map[string]any{"1.10.0": nil, "1.9.0": nil, "1.2.0": nil})
	if got != "1.10.0" {
		t.Fatalf("latestVersion = %q, want 1.10.0 (numeric, not lexical)", got)
	}
	if latestVersion(map[string]any{}) != "" {
		t.Fatalf("latestVersion of empty map = %q, want \"\"", latestVersion(map[string]any{}))
	}
}

// TestParseRouteMatrix: every served npm address parses; everything else
// falls to the strict 404.
func TestParseRouteMatrix(t *testing.T) {
	for _, tc := range []struct {
		rel  string
		kind routeKind
		name string
	}{
		{"", routeRoot, ""},
		{"-/ping", routePing, ""},
		{"-/whoami", routeWhoami, ""},
		{"-/user/org.couchdb.user:admin", routeUserLogin, ""},
		{"-/user/org.couchdb.user:admin/-rev/2-x", routeUserLogin, ""},
		{"-/package/demo-pkg/dist-tags", routeDistTags, "demo-pkg"},
		{"-/package/demo-pkg/dist-tags/beta", routeDistTag, "demo-pkg"},
		{"-/package/@scope/pkg/dist-tags/beta", routeDistTag, "@scope/pkg"},
		{"demo-pkg", routePackument, "demo-pkg"},
		{"@scope/pkg", routePackument, "@scope/pkg"},
		{"demo-pkg/1.0.0", routeTagOrVersion, "demo-pkg"},
		{"demo-pkg/beta", routeTagOrVersion, "demo-pkg"},
		{"demo-pkg/-rev/3-x", routePackageRev, "demo-pkg"},
		{"@scope/pkg/-rev/3-x", routePackageRev, "@scope/pkg"},
		{"demo-pkg/-/demo-pkg-1.0.0.tgz", routeTarball, "demo-pkg"},
		{"@scope/pkg/-/@scope/pkg-1.0.0.tgz", routeTarball, "@scope/pkg"},
		{"demo-pkg/-/demo-pkg-1.0.0.tgz/-rev/4", routeTarballRev, "demo-pkg"},
	} {
		rt, ok := parseRoute(tc.rel)
		if !ok || rt.kind != tc.kind || rt.name != tc.name {
			t.Errorf("parseRoute(%q) = (%d,%q,%v), want (%d,%q)", tc.rel, rt.kind, rt.name, ok, tc.kind, tc.name)
		}
	}
	for _, rel := range []string{
		"-/", "-/v1/search", "-/npm/v1/security/audits", "-/package/x/y/z/dist-tags/t/u",
		"@scope", "@scope/pkg/extra/-rev/1", "a/b/-/c/d/e",
		"-/user/notcouch", "-/all",
	} {
		if _, ok := parseRoute(rel); ok {
			t.Errorf("parseRoute(%q) accepted, want strict 404", rel)
		}
	}
}
