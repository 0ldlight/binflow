package maven

// T-531: the four-bucket seam consumption of the virtual metadata
// aggregation. FacetCache steps never participate in the merge walk
// (virtual-resolution.md §5.1 "跳过所有 cache 仓": the remote 本体 step
// carries the cache semantics — one remote merges as ONE unit, its
// standing copy is never read too; design virtual-four-bucket.md §5.3
// maven row, invariant I11), and the snapshot-level policy skip applies
// (snapshot-level documents skip handleSnapshots=false members, §5.1
// explicit — L033 Arm C kept that face double-sided). The module-level
// handleReleases skip was REMOVED in T-556: L033 Arm C's differential
// (live A 7.161.26, r3≡r4) proved the reference INCLUDES a
// handleReleases=false member's SNAPSHOT version in the module list.
//
// Two legs: the cache-facet FILTER runs on seam-shaped step tables
// (design §2.1's ResolutionStep form) for the table-driven matrix, and
// the REAL four-bucket order (T-530's repo.VirtualMember.Facet seam)
// carries the integration assertions — the emitted cache step never
// reaches a member read (plan layer) and costs zero upstream traffic
// (execution layer, I9). The policy legs run the real stack through the
// adapter's GET face.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestMetadataWalkCacheFacetSkip: the §5.1 traversal filter over
// seam-shaped step tables. A remote's cache projection is dropped wherever
// it sits (the 本体 step carries the cache semantics — the merged document
// reads one remote exactly once), and a sequence of ONLY cache steps
// yields nothing at all (theoretically unreachable, asserted defensively:
// a future seam regression must fail here, not as a doubled upstream read).
func TestMetadataWalkCacheFacetSkip(t *testing.T) {
	remBody := metadataWalkStep{Key: "rem", Facet: facetPlain, HandleReleases: true, HandleSnapshots: true}
	remCache := metadataWalkStep{Key: "rem", Facet: facetCache, Priority: true, HandleReleases: true, HandleSnapshots: true}
	locA := metadataWalkStep{Key: "loc-a", Facet: facetPlain, HandleReleases: true, HandleSnapshots: true}
	locBCache := metadataWalkStep{Key: "loc-b-remote", Facet: facetCache, HandleReleases: true, HandleSnapshots: true}
	locBBody := metadataWalkStep{Key: "loc-b-remote", Facet: facetPlain, HandleReleases: true, HandleSnapshots: true}

	cases := []struct {
		name  string
		steps []metadataWalkStep
		level metadataLevel
		want  []string // the Keys the walk reads, in order
	}{
		{
			name:  "four-bucket sequence: cache projections before their bodies, all dropped",
			steps: []metadataWalkStep{locA, remCache, remBody, locBCache, locBBody},
			level: levelModule,
			want:  []string{"loc-a", "rem", "loc-b-remote"},
		},
		{
			name:  "inherited-priority cache step dropped without tripping the priority bookkeeping",
			steps: []metadataWalkStep{remCache, remBody},
			level: levelSnapshot,
			want:  []string{"rem"},
		},
		{
			name:  "pure cache sequence (defensive): the walk reads nothing",
			steps: []metadataWalkStep{remCache, locBCache},
			level: levelModule,
			want:  nil,
		},
		{
			name:  "two-bucket shape (all plain, lenient flags) is the identity transform",
			steps: []metadataWalkStep{locA, remBody},
			level: levelModule,
			want:  []string{"loc-a", "rem"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterMetadataSteps(tc.steps, tc.level)
			keys := make([]string, 0, len(got))
			for _, s := range got {
				keys = append(keys, s.Key)
			}
			if len(keys) != len(tc.want) {
				t.Fatalf("filtered walk = %v, want %v", keys, tc.want)
			}
			for i := range keys {
				if keys[i] != tc.want[i] {
					t.Fatalf("filtered walk = %v, want %v", keys, tc.want)
				}
			}
		})
	}
}

// TestMetadataWalkLevelPolicySkip: the level-keyed member skips. Snapshot
// level keys on handleSnapshots; module level consults NO handle flag
// since T-556 (L033 Arm C: live A includes the handleReleases=false
// member's versions) — and the cache-facet drop holds at BOTH levels
// regardless of the flags.
func TestMetadataWalkLevelPolicySkip(t *testing.T) {
	cases := []struct {
		name  string
		step  metadataWalkStep
		level metadataLevel
		want  bool
	}{
		{name: "snapshot level, handleSnapshots=false: skipped", step: metadataWalkStep{Key: "m", Facet: facetPlain, HandleReleases: true, HandleSnapshots: false}, level: levelSnapshot, want: false},
		{name: "snapshot level, handleSnapshots=true: kept", step: metadataWalkStep{Key: "m", Facet: facetPlain, HandleReleases: false, HandleSnapshots: true}, level: levelSnapshot, want: true},
		{name: "module level, handleReleases=false: kept (T-556 — the pre-flip skip dropped it)", step: metadataWalkStep{Key: "m", Facet: facetPlain, HandleReleases: false, HandleSnapshots: true}, level: levelModule, want: true},
		{name: "module level, handleReleases=true: kept", step: metadataWalkStep{Key: "m", Facet: facetPlain, HandleReleases: true, HandleSnapshots: false}, level: levelModule, want: true},
		{name: "cache facet, snapshot level, both handles true: still skipped", step: metadataWalkStep{Key: "m", Facet: facetCache, HandleReleases: true, HandleSnapshots: true}, level: levelSnapshot, want: false},
		{name: "cache facet, module level, both handles true: still skipped", step: metadataWalkStep{Key: "m", Facet: facetCache, HandleReleases: true, HandleSnapshots: true}, level: levelModule, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterMetadataSteps([]metadataWalkStep{tc.step}, tc.level)
			if (len(got) == 1) != tc.want {
				t.Fatalf("filterMetadataSteps(%+v, level=%d) kept %d steps, want kept=%v", tc.step, tc.level, len(got), tc.want)
			}
		})
	}
}

// TestMetadataLevelOfClassification: the level a path resolves to — a
// -SNAPSHOT version directory is the snapshot-level document, everything
// else merges at module level (the same suffix heuristic the merge
// algorithm splits on).
func TestMetadataLevelOfClassification(t *testing.T) {
	cases := []struct {
		path string
		want metadataLevel
	}{
		{"com/acme/lib/maven-metadata.xml", levelModule},
		{"com/acme/lib/1.0-SNAPSHOT/maven-metadata.xml", levelSnapshot},
		{"com/acme/lib/1.0/maven-metadata.xml", levelModule},
		{"maven-metadata.xml", levelModule},
	}
	for _, tc := range cases {
		if got := metadataLevelOf(tc.path); got != tc.want {
			t.Errorf("metadataLevelOf(%q) = %d, want %d", tc.path, got, tc.want)
		}
	}
}

// TestVirtualMetadataSnapshotPolicySkip: the real stack — a member with
// handleSnapshots=false does not contribute to the SNAPSHOT version
// directory's document (§5.1), while the module-level list above it still
// carries the GAV (the skip is level-keyed, not member-wide).
func TestVirtualMetadataSnapshotPolicySkip(t *testing.T) {
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)
	f.deploySnapshotPom(t, "mv-a", "20240101.120000", 1)
	f.deploySnapshotPom(t, "mv-b", "20240102.130000", 1)

	// Precondition: both members merge — equal buildNumbers, so the newer
	// timestamp's block wins (mv-b's 20240102; the snapshot block keeps the
	// winner's spelling, it does not union).
	_, body := f.hs.getMeta("mv-virt", "com.acme", "lib", "1.0-SNAPSHOT")
	mustContain(t, "both-member snapshot merge", body,
		[]string{"<timestamp>20240102.130000</timestamp>"}, nil)

	// mv-b refuses snapshots: its build vanishes from the version-level
	// merge; mv-a's earlier block becomes the winner.
	if _, err := f.hs.svc.UpdateRepo(context.Background(), adminP, &metadata.Repo{
		RepoKey: "mv-b", Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"handleSnapshots":false}`,
	}); err != nil {
		t.Fatalf("set mv-b handleSnapshots=false: %v", err)
	}
	status, body := f.hs.getMeta("mv-virt", "com.acme", "lib", "1.0-SNAPSHOT")
	if status != http.StatusOK {
		t.Fatalf("snapshot-level merge with one refusing member = %d (%s)", status, body)
	}
	mustContain(t, "snapshot-level policy skip", body,
		[]string{"<timestamp>20240101.120000</timestamp>"},
		[]string{"<timestamp>20240102.130000</timestamp>"})

	// The module-level list above the refused version directory is NOT
	// governed by handleSnapshots: the GAV stays listed (level-keyed).
	status, body = f.hs.getMeta("mv-virt", "com.acme", "lib", "")
	if status != http.StatusOK {
		t.Fatalf("module-level list with one snapshot-refusing member = %d (%s)", status, body)
	}
	if !containsAll(body, "<version>1.0-SNAPSHOT</version>") {
		t.Errorf("module-level list dropped the GAV under a snapshot-only policy refusal:\n%s", body)
	}
}

// TestVirtualMetadataModuleLevelIgnoresHandleReleases: the real stack —
// the module version list keeps a handleReleases=false member's versions
// (T-556, L033 Arm C's differential refutation: live A 7.161.26, r3≡r4,
// INCLUDES the hr=false member's SNAPSHOT version in the virtual module
// list — the pre-T-556 skip answered 404 there). The leg mirrors the
// evidenced spelling — a SNAPSHOT version contributed by the refusing
// member; the release-pom twin is unconstructible through the ordinary
// plane on the reference (A 409s release PUTs to hr=false locals), and
// the walk layer's release-skip mirror flipped with this face (both
// sites, one rule).
func TestVirtualMetadataModuleLevelIgnoresHandleReleases(t *testing.T) {
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)
	f.deployReleasePom(t, "mv-a", "1.0.0")
	f.deploySnapshotPom(t, "mv-b", "20240101.120000", 1)

	// mv-b refuses releases: its SNAPSHOT version STAYS in the module
	// list (no handle filter at module level); latest/release recompute
	// over the whole union — 1.0.0 stays latest (a -SNAPSHOT qualifier
	// ranks below its release, the comparator's own rule).
	if _, err := f.hs.svc.UpdateRepo(context.Background(), adminP, &metadata.Repo{
		RepoKey: "mv-b", Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"handleReleases":false}`,
	}); err != nil {
		t.Fatalf("set mv-b handleReleases=false: %v", err)
	}
	status, body := f.hs.getMeta("mv-virt", "com.acme", "lib", "")
	if status != http.StatusOK {
		t.Fatalf("module-level merge with one release-refusing member = %d (%s)", status, body)
	}
	mustContain(t, "module-level ignores handleReleases", body,
		[]string{"<version>1.0.0</version>", "<version>1.0-SNAPSHOT</version>", "<latest>1.0.0</latest>", "<release>1.0.0</release>"},
		nil)
}

// TestVirtualMetadataAggregationSkipsCacheFacetSteps: the REAL four-bucket
// order (T-530's repo.VirtualMember.Facet) through the real walk plan —
// the remote's FacetCache step never reaches a member read (exactly one
// mv-rem step survives the filter, the plain body), and the GET costs
// exactly one upstream fetch (I9: the cache step adds none).
func TestVirtualMetadataAggregationSkipsCacheFacetSteps(t *testing.T) {
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-rem"]}`)
	ctx := context.Background()

	// Precondition, the test's teeth: the four-bucket order DOES emit a
	// FacetCache step for the remote (design §2.1) — otherwise the skip
	// assertions below would be vacuous.
	order, err := f.hs.svc.VirtualMemberOrder(ctx, "mv-virt")
	if err != nil {
		t.Fatalf("VirtualMemberOrder: %v", err)
	}
	cacheSteps := 0
	for _, m := range order {
		if m.Key == "mv-rem" && m.Facet == repo.FacetCache {
			cacheSteps++
		}
	}
	if cacheSteps != 1 {
		t.Fatalf("four-bucket order emits %d mv-rem FacetCache steps, want exactly 1 (order: %+v)", cacheSteps, order)
	}

	// Plan layer (the discriminator): the aggregation walk plan keeps
	// exactly ONE mv-rem step — the plain body. A cache step in the plan
	// would become a second ReadVirtualMember of the same remote
	// (ReadVirtualMember matches by key and is facet-insensitive).
	steps, err := f.hs.h.virtualMetadataSteps(ctx, "mv-virt")
	if err != nil {
		t.Fatalf("virtualMetadataSteps: %v", err)
	}
	rem, caches := 0, 0
	for _, s := range filterMetadataSteps(steps, levelModule) {
		if s.Key == "mv-rem" {
			rem++
			if s.Facet != facetPlain {
				t.Errorf("surviving mv-rem step facet = %d, want facetPlain", s.Facet)
			}
		}
		if s.Facet == facetCache {
			caches++
		}
	}
	if rem != 1 || caches != 0 {
		t.Fatalf("filtered walk plan = %d mv-rem steps / %d cache steps, want 1/0", rem, caches)
	}

	// Execution layer (I9 smoke): the remote-only GAV aggregates with
	// exactly one upstream fetch — the body step's. FR-20's own cache
	// would dedupe a second fetch, which is exactly why the plan-layer
	// assertions above carry the discrimination; this pins the zero
	// upstream increment end to end.
	status, body := f.hs.getMeta("mv-virt", "com.acme", "only-remote", "")
	if status != http.StatusOK || !strings.Contains(body, "<version>3.0.0</version>") {
		t.Fatalf("remote-only GAV through the four-bucket virtual = %d (%s)", status, body)
	}
	if got := f.hits.Load(); got != 1 {
		t.Errorf("upstream reads for one remote member = %d, want 1 (the cache step adds none)", got)
	}
}

// containsAll is mustContain's want-only half (a nil ban).
func containsAll(body string, wants ...string) bool {
	for _, w := range wants {
		if !strings.Contains(body, w) {
			return false
		}
	}
	return true
}
