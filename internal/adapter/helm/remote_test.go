package helm

// The REMOTE repository's full-stack wire tests (helm.md section 6): the
// index and chart pull-throughs ride the shared FR-20 engine (the
// MISS/HIT assertions read the upstream's own request counter), and the
// two dependency-proxy faces assert their exact wire shapes.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// remoteFixture is one stack with a loopback upstream chart repository and
// a remote helm repository pointing at it.
type remoteFixture struct {
	*stack
	up *chartUpstream
}

// newRemoteFixture assembles the fixture.
func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	s := newStack(t)
	up := newChartUpstream(t, map[string]string{})
	s.seedRemoteRepo(t, "helm-r", up.srv.URL)
	return &remoteFixture{stack: s, up: up}
}

// extURL renders one absolute loopback URL under the upstream server.
func (f *remoteFixture) extURL(path string) string { return f.up.srv.URL + path }

// externalWirePath folds one absolute URL into the _external wire path.
func (f *remoteFixture) externalWirePath(abs string) string {
	folded, ok := foldExternalURL(abs)
	if !ok {
		f.t.Fatalf("fold %q", abs)
	}
	return "/binflow/helm-r/" + folded
}

func TestRemoteIndexPullThrough(t *testing.T) {
	f := newRemoteFixture(t)
	index := "apiVersion: v1\nentries:\n  acs-engine-autoscaler:\n  - name: acs-engine-autoscaler\n    version: \"2.1.1\"\n    urls:\n    - acs-engine-autoscaler-2.1.1.tgz\n"
	f.up.files["/index.yaml"] = index

	status, body, hdr := f.get("/binflow/helm-r/index.yaml")
	if status != http.StatusOK || body != index {
		t.Fatalf("remote index = (%d, %d bytes), want the upstream body verbatim", status, len(body))
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/yaml") {
		t.Errorf("index Content-Type = %q", ct)
	}
	if hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Errorf("first index fetch X-BinFlow-Cache = %q, want MISS", hdr.Get("X-BinFlow-Cache"))
	}
	// The second read is the engine's metadata-TTL cache hit: one upstream
	// request total.
	if status, _, hdr = f.get("/binflow/helm-r/index.yaml"); status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second index fetch = (%d, cache %q), want 200/HIT", status, hdr.Get("X-BinFlow-Cache"))
	}
	if n := f.up.hitCount("/index.yaml"); n != 1 {
		t.Errorf("upstream index hits = %d, want 1 (cached)", n)
	}

	// The alias serves the same face (HL-1).
	if status, _, _ = f.get("/binflow/api/helm/helm-r/index.yaml"); status != http.StatusOK {
		t.Errorf("alias index GET = %d, want 200", status)
	}
}

func TestRemoteChartPullThroughAndCache(t *testing.T) {
	f := newRemoteFixture(t)
	chart := fixtureChart(t, "acs", defaultChartYAML("acs", "2.1.1"), nil)
	f.up.files["/acs-2.1.1.tgz"] = string(chart)

	status, body, hdr := f.get("/binflow/helm-r/acs-2.1.1.tgz")
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(chart) {
		t.Fatalf("remote chart = (%d, %d bytes), want the upstream bytes", status, len(body))
	}
	if hdr.Get("X-Checksum-Sha256") != sha256Hex(chart) {
		t.Errorf("chart X-Checksum-Sha256 = %q", hdr.Get("X-Checksum-Sha256"))
	}
	// AC4's second-hit assertion: the pull-through landed its copy, the
	// next read is a cache HIT with zero further upstream traffic.
	if status, _, hdr = f.get("/binflow/helm-r/acs-2.1.1.tgz"); status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second chart fetch = (%d, cache %q), want 200/HIT", status, hdr.Get("X-BinFlow-Cache"))
	}
	if n := f.up.hitCount("/acs-2.1.1.tgz"); n != 1 {
		t.Errorf("upstream chart hits = %d, want 1 (cached)", n)
	}

	// The .prov sidecar rides the same engine.
	prov := "-----BEGIN PGP SIGNED MESSAGE-----\nprov\n"
	f.up.files["/acs-2.1.1.tgz.prov"] = prov
	if status, body, _ = f.get("/binflow/helm-r/acs-2.1.1.tgz.prov"); status != http.StatusOK || body != prov {
		t.Fatalf("remote prov = (%d, %q)", status, body)
	}

	// An upstream miss is the honest 404.
	if status, _, _ = f.get("/binflow/helm-r/absent-1.0.0.tgz"); status != http.StatusNotFound {
		t.Errorf("absent chart = %d, want 404", status)
	}
}

func TestRemoteWritesRefused(t *testing.T) {
	f := newRemoteFixture(t)
	chart := fixtureChart(t, "acs", defaultChartYAML("acs", "2.1.1"), nil)
	f.up.files["/acs-2.1.1.tgz"] = string(chart)
	status, body, hdr := f.put("/binflow/helm-r/acs-2.1.1.tgz", chart, nil)
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "read-only proxy") {
		t.Fatalf("remote PUT = (%d, %s), want the read-only 405", status, body)
	}
	if hdr.Get("Allow") != "GET" {
		t.Errorf("remote PUT Allow = %q", hdr.Get("Allow"))
	}
	// DELETE is the RE-06 cache-invalidation verb (not a write refusal):
	// 404 with nothing cached, 204 after a copy lands (and the next read
	// re-fetches upstream).
	if status, _, _ = f.delete("/binflow/helm-r/acs-2.1.1.tgz"); status != http.StatusNotFound {
		t.Errorf("remote DELETE (nothing cached) = %d, want the RE-06 404", status)
	}
	f.get("/binflow/helm-r/acs-2.1.1.tgz")
	if status, _, _ = f.delete("/binflow/helm-r/acs-2.1.1.tgz"); status != http.StatusNoContent {
		t.Errorf("remote DELETE (cached copy) = %d, want the RE-06 204", status)
	}
	f.get("/binflow/helm-r/acs-2.1.1.tgz")
	if n := f.up.hitCount("/acs-2.1.1.tgz"); n != 2 {
		t.Errorf("upstream chart hits after the cache drop = %d, want 2 (re-fetched)", n)
	}
	if status, _, _ = f.put("/binflow/helm-r/index.yaml", []byte("apiVersion: v1\n"), nil); status != http.StatusForbidden {
		t.Errorf("remote index PUT = %d, want the server-generated 403", status)
	}
}

func TestRemoteExternalProxy(t *testing.T) {
	f := newRemoteFixture(t)
	dep := fixtureChart(t, "depchart", defaultChartYAML("depchart", "1.0.0"), nil)
	f.up.files["/deps/depchart-1.0.0.tgz"] = string(dep)
	abs := f.extURL("/deps/depchart-1.0.0.tgz")

	status, body, hdr := f.get(f.externalWirePath(abs))
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(dep) {
		t.Fatalf("_external = (%d, %d bytes), want the dependency bytes", status, len(body))
	}
	if hdr.Get("X-Binflow-Upstream") != abs {
		t.Errorf("_external X-Binflow-Upstream = %q", hdr.Get("X-Binflow-Upstream"))
	}

	// The malformed family: 400s, no upstream contact.
	for _, rel := range []string{
		"/binflow/helm-r/_external",
		"/binflow/helm-r/_external/ftp/example.com/x.tgz",
		"/binflow/helm-r/_external/https/",
		"/binflow/helm-r/_external/https/at-sign@example.com/x.tgz",
	} {
		if status, body, _ = f.get(rel); status != http.StatusBadRequest {
			t.Errorf("%s = (%d, %s), want 400", rel, status, body)
		}
	}
	// The upstream miss: 404.
	if status, _, _ = f.get(f.externalWirePath(f.extURL("/deps/absent-1.0.0.tgz"))); status != http.StatusNotFound {
		t.Errorf("absent _external = %d, want 404", status)
	}
	// HEAD is not part of the family (helm.md section 2 lists GET only).
	if status, _, hdr = f.do(http.MethodHead, f.externalWirePath(abs), "", "", nil, nil); status != http.StatusMethodNotAllowed || hdr.Get("Allow") != "GET" {
		t.Errorf("_external HEAD = %d, want the GET-only 405", status)
	}
}

func TestRemoteExternalAllowListRefusal(t *testing.T) {
	// A narrowed allow list: the pinned 400 wording, verbatim head.
	s := newStackOpt(t, stackOptions{extPatterns: []string{"https://internal.example.com/**"}})
	up := newChartUpstream(t, map[string]string{})
	s.seedRemoteRepo(t, "helm-r", up.srv.URL)
	abs := up.srv.URL + "/deps/x-1.0.0.tgz"
	status, body, _ := s.get("/binflow/helm-r/" + mustFold(t, abs))
	if status != http.StatusBadRequest {
		t.Fatalf("off-list _external = (%d, %s), want 400", status, body)
	}
	want := fmt.Sprintf("Could not download HELM package at %s - URL is not configured as an external dependency", abs)
	if body != want+"\n" {
		t.Errorf("allow-list refusal body = %q, want %q", body, want)
	}
}

func TestRemoteTransitiveProxy(t *testing.T) {
	f := newRemoteFixture(t)
	dep := fixtureChart(t, "transitive", defaultChartYAML("transitive", "1.0.0"), nil)
	// The upstream's OWN _external face: the upstream serves the folded
	// path itself (an Artifactory-like upstream would proxy it onward).
	folded, ok := foldExternalURL(f.extURL("/deps/transitive-1.0.0.tgz"))
	if !ok {
		t.Fatalf("fold")
	}
	f.up.files["/"+folded] = string(dep)

	wire := "/binflow/helm-r/_transitive/" + strings.TrimPrefix(folded, segExternal+"/")
	status, body, _ := f.get(wire)
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(dep) {
		t.Fatalf("_transitive = (%d, %d bytes), want the upstream's proxied bytes", status, len(body))
	}
	// The engine's cache holds the landed copy: one upstream request.
	var hdr http.Header
	if status, _, hdr = f.get(wire); status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second _transitive = (%d, cache %q), want 200/HIT", status, hdr.Get("X-BinFlow-Cache"))
	}
	if n := f.up.hitCount("/" + folded); n != 1 {
		t.Errorf("upstream _external-face hits = %d, want 1 (cached)", n)
	}
}

// mustFold is the test helper of foldExternalURL.
func mustFold(t *testing.T, abs string) string {
	t.Helper()
	folded, ok := foldExternalURL(abs)
	if !ok {
		t.Fatalf("foldExternalURL(%q)", abs)
	}
	return folded
}
