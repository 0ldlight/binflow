// The run deletion face's L023-2A semantics (ticket L023-2A, FR-152.2 /
// build-info.md §11.7 — D07-R04's DELETE arm): the E6 verbatim wording
// ("have", the Warning segment, the trailing newline, deleteAll's own
// no-period sentence), the 400 floor, the two-branch 404 law, and the
// same-number multi-run at-most-one quirk.

package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// deleteBuild issues the DELETE and drains the body.
func deleteBuild(t *testing.T, h *harness, path, user, pass string) (int, string) {
	t.Helper()
	resp := h.do(http.MethodDelete, path, user, pass, nil, nil)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, string(body)
}

// seedDelRun publishes one run of "del-app" at the given number/started.
func seedDelRun(t *testing.T, h *harness, number, started string) {
	t.Helper()
	doc := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": "del-app"`, 1)
	doc = strings.Replace(doc, `"number": "51"`, `"number": `+quoteJSON(number), 1)
	doc = strings.Replace(doc, `"started": "2026-09-07T10:00:00.000+0000"`, `"started": `+quoteJSON(started), 1)
	if code, _, _ := putBuildJSON(t, h, doc); code != http.StatusNoContent {
		t.Fatalf("seed %s = %d, want 204", number, code)
	}
}

// TestBuildDeletePartialHitWording: the E6 verbatim body — the success
// segment ("have", trailing ".\n"), then the Warning segment for the
// numbers no run answered, 200 text/plain through it all.
func TestBuildDeletePartialHitWording(t *testing.T) {
	h := newBuildHarness(t)
	seedDelRun(t, h, "3", "2026-09-07T10:00:00.000+0000")
	seedDelRun(t, h, "4", "2026-09-07T11:00:00.000+0000")

	code, body := deleteBuild(t, h, "/binflow/api/build/del-app?buildNumbers=3,99", adminUser, adminPass)
	if code != http.StatusOK {
		t.Fatalf("partial delete = %d %s, want 200", code, body)
	}
	want := "The following builds have been deleted successfully: 'del-app#3'.\n" +
		"Warning - the following builds could not be removed: '99'.\n"
	if body != want {
		t.Fatalf("partial delete body:\n got %q\nwant %q", body, want)
	}

	// All-hit arm: success segment alone, still 200.
	seedDelRun(t, h, "3", "2026-09-07T12:00:00.000+0000")
	code, body = deleteBuild(t, h, "/binflow/api/build/del-app?buildNumbers=3,4", adminUser, adminPass)
	if code != http.StatusOK {
		t.Fatalf("all-hit delete = %d %s, want 200", code, body)
	}
	want = "The following builds have been deleted successfully: 'del-app#3', 'del-app#4'.\n"
	if body != want {
		t.Fatalf("all-hit body:\n got %q\nwant %q", body, want)
	}
}

// TestBuildDeleteAllWording: deleteAll's own sentence — no trailing
// period — and the empty view it leaves behind (the family 404).
func TestBuildDeleteAllWording(t *testing.T) {
	h := newBuildHarness(t)
	seedDelRun(t, h, "1", "2026-09-07T10:00:00.000+0000")
	seedDelRun(t, h, "2", "2026-09-07T11:00:00.000+0000")

	code, body := deleteBuild(t, h, "/binflow/api/build/del-app?deleteAll=1", adminUser, adminPass)
	if code != http.StatusOK {
		t.Fatalf("deleteAll = %d %s, want 200", code, body)
	}
	want := "All builds 'del-app' under 'artifactory-build-info' have been deleted successfully"
	if body != want {
		t.Fatalf("deleteAll body:\n got %q\nwant %q", body, want)
	}
	if resp, doc := getBuild(t, h, "/binflow/api/build/del-app", adminUser, adminPass); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("post-deleteAll numbers = %d %s, want 404", resp.StatusCode, doc)
	}
}

// TestBuildDeleteErrorLadder: the 400 floor (no numbers, no deleteAll),
// the two-branch 404 (missing name vs all-missing numbers), and the
// denied caller's 403.
func TestBuildDeleteErrorLadder(t *testing.T) {
	h := newBuildHarness(t)
	seedDelRun(t, h, "1", "2026-09-07T10:00:00.000+0000")

	code, body := deleteBuild(t, h, "/binflow/api/build/del-app", adminUser, adminPass)
	if code != http.StatusBadRequest || !strings.Contains(body, "Please provide at least one build number to delete") {
		t.Fatalf("no-numbers = %d %s, want the verbatim 400", code, body)
	}

	code, body = deleteBuild(t, h, "/binflow/api/build/ghost-app?buildNumbers=1", adminUser, adminPass)
	if code != http.StatusNotFound || !strings.Contains(body, "Unable to find build 'ghost-app'") {
		t.Fatalf("missing name = %d %s, want 404 Unable to find build", code, body)
	}

	code, body = deleteBuild(t, h, "/binflow/api/build/del-app?buildNumbers=8,9", adminUser, adminPass)
	if code != http.StatusNotFound || !strings.Contains(body, "Unable to find the given build numbers") {
		t.Fatalf("all-missing numbers = %d %s, want 404 Unable to find the given build numbers", code, body)
	}

	// eve holds nothing: 403 before any lookup.
	code, _ = deleteBuild(t, h, "/binflow/api/build/del-app?buildNumbers=1", "eve", "pw")
	if code != http.StatusForbidden {
		t.Fatalf("denied delete = %d, want 403", code)
	}
}

// TestBuildDeleteSameNumberMultiRunQuirk: §11.7 step 3 — one call
// deletes AT MOST ONE run per number (the newest started); the older run
// survives until the next call.
func TestBuildDeleteSameNumberMultiRunQuirk(t *testing.T) {
	h := newBuildHarness(t)
	seedDelRun(t, h, "7", "2026-09-07T10:00:00.000+0000")
	seedDelRun(t, h, "7", "2026-09-07T11:00:00.000+0000")

	code, body := deleteBuild(t, h, "/binflow/api/build/del-app?buildNumbers=7", adminUser, adminPass)
	if code != http.StatusOK ||
		body != "The following builds have been deleted successfully: 'del-app#7'.\n" {
		t.Fatalf("first same-number delete = %d %q", code, body)
	}
	_, doc := getBuild(t, h, "/binflow/api/build/del-app", adminUser, adminPass)
	if !strings.Contains(doc, `"started": "2026-09-07T10:00:00.000+0000"`) {
		t.Fatalf("the older run must survive one call: %s", doc)
	}

	// The second call clears the survivor.
	code, body = deleteBuild(t, h, "/binflow/api/build/del-app?buildNumbers=7", adminUser, adminPass)
	if code != http.StatusOK {
		t.Fatalf("second same-number delete = %d %s", code, body)
	}
	if resp, doc := getBuild(t, h, "/binflow/api/build/del-app", adminUser, adminPass); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("post-clear numbers = %d %s, want 404", resp.StatusCode, doc)
	}
}

// TestBuildDeleteArtifactFlagAccepted: ?artifacts=1 rides through (the
// harness's carrier-less stack skips the node sweep by design — the runs
// still delete, the response shape holds).
func TestBuildDeleteArtifactFlagAccepted(t *testing.T) {
	h := newBuildHarness(t)
	seedDelRun(t, h, "5", "2026-09-07T10:00:00.000+0000")

	code, body := deleteBuild(t, h, "/binflow/api/build/del-app?buildNumbers=5&artifacts=1", adminUser, adminPass)
	if code != http.StatusOK ||
		body != "The following builds have been deleted successfully: 'del-app#5'.\n" {
		t.Fatalf("artifacts-flag delete = %d %q", code, body)
	}

	code, _ = deleteBuild(t, h, "/binflow/api/build/del-app?buildNumbers=5&artifacts=banana", adminUser, adminPass)
	if code != http.StatusBadRequest {
		t.Fatalf("malformed artifacts flag = %d, want 400", code)
	}
}
