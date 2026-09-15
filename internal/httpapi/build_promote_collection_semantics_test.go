// The promote artifact collection's two channels (ticket L023-2F, diff
// report D3): the manifest channel (originalDeploymentRepo + path resolve
// the node directly — no association FK needed) and the build-property
// channel (nodes tagged build.name/build.number, the jf rt upload
// --build-name set) — plus diff D4's echo of the pair and diff D10's
// repo-interpolated batch-deleteAll wording.

package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// seedFile lands one real file node (blob + node rows) in a local repo.
func seedFile(t *testing.T, h *harness, repo, path, sha256 string) {
	t.Helper()
	ctx := context.Background()
	now := "2026-09-15T00:00:00Z"
	if err := h.md.Blobs().Put(ctx, &metadata.Blob{Sha256: sha256, Size: 16, CreatedAt: now}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	if err := h.md.Nodes().Put(ctx, &metadata.Node{
		RepoKey: repo, Path: path, Sha256: sha256, Size: 16,
		CreatedBy: "ci", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}
}

// TestPromoteManifestChannelMigrates: the document's own
// originalDeploymentRepo+path pair IS the address (diff D3) — the artifact
// migrates with no association FK, the source row gone (move), the target
// serving it, and the response body the empty success form (diff D2).
func TestPromoteManifestChannelMigrates(t *testing.T) {
	h := newBuildHarness(t)
	seedLocalGenericRepo(t, h, "dev-libs")
	seedLocalGenericRepo(t, h, "rel-libs")
	seedFile(t, h, "dev-libs", "x/y/m.jar", "aa11"+strings.Repeat("0", 60))

	doc := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": "mc-app"`, 1)
	doc = strings.Replace(doc,
		`{"type": "jar", "sha1": "aa", "sha256": "bb", "md5": "cc",
         "name": "api-1.0.jar", "path": "libs/pub-app/api-1.0.jar"}`,
		`{"type": "jar", "sha256": "aa11`+strings.Repeat("0", 60)+`",
         "name": "m.jar", "path": "x/y/m.jar", "originalDeploymentRepo": "dev-libs"}`, 1)
	if code, _, _ := putBuildJSON(t, h, doc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	resp, body := promoteViaREST(t, h, adminUser, adminPass, "mc-app", "51",
		`{"status":"released","targetRepo":"rel-libs"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest promote = %d %s, want 200", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"messages": []`) && !strings.Contains(body, `"messages":[]`) {
		t.Fatalf("manifest promote body = %s, want the empty success form (diff D2)", body)
	}
	// The move stood: source gone, target serving.
	if n, err := h.md.Nodes().Get(context.Background(), "dev-libs", "x/y/m.jar"); err == nil && n != nil {
		t.Fatal("source node survived the move")
	}
	if _, err := h.md.Nodes().Get(context.Background(), "rel-libs", "x/y/m.jar"); err != nil {
		t.Fatalf("target node missing: %v", err)
	}

	// Diff D4: the echo keeps the pair verbatim.
	_, detail := getBuild(t, h, "/binflow/api/build/mc-app/51", adminUser, adminPass)
	if !strings.Contains(detail, `"path": "x/y/m.jar"`) ||
		!strings.Contains(detail, `"originalDeploymentRepo": "dev-libs"`) {
		t.Fatalf("echo lost the manifest pair: %s", detail)
	}
}

// TestPromotePropertyChannelMigrates: a bare-path artifact row (no
// originalDeploymentRepo, no association) draws from the build-property
// channel — the node tagged build.name/build.number migrates (the jf rt
// upload --build-name shape; diff D3's fallback).
func TestPromotePropertyChannelMigrates(t *testing.T) {
	h := newBuildHarness(t)
	seedLocalGenericRepo(t, h, "dev-libs")
	seedLocalGenericRepo(t, h, "rel-libs")
	seedFile(t, h, "dev-libs", "z/tagged.jar", "bb22"+strings.Repeat("0", 60))
	// The build tagging (jf upload's property triple's name/number pair).
	if err := h.md.NodeProps().Merge(context.Background(), "dev-libs", "z/tagged.jar",
		map[string][]string{"build.name": {"pc-app"}, "build.number": {"51"}}); err != nil {
		t.Fatalf("tag node: %v", err)
	}

	doc := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": "pc-app"`, 1)
	doc = strings.Replace(doc,
		`{"type": "jar", "sha1": "aa", "sha256": "bb", "md5": "cc",
         "name": "api-1.0.jar", "path": "libs/pub-app/api-1.0.jar"}`,
		`{"type": "jar", "name": "tagged.jar", "path": "z/tagged.jar"}`, 1)
	if code, _, _ := putBuildJSON(t, h, doc); code != http.StatusNoContent {
		t.Fatalf("seed = %d, want 204", code)
	}

	resp, body := promoteViaREST(t, h, adminUser, adminPass, "pc-app", "51",
		`{"status":"released","targetRepo":"rel-libs"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("property-channel promote = %d %s, want 200", resp.StatusCode, body)
	}
	if _, err := h.md.Nodes().Get(context.Background(), "rel-libs", "z/tagged.jar"); err != nil {
		t.Fatalf("tagged node did not migrate: %v", err)
	}
	if n, err := h.md.Nodes().Get(context.Background(), "dev-libs", "z/tagged.jar"); err == nil && n != nil {
		t.Fatal("tagged source node survived the move")
	}
}

// TestBatchDeleteAllInterpolatesResolvedRepo: diff D10 — the deleteAll
// wording's `<repo>` slot is the RESOLVED buildRepo (a custom key echoes
// itself, not the default).
func TestBatchDeleteAllInterpolatesResolvedRepo(t *testing.T) {
	h := newBuildHarness(t)
	doc := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": "d10-app"`, 1)
	resp := h.do(http.MethodPut, "/binflow/api/build?buildRepo=l0232f-bi-build-info",
		adminUser, adminPass, []byte(doc), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("custom-repo seed = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	code, body := postBuildDelete(t, h,
		`{"buildName":"d10-app","deleteAll":true,"buildRepo":"l0232f-bi-build-info"}`)
	if code != http.StatusOK ||
		body != "All builds 'd10-app' under 'l0232f-bi-build-info' have been deleted successfully" {
		t.Fatalf("deleteAll = %d %q, want the resolved repo interpolated (diff D10)", code, body)
	}
}
