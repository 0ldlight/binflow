package nuget

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The v3 upstream proxy (nuget.md sections 8.1 and 9) against fake
// upstreams: the search chain (index resolution → SearchQueryService →
// direct GET → 400-retry → URL rewrite), the dynamic document resolution
// (an upstream whose @id spellings are NOT nuget.org's), the semver2
// shape, and the upstream-deleted-still-cached leg.

// v3SearchUpstream is one fake upstream counting per-path contacts with a
// mutable search responder (the retry leg flips behavior).
type v3SearchUpstream struct {
	srv    *httptest.Server
	hits   map[string]*atomic.Int64
	search func(w http.ResponseWriter, r *http.Request, calls int)
}

func newV3SearchUpstream(t *testing.T, search func(w http.ResponseWriter, r *http.Request, calls int)) *v3SearchUpstream {
	t.Helper()
	up := &v3SearchUpstream{hits: map[string]*atomic.Int64{}, search: search}
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		n := up.count("/search").Add(1)
		up.search(w, r, int(n))
	})
	up.srv = httptest.NewServer(mux)
	t.Cleanup(up.srv.Close)
	return up
}

// serveSearchIndex registers the service index citing this fake's base and
// its /search face (call once, after construction).
func (up *v3SearchUpstream) serveSearchIndex(t *testing.T) {
	t.Helper()
	doc := fmt.Sprintf(`{"version":"3.0.0","resources":[`+
		`{"@id":"%s/%s/","@type":"RegistrationsBaseUrl"},`+
		`{"@id":"%s/%s/","@type":"RegistrationsBaseUrl/3.6.0"},`+
		`{"@id":"%s/%s/","@type":"PackageBaseAddress/3.0.0"},`+
		`{"@id":"%s/search","@type":"SearchQueryService"}]}`,
		up.srv.URL, v3FallbackRegPath, up.srv.URL, v3FallbackRegPath,
		up.srv.URL, v3FallbackFlatPath, up.srv.URL)
	up.srv.Config.Handler.(*http.ServeMux).HandleFunc("/v3/index.json", func(w http.ResponseWriter, _ *http.Request) {
		up.count("/v3/index.json").Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(doc)) //nolint:gosec // test fixture
	})
}

func (up *v3SearchUpstream) count(path string) *atomic.Int64 {
	if c, ok := up.hits[path]; ok {
		return c
	}
	c := &atomic.Int64{}
	up.hits[path] = c
	return c
}

// upstreamSearchBody renders one search answer whose registration URLs
// cite the fake base.
func upstreamSearchBody(base, id, version string) []byte {
	return []byte(fmt.Sprintf(`{"totalHits":1,"data":[{"id":%q,"version":%q,`+
		`"description":"upstream fixture","versions":[{"version":%q,"downloads":42}],`+
		`"registration":%q}]}`,
		id, version, version, base+"/v3/registration5-gz-semver2/"+strings.ToLower(id)+"/index.json"))
}

// TestV3RemoteSearchProxy: the remote search answers the UPSTREAM's live
// result with every URL rewritten onto this instance (section 8.1).
func TestV3RemoteSearchProxy(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	var baseURL string
	up := newV3SearchUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(upstreamSearchBody(baseURL, "Serilog", "4.0.2")) //nolint:gosec // test fixture
	})
	baseURL = up.srv.URL
	up.serveSearchIndex(t)
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	status, body, _ := s.get(apiPath("ng-remote") + "/query?q=serilog")
	if status != http.StatusOK {
		t.Fatalf("remote search = %d %s", status, body)
	}
	if !strings.Contains(body, `"Serilog"`) || !strings.Contains(body, `"totalHits":1`) {
		t.Fatalf("proxied body wrong: %s", firstLine(body))
	}
	// Section 8.1-7: the registration URL is rewritten onto this repo's v3
	// registration base (NOT the -semver2 one — no semVerLevel on the request).
	if want := "/binflow/api/nuget/v3/ng-remote/registration/serilog/index.json"; !strings.Contains(body, want) {
		t.Errorf("registration URL not rewritten (want %s):\n%s", want, body)
	}
	if strings.Contains(body, up.srv.URL) {
		t.Errorf("search answer still cites the upstream:\n%s", body)
	}
	// The query BinFlow sent upstream.
	if n := up.count("/search").Load(); n != 1 {
		t.Errorf("search upstream contacts = %d, want 1", n)
	}
}

// TestV3RemoteSearchSemVerLevel: a request carrying semVerLevel rewrites
// the registration family onto the -semver2 base (section 8.1-7) and
// forwards the parameter upstream.
func TestV3RemoteSearchSemVerLevel(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	var gotQuery string
	var baseURL string
	up := newV3SearchUpstream(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(upstreamSearchBody(baseURL, "Serilog", "4.0.2")) //nolint:gosec // test fixture
	})
	baseURL = up.srv.URL
	up.serveSearchIndex(t)
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	status, body, _ := s.get(apiPath("ng-remote") + "/query?q=serilog&semVerLevel=2.0.0&take=5")
	if status != http.StatusOK {
		t.Fatalf("semVerLevel search = %d %s", status, body)
	}
	if want := "/binflow/api/nuget/v3/ng-remote/registration-semver2/serilog/index.json"; !strings.Contains(body, want) {
		t.Errorf("registration URL not rewritten onto the semver2 base (want %s):\n%s", want, body)
	}
	for _, param := range []string{"q=serilog", "semVerLevel=2.0.0", "take=5"} {
		if !strings.Contains(gotQuery, param) {
			t.Errorf("upstream query %q misses %q", gotQuery, param)
		}
	}
}

// TestV3RemoteSearchRetryOn400: an upstream 400 for a big take is retried
// once at take=100 (section 8.1-5).
func TestV3RemoteSearchRetryOn400(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	var baseURL string
	up := newV3SearchUpstream(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		if r.URL.Query().Get("take") != "100" {
			http.Error(w, "take too large", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(upstreamSearchBody(baseURL, "Big.Lib", "9.9.9")) //nolint:gosec // test fixture
	})
	baseURL = up.srv.URL
	up.serveSearchIndex(t)
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	status, body, _ := s.get(apiPath("ng-remote") + "/query?take=1000")
	if status != http.StatusOK || !strings.Contains(body, "Big.Lib") {
		t.Fatalf("retry search = (%d, %s…), want the take=100 retry's answer", status, firstLine(body))
	}
	if n := up.count("/search").Load(); n != 2 {
		t.Errorf("search upstream contacts = %d, want 2 (the 400 then the retry)", n)
	}
}

// TestV3RemoteSearchFallback: an upstream whose search face answers
// nothing degrades to the landed-facts arm — 200 with the empty set,
// never a 5xx (section 8.1-6's null-contribution reading for the single
// repository face). The local-only listing seam (svc.List) cannot
// enumerate a remote repository's landed nodes — the same degradation
// T-337 registered for the v2 face — so the empty set is the answer.
func TestV3RemoteSearchFallback(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	up := newV3SearchUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		http.Error(w, "gone", http.StatusNotFound)
	})
	up.serveSearchIndex(t)
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	status, body, _ := s.get(apiPath("ng-remote") + "/query?q=anything")
	if status != http.StatusOK || !strings.Contains(body, `"totalHits":0`) {
		t.Fatalf("fallback search = (%d, %s), want the empty 200", status, firstLine(body))
	}

	// An upstream with NO search service at all (the index names none):
	// the same never-5xx degradation.
	bare := newFakeUpstream(t)
	bare.serve(t, "/v3/index.json", []byte(`{"version":"3.0.0","resources":[]}`), "application/json")
	s2 := newStack(t)
	s2.seedRepo(t, "ng-remote2", repo.TypeRemote)
	s2.seedRemoteConfig(t, "ng-remote2", bare.srv.URL)
	status, body, _ = s2.get(apiPath("ng-remote2") + "/query")
	if status != http.StatusOK || !strings.Contains(body, `"totalHits":0`) {
		t.Fatalf("no-search-service query = (%d, %s), want the empty 200", status, firstLine(body))
	}
}

// TestV3DynamicUpstreamSpellings: an upstream whose registration and
// flatcontainer @id paths are NOT nuget.org's spellings still proxies —
// the type-driven resolution (section 9.2), the very case the T-304 L4
// ruling cites against the prefix constants.
func TestV3DynamicUpstreamSpellings(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)

	pkg := buildNupkg(t, "Hetero.Lib", "1.0.0", flatDeps("none"))
	const (
		regPath  = "nuget/registration-hive-a"
		flatPath = "nuget/packages-b"
	)
	up := newFakeUpstream(t)
	index := fmt.Sprintf(`{"version":"3.0.0","resources":[`+
		`{"@id":"%s/%s/","@type":"RegistrationsBaseUrl"},`+
		`{"@id":"%s/%s/","@type":"RegistrationsBaseUrl/3.6.0"},`+
		`{"@id":"%s/%s/","@type":"PackageBaseAddress/3.0.0"},`+
		`{"@id":"%s/search","@type":"SearchQueryService"}]}`,
		up.srv.URL, regPath, up.srv.URL, regPath, up.srv.URL, flatPath, up.srv.URL)
	up.serve(t, "/v3/index.json", []byte(index), "application/json")
	up.serve(t, "/"+regPath+"/hetero.lib/index.json",
		upstreamRegistrationAt(up.srv.URL, regPath, flatPath, "hetero.lib", "Hetero.Lib", "1.0.0", pkg), "application/json")
	up.serve(t, "/"+flatPath+"/hetero.lib/index.json", []byte(`{"versions":["1.0.0"]}`), "application/json")
	up.serve(t, "/"+flatPath+"/hetero.lib/1.0.0/hetero.lib.1.0.0.nupkg", pkg.body, "application/octet-stream")
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	// The registration document proxies under the custom spelling and its
	// URLs re-anchor onto BinFlow's faces.
	status, body, _ := s.get(apiPath("ng-remote") + "/registration/hetero.lib/index.json")
	if status != http.StatusOK {
		t.Fatalf("heterogeneous registration = %d %s", status, body)
	}
	if strings.Contains(body, up.srv.URL) || !strings.Contains(body, "/binflow/api/nuget/v3/ng-remote/registration/hetero.lib/index.json") {
		t.Errorf("heterogeneous registration not re-anchored:\n%s", body)
	}

	// The versions document and the package ride the custom flat spelling.
	status, body, _ = s.get(apiPath("ng-remote") + "/flatcontainer/hetero.lib/index.json")
	if status != http.StatusOK || !strings.Contains(body, "1.0.0") {
		t.Fatalf("heterogeneous versions = (%d, %s)", status, firstLine(body))
	}
	if n := up.count("/" + flatPath + "/hetero.lib/index.json"); n != 1 {
		t.Errorf("custom flat path contacts = %d, want 1", n)
	}
	status, body, _ = s.get(apiPath("ng-remote") + "/flatcontainer/hetero.lib/1.0.0/hetero.lib.1.0.0.nupkg")
	if status != http.StatusOK || body != string(pkg.body) {
		t.Fatalf("heterogeneous nupkg = %d (len %d)", status, len(body))
	}

	// The semver2 shape resolves the SAME custom base (the /3.6.0 row).
	status, body, _ = s.get(apiPath("ng-remote") + "/registration-semver2/hetero.lib/index.json")
	if status != http.StatusOK || !strings.Contains(body, "\"Hetero.Lib\"") {
		t.Fatalf("semver2 registration = (%d, %s…)", status, firstLine(body))
	}
}

// TestV3RegistrationUpstreamDeleted: a document whose upstream copy is
// DELETED still serves from the cache inside the TTL (the engine's cache
// semantics — the upstream-deletion comparison leg).
func TestV3RegistrationUpstreamDeleted(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	pkg := buildNupkg(t, "Gone.Lib", "1.0.0", flatDeps("none"))

	deleted := &atomic.Bool{}
	up := newFakeUpstream(t)
	up.serveV3Index(t)
	up.srv.Config.Handler.(*http.ServeMux).HandleFunc("/"+v3FallbackRegPath+"/gone.lib/index.json", func(w http.ResponseWriter, _ *http.Request) {
		if deleted.Load() {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(upstreamRegistration("gone.lib", "Gone.Lib", "1.0.0", up.srv.URL, pkg)) //nolint:gosec // test fixture
	})
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	if status, body, _ := s.get(apiPath("ng-remote") + "/registration/gone.lib/index.json"); status != http.StatusOK {
		t.Fatalf("first registration GET = %d %s", status, body)
	}
	deleted.Store(true)
	status, body, _ := s.get(apiPath("ng-remote") + "/registration/gone.lib/index.json")
	if status != http.StatusOK || !strings.Contains(body, "Gone.Lib") {
		t.Fatalf("post-deletion registration GET = (%d, %s…), want the cached copy", status, firstLine(body))
	}
}

// TestV3Ladders is the table-driven unit probe of the section 9.2 ladders
// and the section 8.1-2 search ladder.
func TestV3Ladders(t *testing.T) {
	mk := func(types ...string) *upstreamIndex {
		idx := &upstreamIndex{}
		for _, typ := range types {
			idx.resources = append(idx.resources, serviceIndexResource{
				ID: "https://up.example/hive-" + typ, Type: typ,
			})
		}
		return idx
	}
	cases := []struct {
		name string
		idx  *upstreamIndex
	}{
		{"nil index (the fallback posture)", nil},
		{"plain only", mk("RegistrationsBaseUrl")},
		{"versioned only", mk("RegistrationsBaseUrl/3.6.0")},
		{"rc only", mk("RegistrationsBaseUrl/3.0.0-rc")},
		{"search beta only", mk("SearchQueryService/3.0.0-beta")},
	}
	for _, tc := range cases {
		if got := tc.idx.registrationPath(false); got == "" {
			t.Errorf("%s: plain registration path empty", tc.name)
		}
		if got := tc.idx.registrationPath(true); got == "" {
			t.Errorf("%s: semver2 registration path empty", tc.name)
		}
	}
	// The ladder is one-directional (versioned falls back to plain,
	// section 9.2): the semver2 shape over a versioned-only index answers
	// the versioned hive; the semver2 shape over a plain-only index falls
	// back to the plain hive; the PLAIN shape over a versioned-only index
	// is degenerate (every real index carries the plain type) and takes
	// the nuget.org-family fallback.
	if got := mk("RegistrationsBaseUrl/3.6.0").registrationPath(true); got != "hive-RegistrationsBaseUrl/3.6.0" {
		t.Errorf("semver2 shape over versioned-only index = %q", got)
	}
	if got := mk("RegistrationsBaseUrl").registrationPath(true); got != "hive-RegistrationsBaseUrl" {
		t.Errorf("semver2 shape over plain-only index = %q", got)
	}
	if got := mk("RegistrationsBaseUrl/3.6.0").registrationPath(false); got != v3FallbackRegPath {
		t.Errorf("plain shape over versioned-only index = %q, want the fallback", got)
	}
	// The search ladder: plain → beta → rc, first hit.
	if got := mk("SearchQueryService", "SearchQueryService/3.0.0-beta").searchServiceAbsolute(); !strings.HasSuffix(got, "hive-SearchQueryService") {
		t.Errorf("search ladder plain-first = %q", got)
	}
	if got := mk("SearchQueryService/3.0.0-rc", "SearchQueryService/3.0.0-beta").searchServiceAbsolute(); !strings.HasSuffix(got, "3.0.0-beta") {
		t.Errorf("search ladder beta-before-rc = %q", got)
	}
	if got := (&upstreamIndex{}).searchServiceAbsolute(); got != "" {
		t.Errorf("empty index search = %q, want \"\"", got)
	}
}

// TestV2DownloadURLMapping is the section 9.4 packageContent mapping's
// unit probe (lowercasing, build-metadata stripping, shape refusals).
func TestV2DownloadURLMapping(t *testing.T) {
	cases := []struct {
		tail string
		want string
		ok   bool
	}{
		{"serilog/4.0.2/serilog.4.0.2.nupkg", "/binflow/api/nuget/v2/r/Download/serilog/4.0.2", true},
		{"Newsoft.Json/2.0.0-Beta.1+meta/Newsoft.Json.2.0.0-Beta.1+meta.nupkg", "/binflow/api/nuget/v2/r/Download/newsoft.json/2.0.0-beta.1", true},
		{"serilog/4.0.2/other-name.nupkg", "", false},
		{"not-a-tail", "", false},
	}
	for _, tc := range cases {
		got, ok := v2DownloadURL("http://x", "r", tc.tail)
		if ok != tc.ok || (ok && got != "http://x"+tc.want) {
			t.Errorf("v2DownloadURL(%q) = (%q, %v), want (%q, %v)", tc.tail, got, ok, "http://x"+tc.want, tc.ok)
		}
	}
}
