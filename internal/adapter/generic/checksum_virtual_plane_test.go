package generic_test

// T-583 / BIN-65 (L039 Arm 1b / C1): the terminal-checksum interception
// family passes THROUGH a virtual repository — every action runs against
// the write route's deployment-target member (the miss 404's repo segment,
// the SET and the 201 Location name the member), the ordinary deploy
// envelope renders the LANDED member repository, and the GET face echoes a
// stored client value through the virtual read plane. A virtual repository
// without a defaultDeploymentRepo keeps its already-matching 405.
// 权威口径：reports/compatibility/L039-checksum-put-adjacent.md Arm 1
// （A 7.161.26 双轮，a1b-v-* / a1-v-* 腿）。

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// newVirtualEnv extends the harness with a routed virtual repository
// (gvirt → generic-local) and an un-routed control (gvirt-noroute).
func newVirtualEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	ctx := context.Background()
	for _, r := range []*metadata.Repo{
		{RepoKey: "gvirt", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
			Config: `{"repositories":["generic-local"],"defaultDeploymentRepo":"generic-local"}`},
		{RepoKey: "gvirt-noroute", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
			Config: `{"repositories":["generic-local"]}`},
	} {
		if _, err := e.svc.CreateRepo(ctx, admin(), r); err != nil {
			t.Fatalf("CreateRepo %s: %v", r.RepoKey, err)
		}
	}
	return e
}

// TestChecksumPutVirtualPlanePassThrough walks L039 Arm 1b's leg set: the
// seed deploy renders the member, the family's miss/ok/wrong arms address
// the member, and the GET face echoes the stored value through the virtual
// plane.
func TestChecksumPutVirtualPlanePassThrough(t *testing.T) {
	e := newVirtualEnv(t)
	src := "t583v/src.bin"

	// Seed through the virtual key: the envelope and Location render the
	// LANDED member repository (A: repo=difftest-…-generic, Location→member).
	resp := e.do(t, http.MethodPut, "/binflow/gvirt/"+src, strings.NewReader("source-bytes"), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed PUT = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	memberLoc := e.srv.URL + "/binflow/generic-local/" + src
	if got := resp.Header.Get("Location"); got != memberLoc {
		t.Errorf("seed Location = %q, want the member artifact %q", got, memberLoc)
	}
	var fi fileInfoJSON
	if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
		t.Fatalf("envelope decode: %v", err)
	}
	if fi.Repo != "generic-local" {
		t.Errorf("envelope repo = %q, want the landed member generic-local", fi.Repo)
	}
	if fi.URI != memberLoc || fi.DownloadURI != memberLoc {
		t.Errorf("envelope uri/downloadUri = %q/%q, want the member path %q", fi.URI, fi.DownloadURI, memberLoc)
	}
	// The landing really is the member's node.
	if _, err := e.md.Nodes().Get(context.Background(), "generic-local", src); err != nil {
		t.Fatalf("member node after virtual seed: %v", err)
	}

	// Missing source: the family's 404 verbatim with the MEMBER repo key.
	_, sha1S, md5S := digestsOf("source-bytes")
	for _, sfx := range []string{".sha1", ".md5", ".sha256"} {
		resp := e.do(t, http.MethodPut, "/binflow/gvirt/t583v/miss.txt"+sfx,
			strings.NewReader(strings.Repeat("ab", 20)), nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("miss PUT%s = %d, want 404 (body=%s)", sfx, resp.StatusCode, body(t, resp))
		}
		want := "Target file to set checksum on doesn't exist: generic-local:t583v/miss.txt"
		if got := body(t, resp); !strings.Contains(got, `"`+want+`"`) {
			t.Errorf("miss PUT%s body = %s\nwant message = %q", sfx, got, want)
		}
	}

	// Correct value (uppercase suffix — the C1 pass-through and the C3
	// folding compose): 201, empty body, Location = the MEMBER source.
	resp = e.do(t, http.MethodPut, "/binflow/gvirt/"+src+".SHA1", strings.NewReader(sha1S), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("set-ok PUT = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	if got := resp.Header.Get("Location"); got != memberLoc {
		t.Errorf("set-ok Location = %q, want the member source %q", got, memberLoc)
	}
	if got := body(t, resp); got != "" {
		t.Errorf("set-ok body = %q, want empty", got)
	}

	// Wrong value: 409 with the local face's wording (relPath, no repo
	// prefix) AND the write-through on the MEMBER node's client column.
	wrongMd5 := strings.Repeat("0", 32)
	resp = e.do(t, http.MethodPut, "/binflow/gvirt/"+src+".md5", strings.NewReader(wrongMd5), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("set-wrong PUT = %d, want 409 (body=%s)", resp.StatusCode, body(t, resp))
	}
	want := "Checksum error for '" + src + ".md5': received '" + wrongMd5 + "' but actual is '" + md5S + "'"
	if got := body(t, resp); !strings.Contains(got, `"`+want+`"`) {
		t.Errorf("set-wrong body = %s\nwant message = %q", got, want)
	}
	node, err := e.md.Nodes().Get(context.Background(), "generic-local", src)
	if err != nil {
		t.Fatalf("member node: %v", err)
	}
	if node.ClientMd5 != wrongMd5 {
		t.Errorf("member ClientMd5 after 409 = %q, want the written-through %q", node.ClientMd5, wrongMd5)
	}
	if node.ClientSha1 != sha1S {
		t.Errorf("member ClientSha1 = %q, want the earlier registration %q", node.ClientSha1, sha1S)
	}

	// No sidecar file materialized in the member: the namespace holds the
	// folder marker and the source alone.
	nodes, err := e.svc.List(context.Background(), admin(), "generic-local", "t583v")
	if err != nil {
		t.Fatalf("List member: %v", err)
	}
	for _, n := range nodes {
		lower := strings.ToLower(n.Path)
		if strings.HasSuffix(lower, ".sha1") || strings.HasSuffix(lower, ".md5") || strings.HasSuffix(lower, ".sha256") {
			t.Errorf("unexpected sidecar node in member: %s", n.Path)
		}
	}
	if len(nodes) != 2 { // "t583v/" marker + src
		var paths []string
		for _, n := range nodes {
			paths = append(paths, n.Path)
		}
		t.Fatalf("member nodes under t583v = %v, want exactly [t583v/ %s]", paths, src)
	}

	// GET through the virtual key: the stored client value echoes with the
	// family's content type (A: 200 x-checksum, body=client value).
	get := e.do(t, http.MethodGet, "/binflow/gvirt/"+src+".SHA1", nil, nil)
	if get.StatusCode != http.StatusOK {
		t.Fatalf("GET echo = %d (body=%s)", get.StatusCode, body(t, get))
	}
	if ct := get.Header.Get("Content-Type"); ct != "application/x-checksum" {
		t.Errorf("GET echo Content-Type = %q, want application/x-checksum", ct)
	}
	if got := body(t, get); got != sha1S {
		t.Errorf("GET echo body = %q, want the stored client sha1 %q", got, sha1S)
	}

	// Unset algorithm on the virtual plane: NOT the on-demand face — the
	// ordinary chain keeps rendering (the C2 family's open posture: the
	// 404 addresses the .sha256 PATH, download-side wording).
	unset := e.do(t, http.MethodGet, "/binflow/gvirt/"+src+".sha256", nil, nil)
	if unset.StatusCode != http.StatusNotFound {
		t.Fatalf("GET unset = %d, want 404 (body=%s)", unset.StatusCode, body(t, unset))
	}
	if got := body(t, unset); !strings.Contains(got, "Failed to find the requested resource 'gvirt/"+src+".sha256'.") {
		t.Errorf("GET unset body = %s, want the ordinary chain's download-side miss", got)
	}
}

// TestChecksumPutVirtualNoRouteKeeps405 pins the already-matching face this
// ticket must not touch: a virtual repository without a defaultDeploymentRepo
// refuses the checksum-suffix PUT with the same 405 as any other write
// (L039 Arm 1a), and its GET keeps the ordinary chain's miss.
func TestChecksumPutVirtualNoRouteKeeps405(t *testing.T) {
	e := newVirtualEnv(t)
	resp := e.do(t, http.MethodPut, "/binflow/gvirt-noroute/t583n/src.bin.sha1",
		strings.NewReader(strings.Repeat("ab", 20)), nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("no-route PUT = %d, want 405 (body=%s)", resp.StatusCode, body(t, resp))
	}
	want := "No local repository was configured as local deployment repository for the (gvirt-noroute) virtual repository."
	if got := body(t, resp); !strings.Contains(got, want) {
		t.Errorf("no-route PUT body = %s\nwant message = %q", got, want)
	}
	get := e.do(t, http.MethodGet, "/binflow/gvirt-noroute/t583n/src.bin.sha1", nil, nil)
	if get.StatusCode != http.StatusNotFound {
		t.Fatalf("no-route GET = %d, want 404", get.StatusCode)
	}
	if got := body(t, get); !strings.Contains(got, "Failed to find the requested resource 'gvirt-noroute/t583n/src.bin.sha1'.") {
		t.Errorf("no-route GET body = %s, want the ordinary chain's miss", got)
	}
}
