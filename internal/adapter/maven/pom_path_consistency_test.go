package maven

// T-543 (D-4, L030 case 2): the pom-coordinates-vs-path consistency gate.
// A .pom deploy whose content GAV disagrees with the deployment path is
// 409-refused before anything lands — errors[] envelope, the reference's
// message verbatim (captured live on 7.161.26, virtual and local legs
// identical) — and suppressPomConsistencyChecks=true (the knob httpapi has
// rendered all along) skips the check.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// pomGAV renders a minimal pom (the _mavenlib.pom_fixture shape, default
// namespace included — the parser must be namespace-tolerant).
func pomGAV(group, artifact, version string) []byte {
	return []byte(fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8"?><project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><groupId>%s</groupId><artifactId>%s</artifactId><version>%s</version><packaging>jar</packaging></project>`,
		group, artifact, version))
}

func wantMismatchMessage(relPath, wantPrefix string) string {
	return fmt.Sprintf(
		"The target deployment path '%s' does not match the POM's expected path prefix '%s'. "+
			"Please verify your POM content for correctness and make sure the source path is a valid Maven repository root path.",
		relPath, wantPrefix)
}

// TestPomPathConsistencyGate is the mismatch family: every disagreeing
// coordinate (artifactId, groupId, version) is a 409 with the reference's
// exact message, and the path stays unlanded (GET 404 afterwards).
func TestPomPathConsistencyGate(t *testing.T) {
	hs := newHarness(t)
	cases := []struct {
		name       string
		path       string // under maven-local
		pom        []byte
		wantPrefix string
	}{
		{
			name:       "artifactId disagrees (the L030 shape)",
			path:       "com/diff/wrong-lib/1.0.0/wrong-lib-1.0.0.pom",
			pom:        pomGAV("com.diff", "right-lib", "1.0.0"),
			wantPrefix: "com/diff/right-lib/1.0.0",
		},
		{
			name:       "groupId disagrees",
			path:       "com/diff/lib/1.0.0/lib-1.0.0.pom",
			pom:        pomGAV("com.other", "lib", "1.0.0"),
			wantPrefix: "com/other/lib/1.0.0",
		},
		{
			name:       "version disagrees",
			path:       "com/diff/lib/1.0.0/lib-1.0.0.pom",
			pom:        pomGAV("com.diff", "lib", "2.0.0"),
			wantPrefix: "com/diff/lib/2.0.0",
		},
	}
	for _, tc := range cases {
		resp := hs.serve(http.MethodPut, "/maven-local/"+tc.path, tc.pom, nil, true)
		body := drain(t, resp)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%s: PUT = %d (%s), want 409", tc.name, resp.StatusCode, body)
		}
		var env struct {
			Errors []struct {
				Status  int    `json:"status"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("%s: body is not the errors[] envelope: %v (%s)", tc.name, err, body)
		}
		want := wantMismatchMessage(tc.path, tc.wantPrefix)
		if len(env.Errors) != 1 || env.Errors[0].Status != 409 || env.Errors[0].Message != want {
			t.Errorf("%s: errors[] = %+v, want one 409 with the reference message %q", tc.name, env.Errors, want)
		}
		// Nothing landed: the addressed path stays absent on the member.
		if get := hs.serve(http.MethodGet, "/maven-local/"+tc.path, nil, nil, true); get.StatusCode != http.StatusNotFound {
			t.Errorf("%s: rejected path GET = %d, want the unlanded 404", tc.name, get.StatusCode)
		}
	}
}

// TestPomPathConsistencyAccepts is the no-false-positive family: matching
// coordinates (with and without xmlns, with parent-inherited
// groupId/version), a coordinate-incomplete pom and a non-pom artifact all
// deploy 201.
func TestPomPathConsistencyAccepts(t *testing.T) {
	hs := newHarness(t)
	cases := []struct {
		name string
		path string
		pom  []byte
	}{
		{"matching GAV (xmlns form)", "com/acme/demo-app/1.0.0/demo-app-1.0.0.pom", pomGAV("com.acme", "demo-app", "1.0.0")},
		{"matching GAV (bare form)", "com/acme/demo-app/1.1.0/demo-app-1.1.0.pom",
			[]byte("<project><groupId>com.acme</groupId><artifactId>demo-app</artifactId><version>1.1.0</version></project>")},
		{"parent-inherited groupId and version", "com/parent/child/2.0.0/child-2.0.0.pom",
			[]byte("<project><parent><groupId>com.parent</groupId><artifactId>parent</artifactId><version>2.0.0</version></parent><artifactId>child</artifactId></project>")},
		{"coordinate-incomplete pom deploys unchecked", "com/acme/bare/3.0.0/bare-3.0.0.pom", []byte("<project/>")},
	}
	for _, tc := range cases {
		resp := hs.serve(http.MethodPut, "/maven-local/"+tc.path, tc.pom, nil, true)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s: PUT = %d (%s), want 201", tc.name, resp.StatusCode, drain(t, resp))
		}
	}
	// A jar is not gated at all (only .pom documents carry coordinates).
	if resp := hs.serve(http.MethodPut, jarPath, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("jar PUT = %d, want the ungated 201", resp.StatusCode)
	}
}

// TestPomPathConsistencySuppress is the knob's two states on the same
// mismatch: default false refuses 409, true lands it 201 (the A-side
// behavior captured live: suppress=true answers 201 with the created
// envelope).
func TestPomPathConsistencySuppress(t *testing.T) {
	ctx := context.Background()
	hs := newHarness(t)
	const (
		badPath = "com/diff/wrong-lib/1.0.0/wrong-lib-1.0.0.pom"
		badPom  = `<project><groupId>com.diff</groupId><artifactId>right-lib</artifactId><version>1.0.0</version></project>`
	)
	// Default (knob absent): refuse.
	resp := hs.serve(http.MethodPut, "/maven-local/"+badPath, []byte(badPom), nil, true)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("default-state mismatch PUT = %d (%s), want 409", resp.StatusCode, drain(t, resp))
	}
	// Knob on: the same mismatch lands.
	if _, err := hs.svc.CreateRepo(ctx, adminP, &metadata.Repo{
		RepoKey: "maven-suppress", Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"suppressPomConsistencyChecks":true}`,
	}); err != nil {
		t.Fatalf("CreateRepo(maven-suppress): %v", err)
	}
	resp = hs.serve(http.MethodPut, "/maven-suppress/"+badPath, []byte(badPom), nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("suppress=true mismatch PUT = %d (%s), want 201", resp.StatusCode, drain(t, resp))
	}
	if get := hs.serve(http.MethodGet, "/maven-suppress/"+badPath, nil, nil, true); get.StatusCode != http.StatusOK {
		t.Fatalf("suppressed deploy GET = %d, want the landed 200", get.StatusCode)
	}
}

// TestPomPathConsistencyVirtualRoute is the L030 case 2 topology: the
// mismatched pom goes through a VIRTUAL into its defaultDeploymentRepo —
// the refusal names the addressed path and the member stays clean.
func TestPomPathConsistencyVirtualRoute(t *testing.T) {
	ctx := context.Background()
	hs := newHarness(t)
	if _, err := hs.svc.CreateRepo(ctx, adminP, &metadata.Repo{
		RepoKey: "maven-gavv", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["maven-local"],"defaultDeploymentRepo":"maven-local"}`,
	}); err != nil {
		t.Fatalf("CreateRepo(maven-gavv): %v", err)
	}
	const path = "com/diff/wrong-lib/1.0.0/wrong-lib-1.0.0.pom"
	resp := hs.serve(http.MethodPut, "/maven-gavv/"+path, pomGAV("com.diff", "right-lib", "1.0.0"), nil, true)
	body := drain(t, resp)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("virtual-routed mismatch PUT = %d (%s), want 409", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), wantMismatchMessage(path, "com/diff/right-lib/1.0.0")) {
		t.Errorf("virtual-routed refusal message = %s, want the reference wording", body)
	}
	for _, absent := range []string{"/maven-gavv/" + path, "/maven-local/" + path} {
		if get := hs.serve(http.MethodGet, absent, nil, nil, true); get.StatusCode != http.StatusNotFound {
			t.Errorf("rejected path %s GET = %d, want the unlanded 404", absent, get.StatusCode)
		}
	}
}
