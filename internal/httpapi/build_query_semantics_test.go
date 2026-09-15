// The query ladder's L023-2A semantics (ticket L023-2A, FR-152.2 /
// build-info.md §11.3/§11.8 — the errata-corrected contract): the
// absolute top-level URIs with the ?buildRepo= query string, the
// repo-scoped addressing (custom key vs the default), the detail face's
// original-timezone started echo, durationMillis' 0 default, the slim
// strip, the statuses[] wire shape with timestampDate and the omitted
// nullable fields, and the family's verbatim 404s including the started
// clause.

package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestBuildQueryAbsoluteURIsEchoBuildRepo: §11.3 — every top-level uri of
// the ladder is the ABSOLUTE URL plus ?buildRepo=<resolved repo>; the
// row URIs stay relative ("/<name>", "/<number>").
func TestBuildQueryAbsoluteURIsEchoBuildRepo(t *testing.T) {
	h := newBuildHarness(t)
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	_, doc := getBuild(t, h, "/binflow/api/build", adminUser, adminPass)
	wantBase := `"uri": "` + h.srv.URL + `/binflow/api/build?buildRepo=artifactory-build-info"`
	if !strings.Contains(doc, wantBase) {
		t.Fatalf("names top-level uri missing the absolute+buildRepo form: %s", doc)
	}

	_, doc = getBuild(t, h, "/binflow/api/build/pub-app", adminUser, adminPass)
	if !strings.Contains(doc, `"uri": "`+h.srv.URL+`/binflow/api/build/pub-app?buildRepo=artifactory-build-info"`) {
		t.Fatalf("numbers top-level uri wrong: %s", doc)
	}

	_, doc = getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	if !strings.Contains(doc, `"uri": "`+h.srv.URL+`/binflow/api/build/pub-app/51?buildRepo=artifactory-build-info"`) {
		t.Fatalf("detail top-level uri wrong: %s", doc)
	}

	// A custom ?buildRepo= addresses that key — the default repo's builds
	// answer the family's 404 under a key that holds nothing.
	resp, doc := getBuild(t, h, "/binflow/api/build?buildRepo=team-build-info", adminUser, adminPass)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(doc, "No builds were found") {
		t.Fatalf("custom empty repo = %d %s, want 404 No builds were found", resp.StatusCode, doc)
	}
}

// TestBuildQueryDetailEchoesOriginalTimezone: §11.3's split — the list
// faces echo the UTC-normalized literal while the DETAIL face echoes the
// payload's original timezone verbatim (a +0800 upload echoes +0800).
func TestBuildQueryDetailEchoesOriginalTimezone(t *testing.T) {
	h := newBuildHarness(t)
	doc := strings.Replace(buildRESTDoc,
		`"started": "2026-09-07T10:00:00.000+0000"`,
		`"started": "2026-09-07T18:00:00.000+0800"`, 1)
	if code, _, _ := putBuildJSON(t, h, doc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	// List faces: normalized (+0800 in, +0000 out).
	_, names := getBuild(t, h, "/binflow/api/build", adminUser, adminPass)
	if !strings.Contains(names, `"lastStarted": "2026-09-07T10:00:00.000+0000"`) {
		t.Fatalf("names lastStarted not UTC-normalized: %s", names)
	}
	_, numbers := getBuild(t, h, "/binflow/api/build/pub-app", adminUser, adminPass)
	if !strings.Contains(numbers, `"started": "2026-09-07T10:00:00.000+0000"`) {
		t.Fatalf("numbers started not UTC-normalized: %s", numbers)
	}

	// Detail: the original offset rides back verbatim.
	_, detail := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	if !strings.Contains(detail, `"started": "2026-09-07T18:00:00.000+0800"`) {
		t.Fatalf("detail started not the original timezone: %s", detail)
	}
}

// TestBuildQueryDetailDefaultsAndSlim: durationMillis echoes 0 when the
// document carried none (§3.1); ?slim=true answers modules [] and
// properties null (the jf CLI consumption shape).
func TestBuildQueryDetailDefaultsAndSlim(t *testing.T) {
	h := newBuildHarness(t)
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	_, detail := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	if !strings.Contains(detail, `"durationMillis": 0`) {
		t.Fatalf("durationMillis default 0 missing: %s", detail)
	}
	var full struct {
		BuildInfo struct {
			Properties map[string]string `json:"properties"`
		} `json:"buildInfo"`
	}
	if err := json.Unmarshal([]byte(detail), &full); err != nil || full.BuildInfo.Properties["env"] != "prod" {
		t.Fatalf("full detail lost properties: %s (%v)", detail, err)
	}

	_, slim := getBuild(t, h, "/binflow/api/build/pub-app/51?slim=true", adminUser, adminPass)
	if !strings.Contains(slim, `"modules": []`) || !strings.Contains(slim, `"properties": null`) {
		t.Fatalf("slim shape wrong: %s", slim)
	}
}

// TestBuildQueryStatusesWireShape: §3.1's statuses[] echo — status,
// timestamp, timestampDate (epoch-ms), comment and user always;
// repository/ciUser omitted whole when the promotion carried none.
func TestBuildQueryStatusesWireShape(t *testing.T) {
	h := newBuildHarness(t)
	ctx := context.Background()
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}
	const promotedAt = "2026-09-08T09:00:00Z"
	if err := h.md.Builds().AppendPromotion(ctx, &metadata.BuildPromotion{
		ID: "t-1", Name: "pub-app", Number: "51",
		Started: "2026-09-07T10:00:00.000+0000",
		Status:  "released", Comment: "ship it", PromotedBy: "admin", PromotedAt: promotedAt,
	}); err != nil {
		t.Fatalf("seed promotion: %v", err)
	}

	_, detail := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	var parsed struct {
		BuildInfo struct {
			Statuses []map[string]any `json:"statuses"`
		} `json:"buildInfo"`
	}
	if err := json.Unmarshal([]byte(detail), &parsed); err != nil {
		t.Fatalf("detail JSON: %v (%s)", err, detail)
	}
	if len(parsed.BuildInfo.Statuses) != 1 {
		t.Fatalf("statuses = %+v, want the one promotion: %s", parsed.BuildInfo.Statuses, detail)
	}
	st := parsed.BuildInfo.Statuses[0]
	if st["status"] != "released" || st["user"] != "admin" || st["comment"] != "ship it" {
		t.Fatalf("statuses core fields = %+v", st)
	}
	if st["timestamp"] != promotedAt {
		t.Fatalf("statuses timestamp = %v", st["timestamp"])
	}
	wantMS, err := time.Parse(time.RFC3339, promotedAt)
	if err != nil {
		t.Fatalf("fixture timestamp: %v", err)
	}
	if td, ok := st["timestampDate"].(float64); !ok || int64(td) != wantMS.UnixMilli() {
		t.Fatalf("timestampDate = %v, want the epoch-ms of %s", st["timestampDate"], promotedAt)
	}
	if _, ok := st["repository"]; ok {
		t.Fatalf("repository must be omitted whole when unset: %+v", st)
	}
	if _, ok := st["ciUser"]; ok {
		t.Fatalf("ciUser must be omitted whole when unset: %+v", st)
	}
}

// TestBuildQueryDetail404WithStartedClause: the detail miss message
// carries the addressed coordinates, the started clause only when the
// caller disambiguated (§1 detail row's verbatim shape).
func TestBuildQueryDetail404WithStartedClause(t *testing.T) {
	h := newBuildHarness(t)
	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	resp, doc := getBuild(t, h, "/binflow/api/build/pub-app/99", adminUser, adminPass)
	if resp.StatusCode != http.StatusNotFound ||
		!strings.Contains(doc, "No build was found for build name: pub-app, build number: 99") ||
		strings.Contains(doc, "build started") {
		t.Fatalf("plain detail 404 = %d %s", resp.StatusCode, doc)
	}

	resp, doc = getBuild(t, h,
		"/binflow/api/build/pub-app/51?started=2026-09-07T09:00:00.000%2B0000", adminUser, adminPass)
	if resp.StatusCode != http.StatusNotFound ||
		!strings.Contains(doc, "No build was found for build name: pub-app, build number: 51, build started: 2026-09-07T09:00:00.000+0000") {
		t.Fatalf("started-clause detail 404 = %d %s", resp.StatusCode, doc)
	}
}
