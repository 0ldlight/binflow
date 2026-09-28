package npm

// T-538 (BIN-21): the four-bucket seam consumption of the virtual packument
// merge. FacetCache steps never participate in the merge walk (design
// virtual-four-bucket.md §5.3 npm row — "缓存仓去重：序列含任一 remote
// 本体 → 滤掉全部 cache 投影（防同仓双查）": the remote 本体 step's FR-20
// chain already carries the cache semantics, and ReadVirtualMember matches
// members BY KEY, so an unfiltered walk would query one remote twice and
// merge its document twice).
//
// Two legs, maven T-531's precedent shape: the dedup filter runs on
// seam-shaped step tables (repo.VirtualMember, design §2.1's ResolutionStep
// form) for the table-driven matrix, and the REAL four-bucket order carries
// the integration assertions — the emitted cache step never reaches a
// member read (the plan layer, where the discrimination lives:
// ReadVirtualMember is facet-insensitive and FR-20's own cache would mask a
// doubled fetch) and the merged packument costs exactly one upstream
// request (the execution layer's end-to-end pin).

import (
	"context"
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestPackumentWalkCacheFacetDedup: the §5.3 cache dedup over seam-shaped
// step tables. A remote's cache projection is dropped wherever it sits (the
// 本体 step carries the cache semantics — the merged document reads one
// remote exactly once), a sequence of ONLY cache steps survives verbatim
// (the F7 far-end-suppression shape, unreachable today, asserted
// defensively: a future seam regression must fail here, not as a doubled
// member read), and an unknown facet value degrades to plain WITH the
// fail-open guard's semantics — kept as a body step, never a silent drop.
func TestPackumentWalkCacheFacetDedup(t *testing.T) {
	locA := repo.VirtualMember{Key: "npmv-a", Type: repo.TypeLocal, Facet: repo.FacetPlain}
	remCache := repo.VirtualMember{Key: "npmv-rem", Type: repo.TypeRemote, Facet: repo.FacetCache, Priority: true}
	remBody := repo.VirtualMember{Key: "npmv-rem", Type: repo.TypeRemote, Facet: repo.FacetPlain, Priority: true}
	remBcache := repo.VirtualMember{Key: "npmv-b-rem", Type: repo.TypeRemote, Facet: repo.FacetCache}
	remBbody := repo.VirtualMember{Key: "npmv-b-rem", Type: repo.TypeRemote, Facet: repo.FacetPlain}
	remUnknown := repo.VirtualMember{Key: "npmv-x", Type: repo.TypeRemote, Facet: repo.Facet(42)}

	cases := []struct {
		name  string
		steps []repo.VirtualMember
		want  []string // the member keys the walk reads, in order
	}{
		{
			name:  "four-bucket sequence: cache projections dropped wherever they sit",
			steps: []repo.VirtualMember{locA, remCache, remBody, remBcache, remBbody},
			want:  []string{"npmv-a", "npmv-rem", "npmv-b-rem"},
		},
		{
			name:  "inherited-priority cache step dropped alongside its body",
			steps: []repo.VirtualMember{remCache, remBody},
			want:  []string{"npmv-rem"},
		},
		{
			name:  "pure cache sequence (the F7 shape, defensive): kept verbatim",
			steps: []repo.VirtualMember{remCache, remBcache},
			want:  []string{"npmv-rem", "npmv-b-rem"},
		},
		{
			name:  "local + cache steps, no remote body (the F7 branch): kept verbatim",
			steps: []repo.VirtualMember{locA, remCache, remBcache},
			want:  []string{"npmv-a", "npmv-rem", "npmv-b-rem"},
		},
		{
			name:  "two-bucket shape (all plain) is the identity transform",
			steps: []repo.VirtualMember{locA, remBody},
			want:  []string{"npmv-a", "npmv-rem"},
		},
		{
			name:  "unknown facet degrades to plain: kept, and counts as the body step",
			steps: []repo.VirtualMember{remCache, remUnknown},
			want:  []string{"npmv-x"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupeCacheFacetSteps(context.Background(), tc.steps)
			keys := make([]string, 0, len(got))
			for _, s := range got {
				keys = append(keys, s.Key)
			}
			if len(keys) != len(tc.want) {
				t.Fatalf("deduped walk = %v, want %v", keys, tc.want)
			}
			for i := range keys {
				if keys[i] != tc.want[i] {
					t.Fatalf("deduped walk = %v, want %v", keys, tc.want)
				}
			}
		})
	}
}

// TestVirtualPackumentAggregationSkipsCacheFacetSteps: the REAL four-bucket
// order (T-530's repo.VirtualMember.Facet seam) through the real merge walk
// — the remote's FacetCache step never reaches a member read (exactly one
// npmv-rem step survives the dedup, the plain body), and the remote-only
// package's GET costs exactly one upstream request (the body step's; the
// cache step adds none).
func TestVirtualPackumentAggregationSkipsCacheFacetSteps(t *testing.T) {
	f := newNPMVirtualFixture(t, `{"repositories":["npmv-a","npmv-rem"]}`)
	ctx := context.Background()

	// Precondition, the test's teeth: the four-bucket order DOES emit a
	// FacetCache step for the remote (design §2.1) — otherwise the dedup
	// assertions below would be vacuous.
	order, err := f.s.svc.VirtualMemberOrder(ctx, "npmv-virt")
	if err != nil {
		t.Fatalf("VirtualMemberOrder: %v", err)
	}
	cacheSteps := 0
	for _, m := range order {
		if m.Key == "npmv-rem" && m.Facet == repo.FacetCache {
			cacheSteps++
		}
	}
	if cacheSteps != 1 {
		t.Fatalf("four-bucket order emits %d npmv-rem FacetCache steps, want exactly 1 (order: %+v)", cacheSteps, order)
	}

	// Plan layer (the discriminator): the merge walk keeps exactly ONE
	// npmv-rem step — the plain body. A cache step in the plan would become
	// a second ReadVirtualMember of the same remote (ReadVirtualMember
	// matches by key and is facet-insensitive).
	steps := dedupeCacheFacetSteps(ctx, order)
	rem, caches := 0, 0
	for _, s := range steps {
		if s.Key == "npmv-rem" {
			rem++
			if s.Facet != repo.FacetPlain {
				t.Errorf("surviving npmv-rem step facet = %d, want FacetPlain", s.Facet)
			}
		}
		if s.Facet == repo.FacetCache {
			caches++
		}
	}
	if rem != 1 || caches != 0 {
		t.Fatalf("deduped merge walk = %d npmv-rem steps / %d cache steps, want 1/0", rem, caches)
	}

	// Execution layer: the remote-only package aggregates with exactly one
	// upstream request — the body step's. FR-20's own cache would dedupe a
	// second fetch, which is exactly why the plan-layer assertions above
	// carry the discrimination; this pins the zero extra upstream traffic
	// end to end.
	code, doc := f.packument(t, "npmv-virt", "up-pkg")
	if code != http.StatusOK {
		t.Fatalf("remote-only packument through the four-bucket virtual = %d", code)
	}
	versions := versionsOf(doc)
	if versions["1.0.0"] == nil || versions["3.0.0"] == nil {
		t.Errorf("remote member's versions lost from the union: %v", versions)
	}
	if tags := distTagsOf(doc); tags["latest"] != "3.0.0" {
		t.Errorf("latest = %q, want 3.0.0 (the base member's original, which here is also the union's greatest)", tags["latest"])
	}
	if got := f.hits.Load(); got != 1 {
		t.Errorf("upstream reads for one remote member = %d, want 1 (the cache step adds none)", got)
	}
}
