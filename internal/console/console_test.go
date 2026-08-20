package console_test

// The T-89 console handler contract (PRD FR-23 / ADR-0014 T-108 errata):
// the mount redirect, the no-cache SPA shell with its segment-internal
// history fallback, the immutable fingerprinted assets — and the boundary
// rule that the handler never answers anything outside its two segments, so
// mounting it can never shadow /binflow/<repo>/<path>.
//
// Every assertion passes in BOTH embed states: a fresh clone (the committed
// placeholder shell, no assets) and a `make console` build (the real SPA).
// State-dependent checks probe the embedded tree first and skip cleanly.

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/console"
)

// assetDirName mirrors the assets/ directory the build output carries.
const assetDirName = "assets"

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestRootRedirectsToUISegment(t *testing.T) {
	h := console.Handler()
	for _, target := range []string{"/binflow", "/binflow/"} {
		rec := get(t, h, target)
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("GET %s = %d, want 301", target, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/binflow/ui/" {
			t.Fatalf("GET %s Location = %q, want /binflow/ui/", target, loc)
		}
	}
}

func TestUISegmentServesShellWithFallback(t *testing.T) {
	h := console.Handler()
	for _, target := range []string{"/binflow/ui/", "/binflow/ui", "/binflow/ui/repositories"} {
		rec := get(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", target, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Fatalf("GET %s Content-Type = %q", target, ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("GET %s Cache-Control = %q, want no-cache (W02)", target, cc)
		}
		if !strings.Contains(rec.Body.String(), `id="root"`) {
			t.Fatalf("GET %s body lacks the #root mount point", target)
		}
	}
}

func TestOutOfSegmentPathsAreNotSwallowed(t *testing.T) {
	h := console.Handler()
	// The boundary rule: content paths are NOT the console's. A repo named
	// anything (or a stray spelling under /binflow that is neither segment)
	// must see 404 from this handler, never the SPA shell — mounting the
	// handler can never shadow the content plane.
	for _, target := range []string{
		"/binflow/generic-local/lib-1.0.jar",
		"/binflow/assets",
		"/binflow/console/",
		"/",
		"/binflow/ui/../generic-local/x.bin", // cleaned escape leaves the segment
	} {
		rec := get(t, h, target)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404 (out of segment)", target, rec.Code)
		}
	}
}

func TestAssetsImmutableOrMissing(t *testing.T) {
	h := console.Handler()

	// A hash-shaped miss is a plain 404 (never the shell).
	rec := get(t, h, "/binflow/assets/nonexistent-deadbeef.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatal("missing asset fell back to the SPA shell")
	}

	// Traversal-shaped names are refused outright.
	rec = get(t, h, "/binflow/assets/..%2Fplaceholder.html")
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("traversal asset name = %d, want 404/400", rec.Code)
	}

	// When a real build is embedded, its files serve immutable.
	embedded, err := fs.ReadDir(console.Dist(), assetDirName)
	if err != nil || len(embedded) == 0 {
		t.Skipf("no embedded build output (placeholder-only embed): %v", err)
	}
	name := embedded[0].Name()
	rec = get(t, h, "/binflow/assets/"+name)
	if rec.Code != http.StatusOK {
		t.Fatalf("embedded asset %s = %d, want 200", name, rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control = %q, want immutable (W02)", cc)
	}
}

func TestMethodGuard(t *testing.T) {
	h := console.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/binflow/ui/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow = %q", allow)
	}
}
