package goproxy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// layoutReq builds a bare request whose URL carries the raw spelling —
// net/http sets URL.Path to the decoded form and keeps RawPath when they
// differ, exactly the shape httpapi's prefix-stripping hands the adapter.
func layoutReq(t *testing.T, raw string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, "http://host"+raw, nil)
}

// TestLayoutWireToStorage: the repoKey split plus the wire->storage case
// decoding (goproxy.md section 3.2 — storage keeps the decoded form).
func TestLayoutWireToStorage(t *testing.T) {
	cases := []struct {
		raw           string
		repo, storage string
		wantErr       bool
	}{
		{raw: "/go-local/example.com/mymod/@v/v1.0.2.zip", repo: "go-local", storage: "example.com/mymod/@v/v1.0.2.zip"},
		{raw: "/go-local/example.com/!my!mod/@v/v1.0.2.zip", repo: "go-local", storage: "example.com/MyMod/@v/v1.0.2.zip"},
		{raw: "/go-local/example.com/!my!mod/@v/v1.0.0-!b!e!t!a.info", repo: "go-local", storage: "example.com/MyMod/@v/v1.0.0-BETA.info"},
		{raw: "/go-local/example.com/mymod/@v/list", repo: "go-local", storage: "example.com/mymod/@v/list"},
		{raw: "/go-local/example.com/mymod/@latest", repo: "go-local", storage: "example.com/mymod/@latest"},
		{raw: "/go-local/", repo: "go-local", storage: ""},
		// The !! and trailing-! rejections die at Layout (400 family).
		{raw: "/go-local/example.com/!!mod/@v/v1.0.0.zip", wantErr: true},
		{raw: "/go-local/example.com/mod!/@v/v1.0.0.zip", wantErr: true},
		// Shared defenses: dot segments, double slashes, reserved keys,
		// control bytes, backslashes, trailing slash.
		{raw: "/go-local/../etc/@v/v1.0.0.zip", wantErr: true},
		{raw: "/go-local/example.com//x/@v/v1.0.0.zip", wantErr: true},
		{raw: "/api/example.com/@v/list", wantErr: true},
		{raw: "/go-local/example.com/%2e%2e/@v/v1.0.0.zip", wantErr: true},
		{raw: "/go-local/example.com/x/@v/", wantErr: true},
	}
	for _, c := range cases {
		h := &Handler{}
		repo, storage, err := h.Layout(layoutReq(t, c.raw))
		if c.wantErr {
			if err == nil {
				t.Errorf("Layout(%q) = (%q, %q), want error", c.raw, repo, storage)
			} else if !errors.Is(err, adapter.ErrBadRequestPath) {
				t.Errorf("Layout(%q) err = %v, want ErrBadRequestPath", c.raw, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Layout(%q): %v", c.raw, err)
			continue
		}
		if repo != c.repo || storage != c.storage {
			t.Errorf("Layout(%q) = (%q, %q), want (%q, %q)", c.raw, repo, storage, c.repo, c.storage)
		}
	}
}

// TestParseTarget: the routable shape recognition, including the sumdb
// family's deliberate non-routing (goproxy.md section 2 / section 9).
func TestParseTarget(t *testing.T) {
	cases := []struct {
		rel     string
		kind    targetKind
		module  string
		version string
		ext     string
	}{
		{rel: "example.com/m/@v/v1.0.0.zip", kind: kindFile, module: "example.com/m", version: "v1.0.0", ext: "zip"},
		{rel: "example.com/m/@v/v1.0.0.mod", kind: kindFile, module: "example.com/m", version: "v1.0.0", ext: "mod"},
		{rel: "example.com/m/@v/v1.0.0.info", kind: kindFile, module: "example.com/m", version: "v1.0.0", ext: "info"},
		{rel: "example.com/m/@v/list", kind: kindList, module: "example.com/m"},
		{rel: "example.com/m/@latest", kind: kindLatest, module: "example.com/m"},
	}
	for _, c := range cases {
		tg, ok := parseTarget(c.rel)
		if !ok {
			t.Fatalf("parseTarget(%q) not ok", c.rel)
		}
		if tg.kind != c.kind || tg.module != c.module || tg.version != c.version || tg.ext != c.ext {
			t.Errorf("parseTarget(%q) = %+v, want kind=%d module=%q version=%q ext=%q",
				c.rel, tg, c.kind, c.module, c.version, c.ext)
		}
	}
	for _, rel := range []string{
		"sumdb/sum.golang.org/supported",
		"sumdb/sum.golang.org/lookup/example.com/m@v1.0.0",
		"sumdb/sum.golang.org/tile/8/0/x123",
		"example.com/m",               // bare module path
		"example.com/m/@v",            // the @v directory itself
		"example.com/m/@v/v1.0.0.txt", // unknown extension
		"@v/list",                     // no module
		"/@latest",                    // empty module (defensive; Layout rejects earlier)
	} {
		if _, ok := parseTarget(rel); ok {
			t.Errorf("parseTarget(%q) ok, want not routable", rel)
		}
	}
}

// TestWirePathRendering: the storage->wire leg behind Location headers and
// the upstream hop.
func TestWirePathRendering(t *testing.T) {
	tg, ok := parseTarget("example.com/MyMod/@v/v1.0.0-Beta.zip")
	if !ok {
		t.Fatal("parseTarget failed")
	}
	if got, want := tg.wirePath(), "example.com/!my!mod/@v/v1.0.0-!beta.zip"; got != want {
		t.Errorf("wirePath = %q, want %q", got, want)
	}
}

// TestProviderUpstreamPath: the marker translations and the escape leg of
// the upstream hop (goproxy.md sections 3.1/3.2).
func TestProviderUpstreamPath(t *testing.T) {
	p := provider{}
	cases := []struct{ storage, want string }{
		{storage: "example.com/MyMod/@v/v1.0.0.zip", want: "example.com/!my!mod/@v/v1.0.0.zip"},
		{storage: "example.com/m/@v/.versionList", want: "example.com/m/@v/list"},
		{storage: "example.com/MyMod/@v/.versionList", want: "example.com/!my!mod/@v/list"},
		{storage: "example.com/MyMod@latest.latest", want: "example.com/!my!mod/@latest"},
		{storage: "some/other/path", want: "some/other/path"}, // identity on unknown shapes
	}
	for _, c := range cases {
		if got := p.UpstreamPath(c.storage); got != c.want {
			t.Errorf("UpstreamPath(%q) = %q, want %q", c.storage, got, c.want)
		}
	}
}

// TestProviderClassify: the expirable set versus immutable content
// (goproxy.md section 3.2/S4).
func TestProviderClassify(t *testing.T) {
	p := provider{}
	for _, path := range []string{
		"example.com/m/@v/v1.0.0.info",
		"example.com/m/@v/.versionList",
		"example.com/m@latest.latest",
	} {
		if got := p.Classify(path); got != "metadata" {
			t.Errorf("Classify(%q) = %q, want metadata", path, got)
		}
	}
	for _, path := range []string{
		"example.com/m/@v/v1.0.0.zip",
		"example.com/m/@v/v1.0.0.mod",
	} {
		if got := p.Classify(path); got != "content" {
			t.Errorf("Classify(%q) = %q, want content", path, got)
		}
	}
}

// TestProviderPackageName: the aggregation identity, markers included.
func TestProviderPackageName(t *testing.T) {
	p := provider{}
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{path: "example.com/m/@v/v1.0.0.zip", want: "example.com/m", ok: true},
		{path: "example.com/m/@v/list", want: "example.com/m", ok: true},
		{path: "example.com/m/@v/.versionList", want: "example.com/m", ok: true},
		{path: "example.com/m@latest.latest", want: "example.com/m", ok: true},
		{path: "unrelated", ok: false},
	}
	for _, c := range cases {
		got, ok := p.PackageName(c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("PackageName(%q) = (%q, %v), want (%q, %v)", c.path, got, ok, c.want, c.ok)
		}
	}
}
