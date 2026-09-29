package maven

// T-559 / BIN-41: the handle* policy refusal family — the A-form 409
// wording (contract maven/handle-policy-reject-409-wording-family) and the
// member GET class gate that shares it (contract
// maven/handle-policy-member-get-class-gate). Wire anchors:
// tools/difftest/v2/run/l033-r5-r{3,4} put_rel_to_hr_body /
// put_snap_to_hs_body / direct_get_{relpath_hr,snappath_hs}_body
// (live 7.161.26; the GET form's "; Path:" fill confirmed byte-identical
// to the message's inner reference up to the 300-char capture ceiling).

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// refusalEnvelope decodes the errors[] body one refusal leg produced.
func refusalEnvelope(t *testing.T, resp *http.Response) (int, string) {
	t.Helper()
	var env struct {
		Errors []struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(drain(t, resp), &env); err != nil || len(env.Errors) != 1 {
		t.Fatalf("errors[] envelope = %d entries (json err %v)", len(env.Errors), err)
	}
	return env.Errors[0].Status, env.Errors[0].Message
}

// TestHandlePolicyRefusalWordingFamily pins the PUT legs: both policy
// refusals answer 409 with the ONE A-form template — no per-leg policy
// phrase, "resolution" wording on the deploy leg too.
func TestHandlePolicyRefusalWordingFamily(t *testing.T) {
	hs := newHarness(t)
	legs := []struct {
		name string
		repo string // maven-relonly: handleReleases=false / maven-snaponly: handleSnapshots=false
		path string
	}{
		{"release PUT into handleReleases=false", "maven-relonly",
			"/maven-relonly/com/diff/hwr/1.0.0/hwr-1.0.0.pom"},
		{"snapshot PUT into handleSnapshots=false", "maven-snaponly",
			"/maven-snaponly/com/diff/hws/1.0.0-SNAPSHOT/hws-1.0.0-SNAPSHOT.pom"},
	}
	for _, tc := range legs {
		t.Run(tc.name, func(t *testing.T) {
			pom := []byte("<project><groupId>com.diff</groupId><artifactId>hwx</artifactId><version>1.0.0</version></project>")
			resp := hs.serve(http.MethodPut, tc.path, pom, nil, true)
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("PUT = %d (%s)", resp.StatusCode, drain(t, resp))
			}
			relPath := strings.TrimPrefix(tc.path, "/"+tc.repo+"/")
			status, msg := refusalEnvelope(t, resp)
			if status != http.StatusConflict {
				t.Errorf("errors[0].status = %d, want 409", status)
			}
			want := "The repository '" + tc.repo + "' rejected the resolution of an artifact '" +
				tc.repo + ":" + relPath + "' due to conflict in the snapshot release handling policy."
			if msg != want {
				t.Errorf("message =\n  %q\nwant the A-form literal\n  %q", msg, want)
			}
		})
	}
}

// TestHandlePolicyMemberGetClassGate pins the GET legs (contract
// maven/handle-policy-member-get-class-gate): the policy gate runs on the
// read path BEFORE the existence lookup — an unlanded conflicting-class
// path direct-read answers 409 with the wording family's GET form (the
// "; Path:" segment appended), never a plain 404 — plus the family's
// negative arms: non-conflicting classes serve, metadata stays exempt,
// the virtual face keeps walk semantics (skip, never a 409 pass-through).
func TestHandlePolicyMemberGetClassGate(t *testing.T) {
	hs := newHarness(t)
	ctx := t.Context()
	if _, err := hs.svc.CreateRepo(ctx, adminP, &metadata.Repo{
		RepoKey: "maven-vref", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["maven-relonly","maven-snaponly"]}`,
	}); err != nil {
		t.Fatalf("seed maven-vref: %v", err)
	}

	// Landed control bytes: a RELEASE pom on the snapshot-only home and a
	// SNAPSHOT pom on the release-only home — the non-conflicting classes.
	relPom := "/maven-snaponly/com/diff/ctl/2.0.0/ctl-2.0.0.pom"
	if resp := hs.serve(http.MethodPut, relPom,
		[]byte("<project><groupId>com.diff</groupId><artifactId>ctl</artifactId><version>2.0.0</version></project>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("land release on snapshot-only home = %d (%s)", resp.StatusCode, drain(t, resp))
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantMsg    string // "" = no exact-wording assertion
	}{
		{
			name:       "unlanded release GET on handleReleases=false member",
			path:       "/maven-relonly/com/diff/hwr/1.0.0/hwr-1.0.0.pom",
			wantStatus: http.StatusConflict,
			wantMsg: "The repository 'maven-relonly' rejected the resolution of an artifact " +
				"'maven-relonly:com/diff/hwr/1.0.0/hwr-1.0.0.pom' due to conflict in the snapshot " +
				"release handling policy.; Path: 'maven-relonly:com/diff/hwr/1.0.0/hwr-1.0.0.pom'",
		},
		{
			name:       "unlanded snapshot GET on handleSnapshots=false member",
			path:       "/maven-snaponly/com/diff/hws/1.0.0-SNAPSHOT/hws-1.0.0-SNAPSHOT.pom",
			wantStatus: http.StatusConflict,
			wantMsg: "The repository 'maven-snaponly' rejected the resolution of an artifact " +
				"'maven-snaponly:com/diff/hws/1.0.0-SNAPSHOT/hws-1.0.0-SNAPSHOT.pom' due to conflict " +
				"in the snapshot release handling policy.; Path: 'maven-snaponly:com/diff/hws/1.0.0-SNAPSHOT/hws-1.0.0-SNAPSHOT.pom'",
		},
		{
			name:       "unlanded timestamped snapshot GET refuses alike (write-gate taxonomy mirror)",
			path:       "/maven-snaponly/com/diff/hws/1.0.0-SNAPSHOT/hws-1.0.0-20240819.101500-1.pom",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "unlanded sidecar of the refused class refuses (companions follow)",
			path:       "/maven-relonly/com/diff/hwr/1.0.0/hwr-1.0.0.pom.sha1",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "landed non-conflicting class serves (gate is class x policy, not a blanket)",
			path:       relPom,
			wantStatus: http.StatusOK,
		},
		{
			name:       "unlanded SNAPSHOT on handleReleases=false member stays 404 (no conflict)",
			path:       "/maven-relonly/com/diff/hwr/1.0.0-SNAPSHOT/hwr-1.0.0-SNAPSHOT.pom",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unlanded metadata document stays exempt (ME-06, 404 not 409)",
			path:       "/maven-relonly/com/diff/hwr/maven-metadata.xml",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "virtual resolve keeps walk semantics (skip, never 409)",
			path:       "/maven-vref/com/diff/hwr/1.0.0/hwr-1.0.0.pom",
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := hs.serve(http.MethodGet, tc.path, nil, nil, true)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("GET = %d, want %d (%s)", resp.StatusCode, tc.wantStatus, drain(t, resp))
			}
			if tc.wantMsg == "" {
				return
			}
			_, msg := refusalEnvelope(t, resp)
			if msg != tc.wantMsg {
				t.Errorf("message =\n  %q\nwant\n  %q", msg, tc.wantMsg)
			}
		})
	}
}
