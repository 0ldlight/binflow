package nuget

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The remote and virtual faces against a fake upstream that speaks the
// nuget.org v3 path shapes (the provider's prefix join) and embeds
// absolute upstream URLs in its registration documents (the rewrite
// input).

// fakeUpstream is one loopback nuget v3 upstream.
type fakeUpstream struct {
	srv   *httptest.Server
	hits  atomic.Int64
	paths map[string]int
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	up := &fakeUpstream{paths: map[string]int{}}
	mux := http.NewServeMux()
	up.srv = httptest.NewServer(mux)
	t.Cleanup(up.srv.Close)
	return up
}

// serveV3Index serves the upstream service index whose @ids cite this
// fake's base under the nuget.org spellings (the registration gz-semver2
// base, the flatcontainer base, and /search for the search family).
func (up *fakeUpstream) serveV3Index(t *testing.T) {
	t.Helper()
	up.serve(t, "/v3/index.json", v3UpstreamIndexFixture(t, up.srv.URL), "application/json")
}

// serve registers one upstream path with a canned body.
func (up *fakeUpstream) serve(t *testing.T, path string, body []byte, ctype string) {
	t.Helper()
	up.paths[path] = 0
	up.srv.Config.Handler.(*http.ServeMux).HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		up.hits.Add(1)
		up.paths[path]++
		w.Header().Set("Content-Type", ctype)
		_, _ = w.Write(body) //nolint:gosec // G705: test fixture
	})
}

// count reports one path's hit count.
func (up *fakeUpstream) count(path string) int { return up.paths[path] }

// v3UpstreamIndexFixture renders one fake upstream service index (the
// @id spellings the fixtures serve — the search @id lives on the same
// host here, unlike the real nuget.org's azuresearch split, but the
// adapter resolves it as an ABSOLUTE URL either way).
func v3UpstreamIndexFixture(t *testing.T, base string) []byte {
	t.Helper()
	doc := serviceIndexDocument{Version: "3.0.0", Resources: []serviceIndexResource{
		{ID: base + "/" + v3FallbackRegPath + "/", Type: "RegistrationsBaseUrl"},
		{ID: base + "/" + v3FallbackRegPath + "/", Type: "RegistrationsBaseUrl/3.4.0"},
		{ID: base + "/" + v3FallbackRegPath + "/", Type: "RegistrationsBaseUrl/3.6.0"},
		{ID: base + "/" + v3FallbackFlatPath + "/", Type: "PackageBaseAddress/3.0.0"},
		{ID: base + "/search", Type: "SearchQueryService"},
	}}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture index: %v", err)
	}
	return body
}

// TestRemotePullThrough: the versions document, the registration index
// (rewritten onto BinFlow bases) and the nupkg all proxy through, and the
// SECOND read of each is the cache hit. The upstream resources are located
// through the upstream's own service index (nuget.md section 9.2 — the
// .nuGetV3 markers).
func TestRemotePullThrough(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)

	pkg := buildNupkg(t, "Serilog", "4.0.2", flatDeps("Serilog.AspNetCore", "8.0.0"))
	up := newFakeUpstream(t)
	up.serveV3Index(t)
	up.serve(t, "/v3-flatcontainer/serilog/index.json",
		[]byte(`{"versions":["4.0.1","4.0.2"]}`), "application/json")
	up.serve(t, "/"+v3FallbackRegPath+"/serilog/index.json",
		upstreamRegistration("serilog", "Serilog", "4.0.2", up.srv.URL, pkg), "application/json")
	up.serve(t, "/"+remNupkgPath("serilog", "4.0.2"), pkg.body, "application/octet-stream")
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	// The versions document: proxied verbatim.
	for i := 0; i < 2; i++ {
		status, body, _ := s.get(apiPath("ng-remote") + "/flatcontainer/serilog/index.json")
		if status != http.StatusOK || !strings.Contains(body, `"4.0.2"`) {
			t.Fatalf("versions GET #%d = (%d, %s)", i, status, body)
		}
	}
	if n := up.count("/v3-flatcontainer/serilog/index.json"); n != 1 {
		t.Errorf("versions upstream contacts = %d, want 1", n)
	}
	if n := up.count("/v3/index.json"); n != 1 {
		t.Errorf("service index contacts = %d, want 1 (cached on the metadata TTL)", n)
	}

	// The registration: the body served is REWRITTEN — every embedded
	// upstream URL now points into BinFlow's own faces (section 9.4: the
	// id/registration family onto the v3 registration base, packageContent
	// onto the v2 Download face).
	status, body, _ := s.get(apiPath("ng-remote") + "/registration/serilog/index.json")
	if status != http.StatusOK {
		t.Fatalf("registration status = %d, body %s", status, body)
	}
	if strings.Contains(body, up.srv.URL) {
		t.Errorf("registration still cites the upstream:\n%s", body)
	}
	if !strings.Contains(body, "/binflow/api/nuget/v3/ng-remote/registration/serilog/index.json") {
		t.Errorf("page @id was not rewritten onto the v3 registration base:\n%s", body)
	}
	if !strings.Contains(body, "/binflow/api/nuget/v2/ng-remote/Download/serilog/4.0.2") {
		t.Errorf("packageContent was not rewritten onto the v2 Download face (section 9.4):\n%s", body)
	}
	if !strings.Contains(body, `"dependencyGroups"`) && !strings.Contains(body, "Serilog.AspNetCore") {
		t.Logf("note: upstream fixture carries no dependency groups (shape kept verbatim)")
	}

	// The nupkg: byte-for-byte through the streaming class.
	status, body, _ = s.get(apiPath("ng-remote") + "/flatcontainer/serilog/4.0.2/serilog.4.0.2.nupkg")
	if status != http.StatusOK || body != string(pkg.body) {
		t.Fatalf("nupkg GET = %d (len %d, want %d)", status, len(body), len(pkg.body))
	}
	status, _, _ = s.get(apiPath("ng-remote") + "/flatcontainer/serilog/4.0.2/serilog.4.0.2.nupkg")
	if status != http.StatusOK {
		t.Fatalf("nupkg second GET = %d", status)
	}
	if n := up.count("/" + remNupkgPath("serilog", "4.0.2")); n != 1 {
		t.Errorf("nupkg upstream contacts = %d, want 1 (the second read must be the cache hit)", n)
	}
}

// TestRemoteRegistrationGzip: a forced-gzip upstream body (nuget.org's
// registration posture) still serves as readable rewritten JSON.
func TestRemoteRegistrationGzip(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)

	pkg := buildNupkg(t, "Zipped.Pkg", "1.0.0", flatDeps("none"))
	up := newFakeUpstream(t)
	up.serveV3Index(t)
	doc := upstreamRegistration("zipped.pkg", "Zipped.Pkg", "1.0.0", up.srv.URL, pkg)
	up.serve(t, "/"+v3FallbackRegPath+"/zipped.pkg/index.json", gzipBytes(t, doc), "application/json")
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	status, body, _ := s.get(apiPath("ng-remote") + "/registration/zipped.pkg/index.json")
	if status != http.StatusOK || !strings.Contains(body, "/binflow/api/nuget/v3/ng-remote/") {
		t.Fatalf("gzip registration GET = (%d, %s…), want the gunzipped rewritten body", status, firstLine(body))
	}
}

// TestVirtualAggregation: local-first registration/versions merge across
// a local member and a remote member; a local package stays local, a
// remote package walks to the remote member.
func TestVirtualAggregation(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)

	// Local member: one package.
	loc := buildNupkg(t, "Local.Lib", "1.0.0", flatDeps("none"))
	if status, body, _ := s.put(pushPath("ng-local", "local.lib", "1.0.0"), loc.body, nil); status != 201 {
		t.Fatalf("local push: %d %s", status, body)
	}
	// Remote member: another package upstream.
	rem := buildNupkg(t, "Rem.Lib", "2.0.0", flatDeps("none"))
	up := newFakeUpstream(t)
	up.serveV3Index(t)
	up.serve(t, "/"+v3FallbackRegPath+"/rem.lib/index.json",
		upstreamRegistration("rem.lib", "Rem.Lib", "2.0.0", up.srv.URL, rem), "application/json")
	up.serve(t, "/"+remNupkgPath("rem.lib", "2.0.0"), rem.body, "application/octet-stream")
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	s.seedRepo(t, "ng-virt", repo.TypeVirtual)
	s.seedVirtualMembers(t, "ng-virt", "ng-local", "ng-remote")

	// Registration merge: BOTH members' packages appear.
	status, body, _ := s.get(apiPath("ng-virt") + "/registration/local.lib/index.json")
	if status != 200 || !strings.Contains(body, `"Local.Lib"`) {
		t.Errorf("virtual local registration = (%d, %s…)", status, firstLine(body))
	}
	status, body, _ = s.get(apiPath("ng-virt") + "/registration/rem.lib/index.json")
	if status != 200 || !strings.Contains(body, `"Rem.Lib"`) {
		t.Errorf("virtual remote registration = (%d, %s…)", status, firstLine(body))
	}
	if strings.Contains(body, up.srv.URL) {
		t.Errorf("virtual remote registration still cites the upstream:\n%s", body)
	}

	// Downloads resolve: local from the local member, remote through the
	// pull-through.
	status, body, _ = s.get(apiPath("ng-virt") + "/flatcontainer/local.lib/1.0.0/local.lib.1.0.0.nupkg")
	if status != 200 || body != string(loc.body) {
		t.Errorf("virtual local download = %d (len %d)", status, len(body))
	}
	status, body, _ = s.get(apiPath("ng-virt") + "/flatcontainer/rem.lib/2.0.0/rem.lib.2.0.0.nupkg")
	if status != 200 || body != string(rem.body) {
		t.Errorf("virtual remote download = %d (len %d)", status, len(body))
	}
	if n := up.count("/" + remNupkgPath("rem.lib", "2.0.0")); n != 1 {
		t.Errorf("virtual remote download upstream contacts = %d, want 1", n)
	}

	// The v2 feed aggregates the same facts.
	status, body, _ = s.get(apiV2Path("ng-virt") + "/FindPackagesById()?id='Local.Lib'")
	if status != 200 || !strings.Contains(body, "<entry>") {
		t.Errorf("virtual v2 feed = (%d, %s…)", status, firstLine(body))
	}

	// A push onto the virtual is the service's deployment-member face
	// (a configured deploy member required) — not this adapter's
	// surface; the goproxy pilot's identical posture.
}

// TestSearchOnVirtualCoversLocalMember: the search face walks the local
// members (the remote member contribution is the cached documents' —
// doc.go's recorded limitation).
func TestSearchOnVirtualCoversLocalMember(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	s.seedRepo(t, "ng-virt", repo.TypeVirtual)
	s.seedVirtualMembers(t, "ng-virt", "ng-local")

	pkg := buildNupkg(t, "Findable.Lib", "1.0.0", flatDeps("none"))
	if status, body, _ := s.put(pushPath("ng-local", "findable.lib", "1.0.0"), pkg.body, nil); status != 201 {
		t.Fatalf("push: %d %s", status, body)
	}
	status, body, _ := s.get(apiPath("ng-virt") + "/query?q=findable")
	if status != 200 || !strings.Contains(body, "findable.lib") {
		t.Errorf("virtual search = (%d, %s…)", status, firstLine(body))
	}
}

// gzipBytes gzips one body (the forced-gzip upstream posture).
func gzipBytes(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		t.Fatalf("gzip fixture: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip fixture close: %v", err)
	}
	return buf.Bytes()
}
