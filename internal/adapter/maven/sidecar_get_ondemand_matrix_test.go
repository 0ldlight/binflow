// BIN-76 / T-594 (ledger maven/sidecar-get-ondemand-matrix, L041 Arm 1 +
// T-587's mvu-get-*-unset legs): the sidecar GET on-demand matrix — sha256
// ALONE computes on demand; an unset md5/sha1 answers the checksum family's
// own 404 citing the SOURCE (no repo prefix), and a miss's Path points at
// the source artifact, never the checksum suffix spelling. Both read faces
// (local and virtual) share the one model; registered client values echo
// verbatim ahead of the matrix (the write-through family included).
package maven

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestSidecarGetOndemandMatrix walks the unset/registered/miss legs on both
// read faces of one seeded artifact.
func TestSidecarGetOndemandMatrix(t *testing.T) {
	hs := newHarness(t)
	if _, err := hs.svc.CreateRepo(context.Background(), adminP, &metadata.Repo{
		RepoKey:     "t594-virt",
		Type:        repo.TypeVirtual,
		PackageType: Protocol,
		Config:      `{"repositories":["maven-local"]}`,
	}); err != nil {
		t.Fatalf("seed virtual: %v", err)
	}

	// Deploy WITHOUT checksum headers: every client column stays empty.
	jar := "com/diff/t594/1.0.0/t594-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	s1, _, s256 := digests(jarBytes)

	// Unset values: sha256 is the matrix's only 200 arm (the computed
	// digest); md5/sha1 answer the checksum family's 404 citing the source.
	unset := []struct {
		algo string
		want int
		body string // the expected 200 body, unused on the 404 arms
	}{
		{"sha256", http.StatusOK, s256},
		{"sha1", http.StatusNotFound, ""},
		{"md5", http.StatusNotFound, ""},
	}
	for _, face := range []string{"maven-local", "t594-virt"} {
		for _, u := range unset {
			resp := hs.serve(http.MethodGet, "/"+face+"/"+jar+"."+u.algo, nil, nil, true)
			if resp.StatusCode != u.want {
				t.Errorf("%s GET .%s (unset) = %d, want %d (%s)",
					face, u.algo, resp.StatusCode, u.want, drain(t, resp))
				continue
			}
			got := string(drain(t, resp))
			if u.want == http.StatusOK {
				if got != u.body {
					t.Errorf("%s GET .%s body = %q, want the computed %q", face, u.algo, got, u.body)
				}
				continue
			}
			if want := "Checksum not found for " + jar; !strings.Contains(got, want) {
				t.Errorf("%s GET .%s body = %s, want wording %q", face, u.algo, got, want)
			}
		}
	}

	// Registered values echo verbatim on both faces, the wrong-value
	// write-through included (L037 Arm 1: the client-policy 409 registers
	// too — the registered WRONG md5 must surface, not a computed digest).
	wrong := strings.Repeat("0", 32)
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".md5", []byte(wrong), nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("md5 wrong-value registration = %d, want the 409 write-through (%s)",
			resp.StatusCode, drain(t, resp))
	}
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".sha1", []byte(s1), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("sha1 registration = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	for _, tc := range []struct{ algo, want string }{{"md5", wrong}, {"sha1", s1}} {
		for _, face := range []string{"maven-local", "t594-virt"} {
			if got := string(drain(t, hs.serve(http.MethodGet, "/"+face+"/"+jar+"."+tc.algo, nil, nil, true))); got != tc.want {
				t.Errorf("%s GET .%s (registered) = %q, want %q", face, tc.algo, got, tc.want)
			}
		}
	}

	// The miss points at the SOURCE: every family suffix of a nonexistent
	// artifact answers the miss wording citing the stripped source path
	// under the requested face key — never the suffix spelling (L041 Arm 1
	// m1-get-sha1-absent-src, A verbatim; the ghost is a layout-parseable
	// GAV so the sidecar miss face, not the T-562 parse gate, answers).
	// The miss WORDING is per-face: the local plane the ordinary download
	// miss, the virtual plane its resolution family `Could not find
	// resource;` (L039 Arm 1 / L040 c2-v, live re-pinned T-594).
	ghost := "com/diff/t594/9.9.9/t594-9.9.9.jar"
	for _, face := range []string{"maven-local", "t594-virt"} {
		wantMsg := notFoundMessage(face, ghost)
		if face == "t594-virt" {
			wantMsg = fmt.Sprintf("Could not find resource; Path: '%s:%s'", face, ghost)
		}
		for _, algo := range []string{"sha256", "sha1", "md5"} {
			resp := hs.serve(http.MethodGet, "/"+face+"/"+ghost+"."+algo, nil, nil, true)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s GET ghost .%s = %d, want 404 (%s)", face, algo, resp.StatusCode, drain(t, resp))
				continue
			}
			got := string(drain(t, resp))
			if !strings.Contains(got, wantMsg) {
				t.Errorf("%s GET ghost .%s body = %s, want %q", face, algo, got, wantMsg)
			}
			if strings.Contains(got, "."+algo+"'") {
				t.Errorf("%s GET ghost .%s cites the suffix spelling: %s", face, algo, got)
			}
		}
	}
}

// TestSidecarGetSrvgenKeepsComputed pins the policy boundary: under
// server-generated-checksums the overlay gate stays OFF and the GET face
// serves the COMPUTED digest whatever was declared (L039 Arm 6, the
// ADR-0052 6.2 posture) — the md5/sha1 404 arms of the matrix are the
// client-policy/virtual faces' own, not this plane's.
func TestSidecarGetSrvgenKeepsComputed(t *testing.T) {
	hs := newHarness(t)
	jar := "com/diff/t594s/1.0.0/t594s-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/maven-lenient/"+jar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	s1, m5, _ := digests(jarBytes)
	wrong := strings.Repeat("0", 32)
	if resp := hs.serve(http.MethodPut, "/maven-lenient/"+jar+".md5", []byte(wrong), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("srvgen wrong-value .md5 = %d, want 201 (%s)", resp.StatusCode, drain(t, resp))
	}
	for _, tc := range []struct{ algo, want string }{{"md5", m5}, {"sha1", s1}} {
		if got := string(drain(t, hs.serve(http.MethodGet, "/maven-lenient/"+jar+"."+tc.algo, nil, nil, true))); got != tc.want {
			t.Errorf("srvgen GET .%s = %q, want the computed %q", tc.algo, got, tc.want)
		}
	}
}
