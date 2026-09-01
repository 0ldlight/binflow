package repo_test

// T-412 (FR-136.1/136.2/136.4): the virtual repository's aggregate browse —
// List answers the member children UNION over the same two-bucket order the
// pull resolution walks, and the folder spelling of Get answers FolderInfo's
// node face off the members' stored rows. The suite pins: the union shape
// (逐名 + same-name merge + folder/file flags + the deep recursive arm), the
// order's same-source property (pull and browse flip together on reorder and
// on a priority mark), the read gate (an unauthorized caller sees zero member
// rows), the empty states (no members vs all-empty members), and the T-406
// posture on remote members (cache rows only — the browse never contacts an
// upstream).

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// pathsOf renders a listing as the sorted path slice assertions compare on.
func pathsOf(nodes []*metadata.Node) string {
	paths := make([]string, len(nodes))
	for i, n := range nodes {
		paths[i] = n.Path
	}
	return fmt.Sprint(paths)
}

// firstSegments projects a listing onto the direct-children shape the
// FolderInfo children render consumes: one entry per first segment under
// dir, folder flag from the row spelling — the same reduction httpapi's
// childInfos applies.
func firstSegments(nodes []*metadata.Node, dir string) string {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	type child struct {
		name   string
		folder bool
	}
	seen := make(map[string]child, len(nodes))
	var order []string
	for _, n := range nodes {
		rel := n.Path
		if prefix != "" {
			if !startsWithSegment(n.Path, prefix) {
				continue
			}
			rel = n.Path[len(prefix):]
		}
		if rel == "" {
			continue // the folder row of the queried dir itself
		}
		name, folder := rel, false
		for i := 0; i < len(rel); i++ {
			if rel[i] == '/' {
				name, folder = rel[:i], true
				break
			}
		}
		if _, dup := seen[name]; !dup {
			seen[name] = child{name, folder}
			order = append(order, name)
		}
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		c := seen[name]
		tag := "file"
		if c.folder {
			tag = "folder"
		}
		out = append(out, name+":"+tag)
	}
	return fmt.Sprint(out)
}

// startsWithSegment reports whether p sits beneath prefix (prefix including
// its trailing slash).
func startsWithSegment(p, prefix string) bool {
	return len(p) > len(prefix) && p[:len(prefix)] == prefix
}

// TestVirtualAggregateListChildrenUnion: the AC1 core — a two-member fixture
// lists the children union: every name from both members exactly once
// (逐名), same-name paths merged to the first member's row, folder/file flags
// from the actual row spelling, and the deep subtree unioned across members
// under a directory one of them owns.
func TestVirtualAggregateListChildrenUnion(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	// loc-a is seeded by alice so the same-name merge has a provenance
	// fingerprint beyond content order.
	e.az.add("alice", repo.ActionWrite, "")
	fx := buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a"}, {key: "loc-b"}})
	put(t, e, alice(), "loc-a", "com/acme/lib-a/1.0/lib-a-1.0.jar", "from-a")
	put(t, e, alice(), "loc-a", "com/acme/shared/notes.txt", "from-a")
	put(t, e, admin(), "loc-b", "com/acme/lib-b/1.0/lib-b-1.0.jar", "from-b")
	put(t, e, admin(), "loc-b", "com/acme/shared/notes.txt", "from-b")
	put(t, e, admin(), "loc-b", "com/acme/readme.txt", "readme")
	put(t, e, admin(), "loc-b", "com/acme/lib-a/2.0/lib-a-2.0.jar", "from-b-deep")

	// The whole com/acme subtree, both members, path-ordered, one row per
	// path. The same-name merge shows once, as loc-a's row (declaration
	// order): created_by is the fingerprint.
	nodes, err := e.svc.List(ctx, admin(), fx.vkey, "com/acme")
	if err != nil {
		t.Fatalf("List(virtual com/acme): %v", err)
	}
	want := "[com/acme/ com/acme/lib-a/ com/acme/lib-a/1.0/ com/acme/lib-a/1.0/lib-a-1.0.jar " +
		"com/acme/lib-a/2.0/ com/acme/lib-a/2.0/lib-a-2.0.jar " +
		"com/acme/lib-b/ com/acme/lib-b/1.0/ com/acme/lib-b/1.0/lib-b-1.0.jar " +
		"com/acme/readme.txt com/acme/shared/ com/acme/shared/notes.txt]"
	if got := pathsOf(nodes); got != want {
		t.Fatalf("union paths = %s\nwant             %s", got, want)
	}
	var merged *metadata.Node
	for _, n := range nodes {
		if n.Path == "com/acme/shared/notes.txt" {
			merged = n
		}
	}
	if merged == nil || merged.CreatedBy != "alice" {
		t.Fatalf("same-name merge row = %+v, want loc-a's row (created_by alice)", merged)
	}

	// The direct-children face (what FolderInfo children render): names from
	// BOTH members, folder/file flags from the row spelling.
	wantKids := "[lib-a:folder lib-b:folder readme.txt:file shared:folder]"
	if got := firstSegments(nodes, "com/acme"); got != wantKids {
		t.Fatalf("children of com/acme = %s, want %s", got, wantKids)
	}

	// The deep recursive arm: a subtree under a directory loc-a owns unions
	// in loc-b's deeper branch (the FE tree and ?list&deep=1 consume this
	// whole-subtree shape).
	deep, err := e.svc.List(ctx, admin(), fx.vkey, "com/acme/lib-a")
	if err != nil {
		t.Fatalf("List(virtual com/acme/lib-a): %v", err)
	}
	wantDeep := "[com/acme/lib-a/ com/acme/lib-a/1.0/ com/acme/lib-a/1.0/lib-a-1.0.jar " +
		"com/acme/lib-a/2.0/ com/acme/lib-a/2.0/lib-a-2.0.jar]"
	if got := pathsOf(deep); got != wantDeep {
		t.Fatalf("deep union = %s\nwant        %s", got, wantDeep)
	}

	// The repository root is the same union one level up.
	root, err := e.svc.List(ctx, admin(), fx.vkey, "")
	if err != nil {
		t.Fatalf("List(virtual root): %v", err)
	}
	if got := firstSegments(root, ""); got != "[com:folder]" {
		t.Fatalf("children of the root = %s, want [com:folder]", got)
	}
}

// TestVirtualAggregateFolderGetFaces: the folder spelling of a virtual Get —
// the stored marker of the first member that holds one, a synthesized marker
// when members hold only children (a remote-cache landing shape), and the
// honest miss when nothing anywhere backs the directory.
func TestVirtualAggregateFolderGetFaces(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a"}, {key: "loc-b"}})
	put(t, e, admin(), "loc-a", "com/acme/a.jar", "a")
	put(t, e, admin(), "loc-b", "com/acme/b.jar", "b")

	// Both members hold the marker; the first in the order answers.
	_, node, err := e.svc.Get(ctx, admin(), fx.vkey, "com/acme/")
	if !errors.Is(err, repo.ErrIsFolder) || node == nil {
		t.Fatalf("Get(folder) = (%v, %v), want ErrIsFolder + node", err, node)
	}
	if node.Sha256 != metadata.FolderMarkerSHA || node.RepoKey != "loc-a" || node.Path != "com/acme/" {
		t.Fatalf("folder row = %s/%s, want loc-a's marker row", node.RepoKey, node.Path)
	}

	// The slash-less spelling is a FILE probe and keeps missing (folder rows
	// are not downloads — the pull walk is unchanged by the browse face).
	if _, _, err := e.svc.Get(ctx, admin(), fx.vkey, "com/acme"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("slash-less folder probe = %v, want ErrNodeNotFound (the file face)", err)
	}

	// A member whose landing carries no ancestor rows (the pre-ADR-0016
	// remote-cache shape, reproduced by dropping the marker): the directory
	// is proven by its children and answered with a SYNTHESIZED marker —
	// display-only, keyed to the virtual, never written to any member.
	put(t, e, admin(), "loc-b", "d/inner.bin", "x")
	if err := e.md.Nodes().Delete(ctx, "loc-b", "d/"); err != nil {
		t.Fatalf("drop loc-b's d/ marker: %v", err)
	}
	_, node, err = e.svc.Get(ctx, admin(), fx.vkey, "d/")
	if !errors.Is(err, repo.ErrIsFolder) || node == nil {
		t.Fatalf("Get(synthesized folder) = (%v, %v), want ErrIsFolder + node", err, node)
	}
	if node.Sha256 != metadata.FolderMarkerSHA || node.RepoKey != fx.vkey {
		t.Fatalf("synthesized row = %s (%s), want the virtual-keyed marker", node.RepoKey, node.Sha256)
	}
	if _, serr := e.md.Nodes().Get(ctx, "loc-b", "d/"); !errors.Is(serr, metadata.ErrNodeNotFound) {
		t.Fatalf("the browse must not write into the member: d/ = %v", serr)
	}

	// Nothing anywhere backs this directory: the ordinary miss.
	if _, _, err := e.svc.Get(ctx, admin(), fx.vkey, "nope/"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("Get(childless folder) = %v, want ErrNodeNotFound", err)
	}
}

// TestVirtualAggregateOrderMatchesPullResolution: FR-136.2's same-source
// rule — the union's winning row is the member the PULL would serve, and
// both faces flip together when the order changes: by reorder and by a
// priority mark.
func TestVirtualAggregateOrderMatchesPullResolution(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.az.add("alice", repo.ActionWrite, "")
	fx := buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a"}, {key: "loc-b"}})
	put(t, e, alice(), "loc-a", "shared/pkg.bin", "A")
	put(t, e, admin(), "loc-b", "shared/pkg.bin", "B")

	// Declaration order: the pull serves loc-a; the union carries loc-a's
	// row (created_by is the fingerprint).
	rowOf := func() string {
		t.Helper()
		nodes, err := e.svc.List(ctx, admin(), fx.vkey, "shared")
		if err != nil {
			t.Fatalf("List(virtual shared): %v", err)
		}
		for _, n := range nodes {
			if n.Path == "shared/pkg.bin" {
				return n.CreatedBy
			}
		}
		t.Fatal("shared/pkg.bin missing from the union")
		return ""
	}
	if body, hints := mustGetVirtual(t, fx, "shared/pkg.bin"); body != "A" || hints.Get(repo.HdrResolvedFrom) != "loc-a" {
		t.Fatalf("pull = %q from %q, want A from loc-a", body, hints.Get(repo.HdrResolvedFrom))
	}
	if got := rowOf(); got != "alice" {
		t.Fatalf("union row created_by = %q, want alice (loc-a's row)", got)
	}

	// Reorder the members: BOTH faces flip on the very next call.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "virt", Config: `{"repositories":["loc-b","loc-a"]}`,
	}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if body, hints := mustGetVirtual(t, fx, "shared/pkg.bin"); body != "B" || hints.Get(repo.HdrResolvedFrom) != "loc-b" {
		t.Fatalf("post-reorder pull = %q from %q, want B from loc-b", body, hints.Get(repo.HdrResolvedFrom))
	}
	if got := rowOf(); got != "admin" {
		t.Fatalf("post-reorder union row created_by = %q, want admin (loc-b's row)", got)
	}

	// The two-bucket source: a priority mark flips the order without touching
	// the declaration — both faces follow.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "loc-a", Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("mark loc-a: %v", err)
	}
	if body, hints := mustGetVirtual(t, fx, "shared/pkg.bin"); body != "A" || hints.Get(repo.HdrResolvedFrom) != "loc-a" {
		t.Fatalf("post-mark pull = %q from %q, want A from loc-a (priority bucket)", body, hints.Get(repo.HdrResolvedFrom))
	}
	if got := rowOf(); got != "alice" {
		t.Fatalf("post-mark union row created_by = %q, want alice (loc-a's row)", got)
	}
}

// TestVirtualAggregateReadGate: FR-136.4 — the aggregate is gated by the
// content plane's allow() on the VIRTUAL key exactly like the local listing
// and the pull face; an unauthorized caller sees zero member rows, and the
// gate runs before anything member-facing is read.
func TestVirtualAggregateReadGate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a", files: map[string]string{"com/acme/a.jar": "a"}}})

	// Anonymous on a fail-closed authorizer: challenged.
	if _, err := e.svc.List(ctx, nil, fx.vkey, ""); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous aggregate List = %v, want ErrUnauthorized", err)
	}
	// An authenticated principal without the grant: denied, zero rows.
	if _, err := e.svc.List(ctx, alice(), fx.vkey, ""); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("ungranted aggregate List = %v, want ErrForbidden", err)
	}
	if _, err := e.svc.List(ctx, alice(), fx.vkey, "com/acme"); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("ungranted prefix List = %v, want ErrForbidden", err)
	}
	// The folder face sits behind the same gate (Get's top).
	if _, _, err := e.svc.Get(ctx, alice(), fx.vkey, "com/acme/"); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("ungranted folder Get = %v, want ErrForbidden", err)
	}

	// Granted on the virtual: the union answers — members are resolution
	// internals, the virtual is the addressed surface (the pull face's
	// posture, FR-136.4's "与 pull 解析同门").
	e.az.add("alice", repo.ActionRead, "")
	nodes, err := e.svc.List(ctx, alice(), fx.vkey, "")
	if err != nil {
		t.Fatalf("granted aggregate List: %v", err)
	}
	if got := firstSegments(nodes, ""); got != "[com:folder]" {
		t.Fatalf("granted children = %s, want [com:folder]", got)
	}
}

// TestVirtualAggregateEmptyStates: FR-136.4's boundary — a memberless virtual
// (the member rows cascaded away with a deleted member) and an all-empty one
// both answer an honest empty page, never an error; the no-members /
// all-empty distinction the console's empty-state copy keys on rides the
// member ledger (the repositories face), which the two states differ on.
func TestVirtualAggregateEmptyStates(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a"}, {key: "loc-b"}})

	// All members empty: an empty page, and the folder probes miss.
	nodes, err := e.svc.List(ctx, admin(), fx.vkey, "")
	if err != nil || len(nodes) != 0 {
		t.Fatalf("all-empty List = (%d rows, %v), want (0, nil)", len(nodes), err)
	}
	if _, _, err := e.svc.Get(ctx, admin(), fx.vkey, "x/"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("all-empty folder Get = %v, want ErrNodeNotFound", err)
	}
	members, err := e.md.Virtual().ListMembers(ctx, fx.vkey)
	if err != nil || len(members) != 2 {
		t.Fatalf("all-empty member ledger = (%d, %v), want 2 members", len(members), err)
	}

	// No members at all (the member rows cascade with the member repo):
	// still an empty page — the copy distinction comes from this ledger.
	if err := e.svc.DeleteRepo(ctx, admin(), "loc-a", false); err != nil {
		t.Fatalf("DeleteRepo(loc-a): %v", err)
	}
	if err := e.svc.DeleteRepo(ctx, admin(), "loc-b", false); err != nil {
		t.Fatalf("DeleteRepo(loc-b): %v", err)
	}
	members, err = e.md.Virtual().ListMembers(ctx, fx.vkey)
	if err != nil || len(members) != 0 {
		t.Fatalf("memberless ledger = (%d, %v), want 0 members", len(members), err)
	}
	nodes, err = e.svc.List(ctx, admin(), fx.vkey, "")
	if err != nil || len(nodes) != 0 {
		t.Fatalf("memberless List = (%d rows, %v), want (0, nil)", len(nodes), err)
	}
	if _, _, err := e.svc.Get(ctx, admin(), fx.vkey, "x/"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("memberless folder Get = %v, want ErrNodeNotFound", err)
	}
}

// TestVirtualAggregateRemoteMemberCacheOnly: the T-406 listing posture carried
// onto the aggregate — a remote member contributes its CACHE rows only; the
// browse never contacts the upstream, and one pull through the virtual is
// what makes the cache (and therefore the union) grow.
func TestVirtualAggregateRemoteMemberCacheOnly(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "loc-a"},
		{key: "rem-b", remote: true, files: map[string]string{"/com/x/y.jar": "upstream"}},
	})

	// Cold cache: an empty page and ZERO upstream traffic — a dead or slow
	// upstream cannot fail or stall the tree.
	nodes, err := e.svc.List(ctx, admin(), fx.vkey, "")
	if err != nil || len(nodes) != 0 {
		t.Fatalf("cold List = (%d rows, %v), want (0, nil)", len(nodes), err)
	}
	if got := fx.hits["rem-b"].Load(); got != 0 {
		t.Fatalf("cold browse upstream hits = %d, want 0", got)
	}

	// One pull through the virtual lands the cache in the member's
	// namespace; the union picks the landing up (folder rows included via
	// the synthesized marker — the engine materializes no ancestors).
	if body, _ := mustGetVirtual(t, fx, "com/x/y.jar"); body != "upstream" {
		t.Fatalf("pull body = %q", body)
	}
	nodes, err = e.svc.List(ctx, admin(), fx.vkey, "")
	if err != nil {
		t.Fatalf("post-pull List: %v", err)
	}
	if got := pathsOf(nodes); got != "[com/x/y.jar]" {
		t.Fatalf("post-pull union = %s, want [com/x/y.jar] (cache rows only)", got)
	}
	if got := fx.hits["rem-b"].Load(); got != 1 {
		t.Fatalf("upstream hits after pull+browse = %d, want 1 (the browse is silent)", got)
	}
	_, node, err := e.svc.Get(ctx, admin(), fx.vkey, "com/x/")
	if !errors.Is(err, repo.ErrIsFolder) || node == nil || node.Sha256 != metadata.FolderMarkerSHA {
		t.Fatalf("folder over a cache landing = (%v, %v), want ErrIsFolder + the marker", err, node)
	}
}
