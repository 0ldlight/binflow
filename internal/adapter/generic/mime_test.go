package generic_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter/generic"
)

// TestContentTypeMapping covers the BIN-53 / T-571 ownership model: the
// factory table (mimetypes.xml v17, docs/reverse/mime-ownership.md
// section 2) is the only mime authority on the storage faces — the
// request's declared Content-Type is ignored on PUT, and GET/HEAD and the
// upload FileInfo body all render the table value for the path. Unknown
// extensions, no extension and table misses stay application/octet-stream
// on every host (FR-4-AC13; the stdlib fallback is gone, BIN-53 3a).
func TestContentTypeMapping(t *testing.T) {
	e := newEnv(t)

	// T-578/BIN-60: the terminal .sha1/.md5/.sha256 suffixes left the
	// file-deploy family on LOCAL repositories (they are the
	// client-checksum faces now — pinned in checksum_put_test.go), so the
	// v17 table's checksum rows carry no PUT legs here; the GET echo face
	// pins its own application/x-checksum Content-Type.

	// The full v17 table over the wire (no Content-Type declared).
	deterministic := []struct{ ext, want string }{
		{".7z", "application/x-7z-compressed"},
		{".apk", "application/vnd.android.package-archive"},
		{".asc", "text/plain"},
		{".box", "application/x-vagrant-box"},
		{".bz2", "application/x-bzip2"},
		{".c", "text/x-c"},
		{".cc", "text/x-c"},
		{".conda", "application/x-conda"},
		{".cpp", "text/x-c"},
		{".cs", "text/x-csharp.sh"},
		{".css", "text/css"},
		{".ddeb", "application/x-debian-package"},
		{".deb", "application/x-debian-package"},
		{".dtd", "application/xml-dtd"},
		{".ear", "application/java-archive"},
		{".ent", "application/xml-external-parsed-entity"},
		{".fx", "text/x-javafx-source"},
		{".gem", "application/x-rubygems"},
		{".gradle", "text/x-groovy-source"},
		{".groovy", "text/x-groovy-source"},
		{".gz", "application/x-gzip"},
		{".h", "text/x-c"},
		{".har", "application/java-archive"},
		{".hpi", "application/java-archive"},
		{".htm", "text/html"},
		{".html", "text/html"},
		{".info", "application/json+info"},
		{".ivy", "application/x-ivy+xml"},
		{".jar", "application/java-archive"},
		{".java", "text/x-java-source"},
		{".jardiff", "application/x-java-archive-diff"},
		{".jnlp", "application/x-java-jnlp-file"},
		{".jpi", "application/java-archive"},
		{".json", "application/json"},
		{".log", "text/plain"},
		{".md", "text/plain"},
		{".mf", "text/plain"},
		{".mod", "text/plain+mod"},
		{".nupkg", "application/x-nupkg"},
		{".nuspec", "application/x-nuspec+xml"},
		{".pom", "application/x-maven-pom+xml"},
		{".properties", "text/plain"},
		{".py", "text/x-python"},
		{".rar", "application/x-rar-compressed"},
		{".rb", "text/x-ruby-source"},
		{".rpm", "application/x-rpm"},
		{".rz", "application/x-ruby-marshal"},
		{".sar", "application/java-archive"},
		{".scala", "text/x-scala-source"},
		{".sh", "text/x-script.sh"},
		{".swift", "text/x-swift "}, // trailing space: factory spelling, verbatim
		{".tar", "application/x-tar"},
		{".tf", "text/plain"},
		{".tgz", "application/x-gzip"},
		{".txt", "text/plain"},
		{".war", "application/java-archive"},
		{".xhtml", "application/xhtml+xml"},
		{".xml", "application/xml"},
		{".xsi", "application/xml"},
		{".xsl", "text/xsl"},
		{".xslt", "text/xslt"},
		{".xsd", "application/xml-schema"},
		{".xz", "application/x-xz"},
		{".yml", "text/plain"},
		{".yaml", "text/plain"},
		{".zip", "application/zip"},
	}

	for _, tc := range deterministic {
		t.Run("inferred "+tc.ext, func(t *testing.T) {
			path := "/binflow/generic-local/mime/data" + tc.ext
			resp := e.do(t, http.MethodPut, path, strings.NewReader("x"), nil)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT = %d: %s", resp.StatusCode, body(t, resp))
			}
			// Upload FileInfo body carries the table value.
			var fi fileInfoJSON
			if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
				t.Fatalf("FileInfo: %v", err)
			}
			if fi.MimeType != tc.want {
				t.Fatalf("FileInfo mimeType = %q, want %q", fi.MimeType, tc.want)
			}
			// GET and HEAD answer with the same Content-Type. The header
			// face compares trimmed of trailing OWS: the factory's .swift
			// value ends in a space, which the header transport strips
			// (A's own GET header answers "text/x-swift" while its
			// FileInfo keeps the space — live-confirmed T-571).
			headerWant := strings.TrimRight(tc.want, " ")
			get := e.do(t, http.MethodGet, path, nil, nil)
			body(t, get)
			checkHeader(t, get, "Content-Type", headerWant)
			head := e.do(t, http.MethodHead, path, nil, nil)
			head.Body.Close() //nolint:errcheck // read-only probe
			checkHeader(t, head, "Content-Type", headerWant)
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

	// Table misses the stdlib used to answer are octet-stream now: with
	// the fallback deleted (BIN-53 3a) .csv/.pdf/.svg are host-stable
	// octet-stream legs — the A live shape (the factory table has no such
	// rows), no longer the darwin-only "text/csv; charset=utf-8" builtin.
	t.Run("csv pdf svg fall to octet-stream", func(t *testing.T) {
		for _, ext := range []string{".csv", ".pdf", ".svg"} {
			resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/blob"+ext, strings.NewReader("x"), nil)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT %s = %d", ext, resp.StatusCode)
			}
			var fi fileInfoJSON
			if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
				t.Fatalf("FileInfo %s: %v", ext, err)
			}
			if fi.MimeType != "application/octet-stream" {
				t.Errorf("%s mimeType = %q, want octet-stream", ext, fi.MimeType)
			}
			get := e.do(t, http.MethodGet, "/binflow/generic-local/mime/blob"+ext, nil, nil)
			body(t, get)
			checkHeader(t, get, "Content-Type", "application/octet-stream")
		}
	})

	// The ownership flip (BIN-53): a declared Content-Type no longer wins
	// — the table value answers for the path, declaration or not. The
	// 7.161.26 18-leg matrix answered every explicit declaration from the
	// table (the pre-T-571 arm here asserted the declared value verbatim
	// and flipped with the ruling).
	t.Run("declared Content-Type is ignored, extension wins", func(t *testing.T) {
		for _, tc := range []struct{ file, declared, want string }{
			{"custom.json", "application/vnd.binflow+thing", "application/json"},
			{"mirror.txt", "application/json", "text/plain"}, // the mirror leg: ext wins
			{"declared.bin", "application/x-custom", "application/octet-stream"},
		} {
			path := "/binflow/generic-local/mime/" + tc.file
			resp := e.do(t, http.MethodPut, path, strings.NewReader("x"),
				map[string]string{"Content-Type": tc.declared})
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT %s = %d: %s", tc.file, resp.StatusCode, body(t, resp))
			}
			var fi fileInfoJSON
			if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
				t.Fatalf("FileInfo: %v", err)
			}
			if fi.MimeType != tc.want {
				t.Fatalf("%s declared %q: mimeType = %q, want table value %q",
					tc.file, tc.declared, fi.MimeType, tc.want)
			}
			get := e.do(t, http.MethodGet, path, nil, nil)
			body(t, get)
			checkHeader(t, get, "Content-Type", tc.want)
		}
	})

	// Uppercase extensions resolve case-insensitively (uploaders that spell
	// "DATA.JSON" get the same answer) — including compound spellings.
	t.Run("case-insensitive extension", func(t *testing.T) {
		for _, file := range []string{"DATA.JSON", "Bundle.TAR.GZ"} {
			resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/"+file, strings.NewReader("x"), nil)
			var fi fileInfoJSON
			if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
				t.Fatalf("FileInfo: %v", err)
			}
			want := "application/json"
			if file == "Bundle.TAR.GZ" {
				want = "application/x-gzip"
			}
			if fi.MimeType != want {
				t.Errorf("%s mimeType = %q, want %q", file, fi.MimeType, want)
			}
		}
	})

	// Multi-segment extensions: the reference parses the FINAL segment
	// only (live-confirmed on 7.161.26, T-571) — the factory table's
	// jar.pack.gz row never fires, so *.jar.pack.gz is x-gzip like every
	// other .gz; tar.gz/tar.bz2/nar.xz converge with their simple keys.
	t.Run("compound extension uses the final segment", func(t *testing.T) {
		for _, tc := range []struct{ file, want string }{
			{"bundle.tar.gz", "application/x-gzip"},
			{"bundle.tar.bz2", "application/x-bzip2"},
			{"bundle.nar.xz", "application/x-xz"},
			{"app.jar.pack.gz", "application/x-gzip"}, // NOT x-java-pack200
		} {
			resp := e.do(t, http.MethodPut, "/binflow/generic-local/mime/"+tc.file, strings.NewReader("x"), nil)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT %s = %d", tc.file, resp.StatusCode)
			}
			var fi fileInfoJSON
			if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
				t.Fatalf("FileInfo: %v", err)
			}
			if fi.MimeType != tc.want {
				t.Errorf("%s mimeType = %q, want %q", tc.file, fi.MimeType, tc.want)
			}
		}
	})

	// Checksum-deploy inherits the mapping too: the zero-transfer deploy
	// stores the table value for its target path like a body PUT.
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

// TestMimeByExtension pins the resolver's layering at the probe level:
// the table answers for known extensions, everything else is the
// host-stable octet-stream floor (no stdlib consultation since BIN-53
// 3a). It exercises only exported behavior through the package's own
// probe: the map/table and resolver are unexported, so the probe is a
// compile-time check that the deterministic table is a strict subset of
// what the adapter serves.
func TestMimeByExtension(t *testing.T) {
	// The deterministic table is served identically regardless of host.
	for ext, want := range map[string]string{
		".yml":    "text/plain",
		".md":     "text/plain",
		".tgz":    "application/x-gzip",
		".pom":    "application/x-maven-pom+xml",
		".noext":  "application/octet-stream", // unknown -> floor
		".csv":    "application/octet-stream", // stdlib builtin gone (3a)
		".sha512": "application/octet-stream", // B superset removed (T-570)
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
