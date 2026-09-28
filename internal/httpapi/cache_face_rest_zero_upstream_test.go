package httpapi_test

// T-540 (BIN-23, F1-residual): the <K>-cache REST storage face is
// zero-upstream even when the parent remote runs listRemoteFolderItems=true
// (ADR-0051 Errata 一-③; docs/design/virtual-four-bucket.md §9 F1-residual).
// The upstream-enumeration merge hangs on the REMOTE key's browse face
// alone — the projection key reads with local semantics: stored rows only,
// a miss is the ordinary 404, and the upstream counter never moves. The
// control leg keeps the remote key's own T-448 merge behavior untouched.

import (
	"net/http"
	"testing"
)

// TestCacheFaceRestZeroUpstream: the 3-leg acceptance. A helm remote with
// the optional档 on over a counting classic upstream; the cache is warmed
// through the remote key (the only face that pulls), then the projection
// key's REST faces are probed against the upstream counter.
func TestCacheFaceRestZeroUpstream(t *testing.T) {
	h := newBrowseFlagHarness(t)
	up := newDegradedUpstream(t)
	putDegradedRepo(t, h, "cface", up.srv.URL, true)
	// Warm the cache through the remote key: a root chart and a deep one
	// (the deep pull also lands the deep/ and deep/nested/ folder rows).
	pullThrough(t, h, "cface", "solo-0.2.0.tgz")
	pullThrough(t, h, "cface", "deep/nested/deep-1.0.0.tgz")
	warm := up.hits.Load()

	// ---- Leg 1: GET /api/storage/<K>-cache/<folder> answers cached rows
	// only, with zero upstream contact (no enumeration, no merge). ----
	root := storageJSON(t, h, "cface-cache")
	if kids := childNames(root); !kids["solo-0.2.0.tgz"] || !kids["deep"] {
		t.Fatalf("projection root children = %v, want the cached rows", kids)
	}
	for _, derived := range []string{"index.yaml", "charts"} {
		if kids := childNames(root); kids[derived] {
			t.Fatalf("projection root carried the upstream-only row %q: %v", derived, kids)
		}
	}
	if got := up.hits.Load() - warm; got != 0 {
		t.Fatalf("leg 1 root: upstream hits = %d, want 0", got)
	}
	// The subfolder face: cached children only, still zero upstream.
	folder := storageJSON(t, h, "cface-cache/deep")
	if kids := childNames(folder); !kids["nested"] || len(kids) != 1 {
		t.Fatalf("projection folder children = %v, want [nested] only", kids)
	}
	if got := up.hits.Load() - warm; got != 0 {
		t.Fatalf("leg 1 folder: upstream hits = %d, want 0", got)
	}
	// The cached file's item info resolves on the projection key too.
	item := storageJSON(t, h, "cface-cache/solo-0.2.0.tgz")
	if item["repo"] != "cface-cache" {
		t.Fatalf("projection item repo = %v, want cface-cache", item["repo"])
	}
	if got := up.hits.Load() - warm; got != 0 {
		t.Fatalf("leg 1 item: upstream hits = %d, want 0", got)
	}

	// ---- Leg 2: a file miss on the projection key is the ordinary 404
	// with zero upstream (the miss never enumerates, never pulls). ----
	resp := h.do(http.MethodGet, "/binflow/api/storage/cface-cache/never.bin", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("leg 2: projection file miss = %d %s, want 404", resp.StatusCode, body)
	}
	if got := up.hits.Load() - warm; got != 0 {
		t.Fatalf("leg 2: upstream hits = %d, want 0", got)
	}

	// ---- Leg 3 (control): the remote key's own REST face keeps the
	// T-448 merge behavior — upstream-derived rows ride the listing. ----
	remote := storageJSON(t, h, "cface")
	kids := childNames(remote)
	if !kids["index.yaml"] || !kids["solo-0.2.0.tgz"] || !kids["deep"] {
		t.Fatalf("leg 3: remote root children = %v, want the merged (derived+cached) rows", kids)
	}
	if got := up.hits.Load() - warm; got == 0 {
		t.Fatalf("leg 3: remote listing cost 0 upstream hits; the merge face must enumerate")
	}
}
