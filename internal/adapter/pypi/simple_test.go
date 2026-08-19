package pypi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// sha256Hex is the digest helper the hash-reconciliation assertions share.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestSimpleNormalizationMatrix is PE-01's core lookup rule: PEP 503
// normalization (lower + runs of [-_.] collapsing to one -) makes every
// spelling of a project name address ONE index page — the AC trio plus the
// dot-run variants.
func TestSimpleNormalizationMatrix(t *testing.T) {
	s := newStack(t)
	content := []byte("demo bytes")
	s.uploadOK(t, "Demo_Pkg", "1.0.0", "Demo_Pkg-1.0.0.tar.gz", content)

	etags := map[string]string{}
	for _, spelling := range []string{"Demo_Pkg", "demo_pkg", "demo-pkg", "Demo.Pkg", "demo__pkg", "DEMO__PKG....1"} {
		if spelling == "DEMO__PKG....1" {
			// A different project ("demo-pkg-1") must NOT resolve to the
			// demo-pkg page.
			status, _, _ := s.get("/binflow/api/pypi/pypi-local/simple/" + spelling + "/")
			if status != http.StatusNotFound {
				t.Fatalf("%s: status = %d, want 404 (a different normalized name)", spelling, status)
			}
			continue
		}
		status, body, hdr := s.get("/binflow/api/pypi/pypi-local/simple/" + spelling + "/")
		if status != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 (body %s)", spelling, status, body)
		}
		etags[spelling] = hdr.Get("ETag")
		if !strings.Contains(body, "Demo_Pkg-1.0.0.tar.gz") {
			t.Fatalf("%s: page lacks the stored file entry:\n%s", spelling, body)
		}
	}
	// All spellings of one project must render byte-identical pages (the
	// ETag is the sha256 of the body — equality proves it).
	for spelling, etag := range etags {
		if etag != etags["demo-pkg"] {
			t.Fatalf("%s: ETag %q differs from the canonical %q", spelling, etag, etags["demo-pkg"])
		}
	}
}

// TestSimplePageShape pins the PE-01 page contract: the fixed PEP 629 head
// (api-version=2), one anchor per file sorted by filename, sha256-only
// fragments on ../../packages/ hrefs, no private rel attributes, and no
// md5 fragments anywhere.
func TestSimplePageShape(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "zz_last-1.0.0.tar.gz", []byte("zz"))
	s.uploadOK(t, "demo-pkg", "1.0.0", "aa_first-1.0.0-py3-none-any.whl", []byte("aa"))

	status, body, hdr := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if ct := hdr.Get("Content-Type"); ct != simpleHTMLMediaType {
		t.Fatalf("Content-Type = %q, want %q", ct, simpleHTMLMediaType)
	}

	// Fixed head, exactly once.
	if !strings.HasPrefix(body, indexHead) {
		t.Fatalf("page does not start with the fixed head:\n%s", body)
	}
	if got := strings.Count(body, `name="api-version" value="2"`); got != 1 {
		t.Fatalf("api-version meta appears %d times, want 1", got)
	}
	if strings.Contains(body, "#md5=") {
		t.Fatalf("page carries an md5 fragment (sha256-only ruling):\n%s", body)
	}
	if strings.Contains(body, "rel=") {
		t.Fatalf("page carries a rel attribute (Artifactory private form, deliberately omitted):\n%s", body)
	}
	if !strings.HasSuffix(body, indexFoot) {
		t.Fatalf("page does not end with the closing foot:\n%s", body)
	}

	// Entries sorted by filename: aa_first before zz_last.
	ai := strings.Index(body, "aa_first-1.0.0-py3-none-any.whl")
	zi := strings.Index(body, "zz_last-1.0.0.tar.gz")
	if ai < 0 || zi < 0 || ai > zi {
		t.Fatalf("entries not sorted by filename:\n%s", body)
	}

	// The href form: ../../packages/<stored path>#sha256=<hex>.
	want := `href="../../packages/demo-pkg/1.0.0/aa_first-1.0.0-py3-none-any.whl#sha256=` + sha256Hex([]byte("aa"))
	if !strings.Contains(body, want) {
		t.Fatalf("page lacks the exact href form %q:\n%s", want, body)
	}
}

// TestSimpleHTMLEscaping pins PE-01's escaping rule: a stored filename with
// markup bytes must never inject into the page.
func TestSimpleHTMLEscaping(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", `a&b<c>"d-1.0.0.whl`, []byte("esc"))

	_, body, _ := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	for _, raw := range []string{"<c>", `>"d`} {
		if strings.Contains(body, raw) {
			t.Fatalf("page carries the raw filename bytes %q (must be escaped):\n%s", raw, body)
		}
	}
	if !strings.Contains(body, "a&amp;b&lt;c&gt;&quot;d-1.0.0.whl") {
		t.Fatalf("page lacks the escaped filename:\n%s", body)
	}
	// And the escaped href still downloads (the escaped bytes decode back
	// to the real path when the client resolves them).
	status, _, _ := s.get("/binflow/api/pypi/pypi-local/packages/demo-pkg/1.0.0/a%26b%3Cc%3E%22d-1.0.0.whl")
	if status != http.StatusOK {
		t.Fatalf("escaped-filename download status = %d, want 200", status)
	}
}

// TestSimpleRedirect pins PE-01's trailing-slash rule: the no-slash
// spelling answers 302 with a RELATIVE Location that appends the slash (a
// relative target resolves correctly under both the api mount and the bare
// content entrance — the seam rewrote the URL before this handler ran).
func TestSimpleRedirect(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("x"))

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"project without slash", "/binflow/api/pypi/pypi-local/simple/demo-pkg", "demo-pkg/"},
		{"bare mount without slash", "/binflow/pypi-local/simple/demo-pkg", "demo-pkg/"},
		{"simple root without slash", "/binflow/api/pypi/pypi-local/simple", "simple/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.doRaw(http.MethodGet, tc.path, "", "", nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusFound {
				t.Fatalf("status = %d, want 302", resp.StatusCode)
			}
			if got := resp.Header.Get("Location"); got != tc.want {
				t.Fatalf("Location = %q, want the relative %q", got, tc.want)
			}
		})
	}

	// Following the redirect lands on the page (the Go client resolves the
	// relative target against the original URL).
	client := s.srv.Client()
	resp, err := client.Get(s.srv.URL + "/binflow/api/pypi/pypi-local/simple/demo-pkg")
	if err != nil {
		t.Fatalf("follow redirect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status after redirect = %d, want 200 (final URL %s)", resp.StatusCode, resp.Request.URL)
	}
	if got := resp.Request.URL.Path; got != "/binflow/api/pypi/pypi-local/simple/demo-pkg/" {
		t.Fatalf("final path = %q, want the api-mount spelling with the slash", got)
	}
}

// TestSimpleETag304 pins the M31 ETag detail: a stable opaque hash for the
// same content, a different page after a new file lands, and the
// If-None-Match 304 short-circuit (weak spellings included).
func TestSimpleETag304(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("v1"))

	_, _, hdr := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	etag := hdr.Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) {
		t.Fatalf("ETag = %q, want a quoted stable hash", etag)
	}

	// Same content -> same ETag.
	_, _, hdr = s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	if hdr.Get("ETag") != etag {
		t.Fatalf("ETag drifted between identical requests: %q vs %q", hdr.Get("ETag"), etag)
	}

	// 304 on exact, weak and star spellings.
	for _, spelling := range []string{etag, `W/` + etag, `*`} {
		resp := s.do(http.MethodGet, "/binflow/api/pypi/pypi-local/simple/demo-pkg/",
			"", "", nil, map[string]string{"If-None-Match": spelling})
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotModified {
			t.Fatalf("If-None-Match %q: status = %d, want 304", spelling, resp.StatusCode)
		}
	}

	// A non-matching tag serves 200.
	resp := s.do(http.MethodGet, "/binflow/api/pypi/pypi-local/simple/demo-pkg/",
		"", "", nil, map[string]string{"If-None-Match": `"deadbeef"`})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("non-matching If-None-Match: status = %d, want 200", resp.StatusCode)
	}

	// New file -> new page -> new ETag; the old tag no longer matches.
	s.uploadOK(t, "demo-pkg", "1.1.0", "demo_pkg-1.1.0.tar.gz", []byte("v2"))
	resp = s.do(http.MethodGet, "/binflow/api/pypi/pypi-local/simple/demo-pkg/",
		"", "", nil, map[string]string{"If-None-Match": etag})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stale If-None-Match after a new file: status = %d, want 200", resp.StatusCode)
	}
}

// TestSimpleUnknownProject404 pins the unknown-name branch.
func TestSimpleUnknownProject404(t *testing.T) {
	s := newStack(t)
	status, body, _ := s.get("/binflow/api/pypi/pypi-local/simple/no-such-project/")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if !strings.Contains(body, "no-such-project") {
		t.Fatalf("body %q does not name the missing project", body)
	}
}

// TestSimpleReservedVersionPath pins the reserved-endpoint branch:
// /simple/<name>/<version> (and deeper, with or without a trailing slash)
// is always 404 — an endpoint with no implementation semantics
// (maven-npm-pypi.md section 3.1).
func TestSimpleReservedVersionPath(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("x"))
	for _, path := range []string{
		"/binflow/api/pypi/pypi-local/simple/demo-pkg/1.0.0",
		"/binflow/api/pypi/pypi-local/simple/demo-pkg/1.0.0/",
		"/binflow/api/pypi/pypi-local/simple/demo-pkg/1.0.0/json",
	} {
		status, _, _ := s.get(path)
		if status != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404 (reserved endpoint)", path, status)
		}
	}
}

// TestSimpleRootIndex pins the P2 repository-level face: the normalized
// project names, sorted, sharing the fixed head.
func TestSimpleRootIndex(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "Beta_Lib", "1.0.0", "Beta_Lib-1.0.0.tar.gz", []byte("b"))
	s.uploadOK(t, "alpha-pkg", "1.0.0", "alpha_pkg-1.0.0.tar.gz", []byte("a"))

	status, body, _ := s.get("/binflow/api/pypi/pypi-local/simple/")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.HasPrefix(body, indexHead) {
		t.Fatalf("root index lacks the fixed head:\n%s", body)
	}
	ai := strings.Index(body, `<a href="alpha-pkg/">alpha-pkg</a>`)
	bi := strings.Index(body, `<a href="beta-lib/">beta-lib</a>`)
	if ai < 0 || bi < 0 || ai > bi {
		t.Fatalf("root index entries wrong or unsorted:\n%s", body)
	}
}

// TestSimpleJSONNegotiation pins the P2 PEP 691 face: an explicit Accept of
// the JSON media type yields the JSON document (equivalent to the HTML
// page), every other Accept stays HTML (the Artifactory default-off
// posture BinFlow keeps).
func TestSimpleJSONNegotiation(t *testing.T) {
	s := newStack(t)
	content := []byte("wheel-bytes")
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0-py3-none-any.whl", content)

	resp := s.do(http.MethodGet, "/binflow/api/pypi/pypi-local/simple/demo-pkg/", "", "", nil,
		map[string]string{"Accept": simpleJSONMediaType})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != simpleJSONCT {
		t.Fatalf("Content-Type = %q, want %q", ct, simpleJSONCT)
	}
	var doc struct {
		Meta  map[string]string `json:"meta"`
		Name  string            `json:"name"`
		Files []struct {
			Filename string            `json:"filename"`
			URL      string            `json:"url"`
			Hashes   map[string]string `json:"hashes"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decoding the JSON simple index: %v", err)
	}
	if doc.Meta["api-version"] != "2.0" || doc.Name != "demo-pkg" {
		t.Fatalf("meta/name = %v/%q, want api-version 2.0 and the normalized name", doc.Meta, doc.Name)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(doc.Files))
	}
	f := doc.Files[0]
	if f.Filename != "demo_pkg-1.0.0-py3-none-any.whl" {
		t.Fatalf("filename = %q", f.Filename)
	}
	if f.Hashes["sha256"] != sha256Hex(content) {
		t.Fatalf("sha256 = %q, want the content digest", f.Hashes["sha256"])
	}
	if !strings.HasPrefix(f.URL, "../../packages/demo-pkg/1.0.0/") || !strings.HasSuffix(f.URL, "#sha256="+sha256Hex(content)) {
		t.Fatalf("url = %q, want the packages/ href with the sha256 fragment", f.URL)
	}

	// Default and plain-HTML Accepts stay HTML.
	for _, accept := range []string{"", "*/*", "text/html"} {
		t.Run("accept "+accept, func(t *testing.T) {
			hdrs := map[string]string{}
			if accept != "" {
				hdrs["Accept"] = accept
			}
			resp := s.do(http.MethodGet, "/binflow/api/pypi/pypi-local/simple/demo-pkg/", "", "", nil, hdrs)
			defer func() { _ = resp.Body.Close() }()
			if ct := resp.Header.Get("Content-Type"); ct != simpleHTMLMediaType {
				t.Fatalf("Content-Type = %q, want HTML", ct)
			}
			var sb strings.Builder
			_, _ = sb.WriteString(readAll(t, resp))
			if !strings.HasPrefix(sb.String(), "<!DOCTYPE html>") {
				t.Fatalf("body is not the HTML form")
			}
		})
	}
}

// readAll drains a response body for assertions.
func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// TestSimpleSpellingPreservesClientName proves the 302 keeps the client's
// spelling (no silent normalization in the redirect target).
func TestSimpleSpellingPreservesClientName(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "Demo_Pkg", "1.0.0", "Demo_Pkg-1.0.0.tar.gz", []byte("x"))
	resp := s.doRaw(http.MethodGet, "/binflow/api/pypi/pypi-local/simple/Demo_Pkg", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "Demo_Pkg/" {
		t.Fatalf("Location = %q, want the client spelling %q", got, "Demo_Pkg/")
	}
}

// TestSimpleHeadNoBody pins HEAD parity on the index page.
func TestSimpleHeadNoBody(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("x"))
	resp := s.do(http.MethodHead, "/binflow/api/pypi/pypi-local/simple/demo-pkg/", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ContentLength <= 0 {
		t.Fatalf("Content-Length = %d, want the page length", resp.ContentLength)
	}
}

// TestSimpleIndexEtagQuoted keeps the ETag's quoted spelling stable across
// GET and HEAD (pip treats it as opaque, BinFlow keeps the HTTP form).
func TestSimpleIndexEtagQuoted(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("x"))
	_, _, hdr := s.get("/binflow/api/pypi/pypi-local/simple/demo-pkg/")
	resp := s.do(http.MethodHead, "/binflow/api/pypi/pypi-local/simple/demo-pkg/", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("ETag") != hdr.Get("ETag") {
		t.Fatalf("HEAD ETag %q != GET ETag %q", resp.Header.Get("ETag"), hdr.Get("ETag"))
	}
}

// TestSimpleVaryAccept pins the negotiation-correctness header (T-70
// review N2): the project page serves two Accept-chosen representations on
// one URL, so EVERY response — HTML, JSON and the 304 short-circuit —
// must declare Vary: Accept or an intermediary cache could cross-serve
// the forms.
func TestSimpleVaryAccept(t *testing.T) {
	s := newStack(t)
	s.uploadOK(t, "demo-pkg", "1.0.0", "demo_pkg-1.0.0.tar.gz", []byte("x"))
	page := "/binflow/api/pypi/pypi-local/simple/demo-pkg/"

	_, _, hdr := s.get(page)
	if got := hdr.Get("Vary"); got != "Accept" {
		t.Fatalf("HTML form Vary = %q, want Accept", got)
	}

	resp := s.do(http.MethodGet, page, "", "", nil, map[string]string{"Accept": simpleJSONMediaType})
	if got := resp.Header.Get("Vary"); got != "Accept" {
		t.Fatalf("JSON form Vary = %q, want Accept", got)
	}
	etag := resp.Header.Get("ETag")
	_ = resp.Body.Close()

	resp = s.do(http.MethodGet, page, "", "", nil,
		map[string]string{"If-None-Match": etag, "Accept": simpleJSONMediaType})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("304 status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Vary"); got != "Accept" {
		t.Fatalf("304 Vary = %q, want Accept (the short-circuit keeps the variance declaration)", got)
	}
}
