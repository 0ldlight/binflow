package maven

// The deterministic extension→Content-Type table aligned to Artifactory's
// shipped mimetypes.xml v17 (BIN-52 / T-570 aligned the spellings;
// BIN-53 / T-571 completed the table to full v17 coverage and made it the
// only mime authority on the storage faces, spec docs/reverse/
// mime-ownership.md section 2). Asserts through the PUT 201 envelope's
// mimeType field — which under render-time ownership is the table lookup,
// regardless of any declared Content-Type or stored value. The checksum
// family stays pinned at the resolver level: every *.sha512 PUT is the
// sidecar registration face (no table-mime storage leg exists), and the
// sidecar GET face keeps its own x-checksum protocol constant and 404
// gate, ruled separately in L032.
import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMimeTableArtifactorySpellings(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	cases := []struct{ file, want string }{
		// group 1: live-ruled respellings (charset dropped, A spellings)
		{"t570-1.0.0.txt", "text/plain"},
		{"t570-1.0.0.tgz", "application/x-gzip"},
		// group 2: factory-table entries the B table lacked or spelled differently
		{"t570-1.0.0.pom", "application/x-maven-pom+xml"},
		{"t570-1.0.0.deb", "application/x-debian-package"},
		{"t570-1.0.0.ddeb", "application/x-debian-package"},
		{"t570-1.0.0.rpm", "application/x-rpm"},
		{"t570-1.0.0.nupkg", "application/x-nupkg"},
		{"t570-1.0.0.nuspec", "application/x-nuspec+xml"},
		{"t570-1.0.0.sar", "application/java-archive"},
		{"t570-1.0.0.hpi", "application/java-archive"},
		{"t570-1.0.0.properties", "text/plain"},
		{"t570-1.0.0.log", "text/plain"},
		// BIN-53 v17 completion keys (factory rows the table previously lacked)
		{"t570-1.0.0.css", "text/css"},
		{"t570-1.0.0.java", "text/x-java-source"},
		{"t570-1.0.0.gradle", "text/x-groovy-source"},
		{"t570-1.0.0.h", "text/x-c"},
		{"t570-1.0.0.xsd", "application/xml-schema"},
		{"t570-1.0.0.py", "text/x-python"},
		{"t570-1.0.0.xz", "application/x-xz"},
		{"t570-1.0.0.bz2", "application/x-bzip2"},
		{"t570-1.0.0.7z", "application/x-7z-compressed"},
		{"t570-1.0.0.conda", "application/x-conda"},
		{"t570-1.0.0.gem", "application/x-rubygems"},
		{"t570-1.0.0.box", "application/x-vagrant-box"},
		{"t570-1.0.0.swift", "text/x-swift "}, // trailing space: factory spelling, verbatim
		{"t570-1.0.0.scala", "text/x-scala-source"},
		{"t570-1.0.0.rb", "text/x-ruby-source"},
		{"t570-1.0.0.sh", "text/x-script.sh"},
		{"t570-1.0.0.cs", "text/x-csharp.sh"},
		{"t570-1.0.0.xsl", "text/xsl"},
		{"t570-1.0.0.ivy", "application/x-ivy+xml"},
		{"t570-1.0.0.jnlp", "application/x-java-jnlp-file"},
		{"t570-1.0.0.mf", "text/plain"},
		{"t570-1.0.0.jardiff", "application/x-java-archive-diff"},
		// pinned unchanged (same value as before the alignment)
		{"t570-1.0.0.xml", "application/xml"},
		{"t570-1.0.0.jar", "application/java-archive"},
	}

	for _, tc := range cases {
		target := "/maven-local/com/acme/t570/1.0.0/" + tc.file
		resp := hs.serve(http.MethodPut, target, []byte("t570"), nil, true)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT %s = %d", tc.file, resp.StatusCode)
		}
		var fi struct {
			MimeType string `json:"mimeType"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&fi); err != nil {
			t.Fatalf("decode %s envelope: %v", tc.file, err)
		}
		if fi.MimeType != tc.want {
			t.Errorf("%s mimeType = %q, want %q", tc.file, fi.MimeType, tc.want)
		}
	}
}

// TestMimeDeclaredContentTypeIgnored: the ownership flip's headline arm —
// a deploy that declares a Content-Type gets the table value anyway. The
// 7.161.26 18-leg matrix answered every explicit declaration (plain,
// charset-parameterized, custom, extension-disagreeing) from the table.
func TestMimeDeclaredContentTypeIgnored(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	for _, tc := range []struct{ file, declared, want string }{
		{"t571-1.0.0.pom", "text/plain", "application/x-maven-pom+xml"},
		{"t571-1.0.0.jar", "application/x-custom-thing", "application/java-archive"},
		{"t571-1.0.0.txt", "application/json; charset=utf-8", "text/plain"},
		// the mirror leg: extension and declaration disagree, extension wins
		{"t571-1.0.0.md", "application/json", "text/plain"},
	} {
		target := "/maven-local/com/acme/t571/1.0.0/" + tc.file
		resp := hs.serve(http.MethodPut, target, []byte("t571"),
			map[string]string{"Content-Type": tc.declared}, true)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT %s = %d", tc.file, resp.StatusCode)
		}
		var fi struct {
			MimeType string `json:"mimeType"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&fi); err != nil {
			t.Fatalf("decode %s envelope: %v", tc.file, err)
		}
		if fi.MimeType != tc.want {
			t.Errorf("%s declared %q: mimeType = %q, want table value %q",
				tc.file, tc.declared, fi.MimeType, tc.want)
		}
		resp.Body.Close() //nolint:errcheck // read-only probe
		// The GET face renders the same value (one table, both faces).
		get := hs.serve(http.MethodGet, target, nil, nil, true)
		if ct := get.Header.Get("Content-Type"); ct != tc.want {
			t.Errorf("%s GET Content-Type = %q, want %q", tc.file, ct, tc.want)
		}
		get.Body.Close() //nolint:errcheck // read-only probe
	}
}

// TestMimeForPathChecksumSuffixes: the checksum family is pinned at the
// resolver level — no wire leg exercises it (every *.sha1/*.md5/*.sha256/
// *.sha512 PUT is the sidecar registration face, GETs the digest face).
// sha1/sha256/md5 keep their x-checksum entry (the factory table lists
// them); .sha512 is deleted (B superset removed per the ruling), so a
// .sha512 path falls through the table to the octet-stream floor — with
// the stdlib fallback gone (BIN-53 3a) that floor is host-stable.
func TestMimeForPathChecksumSuffixes(t *testing.T) {
	for path, want := range map[string]string{
		"com/acme/t570/1.0.0/t570-1.0.0.sha1":   "application/x-checksum",
		"com/acme/t570/1.0.0/t570-1.0.0.sha256": "application/x-checksum",
		"com/acme/t570/1.0.0/t570-1.0.0.md5":    "application/x-checksum",
		"com/acme/t570/1.0.0/t570-1.0.0.sha512": "application/octet-stream",
		// BIN-53 3a: table misses no longer consult the stdlib — .csv/.pdf/
		// .svg and unknown extensions are octet-stream on every host.
		"com/acme/t570/1.0.0/t570-1.0.0.csv":  "application/octet-stream",
		"com/acme/t570/1.0.0/t570-1.0.0.pdf":  "application/octet-stream",
		"com/acme/t570/1.0.0/t570-1.0.0.svg":  "application/octet-stream",
		"com/acme/t570/1.0.0/t570-1.0.0.zzz":  "application/octet-stream",
		"com/acme/t570/1.0.0/t570-1.0.0":      "application/octet-stream",
		"com/acme/t570/1.0.0/t570-1.0.0.JSON": "application/json", // case-insensitive
		// multi-segment: the reference parses the final segment only
		// (live-confirmed T-571) — jar.pack.gz is x-gzip, not pack200.
		"com/acme/t570/1.0.0/t570-1.0.0.jar.pack.gz": "application/x-gzip",
		"com/acme/t570/1.0.0/t570-1.0.0.tar.bz2":     "application/x-bzip2",
		"com/acme/t570/1.0.0/t570-1.0.0.nar.xz":      "application/x-xz",
	} {
		if got := mimeForPath(path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}
