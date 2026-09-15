// The append and promote faces' L023-2B semantics (ticket L023-2B,
// FR-152.2 / build-info.md §11.4/§11.5 — the errata-corrected contract):
// append's list-concatenation law with DUPLICATE same-id entries echoed,
// promote's status-only INFO row, the verbatim target-404, the E12
// failFast/lenient pair at 400/200, the §11.5-8 status-update skip rows,
// and the statuses[] wire shape the promotion history renders.

package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestAppendDuplicatesEchoAsSeparateEntries: E5's diff assertion — appending
// the SAME module id twice lands TWO entries, each holding exactly its own
// rows; nothing merges, nothing deduplicates, order is seeded-then-appended.
func TestAppendDuplicatesEchoAsSeparateEntries(t *testing.T) {
	h := newBuildHarness(t)
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	same := `[{"id":"com.example:api:1.0","artifacts":[{"type":"pom","name":"api-1.0.pom"}]}]`
	for i := 0; i < 2; i++ {
		resp := h.do(http.MethodPost, "/binflow/api/build/append/pub-app/51",
			adminUser, adminPass, []byte(same),
			map[string]string{"Content-Type": "application/json"})
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("append %d = %d, want 204", i+1, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	_, doc := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	var detail struct {
		BuildInfo struct {
			Modules []struct {
				ID        string `json:"id"`
				Artifacts []struct {
					Name string `json:"name"`
				} `json:"artifacts"`
				Dependencies []struct {
					ID string `json:"id"`
				} `json:"dependencies"`
			} `json:"modules"`
		} `json:"buildInfo"`
	}
	if err := json.Unmarshal([]byte(doc), &detail); err != nil {
		t.Fatalf("detail JSON: %v (%s)", err, doc)
	}
	apiRows := 0
	for _, m := range detail.BuildInfo.Modules {
		if m.ID != "com.example:api:1.0" {
			continue
		}
		apiRows++
		if apiRows > 1 {
			// The duplicates: exactly the appended pom, nothing inherited
			// from the seeded row (no merge).
			if len(m.Artifacts) != 1 || m.Artifacts[0].Name != "api-1.0.pom" || len(m.Dependencies) != 0 {
				t.Fatalf("duplicate api row %d = %+v, want exactly its own pom", apiRows, m)
			}
		}
	}
	if apiRows != 3 { // 1 seeded + 2 appended
		t.Fatalf("api module appears %d times, want 3 (E5 duplicates): %s", apiRows, doc)
	}
}

// TestPromoteStatusOnlyVerbatimRowAndStatusesWire: the no-target arm's
// live-pinned INFO sentence, then the history row's statuses[] echo — the
// SSSZ timestamp, its timestampDate twin, user, comment; repository only
// when a promotion named a target.
func TestPromoteStatusOnlyVerbatimRowAndStatusesWire(t *testing.T) {
	h := newBuildHarness(t)
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	resp, body := promoteViaREST(t, h, adminUser, adminPass, "pub-app", "51",
		`{"status":"released","comment":"ship it"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status-only promote = %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"level": "INFO"`) ||
		!strings.Contains(body, "Skipping build item relocation: no target repository selected.") {
		t.Fatalf("status-only messages = %s, want the §11.5-4 verbatim INFO row", body)
	}

	_, doc := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	var detail struct {
		BuildInfo struct {
			Statuses []map[string]any `json:"statuses"`
		} `json:"buildInfo"`
	}
	if err := json.Unmarshal([]byte(doc), &detail); err != nil {
		t.Fatalf("detail JSON: %v (%s)", err, doc)
	}
	if len(detail.BuildInfo.Statuses) != 1 {
		t.Fatalf("statuses = %+v, want the one promotion: %s", detail.BuildInfo.Statuses, doc)
	}
	st := detail.BuildInfo.Statuses[0]
	if st["status"] != "released" || st["comment"] != "ship it" || st["user"] != "admin" {
		t.Fatalf("statuses core = %+v", st)
	}
	ts, _ := st["timestamp"].(string)
	// The SSSZ form (server-clock millis render, +0000 zone).
	if len(ts) != len("2026-09-07T10:00:00.000+0000") || ts[10] != 'T' ||
		!strings.HasSuffix(ts, "+0000") || ts[19] != '.' {
		t.Fatalf("statuses timestamp = %q, want the SSSZ form", ts)
	}
	if td, ok := st["timestampDate"].(float64); !ok || td <= 0 {
		t.Fatalf("timestampDate = %v, want epoch-ms", st["timestampDate"])
	}
	if _, ok := st["repository"]; ok {
		t.Fatalf("repository must be omitted on the status-only arm: %+v", st)
	}
	if _, ok := st["ciUser"]; ok {
		t.Fatalf("ciUser must be omitted when the body carried none: %+v", st)
	}
}

// TestPromoteE12FailFastAndLenientArms: the missing-artifact pair — the
// default failFast answers the messages body at 400 with the aborting
// sentence plus the status-update skip notice; failFast=false rides ONE
// names warning on a 200 and the status update STILL lands.
func TestPromoteE12FailFastAndLenientArms(t *testing.T) {
	h := newBuildHarness(t)
	seedLocalGenericRepo(t, h, "rel-libs")
	// The artifact's path names no live node: record-only, §11.5-5's
	// unresolvable artifact.
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	// Diff D1: the abort-class refusal rides the errors[] envelope — the
	// aborting sentence verbatim, no messages body.
	resp, body := promoteViaREST(t, h, adminUser, adminPass, "pub-app", "51",
		`{"status":"released","targetRepo":"rel-libs"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("failFast promote = %d %s, want 400", resp.StatusCode, body)
	}
	var env struct {
		Errors []struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal([]byte(body), &env) != nil || len(env.Errors) != 1 || env.Errors[0].Status != 400 ||
		env.Errors[0].Message != "Unable to find artifacts of build 'pub-app' #51 from artifactory-build-info repo: aborting promotion." {
		t.Fatalf("failFast body = %s, want the errors[] envelope with the verbatim sentence", body)
	}

	resp, body = promoteViaREST(t, h, adminUser, adminPass, "pub-app", "51",
		`{"status":"released","targetRepo":"rel-libs","failFast":false}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("lenient promote = %d %s, want 200", resp.StatusCode, body)
	}
	var lenient struct {
		Messages []struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		} `json:"messages"`
	}
	if json.Unmarshal([]byte(body), &lenient) != nil || len(lenient.Messages) != 1 ||
		lenient.Messages[0].Level != "WARNING" ||
		lenient.Messages[0].Message != "Unable to find the following artifacts of build 'pub-app' #51: api-1.0.jar" {
		t.Fatalf("lenient messages = %s, want exactly the one E12 names warning (diff D2: no summary row)", body)
	}

	// The lenient promotion's history row landed (E12: warning 后继续).
	_, doc := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	if !strings.Contains(doc, `"status": "released"`) {
		t.Fatalf("statuses missing the lenient promotion: %s", doc)
	}
}

// TestPromoteTarget404VerbatimOnTheWire: §11.5-4's live-pinned 404 — the
// caller's key, no envelope decoration beyond the family's own.
func TestPromoteTarget404VerbatimOnTheWire(t *testing.T) {
	h := newBuildHarness(t)
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}
	resp, body := promoteViaREST(t, h, adminUser, adminPass, "pub-app", "51",
		`{"status":"x","targetRepo":"no-such-repo"}`)
	if resp.StatusCode != http.StatusNotFound ||
		!strings.Contains(body, "Cannot find target repository by the key 'no-such-repo'") {
		t.Fatalf("missing target = %d %s, want the verbatim 404", resp.StatusCode, body)
	}
}

// TestBuild403WordingFamily: §7's verbatim denials on the wire — the
// read/upload/delete sentences interpolated with the acting user, clean of
// wrap prefixes (eve holds nothing anywhere).
func TestBuild403WordingFamily(t *testing.T) {
	h := newBuildHarness(t)
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	cases := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{"read", http.MethodGet, "/binflow/api/build/pub-app/51",
			"The user: 'eve' is not authorized to access build info. Read permission is needed."},
		{"upload", http.MethodPut, "/binflow/api/build",
			"The user: 'eve' is not authorized to upload build info. Upload permission is needed."},
		{"delete", http.MethodDelete, "/binflow/api/build/pub-app?buildNumbers=51",
			"The user: 'eve' is not authorized to delete build info. Delete permission is needed."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload []byte
			if tc.method == http.MethodPut {
				payload = []byte(buildRESTDoc)
			}
			resp := h.do(tc.method, tc.path, "eve", "pw", payload,
				map[string]string{"Content-Type": "application/json"})
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s = %d (%s), want 403", tc.name, resp.StatusCode, body)
			}
			var rendered struct {
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			}
			if json.Unmarshal(body, &rendered) != nil || len(rendered.Errors) != 1 ||
				rendered.Errors[0].Message != tc.want {
				t.Fatalf("%s body = %s, want the EXACT §7 sentence %q", tc.name, body, tc.want)
			}
		})
	}

	// The overwrite arm's delete sentence: bob holds w but not d — the
	// re-publish of the existing coordinate is the delete-gated arm.
	resp := h.do(http.MethodPost, "/binflow/api/build/append/pub-app/51", "carol", "pw",
		[]byte(`[]`), map[string]string{"Content-Type": "application/json"})
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body),
		"The user: 'carol' is not authorized to delete build info. Delete permission is needed.") {
		t.Fatalf("append denial = %d %s, want the §7 delete sentence first (§11.4-2 order)", resp.StatusCode, body)
	}
}
