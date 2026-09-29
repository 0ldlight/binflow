package httpapi_test

// T-553 (BIN-35): the deploy-refusal family on the maven wire. The two
// fixed legs answer the upload engine's invalid-target 404
// (remote-cache-projection.md section 2.1's PUT row, live-confirmed against
// Artifactory 7.161.26 in L032 Arm 2): a PUT addressed to a maven remote's
// own key and a PUT addressed to its <K>-cache projection key both carry
// "Could not find a local repository named <requested-key> to deploy to."
// with the REQUESTED key (suffix spelling intact) in the named slot. The
// third leg is the §8.2 regression guard: an un-routed maven virtual keeps
// the 405 + Allow: GET + fixed wording. The scoping legs pin the narrow
// circle the fix was drawn into: generic-typed faces keep the shared
// service gate's 405 read-only refusal and the standard unknown-repo 404,
// and the anonymous challenge still precedes every refusal.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter/maven"
	"github.com/lzwzzy/binflow/internal/httpapi"
)

// gavPom is the GAV-consistent pom body the L032 Arm 2 probe rode (the
// path/body agreement keeps the maven policy gates out of the picture, so
// the assertion isolates the routing refusal itself).
const gavPom = `<?xml version="1.0" encoding="UTF-8"?>
<project><groupId>com.diff</groupId><artifactId>wd</artifactId><version>1.0.0</version></project>`

// newMavenWireStack mounts the REAL maven adapter beside the default two
// (cmd's assembly spelling) so the legs address the same wire a real mvn
// deploy would.
func newMavenWireStack(t *testing.T) *harness {
	t.Helper()
	return newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Adapters = append(d.Adapters,
			maven.New(d.ReposSvc, d.Metadata.Repos(), d.Metadata.Blobs(), d.Metadata.Nodes()))
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

// TestDeployRefusalFamilyMavenWire is the three live-confirmed legs of
// L032 Arm 2, asserted verbatim, plus the scoping legs.
func TestDeployRefusalFamilyMavenWire(t *testing.T) {
	h := newMavenWireStack(t)
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
		{"scope: generic remote keeps the shared service gate's 405 read-only refusal",
			"/binflow/t553-grem/x.bin", http.StatusMethodNotAllowed, http.MethodGet,
			"Remote repository 't553-grem' is a read-only proxy cache; deployments to remote repositories are not accepted."},
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
	// 1.2 step 4's ordering).
	resp := h.do(http.MethodPut, "/binflow/t553-rem/"+gav, "", "", []byte(gavPom), nil)
	drain(resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous remote PUT = %d, want the 401 challenge ahead of the refusal", resp.StatusCode)
	}
}

// TestDeployRefusalFamilyMavenWireMetadataToo pins that the remote-key
// refusal is target-resolution shaped, not artifact-shaped: a metadata
// document PUT to the remote key answers the same 404 family (the engine
// resolves the target before any layout or policy step), while the local
// parent spelling of -cache keeps the standard unknown-repo wording.
func TestDeployRefusalFamilyMavenWireMetadataToo(t *testing.T) {
	h := newMavenWireStack(t)
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
