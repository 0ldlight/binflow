package pypi

// L017-1 D1: the simple index renders data-requires-python on package-page
// anchors, derived from each file's OWN metadata — the wheel dist-info/
// METADATA source and the sdist PKG-INFO source (the twine form-field
// source carries the same value by construction: twine 7 reads it from the
// file's metadata, l016-wire/client/twine-raw.req). The value renders
// HTML-escaped exactly as the reference spells it
// (data-requires-python="&gt;=3.8", L016 wire), and rides the PEP 691 JSON
// face under its spec field name requires-python.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestRequiresPythonWheelAnchor: a real wheel whose METADATA carries
// Requires-Python: >=3.8 renders the escaped attribute on its anchor,
// between href and the anchor text; a metadata file without the header
// renders no attribute.
func TestRequiresPythonWheelAnchor(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0-py3-none-any.whl",
		testWheelBytes(t, "demo_pkg-1.0.0", ">=3.8", true))
	s.uploadOK(t, "demo-pkg", "1.1.0", "demo_pkg-1.1.0-py3-none-any.whl",
		testWheelBytes(t, "demo_pkg-1.1.0", "", true))

	status, body, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	want := `<a href="../../packages/demo-pkg/1.0.0/demo_pkg-1.0.0-py3-none-any.whl#sha256=` +
		sha256Hex(testWheelBytes(t, "demo_pkg-1.0.0", ">=3.8", true)) +
		`" data-requires-python="&gt;=3.8">demo_pkg-1.0.0-py3-none-any.whl</a>`
	if !strings.Contains(body, want) {
		t.Fatalf("page lacks the wheel anchor with the escaped attribute:\nwant %s\npage:\n%s", want, body)
	}
	if strings.Contains(body, `data-requires-python=""`) || strings.Count(body, "data-requires-python") != 1 {
		t.Fatalf("the 1.1.0 anchor (metadata without the header) must carry no attribute:\n%s", body)
	}
}

// TestRequiresPythonSdistAnchor: the sdist PKG-INFO source — an upload
// whose form carries NO requires_python field (the curl/y1 arm shape)
// still renders the attribute, derived server-side from the file.
func TestRequiresPythonSdistAnchor(t *testing.T) {
	s := newStack(t)
	sdist := testSdistBytes(t, "demo_pkg-1.0.0", "demo_pkg", ">=3.9", true)
	if status, body := s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action": "file_upload", "name": "demo-pkg", "version": "1.0.0",
		"filetype": "sdist", "protocol_version": "1",
	}, "demo_pkg-1.0.0.tar.gz", sdist); status != http.StatusOK {
		t.Fatalf("sdist upload = %d, body %s", status, body)
	}

	_, body, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	if !strings.Contains(body, `data-requires-python="&gt;=3.9"`) {
		t.Fatalf("sdist anchor lacks the PKG-INFO-derived attribute:\n%s", body)
	}
}

// TestRequiresPythonEscaped: a specifier full of markup bytes renders
// fully HTML-escaped — the attribute is an injection surface like the
// filename.
func TestRequiresPythonEscaped(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0-py3-none-any.whl",
		testWheelBytes(t, "demo_pkg-1.0.0", `<>&"'3.8`, true))

	_, body, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	want := `data-requires-python="&lt;&gt;&amp;&quot;&#39;3.8"`
	if !strings.Contains(body, want) {
		t.Fatalf("attribute not fully escaped (want %s):\n%s", want, body)
	}
	for _, raw := range []string{`"<>&"'3.8"`} {
		if strings.Contains(body, raw) {
			t.Fatalf("raw specifier bytes leaked into the page:\n%s", body)
		}
	}
}

// TestRequiresPythonJSONFace: the PEP 691 JSON form carries the same
// derived value under requires-python, omitted when the metadata has none.
func TestRequiresPythonJSONFace(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0-py3-none-any.whl",
		testWheelBytes(t, "demo_pkg-1.0.0", ">=3.8", true))
	s.uploadOK(t, "demo-pkg", "1.1.0", "demo_pkg-1.1.0-py3-none-any.whl",
		testWheelBytes(t, "demo_pkg-1.1.0", "", true))

	resp := s.do(http.MethodGet, "/binflow/api/pypi/pypi-local/simple/demo-pkg/", "", "", nil,
		map[string]string{"Accept": simpleJSONMediaType})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var doc struct {
		Files []struct {
			Filename       string `json:"filename"`
			RequiresPython string `json:"requires-python"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode JSON simple: %v", err)
	}
	byName := map[string]string{}
	for _, f := range doc.Files {
		byName[f.Filename] = f.RequiresPython
	}
	if byName["demo_pkg-1.0.0-py3-none-any.whl"] != ">=3.8" {
		t.Fatalf("1.0.0 requires-python = %q, want >=3.8", byName["demo_pkg-1.0.0-py3-none-any.whl"])
	}
	if byName["demo_pkg-1.1.0-py3-none-any.whl"] != "" {
		t.Fatalf("1.1.0 (metadata without the header) must omit the field, got %q",
			byName["demo_pkg-1.1.0-py3-none-any.whl"])
	}
}

// TestRequiresPythonVirtualMember: a virtual repository's merged page
// renders the local member's derived attribute (the same enrichment the
// member's own page runs).
func TestRequiresPythonVirtualMember(t *testing.T) {
	s := newStackCfg(t, nil)
	ctx := context.Background()
	if _, err := s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "pyv-loc", Type: repo.TypeLocal, PackageType: repo.PackagePypi,
	}); err != nil {
		t.Fatalf("seed local member: %v", err)
	}
	if _, err := s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "pyv-virt", Type: repo.TypeVirtual, PackageType: repo.PackagePypi,
		Config: `{"repositories":["pyv-loc"]}`,
	}); err != nil {
		t.Fatalf("seed virtual: %v", err)
	}
	// Seed the member through its own upload face (uploadOK targets the
	// default pypi-local repo, absent in this stack).
	if status, body := s.upload("/binflow/api/pypi/pyv-loc", map[string]string{
		":action": "file_upload", "name": "demo-pkg", "version": "1.0.0",
		"filetype": "bdist_wheel", "protocol_version": "1",
	}, "demo_pkg-1.0.0-py3-none-any.whl", testWheelBytes(t, "demo_pkg-1.0.0", ">=3.8", true)); status != http.StatusOK {
		t.Fatalf("member upload = %d, body %s", status, body)
	}
	status, body, _ := s.get("/binflow/api/pypi/pyv-virt/simple/demo-pkg/")
	if status != http.StatusOK {
		t.Fatalf("virtual page = %d, body %s", status, body)
	}
	if !strings.Contains(body, `data-requires-python="&gt;=3.8"`) {
		t.Fatalf("virtual page lacks the member's derived attribute:\n%s", body)
	}
}
