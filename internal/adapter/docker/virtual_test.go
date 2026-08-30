package docker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-365: the /v2 route's VIRTUAL branch. The walk itself is the helmoci
// package's full-stack chain (the real service seam there); this file pins
// the branch's own contract with a class-carrying lookup — writes answer
// the 405 + Allow: GET pair, reads without the seam answer the honest
// 503, and the unimplemented tail keeps the DE-16 spec 404.

// classLookup is a fixed table of class-carrying rows (the static lookup
// spells no class; the virtual branch needs one).
type classLookup map[string]repoRow

func (l classLookup) Get(_ context.Context, key string) (RepoRow, error) {
	row, ok := l.rows(key)
	if !ok {
		return nil, nil
	}
	return row, nil
}

func (l classLookup) List(_ context.Context) ([]RepoRow, error) {
	out := make([]RepoRow, 0, len(l))
	for key := range l {
		row, _ := l.rows(key)
		out = append(out, row)
	}
	return out, nil
}

func (l classLookup) rows(key string) (repoRow, bool) {
	row, ok := l[key]
	return row, ok
}

// newVirtualBranchHandler builds the handler over the class table with NO
// service (the fakeService predates the V2VirtualPlane seam, which is the
// posture under test: the branch answers honestly, never half-serves).
func newVirtualBranchHandler(t *testing.T) *Handler {
	t.Helper()
	return New(nil, classLookup{
		"virt": {key: "virt", pkg: "helmoci", class: repo.TypeVirtual},
	}, adminPassAuthorizer{}, nil, nil, Options{AnonymousAccess: true}, nil)
}

// TestVirtualBranchWriteRefusal: every write verb against a virtual row
// answers 405 + Allow: GET (the route's scope gate has passed; the class
// branch refuses the deploy).
func TestVirtualBranchWriteRefusal(t *testing.T) {
	h := newVirtualBranchHandler(t)
	for _, tc := range []struct {
		method, path string
		body         io.Reader
	}{
		{http.MethodPut, "/v2/virt/mychart/manifests/0.1.0", strings.NewReader(`{"schemaVersion":2}`)},
		{http.MethodPost, "/v2/virt/mychart/blobs/uploads/", nil},
		{http.MethodDelete, "/v2/virt/mychart/manifests/sha256:" + strings.Repeat("a", 64), nil},
	} {
		req := httptest.NewRequest(tc.method, tc.path, tc.body)
		req = req.WithContext(adapter.WithPrincipal(req.Context(), &auth.Principal{Name: "admin", Admin: true}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		resp := rec.Result()
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status = %d body=%s, want 405", tc.method, tc.path, resp.StatusCode, body)
		}
		if allow := resp.Header.Get("Allow"); allow != http.MethodGet {
			t.Fatalf("%s %s Allow = %q, want GET", tc.method, tc.path, allow)
		}
		assertSpecCode(t, body, "UNSUPPORTED")
	}
}

// TestVirtualBranchReadsWithoutSeam: reads against a virtual row with no
// V2VirtualPlane seam answer the honest 503 (the RemoteV2Plane posture —
// never a half-serve), and the unimplemented tail keeps the spec 404.
func TestVirtualBranchReadsWithoutSeam(t *testing.T) {
	h := newVirtualBranchHandler(t)

	for _, path := range []string{
		"/v2/virt/mychart/manifests/0.1.0",
		"/v2/virt/mychart/blobs/sha256:" + strings.Repeat("b", 64),
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(adapter.WithPrincipal(req.Context(), &auth.Principal{Name: "admin", Admin: true}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		resp := rec.Result()
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("GET %s status = %d body=%s, want the honest 503", path, resp.StatusCode, body)
		}
	}

	// The branch's unknown tail keeps the DE-16 spec 404 — the seam is
	// never even consulted for it.
	req := httptest.NewRequest(http.MethodGet, "/v2/virt/mychart/referrers/sha256:"+strings.Repeat("c", 64), nil)
	req = req.WithContext(adapter.WithPrincipal(req.Context(), &auth.Principal{Name: "admin", Admin: true}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	resp := rec.Result()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET referrers status = %d body=%s, want 404", resp.StatusCode, body)
	}
	assertSpecCode(t, body, "UNSUPPORTED")
}
