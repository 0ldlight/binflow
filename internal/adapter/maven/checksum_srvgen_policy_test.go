// BIN-66 / T-584: the checksum-policy write face under
// server-generated-checksums (L039 Arm 6, ledger
// maven/checksum-oc-write-under-srvgen-policy) — registration and
// verification are DECOUPLED: the declared value always lands in the
// client column (originalChecksums), the comparison alone is the policy
// gate (srvgen skips the 409), and the sidecar GET face keeps serving the
// COMPUTED digest whatever was declared. Plus the .sha512 ordinary-file
// face's non-local plane refusal (the arm must not bypass class refusals).
package maven

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestChecksumPutSrvgenRegistersDeclared pins the srvgen arm on the
// maven-lenient repository: a wrong-value .md5 PUT renders 201 (no
// comparison) yet the declared value is REGISTERED — the node's client
// column carries it and repo.OriginalChecksums surfaces it (the FileInfo
// oc face's single source), while the sidecar GET answers the computed
// digest both before and after a correct-value registration.
func TestChecksumPutSrvgenRegistersDeclared(t *testing.T) {
	hs := newHarness(t)
	jar := "com/acme/t584/1.0.0/t584-1.0.0.jar"
	if resp := hs.deployJar("maven-lenient", jar, jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d", resp.StatusCode)
	}
	_, md5Hex, _ := digests(jarBytes)
	wrong := strings.Repeat("0", 32)
	side := "/maven-lenient/" + jar + ".md5"

	resp := hs.serve(http.MethodPut, side, []byte(wrong), nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("srvgen wrong-value .md5 = %d, want 201 (no comparison; body=%s)",
			resp.StatusCode, drain(t, resp))
	}
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/"+jar) || !strings.HasSuffix(loc, ".jar") {
		t.Fatalf("srvgen .md5 Location = %q, want the target artifact", loc)
	}

	// The registration happened despite srvgen: the client column and the
	// oc overlay helper carry the declared (wrong) value.
	node, err := hs.md.Nodes().Get(context.Background(), "maven-lenient", jar)
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if node.ClientMd5 != wrong {
		t.Errorf("node.ClientMd5 under srvgen = %q, want the registered %q", node.ClientMd5, wrong)
	}
	if _, _, ocMd5 := repo.OriginalChecksums(node, ""); ocMd5 != wrong {
		t.Errorf("OriginalChecksums md5 under srvgen = %q, want the client value %q", ocMd5, wrong)
	}

	// The GET face stays computed under srvgen — before and after a
	// correct-value re-PUT alike (registration never flips the echo).
	if got := string(drain(t, hs.serve(http.MethodGet, side, nil, nil, true))); got != md5Hex {
		t.Fatalf("srvgen GET .md5 after wrong = %q, want computed %q", got, md5Hex)
	}
	if resp := hs.serve(http.MethodPut, side, []byte(md5Hex), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("srvgen correct-value .md5 = %d, want 201", resp.StatusCode)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, side, nil, nil, true))); got != md5Hex {
		t.Fatalf("srvgen GET .md5 after correct = %q, want computed %q", got, md5Hex)
	}

	// Client-policy control on the same shape: the comparison is still the
	// policy gate there (409) and the write-through echo stays the client
	// value — the T-578 legs, one-line re-pins.
	cjar := "com/acme/t584c/1.0.0/t584c-1.0.0.jar"
	if resp := hs.deployJar("maven-local", cjar, jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("client seed = %d", resp.StatusCode)
	}
	if resp := hs.serve(http.MethodPut, "/maven-local/"+cjar+".md5", []byte(wrong), nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("client wrong-value .md5 = %d, want 409", resp.StatusCode)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, "/maven-local/"+cjar+".md5", nil, nil, true))); got != wrong {
		t.Fatalf("client GET .md5 after 409 = %q, want the written-through %q", got, wrong)
	}
}

// TestSha512PutNonLocalPlaneRefusal pins that the .sha512 ordinary-file arm
// inherits the ordinary chain's class refusals: a remote repository answers
// the read-only 405 (never an interception, never a landed file) and an
// un-routed virtual repository keeps its own refusal.
func TestSha512PutNonLocalPlaneRefusal(t *testing.T) {
	hs := newHarness(t)
	h512 := []byte(strings.Repeat("f", 128))
	for repoKey, want := range map[string]int{
		"maven-remote":  http.StatusMethodNotAllowed,
		"maven-virtual": http.StatusMethodNotAllowed,
	} {
		resp := hs.serve(http.MethodPut, "/"+repoKey+"/arm/foo.txt.sha512", h512, nil, true)
		if resp.StatusCode != want {
			t.Fatalf("PUT %s/arm/foo.txt.sha512 = %d, want %d (body=%s)",
				repoKey, resp.StatusCode, want, drain(t, resp))
		}
	}
}
