// BIN-66 / T-584: the terminal-checksum family under the
// server-generated-checksums policy (L039 Arm 6's maven model probed on
// the generic leg — the A pre-probe of 2026-09-30): registration and
// verification are DECOUPLED — the declared value still lands in the
// client column (originalChecksums) while the comparison is skipped (the
// wrong-value PUT renders the same 201 Location-to-source), and the GET
// face answers the COMPUTED digest, registered or not.
package generic_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestChecksumPutSrvgenSkipsComparisonRegisters pins the srvgen plane:
// wrong-value PUT -> 201 empty body Location-to-source (no 409), the
// declared value registered in the client column, and the GET face serving
// the computed digest for a registered-wrong algorithm AND a
// never-registered one.
func TestChecksumPutSrvgenSkipsComparisonRegisters(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "generic-srvgen", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"checksumPolicyType":"server-generated-checksums"}`,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	src := "t584/pol/src.bin"
	payload := "srvgen-probe-body-584\n"
	if resp := e.do(t, http.MethodPut, "/binflow/generic-srvgen/"+src,
		strings.NewReader(payload), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	_, sha1C, md5C := digestsOf(payload)

	// Wrong value: 201 (the comparison is the policy gate and srvgen skips
	// it), CL=0, Location addressing the SOURCE artifact.
	wrong := strings.Repeat("0", 32)
	resp := e.do(t, http.MethodPut, "/binflow/generic-srvgen/"+src+".md5",
		strings.NewReader(wrong), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("srvgen wrong-value .md5 = %d, want 201 (body=%s)", resp.StatusCode, body(t, resp))
	}
	if got := body(t, resp); got != "" {
		t.Errorf("srvgen .md5 body = %q, want empty", got)
	}
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/"+src) || strings.HasSuffix(loc, ".md5") {
		t.Errorf("srvgen .md5 Location = %q, want the source artifact", loc)
	}

	// Registration happened anyway: the client column carries the declared
	// (wrong) value — originalChecksums' single source.
	node, err := e.md.Nodes().Get(context.Background(), "generic-srvgen", src)
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if node.ClientMd5 != wrong {
		t.Errorf("node.ClientMd5 under srvgen = %q, want the registered %q", node.ClientMd5, wrong)
	}

	// The GET face answers the computed digest: for the registered-wrong
	// md5 AND the never-registered sha1 alike.
	for _, tc := range []struct{ algo, want string }{
		{"md5", md5C},
		{"sha1", sha1C},
	} {
		get := e.do(t, http.MethodGet, "/binflow/generic-srvgen/"+src+"."+tc.algo, nil, nil)
		if get.StatusCode != http.StatusOK {
			t.Fatalf("srvgen GET .%s = %d (body=%s)", tc.algo, get.StatusCode, body(t, get))
		}
		if ct := get.Header.Get("Content-Type"); ct != "application/x-checksum" {
			t.Errorf("srvgen GET .%s CT = %q, want application/x-checksum", tc.algo, ct)
		}
		if got := body(t, get); got != tc.want {
			t.Errorf("srvgen GET .%s = %q, want computed %q", tc.algo, got, tc.want)
		}
	}

	// A missing source keeps the family's miss wording under srvgen too
	// (the policy never mints a source).
	miss := e.do(t, http.MethodGet, "/binflow/generic-srvgen/t584/pol/never.bin.md5", nil, nil)
	if miss.StatusCode != http.StatusNotFound {
		t.Fatalf("srvgen GET missing-source = %d, want 404", miss.StatusCode)
	}
	if got := body(t, miss); !strings.Contains(got,
		`"File not found.; Path: 'generic-srvgen:t584/pol/never.bin'"`) {
		t.Errorf("srvgen missing-source body = %s, want the colon-separated family wording", got)
	}
}
