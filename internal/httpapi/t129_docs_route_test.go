package httpapi_test

import (
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/docs"
)

// T-129 (PRD FR-41-AC1/DC-01, ADR-0011): the /binflow/docs/** mount.
// Route-level properties only — shell, cache and traversal semantics are
// unit-pinned in internal/docs:
//
//   - the segment is live on every assembly (nil Deps.Docs defaults to the
//     embedded handler in server.New);
//   - it answers ANONYMOUSLY on a closed instance (anonymous_access=false
//     — product self-description, the /healthz posture);
//   - it never shadows the content plane (/binflow/docsfoo is a repo-key
//     spelling, not the segment).
//
// The assertions hold against both embed states: the committed placeholder
// shell and the real `make docs` build (CI builds the site before testing,
// so the real site is what these turn into there).
func TestDocsRouteMounted(t *testing.T) {
	// A deliberately CLOSED instance: the AC's anonymous leg must pass on
	// anonymous_access=false, not just on the default-open one.
	h := newHarnessCfg(t, func(c *config.Config) {
		c.Security.AnonymousAccess = false
	}, nil)

	t.Run("segment root answers anonymously on a closed instance", func(t *testing.T) {
		resp, err := http.Get(h.srv.URL + "/binflow/docs/")
		if err != nil {
			t.Fatalf("GET /binflow/docs/: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck // test cleanup
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("anonymous GET on anonymous_access=false status = %d, want 200", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), "BinFlow") {
			t.Fatalf("segment root does not carry the site shell")
		}
	})

	// A deep page's status depends on the embed state (placeholder-only
	// checkout vs `make docs` build), but it must never be an auth shape:
	// the mount has no credential gate at all.
	t.Run("deep page never answers an auth challenge", func(t *testing.T) {
		resp, err := http.Get(h.srv.URL + "/binflow/docs/integrations/maven")
		if err != nil {
			t.Fatalf("GET deep page: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck // test cleanup
		built := false
		if _, err := fs.Stat(docs.Dist(), "index.html"); err == nil {
			built = true
		}
		want := http.StatusNotFound
		if built {
			want = http.StatusOK
		}
		if resp.StatusCode != want {
			t.Fatalf("deep page status = %d, want %d (built=%v)", resp.StatusCode, want, built)
		}
	})

	t.Run("writes are refused with no route", func(t *testing.T) {
		resp, err := http.Post(h.srv.URL+"/binflow/docs/", "text/plain", strings.NewReader("x"))
		if err != nil {
			t.Fatalf("POST /binflow/docs/: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck // test cleanup
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("POST status = %d, want 405 (no write surface)", resp.StatusCode)
		}
	})

	t.Run("sibling spelling stays on the content plane", func(t *testing.T) {
		resp, err := http.Get(h.srv.URL + "/binflow/docsfoo")
		if err != nil {
			t.Fatalf("GET /binflow/docsfoo: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck // test cleanup
		// A repo named "docsfoo" does not exist. On a CLOSED instance
		// (anonymous_access=false), the content plane challenges with 401
		// before any repo lookup; on an open instance, the missing repo
		// gives 404. Either way, the docs handler is never reached — the
		// sibling spelling lives outside the docs segment.
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET /binflow/docsfoo status = %d, want 404 (or 401 on closed instance)", resp.StatusCode)
		}
	})
}
