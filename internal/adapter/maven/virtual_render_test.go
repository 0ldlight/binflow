package maven

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

// T-82: the two T-66/T-71 seams on the maven transfer plane, over one
// end-to-end virtual fixture (local member first, remote member second,
// counting mock upstream):
//
//   - a hinted body stream contributes X-BinFlow-Resolved-From (and the
//     remote member's X-BinFlow-Cache beneath it) to artifact and sidecar
//     responses alike — the MISS path through the FR-20 body step; the
//     REPEAT path serves through the cache-facet step with local
//     semantics (design §3, T-530): byte-exact, zero upstream, no cache
//     verdict header;
//   - DELETE through the virtual is virtual-own-storage only: a path the
//     virtual does not hold answers 404 ITEM_NOT_FOUND, members survive
//     (virtual-resolution.md §7.5 errata; the pre-T-524 405 refusal is
//     retired — T-531 refresh of this leg).
func TestVirtualRenderSeams(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	hits := &atomic.Int64{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/com/acme/up/2.0.0/up-2.0.0.jar" {
			_, _ = w.Write([]byte("upstream-jar-bytes"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	for _, row := range []*metadata.Repo{
		{RepoKey: "mv-loc", Type: repo.TypeLocal, PackageType: Protocol, Config: "{}"},
		{RepoKey: "mv-rem", Type: repo.TypeRemote, PackageType: Protocol,
			Config: `{"url":"` + upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "mv-virt", Type: repo.TypeVirtual, PackageType: Protocol,
			Config: `{"repositories":["mv-loc","mv-rem"]}`},
	} {
		if _, err := hs.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, row); err != nil {
			t.Fatalf("CreateRepo(%s): %v", row.RepoKey, err)
		}
	}

	localJar := "com/acme/lib/1.0.0/lib-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/mv-loc/"+localJar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed local member = %d (%s)", resp.StatusCode, drain(t, resp))
	}

	// Local-member hit through the virtual: Resolved-From names the member;
	// a local hit carries no cache header (nothing was fetched).
	resp := hs.serve(http.MethodGet, "/mv-virt/"+localJar, nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual GET local member = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "mv-loc" {
		t.Errorf("local hit Resolved-From = %q, want mv-loc", got)
	}
	if got := resp.Header.Get("X-BinFlow-Cache"); got != "" {
		t.Errorf("local hit carries X-BinFlow-Cache %q, want none", got)
	}
	if body := drain(t, resp); string(body) != string(jarBytes) {
		t.Errorf("local hit body = %q", body)
	}

	// Remote-member pull-through: both hints on one response.
	resp = hs.serve(http.MethodGet, "/mv-virt/com/acme/up/2.0.0/up-2.0.0.jar", nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual GET remote member = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "mv-rem" {
		t.Errorf("remote hit Resolved-From = %q, want mv-rem", got)
	}
	if got := resp.Header.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("remote hit X-BinFlow-Cache = %q, want MISS", got)
	}
	if body := drain(t, resp); string(body) != "upstream-jar-bytes" {
		t.Errorf("remote hit body = %q", body)
	}

	// Repeat: the four-bucket order serves the standing copy through the
	// remote's cache-facet step with LOCAL semantics (design §3: zero
	// upstream, no freshness window) — byte-exact content, the member still
	// named, the upstream frozen, and NO X-BinFlow-Cache header (the cache
	// engine's fetch verdict header only rides the FR-20 body path; a
	// local-semantics read carries none, exactly like the local-member leg
	// above — the header family split is the observable face of the two
	// distinct resolution steps one remote now contributes).
	resp = hs.serve(http.MethodGet, "/mv-virt/com/acme/up/2.0.0/up-2.0.0.jar", nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repeat GET = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get("X-BinFlow-Cache"); got != "" {
		t.Errorf("repeat X-BinFlow-Cache = %q, want none (cache-facet local-semantics serve)", got)
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "mv-rem" {
		t.Errorf("repeat Resolved-From = %q, want mv-rem", got)
	}
	if rb := string(drain(t, resp)); rb != "upstream-jar-bytes" {
		t.Errorf("repeat body = %q, want the byte-exact upstream copy", rb)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1 (the cache-facet step adds none, I9)", got)
	}

	// The checksum sidecar face carries the same resolution hint as its
	// target (the computed body never streams, but the member question is
	// the same one operators ask). Requested at .sha256 — the on-demand
	// matrix's only 200 arm on an unset value (BIN-76 / T-594; an unset
	// .sha1 through the virtual answers the matrix's 404 instead).
	resp = hs.serve(http.MethodGet, "/mv-virt/"+localJar+".sha256", nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual sidecar GET = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "mv-loc" {
		t.Errorf("sidecar Resolved-From = %q, want mv-loc", got)
	}
	drain(t, resp)

	// DELETE through the virtual repository: the delete touches ONLY the
	// virtual's own aggregation storage — a path the virtual itself does
	// not hold answers 404 (ITEM_NOT_FOUND), and no member's entity is
	// touched (virtual-resolution.md §7.5, the 2026-09-24/09-28 errata
	// that retired repo-semantics §8.2's member-walk delete: the pre-T-524
	// 405 "no local deployment repository" refusal no longer exists on
	// this face — the reverse assertion below pins its retirement).
	resp = hs.serve(http.MethodDelete, "/mv-virt/"+localJar, nil, nil, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("virtual DELETE = %d, want 404 ITEM_NOT_FOUND (%s)", resp.StatusCode, drain(t, resp))
	}
	if got := resp.Header.Get("Allow"); got != "" {
		t.Errorf("virtual DELETE Allow = %q, want none (no 405 refusal anymore)", got)
	}
	msg := string(drain(t, resp))
	if !strings.Contains(msg,
		"Could not locate artifact. Path: 'mv-virt/com/acme/lib/1.0.0/lib-1.0.0.jar'.") {
		t.Errorf("virtual DELETE body = %s", msg)
	}
	if strings.Contains(msg, "No local repository was configured") {
		t.Errorf("virtual DELETE body carries the retired 405 wording: %s", msg)
	}

	// The member's node survived: deletes never propagate through the
	// virtual resolution (§7.5 — the delete never even looked at members).
	if resp := hs.serve(http.MethodGet, "/mv-loc/"+localJar, nil, nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("member GET after virtual delete = %d", resp.StatusCode)
	}
}
