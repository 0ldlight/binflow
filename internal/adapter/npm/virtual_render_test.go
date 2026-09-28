package npm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-82: the two T-66/T-71 seams on the npm transfer plane, over one
// end-to-end virtual fixture (local member first, remote member second,
// counting mock upstream):
//
//   - a *repo.StatusError renders verbatim — publish (PUT) through an
//     un-routed virtual repository answers the service's own 405 +
//     Allow: GET + C5 wording (the T-71 bug: npm had no
//     ErrRepoTypeNotSupported arm at all, so it fell into the default
//     500); DELETE (unpublish) through the virtual is the D-2 flip: the
//     delete touches only the virtual's own aggregation storage, so a
//     path it does not hold answers 404 and members survive
//     (virtual-resolution.md section 7.5 errata, T-524's ruling — the
//     pre-T-524 405 refusal is retired, maven T-531's precedent),
//   - a hinted body stream contributes X-BinFlow-Resolved-From (and the
//     remote member's X-BinFlow-Cache beneath it) to the packument and
//     tarball responses alike; the REPEAT pull serves through the
//     remote's cache-facet step with LOCAL semantics (design
//     virtual-four-bucket.md section 3, T-530) — byte-exact, zero
//     upstream, no cache verdict header.
func TestVirtualRenderSeams(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	hits := &atomic.Int64{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/remote-pkg/packument.json":
			_, _ = w.Write([]byte(`{"_id":"remote-pkg","name":"remote-pkg",` +
				`"dist-tags":{"latest":"1.0.0"},` +
				`"versions":{"1.0.0":{"name":"remote-pkg","version":"1.0.0",` +
				`"dist":{"tarball":"-/remote-pkg-1.0.0.tgz"}}}}`))
		case "/remote-pkg/-/remote-pkg-1.0.0.tgz":
			_, _ = w.Write([]byte("REMOTE-TARBALL"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	for _, row := range []*metadata.Repo{
		{RepoKey: "npmv-loc", Type: repo.TypeLocal, PackageType: Protocol},
		{RepoKey: "npmv-rem", Type: repo.TypeRemote, PackageType: Protocol,
			Config: `{"url":"` + upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "npmv-virt", Type: repo.TypeVirtual, PackageType: Protocol,
			Config: `{"repositories":["npmv-loc","npmv-rem"]}`},
	} {
		if _, err := s.svc.CreateRepo(ctx, adminPrincipal, row); err != nil {
			t.Fatalf("CreateRepo(%s): %v", row.RepoKey, err)
		}
	}

	// Seed the local member through the npm publish plane itself.
	doc := publishDoc("local-pkg", "1.0.0", "LOCAL-TARBALL", nil, nil)
	if rr := s.call(http.MethodPut, "/npmv-loc/local-pkg", mustJSON(doc), adminPrincipal, nil); rr.Code != http.StatusCreated {
		t.Fatalf("seed publish = %d; body=%s", rr.Code, bodyOf(rr))
	}

	// Packument via the virtual, local member: Resolved-From names the
	// member (the hints ride loadPackument, not only streamed tarballs).
	rr := s.call(http.MethodGet, "/npmv-virt/local-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("virtual packument (local) = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if got := rr.Header().Get(repo.HdrResolvedFrom); got != "npmv-loc" {
		t.Errorf("packument Resolved-From = %q, want npmv-loc", got)
	}

	// Tarball via the virtual, local member (the streamNode probe).
	rr = s.call(http.MethodGet, "/npmv-virt/local-pkg/-/local-pkg-1.0.0.tgz", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("virtual tarball (local) = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if got := rr.Header().Get(repo.HdrResolvedFrom); got != "npmv-loc" {
		t.Errorf("tarball Resolved-From = %q, want npmv-loc", got)
	}
	if got := rr.Header().Get("X-BinFlow-Cache"); got != "" {
		t.Errorf("local tarball carries X-BinFlow-Cache %q, want none", got)
	}
	if body := bodyOf(rr); body != "LOCAL-TARBALL" {
		t.Errorf("local tarball body = %q", body)
	}

	// Packument via the virtual, remote member: both hints on one response.
	rr = s.call(http.MethodGet, "/npmv-virt/remote-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("virtual packument (remote) = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if got := rr.Header().Get(repo.HdrResolvedFrom); got != "npmv-rem" {
		t.Errorf("remote packument Resolved-From = %q, want npmv-rem", got)
	}
	if got := rr.Header().Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("remote packument X-BinFlow-Cache = %q, want MISS", got)
	}

	// Tarball via the virtual, remote member: the pull-through caches and
	// the next serve is a HIT with the upstream frozen.
	rr = s.call(http.MethodGet, "/npmv-virt/remote-pkg/-/remote-pkg-1.0.0.tgz", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("virtual tarball (remote) = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if got := rr.Header().Get(repo.HdrResolvedFrom); got != "npmv-rem" {
		t.Errorf("remote tarball Resolved-From = %q, want npmv-rem", got)
	}
	if got := rr.Header().Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("remote tarball X-BinFlow-Cache = %q, want MISS", got)
	}
	if body := bodyOf(rr); body != "REMOTE-TARBALL" {
		t.Errorf("remote tarball body = %q", body)
	}
	// Repeat: the four-bucket order serves the standing copy through the
	// remote's cache-facet step with LOCAL semantics (design section 3:
	// zero upstream, no freshness window) — byte-exact content, the member
	// still named, the upstream frozen, and NO X-BinFlow-Cache header (the
	// fetch verdict header only rides the FR-20 body path; a
	// local-semantics read carries none — T-530, maven T-531's precedent).
	rr = s.call(http.MethodGet, "/npmv-virt/remote-pkg/-/remote-pkg-1.0.0.tgz", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("repeat tarball = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if got := rr.Header().Get("X-BinFlow-Cache"); got != "" {
		t.Errorf("repeat tarball X-BinFlow-Cache = %q, want none (cache-facet local-semantics serve)", got)
	}
	if got := rr.Header().Get(repo.HdrResolvedFrom); got != "npmv-rem" {
		t.Errorf("repeat tarball Resolved-From = %q, want npmv-rem", got)
	}
	if body := bodyOf(rr); body != "REMOTE-TARBALL" {
		t.Errorf("repeat tarball body = %q, want the byte-exact upstream copy", body)
	}
	if got := hits.Load(); got != 3 { // T-72: the local-pkg packument now MERGES, so its
		// walk also probes the remote member (a miss, +1 upstream request whose
		// negative-cache row quiets the next one); then the remote-pkg packument
		// and the first tarball. The repeat served from cache.
		t.Errorf("upstream hits = %d, want 3", got)
	}

	// Publish THROUGH the un-routed virtual: the C5 405 renders verbatim
	// (was the default arm's 500).
	pub := publishDoc("virt-pkg", "1.0.0", "V", nil, nil)
	rr = s.call(http.MethodPut, "/npmv-virt/virt-pkg", mustJSON(pub), adminPrincipal, nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("virtual publish = %d, want 405; body=%s", rr.Code, bodyOf(rr))
	}
	if got := rr.Header().Get("Allow"); got != "GET" {
		t.Errorf("virtual publish Allow = %q, want GET", got)
	}
	if body := bodyOf(rr); !strings.Contains(body,
		"No local repository was configured as local deployment repository for the (npmv-virt) virtual repository.") {
		t.Errorf("virtual publish body = %s", body)
	}

	// Unpublish DELETE through the virtual: the delete touches ONLY the
	// virtual's own aggregation storage — a path the virtual itself does
	// not hold answers 404 (npm's ITEM_NOT_FOUND face) and no member's
	// entity is touched (virtual-resolution.md section 7.5, the errata
	// that retired repo-semantics section 8.2's member-walk delete; the
	// pre-T-524 405 "no local deployment repository" refusal no longer
	// exists on this face — the reverse assertion below pins its
	// retirement, maven T-531's precedent).
	rr = s.call(http.MethodDelete, "/npmv-virt/local-pkg/-rev/1-deadbeef", "", adminPrincipal, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("virtual unpublish = %d, want 404; body=%s", rr.Code, bodyOf(rr))
	}
	if got := rr.Header().Get("Allow"); got != "" {
		t.Errorf("virtual unpublish Allow = %q, want none (no 405 refusal anymore)", got)
	}
	body := bodyOf(rr)
	if !strings.Contains(body, `"message": "not found"`) {
		t.Errorf("virtual unpublish body = %s, want npm's 404 envelope", body)
	}
	if strings.Contains(body, "No local repository was configured") {
		t.Errorf("virtual unpublish body carries the retired 405 wording: %s", body)
	}

	// The member's nodes survived: deletes never propagate.
	rr = s.call(http.MethodGet, "/npmv-loc/local-pkg/-/local-pkg-1.0.0.tgz", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("member tarball after refused unpublish = %d; body=%s", rr.Code, bodyOf(rr))
	}
}
