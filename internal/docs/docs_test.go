package docs_test

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/docs"
)

// The handler's contract is binary-independent: the committed placeholder
// shell keeps every mount semantics testable on a node-less checkout, and
// the same assertions hold against the real build (CI runs `make docs`
// before `make test`, so the real site is what gets exercised there).

func TestHandlerServesSegmentRoot(t *testing.T) {
	srv := httptest.NewServer(docs.Handler())
	t.Cleanup(srv.Close)

	// Canonical redirect: no-slash form moves permanently to the slashed
	// segment root (asset and search-index links assume it). Use a client
	// that never follows redirects so the intermediate 301 is observable.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/binflow/docs")
	if err != nil {
		t.Fatalf("GET /binflow/docs: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("GET /binflow/docs status = %d, want 301", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/binflow/docs/" {
		t.Fatalf("GET /binflow/docs Location = %q, want /binflow/docs/", loc)
	}

	resp2, err := http.Get(srv.URL + "/binflow/docs/")
	if err != nil {
		t.Fatalf("GET /binflow/docs/: %v", err)
	}
	defer resp2.Body.Close() //nolint:errcheck // test cleanup
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /binflow/docs/ status = %d, want 200", resp2.StatusCode)
	}
	body := readAll(t, resp2)
	// The shell is honest under both embed states: the placeholder names
	// itself, the real build carries the Docusaurus root.
	if !strings.Contains(body, "BinFlow") {
		t.Fatalf("segment root does not carry the site shell: %q", snippet(body))
	}
	if ct := resp2.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") || !strings.Contains(ct, "charset=utf-8") {
		t.Fatalf("Content-Type = %q, want text/html with charset=utf-8 (Chinese pages)", ct)
	}
	if cc := resp2.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("shell Cache-Control = %q, want no-cache", cc)
	}
}

func TestHandlerMethodGate(t *testing.T) {
	srv := httptest.NewServer(docs.Handler())
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/binflow/docs/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /binflow/docs/: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405 (no write surface of any kind)", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow = %q, want GET, HEAD", allow)
	}
}

// TestHandlerStaysInSegment pins the NFR-S18 backstop: dot-segment and
// sibling-segment spellings must resolve inside (or against) the docs tree
// only — the handler can never be used to reach files outside dist/ or to
// shadow the content plane's spelling.
func TestHandlerStaysInSegment(t *testing.T) {
	srv := httptest.NewServer(docs.Handler())
	t.Cleanup(srv.Close)

	tests := []struct {
		name string
		path string
		want func(t *testing.T, resp *http.Response)
	}{
		{
			name: "dot segments cannot leave the segment",
			path: "/binflow/docs/../../etc/passwd",
			want: func(t *testing.T, resp *http.Response) {
				t.Helper()
				// path.Clean folds the traversal: /etc/passwd is outside the
				// segment, so it is a plain miss — never a file lookup.
				if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("status = %d, want 404", resp.StatusCode)
				}
			},
		},
		{
			name: "sibling segment is not ours",
			path: "/binflow/docsfoo",
			want: func(t *testing.T, resp *http.Response) {
				t.Helper()
				if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("status = %d, want 404 (/binflow/docsfoo is not in the segment)", resp.StatusCode)
				}
			},
		},
		{
			name: "miss inside the segment answers the build's 404 shape",
			path: "/binflow/docs/no/such/page",
			want: func(t *testing.T, resp *http.Response) {
				t.Helper()
				if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("status = %d, want 404", resp.StatusCode)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close() //nolint:errcheck // test cleanup
			tt.want(t, resp)
		})
	}
}

// TestDistPlaceholder pins the T-89-style placeholder strategy: the embed
// pattern always matches on a fresh clone, so `make build` needs no node
// toolchain. `make docs` swaps the tree; this assertion survives both.
func TestDistPlaceholder(t *testing.T) {
	if _, err := fs.Stat(docs.Dist(), "placeholder.html"); err != nil {
		t.Fatalf("committed placeholder.html missing from the embed: %v", err)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func snippet(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}