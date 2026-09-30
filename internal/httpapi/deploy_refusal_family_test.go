package httpapi_test

// T-553 (BIN-35): the deploy-refusal family on the maven wire. The two
// fixed legs answer the upload engine's invalid-target 404
// (remote-cache-projection.md section 2.1's PUT row, live-confirmed against
// Artifactory 7.161.26 in L032 Arm 2): a PUT addressed to a maven remote's
// own key and a PUT addressed to its <K>-cache projection key both carry
// "Could not find a local repository named <requested-key> to deploy to."
// with the REQUESTED key (suffix spelling intact) in the named slot. The
// third leg is the §8.2 regression guard: an un-routed maven virtual keeps
// the 405 + Allow: GET + fixed wording.
//
// BIN-78/T-596 (R12 C7 a-arm): generic remotes joined the engine's 404
// family (L040 N2 — A answers the same wording on generic remotes,
// checksum-suffix paths included, the refusal ahead of any suffix
// interpretation). The scope legs pin the narrow circle the ruling drew:
// docker and cargo stay outside the unified wording domain (their own 405
// / 400 read-only faces, pinned below and in their adapter packages), and
// the anonymous challenge still precedes every refusal.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter/cargo"
	"github.com/lzwzzy/binflow/internal/adapter/maven"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// gavPom is the GAV-consistent pom body the L032 Arm 2 probe rode (the
// path/body agreement keeps the maven policy gates out of the picture, so
// the assertion isolates the routing refusal itself).
const gavPom = `<?xml version="1.0" encoding="UTF-8"?>
<project><groupId>com.diff</groupId><artifactId>wd</artifactId><version>1.0.0</version></project>`

// newDeployWireStack mounts the REAL maven and cargo adapters beside the
// default two (cmd's assembly spelling) so the legs address the same wire a
// real client deploy would, and the cargo scope legs route through the
// shared dispatch chain this file guards.
func newDeployWireStack(t *testing.T) *harness {
	t.Helper()
	return newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		// cargo.New (not cargo.Register): the global adapter registry is
		// cmd assembly's single-call seam — a second Register panics on
		// the duplicate protocol, and this helper mounts one stack per
		// test. dispatchContent routes on Deps.Adapters alone.
		d.Adapters = append(d.Adapters,
			maven.New(d.ReposSvc, d.Metadata.Repos(), d.Metadata.Blobs(), d.Metadata.Nodes()),
			cargo.New(d.ReposSvc, d.Metadata.Repos(), d.Metadata.Blobs(), d.Metadata.NodeProps(),
				cargo.Options{BaseURL: d.Config.Server.BaseURL, AnonymousAccess: d.Config.Security.AnonymousAccess}))
	}, nil)
}

// errorEnvelopeMessage decodes the unified errors[] envelope and returns
// its single message, so the assertions compare the wording VERBATIM.
func errorEnvelopeMessage(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var env struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope %q: %v", string(body), err)
	}
	if len(env.Errors) != 1 {
		t.Fatalf("envelope %q: want exactly one error entry, got %d", string(body), len(env.Errors))
	}
	return env.Errors[0].Message
}

// TestDeployRefusalFamilyWire is the three live-confirmed legs of L032
// Arm 2 plus the generic legs of L040 N2, asserted verbatim, plus the
// scoping legs.
func TestDeployRefusalFamilyWire(t *testing.T) {
	h := newDeployWireStack(t)
	// Order matters: t553-virt's member reference is validated at create
	// time, so t553-mloc must exist first. A map range would randomize the
	// order (BIN-39: main CI run 36508756551 drew virt first and the
	// create 400'd "member does not exist" on roughly 1 of 8 runs).
	repos := []struct{ key, body string }{
		{"t553-rem", `{"rclass":"remote","packageType":"maven","url":"http://127.0.0.1:9/upstream"}`},
		{"t553-grem", `{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9/upstream"}`},
		{"t553-mloc", `{"rclass":"local","packageType":"maven"}`},
		// A member but NO defaultDeploymentRepo: the un-routed 405 leg.
		{"t553-virt", `{"rclass":"virtual","packageType":"maven","repositories":["t553-mloc"]}`},
	}
	for _, r := range repos {
		if status, respBody := putRepoStatus(t, h, r.key, r.body); status != http.StatusOK {
			t.Fatalf("create %s: %d %s", r.key, status, respBody)
		}
	}

	const gav = "com/diff/wd/1.0.0/wd-1.0.0.pom"
	tests := []struct {
		name      string
		path      string
		wantCode  int
		wantAllow string
		wantMsg   string
	}{
		{"remote body leg: 404 family, requested key in the named slot",
			"/binflow/t553-rem/" + gav, http.StatusNotFound, "",
			"Could not find a local repository named t553-rem to deploy to."},
		{"kcache leg: same family, -cache spelling rides the named slot",
			"/binflow/t553-rem-cache/" + gav, http.StatusNotFound, "",
			"Could not find a local repository named t553-rem-cache to deploy to."},
		{"virtual regression leg: un-routed 405 keeps Allow: GET and the fixed wording",
			"/binflow/t553-virt/" + gav, http.StatusMethodNotAllowed, http.MethodGet,
			"No local repository was configured as local deployment repository for the (t553-virt) virtual repository."},
		{"generic remote leg: the engine 404 family (R12 C7 a-arm, L040 N2)",
			"/binflow/t553-grem/x.bin", http.StatusNotFound, "",
			"Could not find a local repository named t553-grem to deploy to."},
		{"generic remote checksum-suffix leg: same family, refusal precedes suffix interpretation",
			"/binflow/t553-grem/dir/x.bin.sha1", http.StatusNotFound, "",
			"Could not find a local repository named t553-grem to deploy to."},
		{"scope: generic kcache keeps the standard unknown-repo 404",
			"/binflow/t553-grem-cache/x.bin", http.StatusNotFound, "",
			"Failed to find the repository 't553-grem-cache' specified in the request."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(http.MethodPut, tc.path, adminUser, adminPass, []byte(gavPom), nil)
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("PUT %s = %d, want %d", tc.path, resp.StatusCode, tc.wantCode)
			}
			if allow := resp.Header.Get("Allow"); allow != tc.wantAllow {
				t.Fatalf("PUT %s Allow = %q, want %q", tc.path, allow, tc.wantAllow)
			}
			if msg := errorEnvelopeMessage(t, resp); msg != tc.wantMsg {
				t.Fatalf("PUT %s message =\n  %q\nwant\n  %q", tc.path, msg, tc.wantMsg)
			}
		})
	}

	// The refusal sits behind the write gate: an anonymous deploy keeps
	// the 401 challenge, never the invalid-target 404 (rest-api.md section
	// 1.2 step 4's ordering) — pinned on the generic face too, since it
	// now shares the engine arm.
	for _, path := range []string{"/binflow/t553-rem/" + gav, "/binflow/t553-grem/x.bin"} {
		resp := h.do(http.MethodPut, path, "", "", []byte(gavPom), nil)
		drain(resp)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous remote PUT %s = %d, want the 401 challenge ahead of the refusal", path, resp.StatusCode)
		}
	}
}

// TestDeployRefusalFamilyWireMetadataToo pins that the remote-key
// refusal is target-resolution shaped, not artifact-shaped: a metadata
// document PUT to the remote key answers the same 404 family (the engine
// resolves the target before any layout or policy step), while the local
// parent spelling of -cache keeps the standard unknown-repo wording.
func TestDeployRefusalFamilyWireMetadataToo(t *testing.T) {
	h := newDeployWireStack(t)
	if status, body := putRepoStatus(t, h, "t553-rem",
		`{"rclass":"remote","packageType":"maven","url":"http://127.0.0.1:9/upstream"}`); status != http.StatusOK {
		t.Fatalf("create remote: %d %s", status, body)
	}
	if status, body := putRepoStatus(t, h, "t553-loc",
		`{"rclass":"local","packageType":"maven"}`); status != http.StatusOK {
		t.Fatalf("create local: %d %s", status, body)
	}

	resp := h.do(http.MethodPut, "/binflow/t553-rem/com/diff/wd/maven-metadata.xml",
		adminUser, adminPass, []byte(gavPom), nil)
	if msg := errorEnvelopeMessage(t, resp); resp.StatusCode != http.StatusNotFound ||
		msg != "Could not find a local repository named t553-rem to deploy to." {
		t.Fatalf("remote metadata PUT = %d %q, want the 404 family", resp.StatusCode, msg)
	}

	// A local parent has no projection: the -cache spelling is just an
	// unknown repository, and it keeps the standard wording.
	resp = h.do(http.MethodPut, "/binflow/t553-loc-cache/com/diff/wd/1.0.0/wd-1.0.0.pom",
		adminUser, adminPass, []byte(gavPom), nil)
	if msg := errorEnvelopeMessage(t, resp); resp.StatusCode != http.StatusNotFound ||
		!strings.Contains(msg, "Failed to find the repository 't553-loc-cache' specified in the request.") {
		t.Fatalf("local -cache PUT = %d %q, want the standard unknown-repo 404", resp.StatusCode, msg)
	}
}

// TestDeployRefusalScopeDockerCargoWire pins the C7 a-arm's negative
// space on the product wire (BIN-78/T-596): docker and cargo remotes stay
// OUTSIDE the engine's 404 family — the ruling explicitly forbids flipping
// them — so their write refusals keep their own faces (docker's observed
// 400 families plus the standing 405 default arm, cargo's RE-05 405). Any
// widening of deployEngineRemote beyond maven+generic turns these red.
func TestDeployRefusalScopeDockerCargoWire(t *testing.T) {
	h := newDeployWireStack(t)
	if status, respBody := putRepoStatus(t, h, "t596-drem",
		`{"rclass":"remote","packageType":"docker","url":"http://127.0.0.1:9/upstream"}`); status != http.StatusOK {
		t.Fatalf("create t596-drem: %d %s", status, respBody)
	}
	// cargo rows seed straight into the store (the cargo fixture's own
	// posture): the REST create path gates on the license tier, which this
	// harness does not wire, and the legs below never reach config-time
	// validation.
	if err := h.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "t596-crem", Type: repo.TypeRemote, PackageType: "cargo",
		Config: `{"url":"http://127.0.0.1:9/upstream"}`,
	}); err != nil {
		t.Fatalf("seed t596-crem: %v", err)
	}

	tests := []struct {
		name      string
		method    string
		path      string
		wantCode  int
		wantAllow string
		wantInMsg string
	}{
		{"docker remote blob upload: the observed 400 family, never the engine 404",
			http.MethodPost, "/v2/t596-drem/img/blobs/uploads/", http.StatusBadRequest, "",
			"Unable to upload blobs to a remote repository."},
		{"docker remote blob delete: the standing 405 default arm + read-only wording",
			http.MethodDelete, "/v2/t596-drem/img/blobs/sha256:" + strings.Repeat("0", 64),
			http.StatusMethodNotAllowed, http.MethodGet,
			"Remote repository 't596-drem' is a read-only proxy cache"},
		{"cargo remote bare PUT: the service 405 through the shared chain",
			http.MethodPut, "/binflow/t596-crem/docs/readme.txt", http.StatusMethodNotAllowed, http.MethodGet,
			"Remote repository 't596-crem' is a read-only proxy cache; deployments to remote repositories are not accepted."},
		{"cargo remote publish face: the adapter's own 405 + Allow: GET",
			http.MethodPut, "/binflow/t596-crem/api/v1/crates/new", http.StatusMethodNotAllowed, http.MethodGet,
			"Remote repository 't596-crem' is a read-only proxy cache"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(tc.method, tc.path, adminUser, adminPass, []byte("x"), nil)
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("%s %s = %d (%s), want %d", tc.method, tc.path, resp.StatusCode, string(body), tc.wantCode)
			}
			if allow := resp.Header.Get("Allow"); allow != tc.wantAllow {
				t.Fatalf("%s %s Allow = %q, want %q", tc.method, tc.path, allow, tc.wantAllow)
			}
			if !strings.Contains(string(body), tc.wantInMsg) {
				t.Fatalf("%s %s body = %s, want it to contain %q", tc.method, tc.path, string(body), tc.wantInMsg)
			}
		})
	}
}
