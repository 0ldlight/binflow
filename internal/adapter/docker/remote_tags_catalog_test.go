package docker

// L001-1 (L000-B C15/C16, evidence E7): the remote plane's LIST endpoints
// — tags/list and the repo-domain _catalog — are the LIVE upstream
// aggregation, never the cached rows alone: the upstream is contacted on
// every call, its Link rel="next" pages are followed (the reference's
// list-iteration default of 3), the body is the pretty full listing with
// the name the upstream itself reported, and a failed upstream still
// answers 200 (tags render null, the catalog renders empty — E7-2's
// observed degradation). The stub upstreams speak raw registry JSON so
// the pagination and aggregation behavior is pinned without a second
// BinFlow stack in the loop.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newTagsStubUpstream serves /v2/myimg/tags/list from a mutable tag slice
// (the live-aggregation tests flip the slice between calls) and counts
// every request.
func newTagsStubUpstream(t *testing.T, tags *[]string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/v2/myimg/tags/list" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "myimg", "tags": *tags})
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestRemoteTagsListAggregatesUpstreamLive: the listing follows the
// upstream's CURRENT tag set on every call — a tag that appears upstream
// after BinFlow has cached a pull shows up immediately (the L000-F
// residual: the cached rows answered ["t1"] while the upstream held
// t1,t2), the upstream is contacted on every listing, the body is pretty,
// and the name is the UPSTREAM's image name (no repoKey prefix, E7-1).
func TestRemoteTagsListAggregatesUpstreamLive(t *testing.T) {
	tags := []string{"t1"}
	up, hits := newTagsStubUpstream(t, &tags)
	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", up.URL)

	code, body, _ := down.get("/v2/docker-remote/myimg/tags/list", nil)
	if code != http.StatusOK {
		t.Fatalf("first tags/list = (%d, %s), want 200", code, body)
	}
	var first tagsBody
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatalf("first body %q: %v", body, err)
	}
	if first.Name != "myimg" || len(first.Tags) != 1 || first.Tags[0] != "t1" {
		t.Fatalf("first listing = %+v, want the upstream's t1 under the upstream name", first)
	}
	if !strings.Contains(body, "\n") {
		t.Errorf("body %q is not pretty-printed", body)
	}

	// The upstream gains a tag the cache has never seen: the NEXT listing
	// carries it (live aggregation, not the cached rows).
	tags = append(tags, "t2")
	code, body, _ = down.get("/v2/docker-remote/myimg/tags/list", nil)
	if code != http.StatusOK {
		t.Fatalf("second tags/list = (%d, %s), want 200", code, body)
	}
	var second tagsBody
	if err := json.Unmarshal([]byte(body), &second); err != nil {
		t.Fatalf("second body %q: %v", body, err)
	}
	if len(second.Tags) != 2 || second.Tags[0] != "t1" || second.Tags[1] != "t2" {
		t.Fatalf("second listing = %+v, want the live t1,t2 in dictionary order", second)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("upstream hits = %d, want one per listing (2)", got)
	}
}

// TestRemoteTagsListFollowsUpstreamPages: the upstream's Link rel="next"
// chain is followed and the pages merged in dictionary order (E7-3's
// pagination aggregation), capped at the reference's 3-request iteration
// default.
func TestRemoteTagsListFollowsUpstreamPages(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.RawQuery {
		case "":
			w.Header().Set("Link", fmt.Sprintf(`<%s/v2/myimg/tags/list?last=b>; rel="next"`, srv.URL))
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "myimg", "tags": []string{"b", "a"}})
		case "last=b":
			w.Header().Set("Link", fmt.Sprintf(`<%s/v2/myimg/tags/list?last=c>; rel="next"`, srv.URL))
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "myimg", "tags": []string{"d", "c"}})
		default: // "last=c": the terminal page
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "myimg", "tags": []string{"e"}})
		}
	}))
	t.Cleanup(srv.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", srv.URL)

	code, body, _ := down.get("/v2/docker-remote/myimg/tags/list", nil)
	if code != http.StatusOK {
		t.Fatalf("tags/list = (%d, %s), want 200", code, body)
	}
	var got tagsBody
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	want := []string{"a", "b", "c", "d", "e"}
	if len(got.Tags) != len(want) {
		t.Fatalf("tags = %v, want the merged %v", got.Tags, want)
	}
	for i := range want {
		if got.Tags[i] != want[i] {
			t.Fatalf("tags = %v, want the dictionary-ordered %v", got.Tags, want)
		}
	}
}

// TestRemoteTagsListDegradedToNull: an unreachable upstream still answers
// 200 with "tags":null — the catalog arm's empty-200 degradation applied
// to the tags arm (the exact null-vs-empty spelling of this unobserved
// corner is BinFlow's own, pinned here).
func TestRemoteTagsListDegradedToNull(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	dead.Close() // refused from here on

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", dead.URL)

	code, body, _ := down.get("/v2/docker-remote/myimg/tags/list", nil)
	if code != http.StatusOK {
		t.Fatalf("degraded tags/list = (%d, %s), want 200", code, body)
	}
	if !strings.Contains(body, `"tags": null`) {
		t.Fatalf("degraded body %q lacks the null tags form", body)
	}
}

// TestRemoteRepoCatalogAggregatesUpstream: GET /v2/<repoKey>/_catalog is
// the live upstream catalog when the upstream serves one (E7-2/E7-3), and
// the empty list when it does not (the docker.io shape: upstream 404,
// still 200 {"repositories":[]}).
func TestRemoteRepoCatalogAggregatesUpstream(t *testing.T) {
	hasCatalog := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/_catalog" || !hasCatalog {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"y", "x"}})
	}))
	t.Cleanup(srv.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", srv.URL)

	code, body, _ := down.get("/v2/docker-remote/_catalog", nil)
	if code != http.StatusOK {
		t.Fatalf("repo _catalog = (%d, %s), want 200", code, body)
	}
	var cat catalogBody
	if err := json.Unmarshal([]byte(body), &cat); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if len(cat.Repositories) != 2 || cat.Repositories[0] != "x" || cat.Repositories[1] != "y" {
		t.Fatalf("repositories = %v, want the sorted upstream x,y", cat.Repositories)
	}
	if !strings.Contains(body, "\n") {
		t.Errorf("body %q is not pretty-printed", body)
	}

	// The upstream stops serving a catalog: still 200, now empty (E7-2).
	hasCatalog = false
	code, body, _ = down.get("/v2/docker-remote/_catalog", nil)
	if code != http.StatusOK || !strings.Contains(body, `"repositories": []`) {
		t.Fatalf("empty upstream catalog = (%d, %s), want 200 with []", code, body)
	}
}

// TestRemoteRepoCatalogDegradedOnDeadUpstream: an unreachable upstream
// still answers the empty 200 (never a 5xx, never a 404).
func TestRemoteRepoCatalogDegradedOnDeadUpstream(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	dead.Close()

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", dead.URL)

	code, body, _ := down.get("/v2/docker-remote/_catalog", nil)
	if code != http.StatusOK || !strings.Contains(body, `"repositories": []`) {
		t.Fatalf("dead-upstream catalog = (%d, %s), want 200 with []", code, body)
	}
}

// TestRepoCatalogRouteGates: the repo-domain catalog route keeps the
// plane's gates — a NON-remote row answers the standing not-a-name 404
// (the aggregation face is the remote plane's alone), a missing row is
// NAME_UNKNOWN, other verbs 405, and the nested /v2/<repo>/<img>/_catalog
// spelling is not hijacked.
func TestRepoCatalogRouteGates(t *testing.T) {
	up, _ := newTagsStubUpstream(t, &[]string{"t1"})
	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", up.URL)
	down.seedDockerRepo("plain-local")

	cases := []struct {
		method, path string
		wantStatus   int
		wantBody     string
	}{
		{http.MethodGet, "/v2/plain-local/_catalog", http.StatusNotFound, "carries no registry route"},
		{http.MethodGet, "/v2/no-such-repo/_catalog", http.StatusNotFound, ErrCodeNameUnknown},
		{http.MethodPost, "/v2/docker-remote/_catalog", http.StatusMethodNotAllowed, ""},
		{http.MethodGet, "/v2/docker-remote/myimg/_catalog", http.StatusNotFound, "carries no registry route"},
		{http.MethodGet, "/v2/docker-remote/_catalog/extra", http.StatusNotFound, "carries no registry route"},
	}
	for _, tc := range cases {
		code, body, _ := down.serveReq(tc.method, tc.path, nil, nil)
		if code != tc.wantStatus {
			t.Errorf("%s %s = (%d, %s), want %d", tc.method, tc.path, code, body, tc.wantStatus)
			continue
		}
		if tc.wantBody != "" && !strings.Contains(body, tc.wantBody) {
			t.Errorf("%s %s body %q lacks %q", tc.method, tc.path, body, tc.wantBody)
		}
	}
}
