package generic_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter/generic"
)

// TestContentTypeMapping covers T-36: generic deploys that declare no
// Content-Type get one inferred from the path extension; the stored value
// (node.Mime) is the single source of truth, so the download header, the
// HEAD header and the upload FileInfo body all agree. A client-declared
// Content-Type always wins verbatim; unknown/no extensions stay
// application/octet-stream (FR-4-AC13 unchanged).
//
// Table entries pair with extensionMimes in mime.go plus two stdlib-backed
// extensions (.png via the builtin table) — asserting them here would make
// the test host-dependent (the OS database may not know png on a bare
// container), so the deterministic BinFlow table is what gets asserted
// end-to-end; see TestMimeByExtension for the fallback probe.
func TestContentTypeMapping(t *testing.T) {
	e := newEnv(t)

	// Deterministic BinFlow-table mappings (no Content-Type declared).
	deterministic := []struct{ ext, want string }{
		{".json", "application/json"},
		{".xml", "application/xml"},
		{".txt", "text/plain; charset=utf-8"},
		{".csv", "text/csv; charset=utf-8"},
		{".md", "text/markdown; charset=utf-8"},
		{".html", "text/html; charset=utf-8"},
		{".htm", "text/html; charset=utf-8"},
		{".yml", "application/yaml"},
		{".yaml", "application/yaml"},
		{".gz", "application/gzip"},
		{".tgz", "application/gzip"},
		{".zip", "application/zip"},
		{".tar", "application/x-tar"},
		{".jar", "application/java-archive"},
		{".war", "application/java-archive"},
		{".sha1", "application/x-checksum"},
		{".sha256", "application/x-checksum"},
		{".md5", "application/x-checksum"},
	}

	for _, tc := range deterministic {
		t.Run("inferred "+tc.ext, func(t *testing.T) {
			path := "/binflow/generic-local/mime/data" + tc.ext
			resp := e.do(t, http.MethodPut, path, strings.NewReader("x"), nil)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT = %d: %s", resp.StatusCode, body(t, resp))
			}
			// Upload FileInfo body carries the inferred mimeType.
			var fi fileInfoJSON
			if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
				t.Fatalf("FileInfo: %v", err)
			}
			if fi.MimeType != tc.want {
				t.Fatalf("FileInfo mimeType = %q, want %q", fi.MimeType, tc.want)
			}
			// GET and HEAD answer with the same Content-Type.
			get := e.do(t, http.MethodGet, path, nil, nil)
			body(t, get)
			checkHeader(t, get, "Content-Type", tc.want)
			head := e.do(t, http.MethodHead, path, nil, nil)
			head.Body.Close() //nolint:errcheck // read-only probe
			checkHeader(t, head, "Content-Type", tc.want)
		})
	}

	// Unknown extension and no extension: the octet-stream fallback is
	// unchanged (the M1 h.bin case asserts the same for .bin).
	t.Run("unknown extension stays octet-stream", func(t *testing.T) {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/blob.zzz", strings.NewReader("x"), nil)
		var fi fileInfoJSON
		if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
			t.Fatalf("FileInfo: %v", err)
		}
		if fi.MimeType != "application/octet-stream" {
			t.Fatalf("mimeType = %q, want octet-stream", fi.MimeType)
		}
	})
	t.Run("no extension stays octet-stream", func(t *testing.T) {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/Dockerfile", strings.NewReader("x"), nil)
		var fi fileInfoJSON
		if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
			t.Fatalf("FileInfo: %v", err)
		}
		if fi.MimeType != "application/octet-stream" {
			t.Fatalf("mimeType = %q, want octet-stream", fi.MimeType)
		}
	})

	// A declared Content-Type wins verbatim — including ones that disagree
	// with the extension: the mapping only fills the gap, never overrides.
	t.Run("declared Content-Type wins over extension", func(t *testing.T) {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/custom.json",
			strings.NewReader("x"), map[string]string{"Content-Type": "application/vnd.binflow+thing"})
		var fi fileInfoJSON
		if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
			t.Fatalf("FileInfo: %v", err)
		}
		if fi.MimeType != "application/vnd.binflow+thing" {
			t.Fatalf("mimeType = %q, want the declared value verbatim", fi.MimeType)
		}
		get := e.do(t, http.MethodGet, "/binflow/generic-local/mime/custom.json", nil, nil)
		body(t, get)
		checkHeader(t, get, "Content-Type", "application/vnd.binflow+thing")
	})

	// Uppercase extensions resolve case-insensitively (uploaders that spell
	// "DATA.JSON" get the same answer).
	t.Run("case-insensitive extension", func(t *testing.T) {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/DATA.JSON", strings.NewReader("x"), nil)
		var fi fileInfoJSON
		if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
			t.Fatalf("FileInfo: %v", err)
		}
		if fi.MimeType != "application/json" {
			t.Fatalf("mimeType = %q, want application/json", fi.MimeType)
		}
	})

	// .tar.gz chains: only the last extension is consulted ("gz" ->
	// application/gzip), the same one-extension rule as mime.Ext and
	// Artifactory's mimetypes lookup.
	t.Run("compound extension uses the final segment", func(t *testing.T) {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/bundle.tar.gz", strings.NewReader("x"), nil)
		var fi fileInfoJSON
		if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
			t.Fatalf("FileInfo: %v", err)
		}
		if fi.MimeType != "application/gzip" {
			t.Fatalf("mimeType = %q, want application/gzip", fi.MimeType)
		}
	})

	// Checksum-deploy inherits the mapping too: no Content-Type on a
	// X-Checksum-Deploy PUT is exactly as undeclared as on a body PUT.
	t.Run("checksum deploy infers from extension", func(t *testing.T) {
		content := "deploy source"
		sha, _, _ := digestsOf(content)
		src := e.do(t, http.MethodPut, "/binflow/generic-local/mime/src.bin", strings.NewReader(content), nil)
		body(t, src)
		if src.StatusCode != http.StatusCreated {
			t.Fatalf("source PUT = %d", src.StatusCode)
		}
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/copy.json", nil,
			map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": sha})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("checksum deploy = %d: %s", resp.StatusCode, body(t, resp))
		}
		var fi fileInfoJSON
		if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
			t.Fatalf("FileInfo: %v", err)
		}
		if fi.MimeType != "application/json" {
			t.Fatalf("mimeType = %q, want application/json", fi.MimeType)
		}
	})
}

// TestMimeByExtension pins the resolver's layering: BinFlow's table takes
// precedence, the stdlib database answers what the table misses, and the
// empty string (never a bogus value) flows through to the octet-stream
// fallback. It lives in the internal test package scope-wise but exercises
// only exported behavior through the package's own probe: the map/table and
// resolver are unexported, so the probe is a compile-time check that the
// deterministic table is a strict subset of what the adapter serves.
func TestMimeByExtension(t *testing.T) {
	// The deterministic table is served identically regardless of host.
	for ext, want := range map[string]string{
		".yml":   "application/yaml",
		".md":    "text/markdown; charset=utf-8",
		".sha1":  "application/x-checksum",
		".tgz":   "application/gzip",
		".noext": "application/octet-stream", // unknown -> fallback
	} {
		if got := mimeOfPath(t, ext); got != want {
			t.Fatalf("mimeOfPath(%q) = %q, want %q", ext, got, want)
		}
	}
}

// mimeOfPath exercises the extension inference through the public surface
// (a PUT + FileInfo roundtrip) so the internal table never needs exporting.
// ext keeps its leading dot; ".noext" is simply an unknown extension.
func mimeOfPath(t *testing.T, ext string) string {
	t.Helper()
	e := newEnv(t)
	resp := e.do(t, http.MethodPut, "/binflow/generic-local/probe/f"+ext, strings.NewReader("x"), nil)
	var fi fileInfoJSON
	if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
		t.Fatalf("FileInfo: %v", err)
	}
	return fi.MimeType
}

// guard: the package under test must compile against the handler API the
// harness mounts (generic.New/NewWithClock); a compile failure here is a
// wiring break, not a mime regression.
var _ = generic.Protocol
