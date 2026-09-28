package maven

// T-542 (BIN-16, L030 case 1): <snapshotVersions> is served conditionally
// on the client's M3 snapshot-marker capability (virtual-resolution.md
// §5.1 row 4). Two exposure faces of one root cause: the VIRTUAL merge leg
// and the LOCAL member document leg — both serve the merge minus
// <snapshotVersions> (the <snapshot> block intact) to a java-agent UA,
// both merge/serve it whole to a Maven 3 UA. The virtual sidecars follow
// the derived (stripped) bytes.

import (
	"crypto/sha1" //nolint:gosec // protocol digest of a test body, never a security primitive
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

func TestClientSupportsM3SnapshotVersions(t *testing.T) {
	cases := []struct {
		ua   string
		want bool
	}{
		// L030 case 1's two evidenced spellings.
		{"Apache-Maven/3.9.16 (Java 17.0.11; Mac OS X 15.0)", true},
		{"Java/1.8.0_391", false},
		// The report's advisory corners (decompile-anchored, unimplemented
		// before): absent UA defaults capable; the [Jj]ava/(.+) family and
		// the Ivy/Wharf deployers do not.
		{"", true},
		{"   ", true},
		{"java/17.0.2", false},
		{"Apache Ivy/2.5.2", false},
		{"Ivy/2.4.0", false},
		{"Wharf/1.0", false},
		// Anything else — Gradle, a browser, a probing script — is capable.
		{"Gradle/8.5 (JVM 17.0.2)", true},
		{"curl/8.4.0", true},
	}
	for _, tc := range cases {
		if got := clientSupportsM3SnapshotVersions(tc.ua); got != tc.want {
			t.Errorf("clientSupportsM3SnapshotVersions(%q) = %v, want %v", tc.ua, got, tc.want)
		}
	}
}

// TestSnapshotVersionsUAStripping walks the four legs of L030 case 1
// against the real stack: virtual x {M3-capable, java-agent} and the local
// member document x {M3-capable, java-agent}.
func TestSnapshotVersionsUAStripping(t *testing.T) {
	const (
		capableUA = "Apache-Maven/3.9.16 (Java 17.0.11; Mac OS X 15.0)"
		agentUA   = "Java/1.8.0_391"
	)
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)
	f.deploySnapshotPom(t, "mv-a", "20240101.120000", 1)
	f.deploySnapshotPom(t, "mv-b", "20240102.130000", 2)

	vmeta := "/mv-virt/com/acme/lib/1.0-SNAPSHOT/maven-metadata.xml"
	mmeta := "/mv-a/com/acme/lib/1.0-SNAPSHOT/maven-metadata.xml"
	legs := []struct {
		name    string
		path    string
		ua      string
		wantSV  bool
		wantBNs []string
	}{
		{"virtual M3-capable UA merges", vmeta, capableUA, true, []string{"<buildNumber>2</buildNumber>"}},
		{"virtual java-agent UA strips", vmeta, agentUA, false, []string{"<buildNumber>2</buildNumber>"}},
		{"member M3-capable UA serves whole", mmeta, capableUA, true, []string{"<buildNumber>1</buildNumber>"}},
		{"member java-agent UA strips", mmeta, agentUA, false, []string{"<buildNumber>1</buildNumber>"}},
	}
	var strippedVirtual string
	for _, tc := range legs {
		resp := f.hs.serve(http.MethodGet, tc.path, nil, map[string]string{"User-Agent": tc.ua}, true)
		body := string(drain(t, resp))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: GET = %d (%s)", tc.name, resp.StatusCode, body)
		}
		if strings.Contains(body, "<snapshotVersions>") != tc.wantSV {
			t.Errorf("%s: snapshotVersions present = %v, want %v\n%s", tc.name, !tc.wantSV, tc.wantSV, body)
		}
		for _, want := range tc.wantBNs {
			if !strings.Contains(body, want) {
				t.Errorf("%s: snapshot block lost %s\n%s", tc.name, want, body)
			}
		}
		if strings.Contains(body, "<snapshot>") != true {
			t.Errorf("%s: the <snapshot> block must survive either way\n%s", tc.name, body)
		}
		if tc.name == "virtual java-agent UA strips" {
			strippedVirtual = body
		}
	}

	// The virtual sidecar of the stripped face is the checksum of the
	// STRIPPED merged bytes (a client verification must hold against what
	// was served).
	sum := sha1.Sum([]byte(strippedVirtual)) //nolint:gosec // test digest
	resp := f.hs.serve(http.MethodGet, vmeta+".sha1", nil, map[string]string{"User-Agent": "Java/1.8.0_391"}, true)
	sidecar := strings.TrimSpace(string(drain(t, resp)))
	if resp.StatusCode != http.StatusOK || sidecar != hex.EncodeToString(sum[:]) {
		t.Errorf("stripped virtual sidecar = (%d, %s), want sha1 of the stripped body %s",
			resp.StatusCode, sidecar, hex.EncodeToString(sum[:]))
	}
}
