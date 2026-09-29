package maven

// The deterministic extension→Content-Type table aligned to Artifactory's
// shipped mimetypes.xml v17 (BIN-52 / T-570, spec docs/reverse/
// mime-ownership.md section 2). Asserts the entries this ticket changed or
// deleted through the PUT 201 envelope's mimeType field (undeclared
// Content-Type, exactly the leg the table owns): .pom gets the dedicated
// spelling, text/gzip entries lose their charset/RFC spellings, and
// .sha512 — absent from the factory table — is pinned at the resolver
// level: every *.sha512 PUT is the sidecar registration face (no table-mime
// storage leg exists), and the sidecar GET face keeps its own x-checksum
// protocol constant and 404 gate, ruled separately in L032.
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

// TestMimeForPathChecksumSuffixes: the checksum family is pinned at the
// resolver level — no wire leg exercises it (every *.sha1/*.md5/*.sha256/
// *.sha512 PUT is the sidecar registration face, GETs the digest face).
// sha1/sha256/md5 keep their x-checksum entry (the factory table lists
// them); .sha512 is deleted (B superset removed per the ruling), so an
// undeclared-CT .sha512 path falls through the table to the octet-stream
// floor (the stdlib knows no .sha512 on the hosts we run: builtin table
// has none, and the OS databases sampled — darwin — answer "").
func TestMimeForPathChecksumSuffixes(t *testing.T) {
	for path, want := range map[string]string{
		"com/acme/t570/1.0.0/t570-1.0.0.sha1":   "application/x-checksum",
		"com/acme/t570/1.0.0/t570-1.0.0.sha256": "application/x-checksum",
		"com/acme/t570/1.0.0/t570-1.0.0.md5":    "application/x-checksum",
		"com/acme/t570/1.0.0/t570-1.0.0.sha512": "application/octet-stream",
	} {
		if got := mimeForPath(path, ""); got != want {
			t.Errorf("%s undeclared mime = %q, want %q", path, got, want)
		}
	}
	// A stored value still wins — the deletion touches only inference.
	if got := mimeForPath("com/acme/t570/1.0.0/t570-1.0.0.sha512", "application/x-checksum"); got != "application/x-checksum" {
		t.Errorf(".sha512 stored mime = %q, want the stored value", got)
	}
}
