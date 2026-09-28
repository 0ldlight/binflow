package repo_test

// T-530: the four-bucket resolution-order suite (virtual-resolution.md
// sections 1-2 / virtual-four-bucket.md section 8, invariants I1-I8+I12) —
// expansion semantics (declaration-order DFS, cycle cut, dedupe, silent
// drop of missing members) asserted through the Service.VirtualMemberOrder
// seam, plus one end-to-end nested resolution through Get.

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// stepOf is the assertion shorthand: one order entry rendered as
// "key:facet" text (cache facets carry the REMOTE key per the contract).
func stepOf(m repo.VirtualMember) string {
	if m.Facet == repo.FacetCache {
		return m.Key + ":cache"
	}
	return m.Key
}

// orderOf renders a whole order for the table assertions.
func orderOf(t *testing.T, e *env, vkey string) []repo.VirtualMember {
	t.Helper()
	order, err := e.svc.VirtualMemberOrder(context.Background(), vkey)
	if err != nil {
		t.Fatalf("VirtualMemberOrder(%s): %v", vkey, err)
	}
	return order
}

// mustRenderOrder creates the virtual (members in the given declaration
// order) and renders its order.
func mustRenderOrder(t *testing.T, e *env, vkey string, members ...string) []string {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: vkey, Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
		Config: `{"repositories":["` + joinStringKeys(members) + `"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(virtual %s): %v", vkey, err)
	}
	order := orderOf(t, e, vkey)
	out := make([]string, len(order))
	for i, m := range order {
		out[i] = stepOf(m)
	}
	return out
}

// joinStringKeys is joinKeys for plain strings.
func joinStringKeys(keys []string) string {
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += `","`
		}
		out += k
	}
	return out
}

// assertOrder is the deep-equal shorthand with a readable failure.
func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// seedFourBucketEnv builds the standing cast: two locals (first marked), a
// marked remote and an unmarked remote.
func seedFourBucketEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	mustCreateRepo(t, e, "loc-p")
	put(t, e, admin(), "loc-p", "a.bin", "p")
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "loc-p", Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("mark loc-p: %v", err)
	}
	mustCreateRepo(t, e, "loc-n")
	mustCreateRemote(t, e, "rem-p", `{"url":"http://127.0.0.1:1/m2","priorityResolution":true}`)
	mustCreateRemote(t, e, "rem-n", `{"url":"http://127.0.0.1:1/m2"}`)
	return e
}

// TestFourBucketOrderMatrix pins I1-I4: within each priority class the
// local-class entities (real locals first, then cache facets) precede the
// remote bodies, and the priority class boundary is strict (I3).
func TestFourBucketOrderMatrix(t *testing.T) {
	tests := []struct {
		name    string
		members []string
		want    []string
	}{
		{
			// I1: the unmarked local beats the unmarked remote body despite
			// the declaration order.
			name:    "I1 local-class first within a priority class",
			members: []string{"rem-n", "loc-n"},
			want:    []string{"loc-n", "rem-n:cache", "rem-n"},
		},
		{
			// I3: the strict cross-segment order P-local -> P-remote ->
			// NP-local -> NP-remote, each remote contributing cache+body.
			name:    "I3 strict four-segment order",
			members: []string{"rem-n", "loc-n", "rem-p", "loc-p"},
			want: []string{
				"loc-p", "rem-p:cache", "rem-p",
				"loc-n", "rem-n:cache", "rem-n",
			},
		},
		{
			// I4: real locals precede cache facets inside the local-class
			// segment, each group in encounter order.
			name:    "I4 real locals before cache facets in-segment",
			members: []string{"loc-n", "rem-n", "loc-p"},
			want: []string{
				"loc-p", "loc-n", "rem-n:cache", "rem-n",
			},
		},
		{
			// I2: every cache facet precedes ITS OWN body facet (structural:
			// the cache scan runs before the body scan).
			name:    "I2 cache facet precedes its body",
			members: []string{"rem-n", "rem-p"},
			want: []string{
				"rem-p:cache", "rem-p", "rem-n:cache", "rem-n",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := seedFourBucketEnv(t)
			assertOrder(t, mustRenderOrder(t, e, "virt", tt.members...), tt.want)
		})
	}
}

// TestFourBucketNestedExpansion pins I8 (declaration-order DFS: a nested
// virtual's subtree lands where the nested member is declared), I5 (key
// dedupe at first occurrence across levels) and I7 (a missing member drops
// silently — the rest of the order is untouched).
func TestFourBucketNestedExpansion(t *testing.T) {
	t.Run("I8 nested subtree lands at the declaration slot", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "loc-in")
		mustCreateRemote(t, e, "rem-in", `{"url":"http://127.0.0.1:1/m2"}`)
		// wrap declares [rem-in, loc-in] — the expansion bucketing still
		// puts the local first (§1: locals foremost in the expansion).
		mustRenderOrder(t, e, "wrap", "rem-in", "loc-in")
		mustCreateRepo(t, e, "loc-after")
		got := mustRenderOrder(t, e, "root", "wrap", "loc-after")
		assertOrder(t, got, []string{"loc-in", "loc-after", "rem-in:cache", "rem-in"})
	})

	t.Run("I5 cross-level duplicate keeps the first occurrence", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "loc-dup")
		mustCreateRemote(t, e, "rem-w", `{"url":"http://127.0.0.1:1/m2"}`)
		mustRenderOrder(t, e, "wrap", "loc-dup", "rem-w")
		// loc-dup re-declared at the root AFTER the nested member: the
		// first occurrence (inside wrap) wins; the key appears once.
		got := mustRenderOrder(t, e, "root", "wrap", "loc-dup")
		assertOrder(t, got, []string{"loc-dup", "rem-w:cache", "rem-w"})
		// And the mirror: declared at the root BEFORE the nested member —
		// the root's position wins, wrap's copy dedupes away.
		e2 := newEnv(t)
		mustCreateRepo(t, e2, "loc-dup")
		mustCreateRemote(t, e2, "rem-w", `{"url":"http://127.0.0.1:1/m2"}`)
		mustRenderOrder(t, e2, "wrap", "loc-dup", "rem-w")
		got2 := mustRenderOrder(t, e2, "root", "loc-dup", "wrap")
		assertOrder(t, got2, []string{"loc-dup", "rem-w:cache", "rem-w"})
	})

	t.Run("I7 vanished member drops silently", func(t *testing.T) {
		// The FK on virtual_members cascades a member's deletion away
		// (001_init.sql), so the pure "row names a missing repository"
		// spelling is the expansion's race guard, not a reachable rest
		// state. Two reachable legs instead: the cascaded disappearance
		// through the service, and a hand-mangled row of unknown CLASS —
		// the sibling silent-drop branch (warn + skip, rest untouched).
		e := newEnv(t)
		mustCreateRepo(t, e, "loc-doomed")
		mustCreateRepo(t, e, "loc-live")
		// The unknown-class row: the metadata store does not validate Type.
		if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
			RepoKey: "weird", Type: "federated", PackageType: repo.PackageGeneric, Config: "{}",
		}); err != nil {
			t.Fatalf("seed unknown-class member: %v", err)
		}
		mustRenderOrder(t, e, "virt", "weird", "loc-doomed", "loc-live")

		got := orderOf(t, e, "virt")
		if len(got) != 2 || got[0].Key != "loc-doomed" || got[1].Key != "loc-live" {
			t.Fatalf("order = %v, want the two live locals (unknown class skipped)", got)
		}

		// The cascade leg: delete one member through the service — the
		// next order call simply lacks it.
		if err := e.svc.DeleteRepo(context.Background(), admin(), "loc-doomed", false); err != nil {
			t.Fatalf("delete member: %v", err)
		}
		got = orderOf(t, e, "virt")
		if len(got) != 1 || got[0].Key != "loc-live" {
			t.Fatalf("post-delete order = %v, want the surviving local only", got)
		}
	})
}

// TestFourBucketCycleCut pins I6: a member cycle (root -> wrap -> root)
// terminates at the visited check; the subtree the cycle wraps still
// resolves.
func TestFourBucketCycleCut(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "loc-deep")
	mustRenderOrder(t, e, "wrap", "loc-deep") // wrap: [loc-deep]
	// root: [wrap]; then wrap is re-pointed at root — the cycle root ->
	// wrap -> root must cut at the root's pre-registered key.
	mustRenderOrder(t, e, "root", "wrap")
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "wrap", Config: `{"repositories":["root","loc-deep"]}`,
	}); err != nil {
		t.Fatalf("point wrap back at root: %v", err)
	}
	got := orderOf(t, e, "root")
	if len(got) != 1 || got[0].Key != "loc-deep" {
		t.Fatalf("cyclic order = %v, want the wrapped local exactly once", got)
	}
	// The mirror entry point resolves identically (wrap's own view).
	gotWrap := orderOf(t, e, "wrap")
	if len(gotWrap) != 1 || gotWrap[0].Key != "loc-deep" {
		t.Fatalf("cyclic order from wrap = %v, want the local exactly once", gotWrap)
	}
}

// TestFourBucketNestedResolutionServesContent: the expansion is not just
// the order seam — a Get through the nesting resolves a member artifact
// that only the nested virtual declares (the end-to-end I8 leg).
func TestFourBucketNestedResolutionServesContent(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "loc-deep")
	put(t, e, admin(), "loc-deep", "nested/a.bin", "deep-content")
	mustRenderOrder(t, e, "wrap", "loc-deep")
	mustRenderOrder(t, e, "root", "wrap")
	rc, _, err := e.svc.Get(context.Background(), admin(), "root", "nested/a.bin")
	if err != nil {
		t.Fatalf("nested virtual Get: %v", err)
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	buf := make([]byte, len("deep-content"))
	if _, err := rc.Read(buf); err != nil {
		t.Fatalf("read nested body: %v", err)
	}
	if string(buf) != "deep-content" {
		t.Fatalf("nested body = %q", string(buf))
	}
}

// TestFourBucketOrderRecomputedPerRequest pins I12: member and priority
// changes are visible on the very next VirtualMemberOrder call (no cached
// order anywhere).
func TestFourBucketOrderRecomputedPerRequest(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "loc-a")
	mustCreateRemote(t, e, "rem-b", `{"url":"http://127.0.0.1:1/m2"}`)
	mustRenderOrder(t, e, "virt", "rem-b", "loc-a")

	// Promote the remote: cache+body jump ahead of the local.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "rem-b", Config: `{"url":"http://127.0.0.1:1/m2","priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("mark rem-b: %v", err)
	}
	order := orderOf(t, e, "virt")
	if stepOf(order[0]) != "rem-b:cache" || stepOf(order[1]) != "rem-b" || stepOf(order[2]) != "loc-a" {
		t.Fatalf("post-mark order = %v, want the marked remote pair first", order)
	}

	// Re-point the virtual's member list: the next call reflects it.
	if err := e.md.Virtual().SetMembers(ctx, "virt", []string{"loc-a"}); err != nil {
		t.Fatalf("re-point members: %v", err)
	}
	order = orderOf(t, e, "virt")
	if len(order) != 1 || order[0].Key != "loc-a" {
		t.Fatalf("post-repoint order = %v, want the single local", order)
	}
}
