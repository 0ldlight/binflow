package conan

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The search legs: the pattern query (both planes), the per-ref packageId
// metadata assembly and the conaninfo parser.

// TestSearchPatterns: the `*` wildcard over name/version@user/channel,
// case-insensitive, empty matches all.
func TestSearchPatterns(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	for _, r := range []ref{
		{name: "Hello", version: "1.0", user: "myuser", channel: "stable"},
		{name: "hello-extra", version: "2.0", user: "_", channel: "_"},
		{name: "other", version: "1.0", user: "u", channel: "c"},
	} {
		s.putRecipeFile("cn-local", r, fixtureRev(1), "conanfile.py", []byte("x"))
	}

	cases := []struct {
		pattern string
		want    []string
	}{
		{"hello*", []string{"Hello/1.0@myuser/stable", "hello-extra/2.0@_/_"}},
		{"*extra*", []string{"hello-extra/2.0@_/_"}},
		{"hello/1.0@myuser/stable", []string{"Hello/1.0@myuser/stable"}},
		{"hello/1.0@myuser/stable/*", []string{"Hello/1.0@myuser/stable"}}, // the revision wildcard (L-c3)
		{"HELLO/1.0@*", []string{"Hello/1.0@myuser/stable"}},
		{"other/*", []string{"other/1.0@u/c"}},
		{"zzz*", nil},
		{"", []string{"Hello/1.0@myuser/stable", "hello-extra/2.0@_/_", "other/1.0@u/c"}},
	}
	for _, tc := range cases {
		code, body, _ := s.get(v2("cn-local", "search?q="+tc.pattern))
		if code != http.StatusOK {
			t.Errorf("search %q = %d (body %s)", tc.pattern, code, body)
			continue
		}
		var resp searchResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Errorf("search %q body %q: %v", tc.pattern, body, err)
			continue
		}
		if len(resp.Results) != len(tc.want) {
			t.Errorf("search %q = %v, want %v", tc.pattern, resp.Results, tc.want)
			continue
		}
		for i := range tc.want {
			if resp.Results[i] != tc.want[i] {
				t.Errorf("search %q[%d] = %q, want %q", tc.pattern, i, resp.Results[i], tc.want[i])
			}
		}
	}

	// An empty result set renders as [] not null (the client's iteration).
	code, body, _ := s.get(v2("cn-local", "search?q=zzz*"))
	if code != http.StatusOK || !strings.Contains(body, `"results":[]`) {
		t.Errorf("empty search = (%d, %s), want an empty array", code, body)
	}
}

// TestRefSearchMetadata: <ref>/search and its rrev-limited twin assemble
// every pid's row from the newest conaninfo.txt, with recipe_hash = the
// recipe revision.
func TestRefSearchMetadata(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	rev, pidA, pidB := fixtureRev(2), fixturePID(1), fixturePID(6)
	s.putRecipeFile("cn-local", r, rev, "conanfile.py", []byte("x"))
	s.putPkgFile("cn-local", r, rev, pidA, fixtureRev(4), "conaninfo.txt",
		[]byte(conaninfoFixture([]string{"os=Macos", "arch=x86_64"}, []string{"shared=True"}, []string{"zlib/1.2.11#rev1", "fmt/9.1.0"})))
	s.putPkgFile("cn-local", r, rev, pidA, fixtureRev(4), "conan_package.tgz", []byte("tgz"))
	s.putPkgFile("cn-local", r, rev, pidB, fixtureRev(8), "conan_package.tgz", []byte("tgz2")) // no conaninfo

	for _, path := range []string{
		v2("cn-local", "hello/1.0/myuser/stable/search"),
		v2("cn-local", "hello/1.0/myuser/stable/revisions/"+rev+"/search"),
		v1("cn-local", "conans/hello/1.0/myuser/stable/search"),
	} {
		code, body, _ := s.get(path)
		if code != http.StatusOK {
			t.Errorf("%s = %d (body %s)", path, code, body)
			continue
		}
		var out map[string]*pkgMeta
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Errorf("%s body %q: %v", path, body, err)
			continue
		}
		if len(out) != 2 {
			t.Errorf("%s = %d pids, want 2", path, len(out))
			continue
		}
		a := out[pidA]
		if a == nil || a.Settings["os"] != "Macos" || a.Settings["arch"] != "x86_64" {
			t.Errorf("%s pidA settings = %+v", path, a)
			continue
		}
		if a.Options["shared"] != "True" {
			t.Errorf("%s pidA options = %+v", path, a.Options)
		}
		if _, ok := a.Requires["zlib/1.2.11#rev1"]; !ok {
			t.Errorf("%s pidA requires = %+v", path, a.Requires)
		}
		if a.RecipeHash != rev {
			t.Errorf("%s pidA recipe_hash = %q, want the rrev %q", path, a.RecipeHash, rev)
		}
		b := out[pidB]
		if b == nil || b.Settings == nil || b.RecipeHash != rev {
			t.Errorf("%s pidB = %+v, want empty maps + the rrev", path, b)
		}
	}

	// An unknown ref answers the plain 404.
	code, _, _ := s.get(v2("cn-local", "nope/1.0/_/_/search"))
	if code != http.StatusNotFound {
		t.Errorf("unknown ref search = %d, want 404", code)
	}
}

// TestParseConaninfo: the section parser's own matrix.
func TestParseConaninfo(t *testing.T) {
	meta := &pkgMeta{}
	parseConaninfo(`[settings]
    arch=x86_64
    build_type=Release
# a comment

[requires]
    zlib/1.2.11#abc
    fmt/9.1.0

[options]
    shared=True
    *:fPIC=True

[recipe_hash]
whatever=this-is-ignored
`, meta)
	if meta.Settings["arch"] != "x86_64" || meta.Settings["build_type"] != "Release" {
		t.Errorf("settings = %v", meta.Settings)
	}
	if len(meta.Requires) != 2 {
		t.Errorf("requires = %v", meta.Requires)
	}
	if meta.Options["shared"] != "True" || meta.Options["*:fPIC"] != "True" {
		t.Errorf("options = %v", meta.Options)
	}
}

// TestSearchRepoScanBounds: search only surfaces coordinates (index.json
// roots), not stray files.
func TestSearchRepoScanBounds(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "only", version: "1.0", user: "u", channel: "c"}
	s.putRecipeFile("cn-local", r, fixtureRev(1), "conanfile.py", []byte("x"))

	// A stray node outside any coordinate.
	if _, err := s.svc.Put(context.Background(), adminPrincipal(), "cn-local",
		"stray.txt", strings.NewReader("x"), blobRefOf([]byte("x")), "text/plain"); err != nil {
		t.Fatalf("stray put: %v", err)
	}
	// A deeper index (a package index) must not surface as its own ref.
	_, _, pid := seedV2Fixture(t, s, "cn-local2")
	_ = pid

	code, body, _ := s.get(v2("cn-local", "search?q=*"))
	if code != http.StatusOK || !strings.Contains(body, `"only/1.0@u/c"`) || strings.Contains(body, "stray") {
		t.Errorf("search = (%d, %s), want only the coordinate roots", code, body)
	}
}
