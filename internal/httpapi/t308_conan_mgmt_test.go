package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/httpapi"
)

// The conan reindex family's router legs (T-308's deferred §5-D9 wire-up,
// landed with the B4/B5 assembly): the two spellings intercept before the
// generic protocol mount, AUTHENTICATION is the router's only
// contribution, and the delegation reaches the mounted management face.
// The face's own behavior (the CanManageRepo walk, the blank-key 400, the
// local-only class refusal, the 405) is pinned in the adapter package
// direct-mount (internal/adapter/conan/reindex_test.go) — this file pins
// the ROUTER contract: which spellings route to the face, the 401 in
// front of it, the data plane's non-reindex spellings staying on the
// generic mount, and the E-26 posture when no face is mounted.
func TestConanReindexRoutesDelegateToMgmtFace(t *testing.T) {
	var hit string
	face := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.MgmtHandlers = map[string]http.Handler{"conan": face}
	}, nil)

	t.Run("whole-repo spelling authed reaches the face", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/conan/reindex?repoKey=cn-local", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /api/conan/reindex status = %d, want 200 (face reached)", resp.StatusCode)
		}
		if hit != "/binflow/api/conan/reindex" {
			t.Fatalf("face saw path %q, want the untouched /binflow/api/conan/reindex", hit)
		}
	})
	t.Run("path spelling authed reaches the face", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/conan/cn-local/myuser/hello/reindex", adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /api/conan/{repo}/.../reindex status = %d, want 200 (face reached)", resp.StatusCode)
		}
	})
	t.Run("unauthenticated is the router's 401", func(t *testing.T) {
		hit = ""
		resp := h.do(http.MethodPost, "/binflow/api/conan/reindex", "", "", nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated POST status = %d, want 401 before the face", resp.StatusCode)
		}
		if hit != "" {
			t.Fatalf("unauthenticated request reached the face (path %q)", hit)
		}
	})
	t.Run("non-reindex conan spelling stays off the face", func(t *testing.T) {
		hit = ""
		resp := h.do(http.MethodPost, "/binflow/api/conan/v1/users/authenticate", adminUser, adminPass, nil, nil)
		if hit != "" {
			t.Fatalf("data-plane spelling reached the management face (path %q)", hit)
		}
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("data-plane spelling answered 200 from somewhere unexpected")
		}
	})
}

// Without a mounted face the two spellings keep the E-26 404 — the
// pre-B4/B5 posture stands for stacks assembled without the conan
// management plane (unit stacks; the assembled server always mounts it).
func TestConanReindexRoutesUnmountedStayE26(t *testing.T) {
	h := newHarness(t)
	for _, spelling := range []string{
		"/binflow/api/conan/reindex",
		"/binflow/api/conan/cn-local/reindex",
	} {
		resp := h.do(http.MethodPost, spelling, adminUser, adminPass, nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("POST %s status = %d, want the E-26 404", spelling, resp.StatusCode)
		}
	}
}
