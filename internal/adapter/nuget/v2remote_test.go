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

// The v2 remote search family's upstream proxy (nuget.md section 7.1)
// against a fake upstream that speaks the v2 OData wire under the api/v2
// context path: the feed is proxied and re-anchored, the $count family
// answers the upstream digits, a non-numeric count body is the -1
// sentinel, the engine caches per query, and an upstream fault falls back
// to the repository's landed facts (the offline arm).

// v2Upstream is one fake v2 OData upstream counting contacts per resource.
type v2Upstream struct {
	srv   *httptest.Server
	hits  atomic.Int64
	paths map[string]*atomic.Int64
}

func newV2Upstream(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, res string)) *v2Upstream {
	t.Helper()
	up := &v2Upstream{paths: map[string]*atomic.Int64{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		res := strings.TrimPrefix(r.URL.Path, "/")
		if r.URL.RawQuery != "" {
			res += "?" + r.URL.RawQuery
		}
		counter, ok := up.paths[res]
		if !ok {
			counter = &atomic.Int64{}
			up.paths[res] = counter
		}
		counter.Add(1)
		up.hits.Add(1)
		handler(w, r, res)
	})
	up.srv = httptest.NewServer(mux)
	t.Cleanup(up.srv.Close)
	return up
}

func (up *v2Upstream) count(res string) int64 {
	if c, ok := up.paths[res]; ok {
		return c.Load()
	}
	return 0
}

// upstreamV2Feed renders one minimal Atom feed over the given versions.
func upstreamV2Feed(id string, versions ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	b.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:d="http://schemas.microsoft.com/ado/2007/08/dataservices" xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata" xml:base="https://fake.up/api/v2">`)
	for _, v := range versions {
		fmt.Fprintf(&b, `<entry><id>https://fake.up/api/v2/Packages(Id='%s',Version='%s')</id><updated>2026-08-01T00:00:00Z</updated>`, id, v)
		fmt.Fprintf(&b, `<content type="application/zip" src="https://fake.up/api/v2/Download/%s/%s"/>`, id, v)
		b.WriteString(`<m:properties>`)
		fmt.Fprintf(&b, `<d:Id>%s</d:Id><d:Version>%s</d:Version>`, id, v)
		b.WriteString(`<d:Authors>Fake Up</d:Authors><d:PackageHashAlgorithm>SHA512</d:PackageHashAlgorithm>`)
		b.WriteString(`</m:properties></entry>`)
	}
	b.WriteString(`</feed>`)
	return b.String()
}

// TestV2RemoteProxyFeed: Search()/FindPackagesById()/Packages() proxy the
// upstream feed, re-anchor every URL onto this repository's v2 faces, and
// the second identical read rides the marker cache.
func TestV2RemoteProxyFeed(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	up := newV2Upstream(t, func(w http.ResponseWriter, _ *http.Request, res string) {
		switch {
		case strings.HasPrefix(res, "api/v2/Search()"):
			w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
			_, _ = w.Write([]byte(upstreamV2Feed("Fake.Lib", "1.0.0", "1.1.0"))) //nolint:gosec // test fixture
		case strings.HasPrefix(res, "api/v2/FindPackagesById()"):
			w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
			_, _ = w.Write([]byte(upstreamV2Feed("Fake.Lib", "1.0.0"))) //nolint:gosec // test fixture
		default:
			http.NotFound(w, nil)
		}
	})
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	for i := 0; i < 2; i++ {
		status, body, _ := s.get(apiV2Path("ng-remote") + "/Search()")
		if status != http.StatusOK || !strings.Contains(body, "Fake.Lib") {
			t.Fatalf("proxied Search #%d = (%d, %s…)", i, status, firstLine(body))
		}
		if strings.Contains(body, "fake.up") {
			t.Errorf("proxied Search still cites the upstream:\n%s", firstLine(body))
		}
		if !strings.Contains(body, "/binflow/api/nuget/v2/ng-remote/Download/fake.lib/") {
			t.Errorf("proxied Search download link not re-anchored:\n%s", firstLine(body))
		}
	}
	if n := up.count("api/v2/Search()"); n != 1 {
		t.Errorf("Search upstream contacts = %d, want 1 (second read is the marker cache hit)", n)
	}

	// FindPackagesById proxies with its query keyed into the marker.
	status, body, _ := s.get(apiV2Path("ng-remote") + "/FindPackagesById()?id='Fake.Lib'")
	if status != http.StatusOK || !strings.Contains(body, "1.0.0") {
		t.Fatalf("proxied FindPackagesById = (%d, %s…)", status, firstLine(body))
	}
	if n := up.count("api/v2/FindPackagesById()?id='Fake.Lib'"); n != 1 {
		t.Errorf("FindPackagesById resource spelling = %q (contacts %d)", "FindPackagesById()?id='Fake.Lib'", n)
	}
}

// TestV2RemoteProxyCount: the $count family answers the upstream digits;
// a non-numeric body is the -1 sentinel (section 7.1-2).
func TestV2RemoteProxyCount(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	up := newV2Upstream(t, func(w http.ResponseWriter, _ *http.Request, res string) {
		if strings.HasPrefix(res, "api/v2/Search()/$count") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("42")) //nolint:gosec // test fixture
			return
		}
		if strings.HasPrefix(res, "api/v2/Packages()/$count") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("not-a-number")) //nolint:gosec // test fixture
			return
		}
		http.NotFound(w, nil)
	})
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	status, body, _ := s.get(apiV2Path("ng-remote") + "/Search()/$count")
	if status != http.StatusOK || strings.TrimSpace(body) != "42" {
		t.Errorf("proxied $count = (%d, %q)", status, body)
	}
	status, body, _ = s.get(apiV2Path("ng-remote") + "/Packages()/$count")
	if status != http.StatusOK || strings.TrimSpace(body) != "-1" {
		t.Errorf("sentinel $count = (%d, %q), want -1", status, body)
	}
}

// TestV2RemoteProxyFallback: an upstream whose v2 search face answers
// nothing falls back to the repository's landed facts (section 7.1-1's
// offline arm). Until T-406 the local-only listing seam (svc.List) refused
// remote repositories, so this arm degraded to the family 404 — the
// degradation the original ticket report registered. T-406 opened the
// remote cache to the listing face, so the fallback now ENUMERATES the
// landed nodes: the warmed package surfaces as a 200 feed entry.
func TestV2RemoteProxyFallback(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	pkg := buildNupkg(t, "Landed.Lib", "3.0.0", flatDeps("none"))
	up := newV2Upstream(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		// The v2 search face answers nothing; the flatcontainer identity
		// serves the warm-up read so one nupkg lands in the cache.
		if strings.HasPrefix(r.URL.Path, "/v3-flatcontainer/landed.lib/") {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(pkg.body) //nolint:gosec // test fixture
			return
		}
		http.NotFound(w, nil)
	})
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	// Warm the cache with one proxied nupkg read (the engine lands the copy —
	// the DOWNLOAD face can still serve it below).
	if status, body, _ := s.get("/binflow/api/nuget/v3/ng-remote/flatcontainer/landed.lib/3.0.0/landed.lib.3.0.0.nupkg"); status != http.StatusOK {
		t.Fatalf("warm-up nupkg GET = %d %s", status, body)
	}

	// The search face itself: an upstream with no v2 answer and no cached
	// search response falls back to the landed cache facts (T-406 opened
	// svc.List to the remote cache) — the warmed package surfaces as a 200
	// feed entry, never a 5xx; the engine's stale-copy arm (a cached search
	// response served on upstream fault) is covered by the marker-cache
	// assertions of TestV2RemoteProxyFeed.
	status, body, _ := s.get(apiV2Path("ng-remote") + "/Search()")
	if status != http.StatusOK || !strings.Contains(body, "landed.lib") {
		t.Fatalf("fallback Search = (%d, %s…), want the 200 landed-facts feed", status, firstLine(body))
	}
	// The landed package is still DOWNLOADABLE through the canonical arm.
	if status, _, _ := s.get(apiV2Path("ng-remote") + "/Download/landed.lib/3.0.0"); status != http.StatusOK {
		t.Errorf("landed download = %d, want the cached copy", status)
	}
}

// TestV2RemoteDownloadFaces: the remote Download arm — the canonical
// pull-through first, the alternative api/v2/package hop behind it
// (section 5.3), and the miss wording.
func TestV2RemoteDownloadFaces(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	pkg := buildNupkg(t, "Alt.Dl", "1.0.0", flatDeps("none"))
	up := newV2Upstream(t, func(w http.ResponseWriter, _ *http.Request, res string) {
		if res == "api/v2/package/alt.dl/1.0.0" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(pkg.body) //nolint:gosec // test fixture
			return
		}
		http.NotFound(w, nil)
	})
	s.seedRemoteConfig(t, "ng-remote", up.srv.URL)

	status, body, _ := s.get(apiV2Path("ng-remote") + "/Download/alt.dl/1.0.0")
	if status != http.StatusOK || body != string(pkg.body) {
		t.Fatalf("remote Download = %d (len %d), want the alternative-hop bytes", status, len(body))
	}
	if n := up.count("api/v2/package/alt.dl/1.0.0"); n != 1 {
		t.Errorf("alternative-hop contacts = %d, want 1", n)
	}
	status, body, _ = s.get(apiV2Path("ng-remote") + "/Download/alt.dl/9.9.9")
	if status != http.StatusNotFound || !strings.Contains(body, "Unable to find NuPkg 'alt.dl-9.9.9' in 'ng-remote'") {
		t.Errorf("remote Download miss = (%d, %q)", status, firstLine(body))
	}
}

// TestV2QueryDequote is the OData operand family's unit probe.
func TestV2QueryDequote(t *testing.T) {
	cases := map[string]string{
		`'Feed.Pkg'`: "Feed.Pkg",
		`'Can''t'`:   "Can't",
		` Feed.Pkg `: "Feed.Pkg",
		`Feed.Pkg`:   "Feed.Pkg",
		`"Feed.Pkg"`: `"Feed.Pkg"`,
	}
	for raw, want := range cases {
		if got := dequote(raw); got != want {
			t.Errorf("dequote(%q) = %q, want %q", raw, got, want)
		}
	}
}
