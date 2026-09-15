// The batch-deletion and retention faces' L023-2C semantics (ticket
// L023-2C, FR-152.2 / build-info.md §11.6/§11.7 — the E6/E7/E9/E10
// contract): the POST /build/delete body twin over the SAME deletion
// command (special-character numbers a CSV cannot carry), retention's
// count gate at the verbatim text/plain 400, the two-pass window with the
// promoted exemption, the publish-tail trigger, and the synchronous
// default.

package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// postBuildDelete issues the batch-deletion POST and drains the body.
func postBuildDelete(t *testing.T, h *harness, body string) (int, string) {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/build/delete", adminUser, adminPass,
		[]byte(body), map[string]string{"Content-Type": "application/json"})
	return drainBuildBody(t, resp)
}

// drainBuildBody reads one response to its string form.
func drainBuildBody(t *testing.T, resp *http.Response) (int, string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, string(body)
}

// seedRetRun publishes one run of "ret-c" at the given number/started and
// returns nothing (the numbers face verifies survival).
func seedRetRun(t *testing.T, h *harness, number, started string) {
	t.Helper()
	doc := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": "ret-c"`, 1)
	doc = strings.Replace(doc, `"number": "51"`, `"number": `+quoteJSON(number), 1)
	doc = strings.Replace(doc, `"started": "2026-09-07T10:00:00.000+0000"`, `"started": `+quoteJSON(started), 1)
	if code, _, _ := putBuildJSON(t, h, doc); code != http.StatusNoContent {
		t.Fatalf("seed %s = %d, want 204", number, code)
	}
}

// postRetention issues the retention POST and drains the body.
func postRetention(t *testing.T, h *harness, name, query, body string) (int, string) {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/build/retention/"+name+query,
		adminUser, adminPass, []byte(body), map[string]string{"Content-Type": "application/json"})
	return drainBuildBody(t, resp)
}

// retNumbersLeft lists the surviving numbers of one build name.
func retNumbersLeft(t *testing.T, h *harness, name string) string {
	t.Helper()
	resp, doc := getBuild(t, h, "/binflow/api/build/"+name, adminUser, adminPass)
	if resp.StatusCode != http.StatusOK {
		return "404:" + doc
	}
	var parsed struct {
		Numbers []struct {
			URI string `json:"uri"`
		} `json:"buildsNumbers"`
	}
	if err := json.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatalf("numbers JSON: %v (%s)", err, doc)
	}
	var out []string
	for _, n := range parsed.Numbers {
		out = append(out, strings.TrimPrefix(n.URI, "/"))
	}
	return strings.Join(out, ",")
}

// TestBatchDeleteBodyTwin: POST /build/delete rides the SAME command and
// wording family as the path face — the E6 partial-hit pair verbatim,
// deleteAll's own sentence, the two-branch 404, and the ARRAY form's
// special-character numbers (the endpoint's reason to exist).
func TestBatchDeleteBodyTwin(t *testing.T) {
	h := newBuildHarness(t)
	seedRetRun(t, h, "3", "2026-09-07T10:00:00.000+0000")
	seedRetRun(t, h, "1.0.0-rc+1", "2026-09-07T11:00:00.000+0000") // a CSV-hostile number

	code, body := postBuildDelete(t, h,
		`{"buildName":"ret-c","buildNumbers":["3","99","1.0.0-rc+1"]}`)
	if code != http.StatusOK {
		t.Fatalf("batch delete = %d %s, want 200", code, body)
	}
	want := "The following builds have been deleted successfully: 'ret-c#3', 'ret-c#1.0.0-rc+1'.\n" +
		"Warning - the following builds could not be removed: '99'.\n"
	if body != want {
		t.Fatalf("batch delete body:\n got %q\nwant %q", body, want)
	}

	// The 404 pair: missing name vs all-missing numbers (a surviving run
	// keeps the name alive for the second branch).
	seedRetRun(t, h, "7", "2026-09-07T12:00:00.000+0000")
	code, body = postBuildDelete(t, h, `{"buildName":"ghost-c","buildNumbers":["1"]}`)
	if code != http.StatusNotFound || !strings.Contains(body, "Unable to find build 'ghost-c'") {
		t.Fatalf("missing name = %d %s", code, body)
	}
	code, body = postBuildDelete(t, h, `{"buildName":"ret-c","buildNumbers":["8","9"]}`)
	if code != http.StatusNotFound || !strings.Contains(body, "Unable to find the given build numbers") {
		t.Fatalf("all-missing numbers = %d %s", code, body)
	}

	// The 400 floors: blank name (the body face's own arm — the path face
	// cannot reach it) and no numbers without deleteAll.
	code, body = postBuildDelete(t, h, `{"buildNumbers":["1"]}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "Please state the name of the build to be removed") {
		t.Fatalf("blank name = %d %s", code, body)
	}
	code, body = postBuildDelete(t, h, `{"buildName":"ret-c"}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "Please provide at least one build number to delete") {
		t.Fatalf("no numbers = %d %s", code, body)
	}

	// deleteAll over the body form, its no-period sentence, and the empty
	// view left behind.
	code, body = postBuildDelete(t, h, `{"buildName":"ret-c","deleteAll":true}`)
	if code != http.StatusOK ||
		body != "All builds 'ret-c' under 'artifactory-build-info' have been deleted successfully" {
		t.Fatalf("deleteAll = %d %q", code, body)
	}
	if resp, _ := getBuild(t, h, "/binflow/api/build/ret-c", adminUser, adminPass); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("post-deleteAll numbers: want 404")
	}
}

// TestRetentionCountGateVerbatim: §11.6-1's positive gate — an explicit
// count=0 (and a missing body) answers the spec's verbatim sentence at
// 400 text/plain; count absent in a present body is the -1 no-window.
func TestRetentionCountGateVerbatim(t *testing.T) {
	h := newBuildHarness(t)
	seedRetRun(t, h, "1", "2026-09-07T10:00:00.000+0000")

	code, body := postRetention(t, h, "ret-c", "", `{"count":0}`)
	if code != http.StatusBadRequest || body != "Max count retention needs to be a positive number" {
		t.Fatalf("count=0 = %d %q, want the verbatim text/plain 400", code, body)
	}

	// A missing body is the same gate.
	resp := h.do(http.MethodPost, "/binflow/api/build/retention/ret-c", adminUser, adminPass,
		nil, map[string]string{"Content-Type": "application/json"})
	code, body = drainBuildBody(t, resp)
	if code != http.StatusBadRequest || body != "Max count retention needs to be a positive number" {
		t.Fatalf("missing body = %d %q, want the same verbatim 400", code, body)
	}

	// count absent (a floor-only window): legal, zero deletions, 204.
	code, body = postRetention(t, h, "ret-c", "", `{"minimumBuildDate":"2020-01-01T00:00:00Z"}`)
	if code != http.StatusNoContent || strings.TrimSpace(body) != "" {
		t.Fatalf("floor-only = %d %q, want 204 empty", code, body)
	}
}

// TestRetentionTwoPassOrderAndPromotedExemption: §11.6-4's independent
// passes — the date pass's victims join no count arithmetic (the newest
// floor-deleted run does NOT eat the count slot), and §11.6-5's promoted
// exemption survives the window without occupying a slot.
func TestRetentionTwoPassOrderAndPromotedExemption(t *testing.T) {
	h := newBuildHarness(t)
	// newest → oldest: 5 is OUTSIDE the floor; 4 carries a promotion
	// (exempt); 3, 2, 1 inside. count=1 must keep exactly {4 (exempt), 3
	// (the one count slot)} — pass 1 deletes 5 WITHOUT consuming the slot.
	seedRetRun(t, h, "1", "2026-09-01T10:00:00.000+0000")
	seedRetRun(t, h, "2", "2026-09-02T10:00:00.000+0000")
	seedRetRun(t, h, "3", "2026-09-03T10:00:00.000+0000")
	seedRetRun(t, h, "4", "2026-09-04T10:00:00.000+0000")
	seedRetRun(t, h, "5", "2026-08-31T10:00:00.000+0000") // outside the floor
	if err := h.md.Builds().AppendPromotion(t.Context(), &metadata.BuildPromotion{
		ID: "c-1", Name: "ret-c", Number: "4", Started: "2026-09-04T10:00:00.000+0000",
		Status: "released", PromotedBy: "admin", PromotedAt: "2026-09-05T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed promotion: %v", err)
	}

	// The SYNC default (no ?async): the deletions answer before the 204.
	code, body := postRetention(t, h, "ret-c", "",
		`{"count":1,"minimumBuildDate":"2026-09-01T00:00:00Z"}`)
	if code != http.StatusNoContent || strings.TrimSpace(body) != "" {
		t.Fatalf("retention = %d %q, want the sync 204 empty", code, body)
	}
	if left := retNumbersLeft(t, h, "ret-c"); left != "4,3" {
		t.Fatalf("survivors = %q, want 4 (promoted-exempt) and 3 (the count slot)", left)
	}
}

// TestRetentionPublishTailTrigger: §11.6-8 — a document carrying a
// buildRetention block runs its own window at the tail of the publication
// (the just-published older run leaves immediately, the fresh one stays).
func TestRetentionPublishTailTrigger(t *testing.T) {
	h := newBuildHarness(t)
	seedRetRun(t, h, "2", "2026-09-07T10:00:00.000+0000")

	doc := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": "ret-c"`, 1)
	doc = strings.Replace(doc, `"number": "51"`, `"number": "1"`, 1)
	doc = strings.Replace(doc, `"started": "2026-09-07T10:00:00.000+0000"`,
		`"started": "2026-09-01T10:00:00.000+0000", "buildRetention": {"count": 1}`, 1)
	if code, _, _ := putBuildJSON(t, h, doc); code != http.StatusNoContent {
		t.Fatalf("publish with retention block = %d, want 204", code)
	}
	// Poll-free assertion: the tail runs SYNCHRONOUSLY — the older run is
	// already gone when the PUT answers.
	if left := retNumbersLeft(t, h, "ret-c"); left != "2" {
		t.Fatalf("survivors after publish-tail window = %q, want only the fresh run 2", left)
	}
}

// TestRetentionAsyncArmDetaches: ?async=true answers 204 immediately and
// the window completes in the background (polled).
func TestRetentionAsyncArmDetaches(t *testing.T) {
	h := newBuildHarness(t)
	seedRetRun(t, h, "1", "2026-09-07T10:00:00.000+0000")
	seedRetRun(t, h, "2", "2026-09-07T11:00:00.000+0000")

	code, _ := postRetention(t, h, "ret-c", "?async=true", `{"count":1}`)
	if code != http.StatusNoContent {
		t.Fatalf("async retention = %d, want 204", code)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if left := retNumbersLeft(t, h, "ret-c"); left == "2" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("async window never settled: %q", retNumbersLeft(t, h, "ret-c"))
		}
		time.Sleep(50 * time.Millisecond)
	}
}
