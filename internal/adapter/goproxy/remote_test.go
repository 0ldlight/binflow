package goproxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The remote-repository face (goproxy.md section 6.2) against a fake
// upstream GOPROXY: the upstream path carries the RE-ESCAPED wire spelling
// (the three-state rule's third leg), version files land in the cache and
// serve on the second read without a second upstream contact, the list and
// @latest documents ride the internal marker paths verbatim, upstream
// misses are the protocol's plain 404, the +incompatible .mod never
// contacts the upstream, and a dead upstream degrades to the cached copy
// (HIT) or the unfound 404 — never a 5xx.

// fakeUpstream is a GOPROXY-shaped upstream with per-path hit counting and
// a record of every requested path (the escape assertions read it).
type fakeUpstream struct {
	srv  *httptest.Server
	mux  *http.ServeMux
	mu   sync.Mutex
	seen []string
}

// register mounts one exact upstream path (the client-e2e module fixtures).
func (f *fakeUpstream) register(t *testing.T, path, ctype string, body []byte) {
	t.Helper()
	f.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", ctype)
		_, _ = w.Write(body)
	})
}

// newFakeUpstream serves one module's trio plus list/@latest.
func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{mux: http.NewServeMux()}
	serve := func(body string, ctype string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.seen = append(f.seen, r.URL.Path)
			f.mu.Unlock()
			w.Header().Set("Content-Type", ctype)
			_, _ = w.Write([]byte(body))
		}
	}
	f.mux.HandleFunc("/example.com/m/@v/v1.0.0.mod", serve("module example.com/m\n\ngo 1.21\n", "text/plain; charset=utf-8"))
	f.mux.HandleFunc("/example.com/m/@v/v1.0.0.info", serve(`{"Version":"v1.0.0","Time":"2024-01-02T03:04:05Z"}`, "application/json"))
	f.mux.HandleFunc("/example.com/m/@v/v1.0.0.zip", serve("PK-upstream-zip", "application/zip"))
	f.mux.HandleFunc("/example.com/m/@v/list", serve("v0.1.0\nv1.0.0\nv1.1.0\n", "text/plain; charset=utf-8"))
	f.mux.HandleFunc("/example.com/m/@latest", serve(`{"Version":"v1.1.0","Time":"2024-02-03T04:05:06Z"}`, "application/json"))
	f.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.URL.Path)
		f.mu.Unlock()
		http.NotFound(w, r)
	})
	f.srv = httptest.NewServer(f.mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUpstream) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

func (f *fakeUpstream) count(path string) int {
	n := 0
	for _, p := range f.paths() {
		if p == path {
			n++
		}
	}
	return n
}

// newRemoteStack seeds one remote repository against the fake upstream.
func newRemoteStack(t *testing.T) (*stack, *fakeUpstream) {
	t.Helper()
	s := newStack(t)
	up := newFakeUpstream(t)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.srv.URL)
	return s, up
}

// TestRemotePullThroughEscape: the upstream hop carries the escaped wire
// path — the decoded storage path never leaves the process (section 3.1's
// three-state rule, upstream leg).
func TestRemotePullThroughEscape(t *testing.T) {
	s := newStack(t)
	up := newFakeUpstream(t)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.srv.URL)

	// Storage-side module with uppercase, requested by its ESCAPED wire
	// spelling; the upstream must receive the escaped form back.
	status, body, _ := s.get("/binflow/go-remote/example.com/!my!mod/@v/v1.0.0.mod")
	if status != http.StatusNotFound {
		t.Fatalf("escaped miss = (%d, %s), want 404 (the fake upstream has no such module)", status, body)
	}
	want := "/example.com/!my!mod/@v/v1.0.0.mod"
	if got := up.paths(); len(got) == 0 || got[0] != want {
		t.Errorf("upstream saw %v, want first path %q", got, want)
	}

	// The plain module proxies verbatim with the un-escaped spelling.
	status, body, _ = s.get("/binflow/go-remote/example.com/m/@v/v1.0.0.mod")
	if status != http.StatusOK || body != "module example.com/m\n\ngo 1.21\n" {
		t.Fatalf("pull-through .mod = (%d, %q)", status, body)
	}
	if n := up.count("/example.com/m/@v/v1.0.0.mod"); n != 1 {
		t.Errorf("upstream .mod contacts = %d, want 1", n)
	}
}

// TestRemoteCacheSecondHit: the second read serves the landed copy with
// zero upstream traffic, and the cache node lives in the remote
// repository's own namespace at the DECODED storage path.
func TestRemoteCacheSecondHit(t *testing.T) {
	s, up := newRemoteStack(t)
	for i := 0; i < 2; i++ {
		status, body, hdr := s.get("/binflow/go-remote/example.com/m/@v/v1.0.0.zip")
		if status != http.StatusOK || body != "PK-upstream-zip" {
			t.Fatalf("GET #%d = (%d, %q)", i, status, body)
		}
		if state := hdr.Get("X-BinFlow-Cache"); state != "MISS" && state != "HIT" {
			t.Errorf("GET #%d X-BinFlow-Cache = %q", i, state)
		}
	}
	if n := up.count("/example.com/m/@v/v1.0.0.zip"); n != 1 {
		t.Errorf("upstream contacts = %d, want 1 (second read is the cache hit)", n)
	}
	nodes, err := s.md.Nodes().ListByPrefix(t.Context(), "go-remote", "example.com/m/@v")
	if err != nil {
		t.Fatalf("list remote cache: %v", err)
	}
	found := false
	for _, n := range nodes {
		if n.Path == "example.com/m/@v/v1.0.0.zip" {
			found = true
		}
	}
	if !found {
		t.Error("the landed cache node is not at the decoded storage path")
	}
}

// TestRemoteListAndLatestMarkers: the list/@latest documents proxy through
// the internal marker paths and pass through verbatim (S3/S7's no-rewrite
// rule), including a second read served from the marker cache.
func TestRemoteListAndLatestMarkers(t *testing.T) {
	s, up := newRemoteStack(t)

	status, body, _ := s.get("/binflow/go-remote/example.com/m/@v/list")
	if status != http.StatusOK || body != "v0.1.0\nv1.0.0\nv1.1.0\n" {
		t.Fatalf("remote list = (%d, %q)", status, body)
	}
	if n := up.count("/example.com/m/@v/list"); n != 1 {
		t.Errorf("upstream list contacts = %d, want 1", n)
	}
	// The cached marker is not addressable as module content.
	status, body, _ = s.get("/binflow/go-remote/example.com/m/@v/.versionList")
	if status != http.StatusNotFound {
		t.Errorf("marker as content = (%d, %q), want 404", status, body)
	}

	status, body, _ = s.get("/binflow/go-remote/example.com/m/@latest")
	if status != http.StatusOK || body != `{"Version":"v1.1.0","Time":"2024-02-03T04:05:06Z"}` {
		t.Fatalf("remote @latest = (%d, %q)", status, body)
	}
	if n := up.count("/example.com/m/@latest"); n != 1 {
		t.Errorf("upstream @latest contacts = %d, want 1", n)
	}
	// The marker node landed under the reverse-engineered spelling (no '/'
	// before @latest — S3's noted quirk; the exact-path probe is the only
	// list form that can address it).
	nodes, err := s.md.Nodes().ListByPrefix(t.Context(), "go-remote", "example.com/m@latest.latest")
	if err != nil {
		t.Fatalf("list marker prefix: %v", err)
	}
	found := false
	for _, n := range nodes {
		if n.Path == "example.com/m@latest.latest" {
			found = true
		}
	}
	if !found {
		t.Error("the @latest marker node is missing its quirky path")
	}
}

// TestRemoteUpstreamMissIsPlain404: an upstream 404 answers the protocol's
// plain 404 (the status that lets the go command fall through to the next
// GOPROXY source), text/plain body.
func TestRemoteUpstreamMissIsPlain404(t *testing.T) {
	s, _ := newRemoteStack(t)
	status, body, hdr := s.get("/binflow/go-remote/example.com/nosuch/@v/v1.0.0.info")
	if status != http.StatusNotFound {
		t.Fatalf("upstream miss = %d, want 404", status)
	}
	if strings.TrimSpace(body) == "" {
		t.Error("404 body is empty")
	}
	if got := hdr.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("404 Content-Type = %q, want text/plain", got)
	}
}

// TestRemoteIncompatibleModSynthesis: the +incompatible .mod is synthesized
// locally with ZERO upstream contacts (section 6.2's explicit branch).
func TestRemoteIncompatibleModSynthesis(t *testing.T) {
	s, up := newRemoteStack(t)
	status, body, _ := s.get("/binflow/go-remote/example.com/legacy/@v/v2.0.0+incompatible.mod")
	if status != http.StatusOK || body != "module example.com/legacy\n" {
		t.Fatalf("+incompatible remote .mod = (%d, %q), want the synthesized line", status, body)
	}
	if n := len(up.paths()); n != 0 {
		t.Errorf("upstream contacts = %d (%v), want 0", n, up.paths())
	}
}

// TestRemoteUpstreamDeathDegrades: with the upstream gone, a cached copy
// keeps serving (the in-TTL HIT) and an uncached path answers the unfound
// 404 — never a 5xx (the assumed-offline no-copy shape).
func TestRemoteUpstreamDeathDegrades(t *testing.T) {
	s, up := newRemoteStack(t)
	if status, body, _ := s.get("/binflow/go-remote/example.com/m/@v/v1.0.0.mod"); status != http.StatusOK {
		t.Fatalf("warm the cache: (%d, %s)", status, body)
	}
	up.srv.Close()

	status, body, _ := s.get("/binflow/go-remote/example.com/m/@v/v1.0.0.mod")
	if status != http.StatusOK || body != "module example.com/m\n\ngo 1.21\n" {
		t.Fatalf("cached copy after upstream death = (%d, %q), want the copy", status, body)
	}
	status, body, _ = s.get("/binflow/go-remote/example.com/m/@v/v1.2.0.mod")
	if status != http.StatusNotFound {
		t.Fatalf("uncached path after upstream death = (%d, %q), want 404", status, body)
	}
	status, body, _ = s.get("/binflow/go-remote/example.com/m/@v/list")
	if status != http.StatusNotFound {
		t.Fatalf("list after upstream death = (%d, %q), want the 404-not-5xx rule", status, body)
	}
}

// TestRemoteInfoSynthesisNotApplied: the .info synthesis chain is the LOCAL
// face's; on a remote repository an upstream .info miss is a plain 404 (no
// zip-based synthesis — the upstream owns the document).
func TestRemoteInfoSynthesisNotApplied(t *testing.T) {
	s, _ := newRemoteStack(t)
	// The fake upstream 404s the v2 .info but HAS no v2 zip either; use the
	// list to prove the module is known, then a missing version's .info.
	if status, _, _ := s.get("/binflow/go-remote/example.com/m/@v/list"); status != http.StatusOK {
		t.Fatalf("list status = %d", status)
	}
	status, body, _ := s.get("/binflow/go-remote/example.com/m/@v/v9.9.9.info")
	if status != http.StatusNotFound {
		t.Errorf("remote missing .info = (%d, %q), want 404", status, body)
	}
}

// TestRemoteListEmptyPassthrough: an upstream EMPTY list is served as the
// 200 empty body (the official empty-list allowance — the S13 tightening is
// the local face's only; goproxy.md section 4.4 note).
func TestRemoteListEmptyPassthrough(t *testing.T) {
	s := newStack(t)
	var contacted atomic.Bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/@v/list") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(""))
			contacted.Store(true)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(up.Close)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.URL)

	status, body, _ := s.get("/binflow/go-remote/example.com/m/@v/list")
	if status != http.StatusOK || body != "" {
		t.Fatalf("empty upstream list = (%d, %q), want 200 empty", status, body)
	}
	if !contacted.Load() {
		t.Error("the upstream was not contacted")
	}
}
