package cargo

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The remote-class integration tests (T-316, spec section 8's remote
// row). Two upstream shapes:
//
//   - a counting MOCK upstream asserting the exact wire paths and queries
//     the engine requests (the UpstreamPath translation is the contract
//     under test);
//   - the stack's OWN local repository as a real BinFlow cargo upstream
//     (the strongest compatibility fixture: the remote hop speaks the
//     same wire grammar the local face serves).

// remoteFixture is one mock-upstream remote stack. The upstream serves
// one crate ("proxied" 1.0.0) across all four planes.
type remoteFixture struct {
	*stack
	upstream *httptest.Server
	mu       sync.Mutex
	hits     []string
	dead     bool // every request answers 500 (the fault arm)
}

// newRemoteFixture builds a stack whose remote repository proxies a mock
// upstream speaking the cargo sparse wire grammar.
func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	s := newStack(t)
	f := &remoteFixture{stack: s}
	indexRow := `{"name":"proxied","vers":"1.0.0","deps":[],"features":{},"cksum":"` + sha256hex([]byte("upstream-crate-bytes")) + `","yanked":false}` + "\n"
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.Method+" "+r.URL.RequestURI())
		dead := f.dead
		f.mu.Unlock()
		if dead {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		switch r.URL.Path {
		case "/index/config.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"dl":"https://upstream.example/v1/crates","api":"https://upstream.example"}`))
		case "/index/pr/ox/proxied":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(indexRow))
		case "/v1/crates/proxied/1.0.0/download":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("upstream-crate-bytes"))
		case "/api/v1/crates":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"crates":[{"name":"proxied","max_version":"1.0.0","description":"upstream facts"}],"meta":{"total":1}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.upstream.Close)

	s.seedRepo(t, "cargo-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "cargo-remote", f.upstream.URL)
	return f
}

// upstreamHits returns the recorded upstream requests (method + request
// URI, query included).
func (f *remoteFixture) upstreamHits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

// markDead flips the upstream into the 5xx fault mode.
func (f *remoteFixture) markDead() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dead = true
}

// TestRemotePullThroughChain: config.json synthesizes self-pointing while
// config.original.json keeps the upstream form; the index file and the
// download proxy verbatim (MISS then HIT, the upstream contacted exactly
// once per path).
func TestRemotePullThroughChain(t *testing.T) {
	f := newRemoteFixture(t)

	// config.json: synthesized, dl/api cite THIS remote repository.
	status, body, _ := f.get(repoPath("cargo-remote") + "/index/config.json")
	if status != http.StatusOK {
		t.Fatalf("remote config.json = %d (body %s)", status, body)
	}
	if !strings.Contains(body, `"dl":"`+f.srv.URL+`/binflow/cargo-remote/v1/crates"`) {
		t.Fatalf("config.json = %s, want dl self-pointing at the remote repository", body)
	}

	// config.original.json: the upstream's own document, verbatim.
	status, body, _ = f.get(repoPath("cargo-remote") + "/" + fileOriginalConfig)
	if status != http.StatusOK || !strings.Contains(body, "upstream.example") {
		t.Fatalf("config.original.json = (%d, %s), want the preserved upstream form", status, body)
	}
	for _, hit := range f.upstreamHits() {
		if !strings.HasPrefix(hit, "GET /index/config.json") {
			t.Fatalf("config family upstream hits = %v, want only the upstream config path", f.upstreamHits())
		}
	}

	// The index file: proxied verbatim, MISS then HIT.
	status, body, hdr := f.get(repoPath("cargo-remote") + "/index/pr/ox/proxied")
	if status != http.StatusOK || !strings.Contains(body, `"name":"proxied"`) {
		t.Fatalf("remote index = (%d, %s), want the upstream row", status, body)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first index fetch cache state = %q, want MISS", got)
	}
	status, _, hdr = f.get(repoPath("cargo-remote") + "/index/pr/ox/proxied")
	if status != http.StatusOK {
		t.Fatalf("second index fetch = %d", status)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("second index fetch cache state = %q, want HIT", got)
	}

	// The download: the .crate storage path crosses the hop as the
	// download endpoint, byte-identical, MISS then HIT.
	status, body, hdr = f.get(repoPath("cargo-remote") + "/v1/crates/proxied/1.0.0/download")
	if status != http.StatusOK || body != "upstream-crate-bytes" {
		t.Fatalf("remote download = (%d, %q), want the upstream bytes", status, body)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first download cache state = %q, want MISS", got)
	}
	status, _, hdr = f.get(repoPath("cargo-remote") + "/v1/crates/proxied/1.0.0/download")
	if status != http.StatusOK {
		t.Fatalf("second download fetch = %d", status)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("second download cache state = %q, want HIT", got)
	}

	// An upstream miss keeps the pinned download 404.
	status, body, _ = f.get(repoPath("cargo-remote") + "/v1/crates/nosuch/9.9.9/download")
	if status != http.StatusNotFound || body != `{"errors":[{"detail":"unable to download crate"}]}` {
		t.Fatalf("upstream miss = (%d, %s), want the pinned 404 body", status, body)
	}

	// The upstream wire contract: exactly the expected paths, each once.
	want := map[string]int{
		"GET /index/config.json":                1,
		"GET /index/pr/ox/proxied":              1,
		"GET /v1/crates/proxied/1.0.0/download": 1,
	}
	got := map[string]int{}
	for _, hit := range f.upstreamHits() {
		got[hit]++
	}
	for path, n := range want {
		if got[path] != n {
			t.Errorf("upstream hit %q = %d times (all hits %v), want %d", path, got[path], f.upstreamHits(), n)
		}
	}
}

// TestRemoteSearchProxy: the search endpoint proxies VERBATIM (query
// string included byte-for-byte) and caches per query; the upstream fault
// arm answers the CG-2 class-8 conflict face.
func TestRemoteSearchProxy(t *testing.T) {
	f := newRemoteFixture(t)

	status, body, hdr := f.get(repoPath("cargo-remote") + "/api/v1/crates?q=proxied&per_page=10")
	if status != http.StatusOK || !strings.Contains(body, `"max_version":"1.0.0"`) {
		t.Fatalf("remote search = (%d, %s), want the upstream body verbatim", status, body)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first search cache state = %q, want MISS", got)
	}
	status, body, hdr = f.get(repoPath("cargo-remote") + "/api/v1/crates?q=proxied&per_page=10")
	if status != http.StatusOK {
		t.Fatalf("second search = %d (body %s)", status, body)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Fatalf("second search cache state = %q, want HIT", got)
	}
	// A different query is a different cache key — a fresh upstream fetch.
	f.get(repoPath("cargo-remote") + "/api/v1/crates?q=other")
	found := false
	for _, hit := range f.upstreamHits() {
		if hit == "GET /api/v1/crates?q=other" {
			found = true
		}
	}
	if !found {
		t.Fatalf("upstream hits = %v, want the verbatim second query", f.upstreamHits())
	}

	// The hard-fault arm on a FRESH stack with hardFail enabled: the
	// engine's 502 maps onto the CG-2 class-8 conflict face. (The default
	// posture — hardFail off — answers the engine's assumed-offline
	// unfound 404, the registered BinFlow-wide divergence from
	// Artifactory's blanket 409.)
	f2 := newRemoteFixture(t)
	f2.markDead()
	f2.setHardFail(t)
	status, body, _ = f2.get(repoPath("cargo-remote") + "/api/v1/crates?q=anything")
	if status != http.StatusConflict || !strings.Contains(body, `"errors":[{"detail":`) {
		t.Fatalf("upstream fault search = (%d, %s), want 409 + the errors envelope", status, body)
	}
}

// setHardFail flips the fixture repository's canonical config onto the
// hardFail policy (the engine then answers 502 instead of the
// assumed-offline unfound 404).
func (f *remoteFixture) setHardFail(t *testing.T) {
	t.Helper()
	row, err := f.md.Repos().Get(t.Context(), "cargo-remote")
	if err != nil {
		t.Fatalf("load remote row: %v", err)
	}
	row.Config = `{"hardFail":true}`
	if err := f.md.Repos().Update(t.Context(), row); err != nil {
		t.Fatalf("set hardFail: %v", err)
	}
}

// TestRemoteSearchDeadUpstreamPostures (T-355A, T-340 AC2's D-5
// resolution): a TRULY dead upstream — connection refused, the T-351 L24
// shape, not the 500-answering fault mode above — under both R-3 register
// postures. Default (hardFail off): the engine's assumed-offline downgrade
// answers the unfound 404 with the errors envelope naming the offline
// state — BinFlow's registered FR-20-wide divergence from Artifactory's
// blanket 409 (cargo.md section 8's deviation register R-3: "FR-20 全仓
// 统一姿态优先"). hardFail on: the engine's 502 maps onto the CG-2 class-8
// conflict face — 409 + the errors envelope, the only 409 semantics the
// cargo surface carries (the publish family has none by the D-3 final
// ruling; duplicates answer 401/403 or overwrite).
func TestRemoteSearchDeadUpstreamPostures(t *testing.T) {
	f1 := newRemoteFixture(t)
	f1.upstream.Close() // refused dials from here on
	status, body, _ := f1.get(repoPath("cargo-remote") + "/api/v1/crates?q=anything")
	if status != http.StatusNotFound || !strings.Contains(body, `"errors":[{"detail":`) ||
		!strings.Contains(body, "assumed offline") {
		t.Fatalf("dead upstream (default posture) = (%d, %s), want 404 unfound envelope naming the offline state", status, body)
	}

	f2 := newRemoteFixture(t)
	f2.upstream.Close()
	f2.setHardFail(t)
	status, body, _ = f2.get(repoPath("cargo-remote") + "/api/v1/crates?q=anything")
	if status != http.StatusConflict || !strings.Contains(body, `"errors":[{"detail":`) {
		t.Fatalf("dead upstream (hardFail posture) = (%d, %s), want 409 + the errors envelope", status, body)
	}
	if !strings.Contains(body, "hardFail enabled") {
		t.Fatalf("hardFail 409 detail = %s, want the engine's hardFail summary riding the envelope", body)
	}
}

// TestRemoteUpstreamDeletedCacheServes: THE cache proof — once the
// upstream stops serving (or the artifact is deleted upstream), the
// cached copies keep serving with zero upstream traffic.
func TestRemoteUpstreamDeletedCacheServes(t *testing.T) {
	f := newRemoteFixture(t)
	if status, _, _ := f.get(repoPath("cargo-remote") + "/index/pr/ox/proxied"); status != http.StatusOK {
		t.Fatalf("index warmup = %d", status)
	}
	if status, _, _ := f.get(repoPath("cargo-remote") + "/v1/crates/proxied/1.0.0/download"); status != http.StatusOK {
		t.Fatalf("download warmup = %d", status)
	}

	f.markDead() // the upstream "deleted" the crate (every path now 500s)

	status, body, hdr := f.get(repoPath("cargo-remote") + "/v1/crates/proxied/1.0.0/download")
	if status != http.StatusOK || body != "upstream-crate-bytes" {
		t.Fatalf("post-deletion download = (%d, %q), want the cached bytes", status, body)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("post-deletion download cache state = %q, want HIT (zero upstream traffic)", got)
	}
	status, body, _ = f.get(repoPath("cargo-remote") + "/index/pr/ox/proxied")
	if status != http.StatusOK || !strings.Contains(body, `"name":"proxied"`) {
		t.Fatalf("post-deletion index = (%d, %s), want the cached row", status, body)
	}
}

// TestRemoteWriteRefused: the protocol write family answers the read-only
// 405 + envelope before any body drains; the bare face's PUT answers the
// service's own 405 and DELETE is the RE-06 cache eviction.
func TestRemoteWriteRefused(t *testing.T) {
	f := newRemoteFixture(t)

	status, body, hdr := f.put(repoPath("cargo-remote")+"/api/v1/crates/new",
		publishBody(fixtureMeta("nope", "0.1.0"), fixtureCrate("nope", "0.1.0")), nil)
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "read-only proxy cache") {
		t.Fatalf("remote publish = (%d, %s), want the RE-05 405 + envelope", status, body)
	}
	if got := hdr.Get("Allow"); got != http.MethodGet {
		t.Errorf("remote publish Allow = %q, want GET", got)
	}
	status, body, _ = f.delete(repoPath("cargo-remote") + "/api/v1/crates/proxied/1.0.0/yank")
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "read-only proxy cache") {
		t.Fatalf("remote yank = (%d, %s), want the 405 + envelope", status, body)
	}
	status, _, _ = f.put(repoPath("cargo-remote")+"/api/v1/crates/proxied/1.0.0/unyank", nil, nil)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("remote unyank = %d, want 405", status)
	}

	// The bare face: PUT dies at the service's read-only door.
	status, body, _ = f.put(repoPath("cargo-remote")+"/docs/readme.txt", []byte("x"), nil)
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "read-only proxy cache") {
		t.Fatalf("remote bare PUT = (%d, %s), want the service 405", status, body)
	}
	// DELETE is cache eviction: a warmed path answers 204, then a refetch
	// is a fresh MISS (the copy was dropped).
	if status, _, _ := f.get(repoPath("cargo-remote") + "/index/pr/ox/proxied"); status != http.StatusOK {
		t.Fatalf("warmup = %d", status)
	}
	status, _, _ = f.delete(repoPath("cargo-remote") + "/index/pr/ox/proxied")
	if status != http.StatusNoContent {
		t.Fatalf("remote cache DELETE = %d, want the RE-06 204", status)
	}
	if _, _, hdr := f.get(repoPath("cargo-remote") + "/index/pr/ox/proxied"); hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Fatalf("post-eviction refetch must be a fresh MISS, got %q", hdr.Get("X-BinFlow-Cache"))
	}

	// The index-file write face on remote: the read-only 405 (nothing of
	// the D-5 convergence may run against a cache).
	status, _, _ = f.put(repoPath("cargo-remote")+"/index/pr/ox/proxied", []byte("forged\n"), nil)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("remote index PUT = %d, want 405", status)
	}
}

// TestRemoteUpstreamIsBinFlowLocal: the self-referential fixture — a real
// BinFlow local repository as the upstream. The full grammar round-trips
// through the cache: publish locally, resolve the index, download the
// crate and search through the remote.
func TestRemoteUpstreamIsBinFlowLocal(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	s.seedRepo(t, "cargo-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "cargo-remote", s.srv.URL+"/binflow/cargo-local")

	crate := fixtureCrate("selfref", "0.2.0")
	if status, body, _ := s.publish(t, "cargo-local", fixtureMeta("selfref", "0.2.0"), crate); status != http.StatusOK {
		t.Fatalf("local publish = %d (%s)", status, body)
	}

	// The upstream's own entry document, preserved verbatim.
	status, body, _ := s.get(repoPath("cargo-remote") + "/" + fileOriginalConfig)
	if status != http.StatusOK || !strings.Contains(body, `/binflow/cargo-local/v1/crates`) {
		t.Fatalf("config.original.json = (%d, %s), want the upstream BinFlow's dl", status, body)
	}

	// The index file proxies the local repository's generated row, with
	// the cksum reconciliation intact (row cksum == the crate's measured
	// sha256 == the bytes the remote download serves).
	status, body, _ = s.get(repoPath("cargo-remote") + "/index/se/lf/selfref")
	if status != http.StatusOK || !strings.Contains(body, `"cksum":"`+sha256hex(crate)+`"`) {
		t.Fatalf("remote index = (%d, %s), want the upstream row with the measured cksum", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-remote") + "/v1/crates/selfref/0.2.0/download")
	if status != http.StatusOK || body != string(crate) {
		t.Fatalf("remote download = (%d, %d bytes), want the upstream crate", status, len(body))
	}

	// The search proxy rides the upstream's own search face.
	status, body, _ = s.get(repoPath("cargo-remote") + "/api/v1/crates?q=selfref&per_page=10")
	if status != http.StatusOK || !strings.Contains(body, `"name":"selfref"`) || !strings.Contains(body, `"meta":{"total":1}`) {
		t.Fatalf("remote search = (%d, %s), want the upstream search facts", status, body)
	}
}
