// The PUT /api/build face's L023-2A semantics (ticket L023-2A, FR-152.2 /
// build-info.md §11.3 — the errata-corrected contract): the hidden-
// coordinate 400s with the spec's verbatim wording, the server-side
// rewrites (artifactoryPrincipal overwrite, partial checksum backfill),
// the same-number multi-run coexistence, and the overwrite arm's
// replace-whole law — each pinned on the wire.

package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// putBuildJSON issues the upload PUT and returns status + drained body.
func putBuildJSON(t *testing.T, h *harness, doc string) (int, string, http.Header) {
	t.Helper()
	resp := putBuildDoc(t, h, adminUser, adminPass, doc)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, string(body), resp.Header
}

// TestBuildPutHiddenCoordinatesRejected: §11.3 rewrite 4 — a name or
// number whose leading whitespace is stripped and then starts with '.'
// answers the spec's verbatim 400s (the buildinfo layout's hidden-
// directory guard). A dotless coordinate and an interior dot both pass.
func TestBuildPutHiddenCoordinatesRejected(t *testing.T) {
	h := newBuildHarness(t)

	cases := []struct {
		name   string
		number string
		wantIn string
	}{
		{".jfrog", "5", "Build name must not start with '.'"},
		{"  .jfrog", "5", "Build name must not start with '.'"},
		{"app", ".5", "Build number must not start with '.'"},
		{"app", "  .5", "Build number must not start with '.'"},
	}
	for _, tc := range cases {
		doc := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": `+quoteJSON(tc.name), 1)
		doc = strings.Replace(doc, `"number": "51"`, `"number": `+quoteJSON(tc.number), 1)
		code, body, _ := putBuildJSON(t, h, doc)
		var rendered struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if code != http.StatusBadRequest || json.Unmarshal([]byte(body), &rendered) != nil ||
			len(rendered.Errors) != 1 || rendered.Errors[0].Message != tc.wantIn {
			t.Fatalf("hidden coordinate %q/%q = %d %s, want 400 with the EXACT message %q",
				tc.name, tc.number, code, body, tc.wantIn)
		}
	}

	// Control arms: no leading dot (interior dot fine) publishes.
	code, _, _ := putBuildJSON(t, h, strings.Replace(buildRESTDoc,
		`"name": "pub-app"`, `"name": "my.app"`, 1))
	if code != http.StatusNoContent {
		t.Fatalf("interior-dot name = %d, want 204", code)
	}
}

// quoteJSON renders one JSON string literal (test-side shorthand).
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestBuildPutPrincipalOverwriteEchoed: §11.3 rewrite 1 — the stored
// document's artifactoryPrincipal is the AUTHENTICATED user, whatever the
// client claimed (or omitted).
func TestBuildPutPrincipalOverwriteEchoed(t *testing.T) {
	h := newBuildHarness(t)

	doc := strings.Replace(buildRESTDoc, `"url": "https://ci.example.org/job/pub-app/51"`,
		`"url": "https://ci.example.org/job/pub-app/51", "artifactoryPrincipal": "spoofed-ci"`, 1)
	if code, _, _ := putBuildJSON(t, h, doc); code != http.StatusNoContent {
		t.Fatalf("upload = %d, want 204", code)
	}
	_, detail := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	if !strings.Contains(detail, `"artifactoryPrincipal": "admin"`) {
		t.Fatalf("artifactoryPrincipal not overwritten with the actor: %s", detail)
	}
}

// TestBuildPutSameNumberDifferentStartedCoexist: §11.3 — the run identity
// is the four-tuple: the same name+number with a different started is a
// NEW run, and the numbers face answers the number once per run.
func TestBuildPutSameNumberDifferentStartedCoexist(t *testing.T) {
	h := newBuildHarness(t)

	if code, _, _ := putBuildJSON(t, h, buildRESTDoc); code != http.StatusNoContent {
		t.Fatalf("upload run 1 = %d, want 204", code)
	}
	second := strings.Replace(buildRESTDoc,
		`"started": "2026-09-07T10:00:00.000+0000"`,
		`"started": "2026-09-07T11:00:00.000+0000"`, 1)
	if code, _, _ := putBuildJSON(t, h, second); code != http.StatusNoContent {
		t.Fatalf("upload run 2 = %d, want 204", code)
	}

	_, doc := getBuild(t, h, "/binflow/api/build/pub-app", adminUser, adminPass)
	if got := strings.Count(doc, `"uri": "/51"`); got != 2 {
		t.Fatalf("number 51 appears %d times, want 2 (multi-run coexistence): %s", got, doc)
	}
	// Newest run first (§11.8 strict started DESC).
	if strings.Index(doc, `"started": "2026-09-07T11:00:00.000+0000"`) >
		strings.Index(doc, `"started": "2026-09-07T10:00:00.000+0000"`) {
		t.Fatalf("multi-run rows not newest-first: %s", doc)
	}
}

// TestBuildPutOverwriteReplacesWhole: the same-coordinate re-publish is
// the overwrite arm — the new document's interpreted truth wins (PUT is
// the full-save verb) and the answer stays 204 with a fresh checksum.
func TestBuildPutOverwriteReplacesWhole(t *testing.T) {
	h := newBuildHarness(t)

	_, _, hdr1 := putBuildJSON(t, h, buildRESTDoc)
	second := strings.Replace(buildRESTDoc, `"properties": {"env": "prod"}`, `"properties": {"env": "staging"}`, 1)
	second = strings.Replace(second, `"sha1": "aa"`, `"sha1": "zz"`, 1)
	code, _, hdr2 := putBuildJSON(t, h, second)
	if code != http.StatusNoContent {
		t.Fatalf("overwrite = %d, want 204", code)
	}
	if hdr1.Get("X-Checksum-Sha256") == hdr2.Get("X-Checksum-Sha256") {
		t.Fatal("re-publish checksum identical — the manifest was not replaced")
	}
	_, doc := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	if !strings.Contains(doc, `"staging"`) || strings.Contains(doc, `"prod"`) {
		t.Fatalf("properties not replaced whole: %s", doc)
	}
	if !strings.Contains(doc, `"sha1": "zz"`) {
		t.Fatalf("module segment not replaced whole: %s", doc)
	}
}

// TestBuildPutPartialChecksumBackfill: §11.3 rewrite 2 — an artifact or
// dependency row carrying one or two of the three digests is completed
// from the blob ledger; the completed triple rides the echo.
func TestBuildPutPartialChecksumBackfill(t *testing.T) {
	h := newBuildHarness(t)
	ctx := context.Background()

	const (
		bSha1   = "3f5a1b2c4d6e8f0a1b2c3d4e5f60718293a4b5c6"
		bSha256 = "1122334455667788112233445566778811223344556677881122334455667788"
		bMd5    = "0123456789abcdef0123456789abcdef"
	)
	if err := h.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: bSha256, Sha1: bSha1, Md5: bMd5, Size: 10,
	}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}

	doc := strings.Replace(buildRESTDoc,
		`{"type": "jar", "sha1": "aa", "sha256": "bb", "md5": "cc",
         "name": "api-1.0.jar", "path": "libs/pub-app/api-1.0.jar"}`,
		`{"type": "jar", "sha1": "`+bSha1+`",
         "name": "api-1.0.jar"}`, 1)
	doc = strings.Replace(doc,
		`{"type": "jar", "sha1": "dd", "id": "junit:junit:4.13", "scopes": ["test"]}`,
		`{"type": "jar", "sha256": "`+bSha256+`", "id": "junit:junit:4.13", "scopes": ["test"]}`, 1)
	if code, _, _ := putBuildJSON(t, h, doc); code != http.StatusNoContent {
		t.Fatalf("upload = %d, want 204", code)
	}

	_, detail := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	// The sha1-only artifact row gains sha256+md5; the sha256-only
	// dependency row gains sha1+md5.
	for _, want := range []string{bSha1, bSha256, bMd5} {
		if got := strings.Count(detail, `"`+want+`"`); got < 2 {
			t.Fatalf("digest %s appears %d times, want >= 2 (backfilled on both rows): %s", want, got, detail)
		}
	}
}
