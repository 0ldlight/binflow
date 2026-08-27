package conan

import (
	"net/http"
	"net/url"
	"testing"
)

// The wire-grammar legs: ref syntax (the `_` placeholder, case
// preservation, percent-encoded spellings), the v1/v2 route tables and
// the shared path defenses.

// TestParseRouteV2 pins the v2 grammar — every endpoint family parses,
// every look-alike falls to the family 404.
func TestParseRouteV2(t *testing.T) {
	rev := fixtureRev(1)
	cases := []struct {
		rel  string
		want routeKind
	}{
		{"v2/conans/search", kindV2Search},
		{"v2/conans/hello/1.0/myuser/stable", kindV2RecipeDelete},
		{"v2/conans/hello/1.0/myuser/stable/latest", kindV2Latest},
		{"v2/conans/hello/1.0/myuser/stable/revisions", kindV2Revisions},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev, kindV2RevDelete},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/files", kindV2Files},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/files/conanfile.py", kindV2Files},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/files/sub/dir/x.tgz", kindV2Files},
		{"v2/conans/hello/1.0/myuser/stable/search", kindV2RefSearch},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/search", kindV2RevSearch},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/packages", kindV2PackagesDelete},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/packages/" + fixturePID(2) + "/latest", kindV2PkgLatest},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/packages/" + fixturePID(2) + "/revisions", kindV2PkgRevisions},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/packages/" + fixturePID(2) + "/revisions/" + rev, kindV2PkgRevDelete},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/packages/" + fixturePID(2) + "/revisions/" + rev + "/files", kindV2PkgFiles},
		{"v2/conans/hello/1.0/myuser/stable/revisions/" + rev + "/packages/" + fixturePID(2) + "/revisions/" + rev + "/files/conaninfo.txt", kindV2PkgFiles},

		// The family-404 look-alikes.
		{"v2", kindNotFoundFamily},
		{"v2/other/hello/1.0/_/_/latest", kindNotFoundFamily},
		{"v2/conans", kindNotFoundFamily},
		{"v2/conans/hello/1.0", kindNotFoundFamily},                         // incomplete ref
		{"v2/conans/hello/1.0/_/_/revisions/../escape", kindNotFoundFamily}, // dot segments never pass the shared defense
		{"v2/conans/hello/1.0/_/_/latest/extra", kindNotFoundFamily},        // latest takes no tail
		{"v2/conans/hello/1.0/_/_/revisions/z!@/files", kindNotFoundFamily}, // non-hex revision
		{"v2/conans/hello/1.0/_/_/revisions/" + rev + "/packages/not!!/latest", kindNotFoundFamily},
		{"v2/conans/hello/1.0/_/_/revisions/" + rev + "/packages/" + fixturePID(2), kindNotFoundFamily}, // pid alone has no route
	}
	for _, tc := range cases {
		rt, ok := parseRoute(tc.rel)
		if !ok {
			t.Errorf("parseRoute(%q) ok = false, want true", tc.rel)
			continue
		}
		if rt.kind != tc.want {
			t.Errorf("parseRoute(%q).kind = %v, want %v", tc.rel, rt.kind, tc.want)
		}
	}
}

// TestParseRouteV1 pins the v1 grammar — the handshake trio, the full data
// family and the files channel with both `[0/]` spellings.
func TestParseRouteV1(t *testing.T) {
	pid := fixturePID(4)
	cases := []struct {
		rel  string
		want routeKind
	}{
		{"v1/ping", kindV1Ping},
		{"v1/users/authenticate", kindV1Authenticate},
		{"v1/users/check_credentials", kindV1CheckCredentials},
		{"v1/conans/search", kindV1Search},
		{"v1/conans/hello/1.0/myuser/stable", kindV1RecipeSnapshot},
		{"v1/conans/hello/1.0/myuser/stable/search", kindV1RefSearch},
		{"v1/conans/hello/1.0/myuser/stable/digest", kindV1Digest},
		{"v1/conans/hello/1.0/myuser/stable/download_urls", kindV1DownloadURLs},
		{"v1/conans/hello/1.0/myuser/stable/upload_urls", kindV1UploadURLs},
		{"v1/conans/hello/1.0/myuser/stable/remove_files", kindV1RemoveFiles},
		{"v1/conans/hello/1.0/myuser/stable/packages/delete", kindV1PackagesDelete},
		{"v1/conans/hello/1.0/myuser/stable/packages/" + pid, kindV1PkgSnapshot},
		{"v1/conans/hello/1.0/myuser/stable/packages/" + pid + "/digest", kindV1Digest},
		{"v1/conans/hello/1.0/myuser/stable/packages/" + pid + "/download_urls", kindV1DownloadURLs},
		{"v1/conans/hello/1.0/myuser/stable/packages/" + pid + "/upload_urls", kindV1UploadURLs},
		{"v1/conans/hello/1.0/myuser/stable/packages/" + pid + "/remove_files", kindV1RemoveFiles},

		// The files channel: with and without the default revision
		// segment, recipe and package arms.
		{"v1/files/myuser/hello/1.0/stable/export/conanfile.py", kindV1Files},
		{"v1/files/myuser/hello/1.0/stable/0/export/conanfile.py", kindV1Files},
		{"v1/files/myuser/hello/1.0/stable/0/package/" + pid + "/conan_package.tgz", kindV1Files},
		{"v1/files/_/hello/1.0/_/export/conanfile.py", kindV1Files},

		// The family-404 look-alikes.
		{"v1", kindNotFoundFamily},
		{"v1/other", kindNotFoundFamily},
		{"v1/users/other", kindNotFoundFamily},
		{"v1/conans/hello/1.0", kindNotFoundFamily},
		{"v1/conans/hello/1.0/myuser/stable/unknown", kindNotFoundFamily},
		{"v1/files/myuser/hello/1.0/stable", kindNotFoundFamily}, // no export|package arm
		{"v1/files/myuser/hello/1.0/stable/other/conanfile.py", kindNotFoundFamily},
		{"v1/files/myuser/hello/1.0/stable/5/export/conanfile.py", kindNotFoundFamily}, // non-default revision segment
	}
	for _, tc := range cases {
		rt, ok := parseRoute(tc.rel)
		if !ok {
			t.Errorf("parseRoute(%q) ok = false, want true", tc.rel)
			continue
		}
		if rt.kind != tc.want {
			t.Errorf("parseRoute(%q).kind = %v, want %v", tc.rel, rt.kind, tc.want)
		}
	}

	// The files channel's field mapping (coordinate order + the 0 path).
	rt, _ := parseRoute("v1/files/myuser/hello/1.0/stable/0/package/" + pid + "/conan_package.tgz")
	if rt.ref.user != "myuser" || rt.ref.name != "hello" || rt.ref.version != "1.0" || rt.ref.channel != "stable" {
		t.Errorf("files ref = %+v, want the coordinate order", rt.ref)
	}
	if rt.pid != pid || rt.path != "package/"+pid+"/conan_package.tgz" {
		t.Errorf("files fields = (pid %q, path %q), want the singular package storage literal", rt.pid, rt.path)
	}
}

// TestRefSyntax: the `_` placeholder is a literal legal segment; case is
// preserved; illegal characters refuse.
func TestRefSyntax(t *testing.T) {
	r, err := parseRef("Hello_World", "1.0.0-beta+1", "_", "_")
	if err != nil {
		t.Fatalf("parseRef: %v", err)
	}
	if r.wireRef() != "Hello_World/1.0.0-beta+1@_/_" {
		t.Errorf("wireRef = %q", r.wireRef())
	}
	if r.coordinateRoot() != "_/Hello_World/1.0.0-beta+1/_" {
		t.Errorf("coordinateRoot = %q", r.coordinateRoot())
	}
	for _, bad := range []string{"", "a b", "a/b", "á"} {
		if _, err := parseRef(bad, "1.0", "_", "_"); err == nil {
			t.Errorf("parseRef(%q) accepted an illegal name", bad)
		}
	}
	if !validRevision("0") || !validRevision(fixtureRev(1)) || !validRevision("abc123") {
		t.Error("validRevision rejected a legal spelling")
	}
	for _, bad := range []string{"", "xyz", "12 4", "a/b"} {
		if validRevision(bad) {
			t.Errorf("validRevision(%q) accepted an illegal spelling", bad)
		}
	}
}

// TestLayoutDefenses: percent-decoding (uppercase escapes decode to the
// case-preserved storage spelling), dot segments and spaces refuse.
func TestLayoutDefenses(t *testing.T) {
	req := &http.Request{URL: &url.URL{Path: "/cn-local/v2/conans/hello/1.0/myuser/stable/latest"}}
	key, rel, err := layout(req)
	if err != nil || key != "cn-local" || rel != "v2/conans/hello/1.0/myuser/stable/latest" {
		t.Fatalf("layout = (%q, %q, %v)", key, rel, err)
	}

	req = &http.Request{URL: &url.URL{
		Path:    "/cn-local/v2/conans/hello/1.0/myuser/stable/revisions/abc/files/Hello.py",
		RawPath: "/cn-local/v2/conans/hello/1.0/myuser/stable/revisions/abc/files/H%65llo.py",
	}}
	_, rel, err = layout(req)
	if err != nil || rel != "v2/conans/hello/1.0/myuser/stable/revisions/abc/files/Hello.py" {
		t.Fatalf("percent-decoded layout = (%q, %v), want the decoded spelling", rel, err)
	}

	for _, bad := range []string{
		"/cn-local/v2/conans/../escape",
		"/cn-local/v2/conans/he llo/1.0/_/_/latest",
		"/cn-local/v2/conans/hello/1.0/_/_/latest/",
		"/api/v2/conans/x",
	} {
		req := &http.Request{URL: &url.URL{Path: bad, RawPath: bad}}
		if _, _, err := layout(req); err == nil {
			t.Errorf("layout(%q) accepted an illegal path", bad)
		}
	}
}

// TestSearchRecipeNamedSearch: a recipe literally named "search" stays
// reachable — the lone-word endpoint test only fires on the exact single
// segment.
func TestSearchRecipeNamedSearch(t *testing.T) {
	rt, ok := parseRoute("v2/conans/search/1.0/_/_/latest")
	if !ok || rt.kind != kindV2Latest || rt.ref.name != "search" {
		t.Errorf("recipe named search = (%v, %+v)", rt.kind, rt.ref)
	}
	rt, ok = parseRoute("v2/conans/search")
	if !ok || rt.kind != kindV2Search {
		t.Errorf("the search endpoint = %v", rt.kind)
	}
}
