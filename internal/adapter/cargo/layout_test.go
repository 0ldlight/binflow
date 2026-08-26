package cargo

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// parseRoute table (spec section 2's endpoint table + the unknown-path
// family: owners, init, and every non-canonical index shape).
func TestParseRoute(t *testing.T) {
	cases := []struct {
		rel      string
		wantKind routeKind
		wantOK   bool
		name     string
		version  string
		pkgPath  string
	}{
		{rel: "", wantKind: kindRoot, wantOK: true},
		{rel: "index/config.json", wantKind: kindConfig, wantOK: true},
		{rel: "index/1/a", wantKind: kindIndexFile, wantOK: true, name: "a", pkgPath: "1/a"},
		{rel: "index/2/ab", wantKind: kindIndexFile, wantOK: true, name: "ab", pkgPath: "2/ab"},
		{rel: "index/3/m/myc", wantKind: kindIndexFile, wantOK: true, name: "myc", pkgPath: "3/m/myc"},
		{rel: "index/my/cr/mycrate", wantKind: kindIndexFile, wantOK: true, name: "mycrate", pkgPath: "my/cr/mycrate"},
		{rel: "v1/crates/mycrate/0.1.0/download", wantKind: kindDownload, wantOK: true, name: "mycrate", version: "0.1.0"},
		{rel: "api/v1/crates/new", wantKind: kindPublish, wantOK: true},
		{rel: "api/v1/crates", wantKind: kindSearch, wantOK: true},
		{rel: "api/v1/crates/mycrate/0.1.0/yank", wantKind: kindYank, wantOK: true, name: "mycrate", version: "0.1.0"},
		{rel: "api/v1/crates/mycrate/0.1.0/unyank", wantKind: kindUnyank, wantOK: true, name: "mycrate", version: "0.1.0"},
		{rel: "info/refs", wantKind: kindGitFace, wantOK: true},
		{rel: "git-upload-pack", wantKind: kindGitFace, wantOK: true},
		{rel: "crates/mycrate/mycrate-0.1.0.crate", wantKind: kindBareContent, wantOK: true},

		// The unknown-path family (spec sections 1/11.2): owners four
		// endpoints, init, and the malformed protocol shapes.
		{rel: "api/v1/crates/mycrate/owners", wantOK: false},
		{rel: "api/v1/crates/mycrate/owners/user/admin", wantOK: false},
		{rel: "api/v1/crates/init", wantOK: false},
		{rel: "index/", wantOK: false},
		{rel: "index/config.json/extra", wantOK: false},
		{rel: "index/1/ab", wantOK: false},           // 2-char name in the 1 tier
		{rel: "index/2/a", wantOK: false},            // 1-char name in the 2 tier
		{rel: "index/3/mm/myc", wantOK: false},       // 2-char first segment in the 3 tier
		{rel: "index/my/c/mycrate", wantOK: false},   // 1-char second segment in the 4+ tier
		{rel: "index/my/cr/mc", wantOK: false},       // 3-char name in the 4+ tier
		{rel: "index/my/cr/my crate", wantOK: false}, // illegal charset
		{rel: "index/1/1a", wantOK: false},           // digit-first name
		{rel: "v1/crates/mycrate/download", wantOK: false},
		{rel: "v1/crates/mycrate/0.1.0", wantOK: false},
		{rel: "v1/crates/mycrate/notsemver/download", wantOK: false},
		{rel: "api/v1/crates/mycrate/0.1.0/relist", wantOK: false},
		{rel: "api/v2/crates", wantOK: false},
	}
	for _, tc := range cases {
		rt, ok := parseRoute(tc.rel)
		if ok != tc.wantOK {
			t.Errorf("parseRoute(%q) ok = %v, want %v", tc.rel, ok, tc.wantOK)
			continue
		}
		if !tc.wantOK {
			continue
		}
		if rt.kind != tc.wantKind {
			t.Errorf("parseRoute(%q) kind = %d, want %d", tc.rel, rt.kind, tc.wantKind)
		}
		if tc.name != "" && rt.name != tc.name {
			t.Errorf("parseRoute(%q) name = %q, want %q", tc.rel, rt.name, tc.name)
		}
		if tc.version != "" && rt.version != tc.version {
			t.Errorf("parseRoute(%q) version = %q, want %q", tc.rel, rt.version, tc.version)
		}
		if tc.pkgPath != "" && rt.pkgPath != tc.pkgPath {
			t.Errorf("parseRoute(%q) pkgPath = %q, want %q", tc.rel, rt.pkgPath, tc.pkgPath)
		}
	}
}

// indexPath table (spec section 3.2's four tiers; the file name is the
// LOWERCASED name, the original case survives in the index lines).
func TestIndexPath(t *testing.T) {
	cases := []struct{ name, want string }{
		{"a", "1/a"},
		{"Z", "1/z"},
		{"ab", "2/ab"},
		{"Ab", "2/ab"},
		{"myc", "3/m/myc"},
		{"MyC", "3/m/myc"},
		{"mycrate", "my/cr/mycrate"},
		{"MyCrate", "my/cr/mycrate"},
		{"a-b_c9", "a-/b_/a-b_c9"},
	}
	for _, tc := range cases {
		if got := indexPath(tc.name); got != tc.want {
			t.Errorf("indexPath(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// validCrateName table (the adopted crates.io restriction set).
func TestValidCrateName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"a", true},
		{"serde", true},
		{"MyCrate", true},
		{"a-b", true},
		{"a_b", true},
		{"a1", true},
		{"", false},
		{"1a", false},                    // must open with a letter
		{"-a", false},                    // must open with a letter
		{"a b", false},                   // space
		{"a.b", false},                   // dot
		{"a/b", false},                   // slash
		{"über", false},                  // non-ASCII
		{strings.Repeat("a", 64), true},  // at the ceiling
		{strings.Repeat("a", 65), false}, // oversize
	}
	for _, tc := range cases {
		if got := validCrateName(tc.name); got != tc.want {
			t.Errorf("validCrateName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// layout rejects the malformed request shapes with the shared 400 family.
func TestLayoutRejects(t *testing.T) {
	cases := []string{
		"/cargo-local/crates/../index/1/a", // dot segment
		"/cargo-local//index/config.json",  // empty segment
	}
	for _, raw := range cases {
		r := &http.Request{URL: &url.URL{Path: raw}}
		if _, _, err := layout(r); err == nil {
			t.Errorf("layout(%q) = nil error, want rejection", raw)
		}
	}
}
