package conan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The remote-class integration tests (T-312, spec section 7's remote row).
// Two upstream shapes:
//
//   - a counting MOCK upstream asserting the exact wire paths the engine
//     requests (the UpstreamPath translation is the contract under test);
//   - the stack's OWN local repository as a real BinFlow conan upstream
//     (the strongest compatibility fixture: the remote hop speaks the
//     same wire grammar the local face serves).

// remoteFixture is one mock-upstream remote stack.
type remoteFixture struct {
	*stack
	upstream     *httptest.Server
	mu           sync.Mutex
	hits         []string
	rrev         string
	pid, pRevKey string
}

// newRemoteFixture builds a stack whose remote repository proxies a mock
// upstream that serves one complete coordinate (recipe + one package).
func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	s := newStack(t)
	rrev := "aa11" + strings.Repeat("0", 60)
	pid := "bb22" + strings.Repeat("0", 36)
	prev := "cc33" + strings.Repeat("0", 60)
	f := &remoteFixture{stack: s}
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/conans/hello/1.0/myuser/stable/revisions":
			_, _ = fmt.Fprintf(w,
				`{"reference":"hello/1.0@myuser/stable","revisions":[{"revision":"%s","time":"2026-08-27T10:00:00.000Z"}]}`, rrev)
		case "/v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/files":
			_, _ = w.Write([]byte(`{"files":{"conanfile.py":{},"conanmanifest.txt":{}}}`))
		case "/v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/files/conanfile.py":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("upstream-recipe"))
		case "/v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/packages/" + pid + "/revisions":
			_, _ = fmt.Fprintf(w,
				`{"reference":"hello/1.0@myuser/stable#%s:%s","revisions":[{"revision":"%s","time":"2026-08-27T10:05:00.000Z"}]}`, rrev, pid, prev)
		case "/v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/packages/" + pid + "/revisions/" + prev + "/files":
			_, _ = w.Write([]byte(`{"files":{"conan_package.tgz":{},"conaninfo.txt":{}}}`))
		case "/v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/packages/" + pid + "/revisions/" + prev + "/files/conan_package.tgz":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("upstream-package"))
		case "/v2/conans/hello/1.0/myuser/stable/revisions/" + rrev + "/search":
			_, _ = fmt.Fprintf(w,
				`{"%s":{"settings":{"os":"Linux"},"options":{},"requires":{},"recipe_hash":"%s"}}`, pid, rrev)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.upstream.Close)

	s.seedRepo(t, "cn-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "cn-remote", f.upstream.URL)
	f.rrev, f.pid, f.pRevKey = rrev, pid, prev
	return f
}

// upstreamHits returns the recorded upstream requests.
func (f *remoteFixture) upstreamHits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

// TestRemoteRevisionChain: latest/revisions proxy the upstream revisions
// document (cached at the index path), with the engine's cache state
// riding the response headers.
func TestRemoteRevisionChain(t *testing.T) {
	f := newRemoteFixture(t)

	code, body, hdr := f.get(v2("cn-remote", "hello/1.0/myuser/stable/latest"))
	if code != http.StatusOK {
		t.Fatalf("remote latest = %d (body %s), want 200", code, body)
	}
	var latest revEntry
	if err := json.Unmarshal([]byte(body), &latest); err != nil {
		t.Fatalf("latest body %q: %v", body, err)
	}
	if latest.Revision != f.rrev {
		t.Errorf("latest revision = %q, want the upstream %q", latest.Revision, f.rrev)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first latest cache state = %q, want MISS", got)
	}

	// The second read serves the landed copy without an upstream contact.
	code, body, hdr = f.get(v2("cn-remote", "hello/1.0/myuser/stable/revisions"))
	if code != http.StatusOK {
		t.Fatalf("remote revisions = %d (body %s), want 200", code, body)
	}
	var doc recipeIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("revisions body %q: %v", body, err)
	}
	if doc.Reference != "hello/1.0@myuser/stable" || len(doc.Revisions) != 1 || doc.Revisions[0].Revision != f.rrev {
		t.Errorf("revisions doc = %+v, want the upstream chain", doc)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("second read cache state = %q, want HIT", got)
	}
	if n := len(f.upstreamHits()); n != 1 {
		t.Errorf("upstream requests = %d (%v), want 1 (the cached copy served the rest)", n, f.upstreamHits())
	}

	// The upstream miss family answers the pinned no-revisions 404.
	code, body, _ = f.get(v2("cn-remote", "nope/1.0/_/_/latest"))
	if code != http.StatusNotFound || body != msgNoRevisions {
		t.Errorf("missing latest = (%d, %q), want (404, %q)", code, body, msgNoRevisions)
	}
}

// TestRemoteFilesAndPackages: the listing documents cache under their
// markers and serve verbatim; the file bodies ride the engine; the
// package chain behaves the same.
func TestRemoteFilesAndPackages(t *testing.T) {
	f := newRemoteFixture(t)
	filesPath := "hello/1.0/myuser/stable/revisions/" + f.rrev + "/files"

	code, body, _ := f.get(v2("cn-remote", filesPath))
	if code != http.StatusOK {
		t.Fatalf("remote files listing = %d (body %s), want 200", code, body)
	}
	var listing filesResponse
	if err := json.Unmarshal([]byte(body), &listing); err != nil {
		t.Fatalf("listing body %q: %v", body, err)
	}
	if len(listing.Files) != 2 {
		t.Errorf("listing = %v, want the two upstream files", listing.Files)
	}
	if _, ok := listing.Files["conanfile.py"]; !ok {
		t.Errorf("listing lacks conanfile.py: %v", listing.Files)
	}

	code, body, _ = f.get(v2("cn-remote", filesPath+"/conanfile.py"))
	if code != http.StatusOK || body != "upstream-recipe" {
		t.Errorf("remote file GET = (%d, %q), want (200, upstream body)", code, body)
	}

	// The package chain: revisions, listing, body.
	pkgBase := "hello/1.0/myuser/stable/revisions/" + f.rrev + "/packages/" + f.pid
	code, body, _ = f.get(v2("cn-remote", pkgBase+"/latest"))
	if code != http.StatusOK {
		t.Fatalf("remote pkg latest = %d (body %s)", code, body)
	}
	var prev revEntry
	if err := json.Unmarshal([]byte(body), &prev); err != nil || prev.Revision != f.pRevKey {
		t.Errorf("pkg latest body %q: %v", body, err)
	}
	code, body, _ = f.get(v2("cn-remote", pkgBase+"/revisions/"+f.pRevKey+"/files"))
	if code != http.StatusOK || !strings.Contains(body, "conan_package.tgz") {
		t.Errorf("pkg listing = (%d, %q)", code, body)
	}
	code, body, _ = f.get(v2("cn-remote", pkgBase+"/revisions/"+f.pRevKey+"/files/conan_package.tgz"))
	if code != http.StatusOK || body != "upstream-package" {
		t.Errorf("pkg file GET = (%d, %q)", code, body)
	}

	// The upstream wire paths the engine addressed (the translation
	// contract, end to end).
	wantPrefixes := []string{
		"GET /v2/conans/hello/1.0/myuser/stable/revisions/" + f.rrev + "/files",
		"GET /v2/conans/hello/1.0/myuser/stable/revisions/" + f.rrev + "/files/conanfile.py",
		"GET /v2/conans/hello/1.0/myuser/stable/revisions/" + f.rrev + "/packages/" + f.pid + "/revisions",
		"GET /v2/conans/hello/1.0/myuser/stable/revisions/" + f.rrev + "/packages/" + f.pid + "/revisions/" + f.pRevKey + "/files",
		"GET /v2/conans/hello/1.0/myuser/stable/revisions/" + f.rrev + "/packages/" + f.pid + "/revisions/" + f.pRevKey + "/files/conan_package.tgz",
	}
	hits := f.upstreamHits()
	for _, want := range wantPrefixes {
		found := false
		for _, hit := range hits {
			if hit == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("upstream never requested %q (hits: %v)", want, hits)
		}
	}
}

// TestRemoteRefSearch: the packageId metadata document proxies verbatim,
// the implicit-latest variant resolving through the proxied index first.
func TestRemoteRefSearch(t *testing.T) {
	f := newRemoteFixture(t)

	for _, path := range []string{
		"hello/1.0/myuser/stable/search",
		"hello/1.0/myuser/stable/revisions/" + f.rrev + "/search",
	} {
		code, body, _ := f.get(v2("cn-remote", path) + "?q=")
		if code != http.StatusOK {
			t.Fatalf("remote ref search %s = %d (body %s)", path, code, body)
		}
		var rows map[string]*pkgMeta
		if err := json.Unmarshal([]byte(body), &rows); err != nil {
			t.Fatalf("ref search body %q: %v", body, err)
		}
		if len(rows) != 1 {
			t.Errorf("%s rows = %v, want the one upstream pid", path, rows)
		}
		if meta := rows[f.pid]; meta == nil || meta.Settings["os"] != "Linux" {
			t.Errorf("%s pid row = %+v, want the upstream settings", path, meta)
		}
	}
}

// TestRemoteWritesRefused: PUT rides the shared arm to the service's
// read-only 405; the DELETE family refuses with the same wording.
func TestRemoteWritesRefused(t *testing.T) {
	f := newRemoteFixture(t)

	code, body, hdr := f.put(v2("cn-remote",
		"hello/1.0/myuser/stable/revisions/"+fixtureRev(3)+"/files/conanfile.py"), []byte("x"), nil)
	if code != http.StatusMethodNotAllowed {
		t.Errorf("remote PUT = (%d, %q), want 405", code, body)
	}
	if !strings.Contains(body, "read-only proxy cache") {
		t.Errorf("remote PUT body = %q, want the RE-05 wording", body)
	}
	if got := hdr.Get("Allow"); got != http.MethodGet {
		t.Errorf("remote PUT Allow = %q, want GET", got)
	}

	for _, path := range []string{
		"hello/1.0/myuser/stable",
		"hello/1.0/myuser/stable/revisions/" + f.rrev,
		"hello/1.0/myuser/stable/revisions/" + f.rrev + "/packages",
		"hello/1.0/myuser/stable/revisions/" + f.rrev + "/packages/" + f.pid + "/revisions/" + f.pRevKey,
	} {
		code, body, _ = f.delete(v2("cn-remote", path))
		if code != http.StatusMethodNotAllowed || !strings.Contains(body, "read-only proxy cache") {
			t.Errorf("remote DELETE %s = (%d, %q), want the RE-05 405", path, code, body)
		}
	}
}

// TestRemoteUpstreamIsBinFlowLocal: the remote hop against the stack's own
// local repository as upstream — the full protocol round trip (upload
// locally, read through the remote) proves the two faces share one
// grammar.
func TestRemoteUpstreamIsBinFlowLocal(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	s.seedRepo(t, "cn-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "cn-remote", s.srv.URL+"/binflow/cn-local")

	r := ref{name: "hello", version: "2.0", user: "myuser", channel: "stable"}
	rrev := fixtureRev(9)
	if code, body, _ := s.putRecipeFile("cn-local", r, rrev, "conanfile.py", []byte("local-body")); code != http.StatusCreated {
		t.Fatalf("seed upload = %d (body %s)", code, body)
	}

	code, body, _ := s.get(v2("cn-remote", "hello/2.0/myuser/stable/latest"))
	if code != http.StatusOK || !strings.Contains(body, rrev) {
		t.Fatalf("remote latest = (%d, %s), want the local revision", code, body)
	}
	code, body, _ = s.get(v2("cn-remote", "hello/2.0/myuser/stable/revisions/"+rrev+"/files/conanfile.py"))
	if code != http.StatusOK || body != "local-body" {
		t.Errorf("remote file GET = (%d, %q), want the local body", code, body)
	}
	// The landed copy serves the next read from cache.
	code, _, hdr := s.get(v2("cn-remote", "hello/2.0/myuser/stable/revisions/"+rrev+"/files/conanfile.py"))
	if code != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Errorf("second read = (%d, cache %q), want HIT", code, hdr.Get("X-BinFlow-Cache"))
	}
}
