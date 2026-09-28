package repo_test

// T-530: the cache-facet read semantics of the four-bucket walk
// (virtual-four-bucket.md section 3): a remote member's standing copy is
// served by its cache facet with LOCAL semantics — zero upstream, no
// revalidation (I9), the freshness window never consulted (O2), a fresh
// negative row does not shadow a standing copy (O3) — while the SNAPSHOT
// plane skips the cache facets entirely (I10, virtual-resolution.md 3.6).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// warmVirtualCache lands one standing copy in the remote member's namespace
// through the virtual itself (the body step's MISS), returning the body and
// the upstream hit count at that point.
func warmVirtualCache(t *testing.T, fx *virtualFixture, remKey, path string) (string, int64) {
	t.Helper()
	body, _ := mustGetVirtual(t, fx, path)
	hits := fx.hits[remKey].Load()
	if hits == 0 {
		t.Fatalf("warm-up left upstream hits = 0, want the MISS landing")
	}
	return body, hits
}

// TestVirtualCacheFacetZeroUpstream pins I9 (+O2's core): after the first
// pull-through lands the copy, virtual reads serve it through the CACHE
// facet — local semantics, zero upstream traffic, and NO revalidation when
// the copy's freshness window lapses (the walk never asks the freshness
// question on a cache step; the body step would have, per R10).
func TestVirtualCacheFacetZeroUpstream(t *testing.T) {
	const path = "org/lib/2.2/org-lib-2.2.jar"
	files := map[string]string{"/" + path: "standing-copy"}
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "rem-ttl", remote: true, files: files, extra: `,"retrievalCachePeriodSecs":1`},
	})

	body, hits := warmVirtualCache(t, fx, "rem-ttl", path)
	if body != "standing-copy" {
		t.Fatalf("warm body = %q", body)
	}

	// Second read while fresh: cache facet serves, zero upstream.
	body, hints := mustGetVirtual(t, fx, path)
	if body != "standing-copy" {
		t.Fatalf("fresh re-read body = %q", body)
	}
	if got := fx.hits["rem-ttl"].Load(); got != hits {
		t.Fatalf("fresh re-read upstream hits = %d, want unchanged %d", got, hits)
	}
	if got := hints.Get(repo.HdrResolvedFrom); got != "rem-ttl" {
		t.Fatalf("%s = %q, want the cache facet's remote key", repo.HdrResolvedFrom, got)
	}
	if got := hints.Get("X-BinFlow-Cache"); got != "" {
		t.Fatalf("X-BinFlow-Cache = %q, want empty (a cache facet serves with local semantics, no FR-20 header)", got)
	}

	// O2: the window lapses and the upstream forgets the path — the cache
	// facet STILL serves the standing copy with zero upstream traffic.
	e.clk.Advance(2 * time.Second)
	delete(files, "/"+path)
	body, _ = mustGetVirtual(t, fx, path)
	if body != "standing-copy" {
		t.Fatalf("expired-window body = %q, want the standing copy served unconditionally", body)
	}
	if got := fx.hits["rem-ttl"].Load(); got != hits {
		t.Fatalf("expired-window upstream hits = %d, want unchanged %d (the cache facet never revalidates)", got, hits)
	}

	// The contrast face: the SAME expired copy addressed DIRECTLY (body
	// semantics) does revalidate — the R10 stale serve with the upstream
	// error hint, one more upstream hit.
	rc, _, err := e.svc.Get(context.Background(), admin(), "rem-ttl", path)
	if err != nil {
		t.Fatalf("direct stale Get: %v", err)
	}
	rc.Close() //nolint:errcheck // read-only fd
	if got := fx.hits["rem-ttl"].Load(); got != hits+1 {
		t.Fatalf("direct re-read upstream hits = %d, want %d (the body step revalidates)", got, hits+1)
	}
}

// TestVirtualCacheFacetNegativeRowIgnored pins O3: a FRESH negative row
// (the direct face's miss memory) does not shadow the standing copy through
// the virtual — the cache facet reads node rows, the negative cache belongs
// to the body step's pull-through machinery. The coexistence arises from
// the R10 composition: an expired copy served STALE over an upstream 404
// ALSO writes the negative row (the six-step order), so the NEXT direct
// read is a true miss while the standing copy is still there.
func TestVirtualCacheFacetNegativeRowIgnored(t *testing.T) {
	const path = "org/lib/3.3/org-lib-3.3.jar"
	files := map[string]string{"/" + path: "standing-copy"}
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "rem-ttl", remote: true, files: files, extra: `,"retrievalCachePeriodSecs":1`},
	})

	// Warm the copy (the helper asserts the MISS landing's upstream hit).
	warmVirtualCache(t, fx, "rem-ttl", path)

	// Expire the copy, make the upstream forget the path; the direct face
	// serves it STALE — and opens the negative window as its side effect.
	e.clk.Advance(2 * time.Second)
	delete(files, "/"+path)
	if _, _, err := e.svc.Get(context.Background(), admin(), "rem-ttl", path); err != nil {
		t.Fatalf("direct stale serve: %v", err)
	}
	negHits := fx.hits["rem-ttl"].Load()
	// The next DIRECT read inside the window is the true miss.
	if _, _, err := e.svc.Get(context.Background(), admin(), "rem-ttl", path); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("direct negative-window read = %v, want ErrNodeNotFound", err)
	}
	if got := fx.hits["rem-ttl"].Load(); got != negHits {
		t.Fatalf("negative-window direct hits = %d, want unchanged %d (the row answers first)", got, negHits)
	}

	// Through the virtual the standing copy still serves — zero upstream.
	body, _ := mustGetVirtual(t, fx, path)
	if body != "standing-copy" {
		t.Fatalf("negative-window virtual body = %q, want the standing copy", body)
	}
	if got := fx.hits["rem-ttl"].Load(); got != negHits {
		t.Fatalf("negative-window virtual upstream hits = %d, want unchanged %d", got, negHits)
	}
}

// TestVirtualSnapshotPathSkipsCacheFacets pins I10 (virtual-resolution.md
// 3.6): a SNAPSHOT path never consults the cache facets. With an expired
// standing copy and an upstream that forgot the path, the snapshot walk
// runs the BODY step (the R10 stale serve — one upstream hit, the STALE
// header) while the plain twin path serves from the cache facet (zero
// upstream, no FR-20 header); the negative window the stale serve opens
// then turns the NEXT snapshot read into the ordinary miss.
func TestVirtualSnapshotPathSkipsCacheFacets(t *testing.T) {
	const plain = "org/lib/1.0/org-lib-1.0.jar"
	const snap = "org/lib/1.1-SNAPSHOT/org-lib-1.1-SNAPSHOT.jar"
	files := map[string]string{
		"/" + plain: "plain-copy",
		"/" + snap:  "snap-copy",
	}
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "rem-ttl", remote: true, files: files, extra: `,"retrievalCachePeriodSecs":1`},
	})

	// Warm both paths through the virtual (two MISS landings).
	if body, _ := mustGetVirtual(t, fx, plain); body != "plain-copy" {
		t.Fatalf("warm plain body = %q", body)
	}
	if body, _ := mustGetVirtual(t, fx, snap); body != "snap-copy" {
		t.Fatalf("warm snap body = %q", body)
	}
	hits := fx.hits["rem-ttl"].Load()

	// Expire both copies; the upstream forgets both paths.
	e.clk.Advance(2 * time.Second)
	delete(files, "/"+plain)
	delete(files, "/"+snap)

	// Plain path: the cache facet serves the standing copy, zero upstream,
	// no FR-20 header (the cache-facet signature).
	body, hints := mustGetVirtual(t, fx, plain)
	if body != "plain-copy" {
		t.Fatalf("plain expired body = %q, want the standing copy", body)
	}
	if got := fx.hits["rem-ttl"].Load(); got != hits {
		t.Fatalf("plain expired hits = %d, want unchanged %d", got, hits)
	}
	if got := hints.Get("X-BinFlow-Cache"); got != "" {
		t.Fatalf("plain expired X-BinFlow-Cache = %q, want empty (cache facet)", got)
	}

	// Snapshot path: cache facets skipped, the BODY step revalidates —
	// the R10 stale serve (one upstream hit, STALE header).
	body, hints = mustGetVirtual(t, fx, snap)
	if body != "snap-copy" {
		t.Fatalf("snapshot expired body = %q, want the stale copy off the body step", body)
	}
	if got := hints.Get("X-BinFlow-Cache"); got != "STALE" {
		t.Fatalf("snapshot expired X-BinFlow-Cache = %q, want STALE (the body step ran — cache facets skipped)", got)
	}
	if got := fx.hits["rem-ttl"].Load(); got != hits+1 {
		t.Fatalf("snapshot expired hits = %d, want %d (exactly the body step's revalidation)", got, hits+1)
	}

	// The negative window that stale serve opened: the NEXT snapshot read
	// is the true miss — the ordinary 404, no further upstream traffic.
	_, _, _, err := getVirtual(t, fx, snap)
	if !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("snapshot negative-window Get = %v, want ErrNodeNotFound", err)
	}
	if got := fx.hits["rem-ttl"].Load(); got != hits+1 {
		t.Fatalf("snapshot negative-window hits = %d, want unchanged %d", got, hits+1)
	}
}
