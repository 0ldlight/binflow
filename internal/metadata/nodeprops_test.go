package metadata_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The 013 node_props sub-store (M10 T-286, architecture section 15.3.2):
// set semantics, the section-11.40 merge law, the FK cascade contracts
// (node delete / prefix delete / repo teardown) and the orphan refusal.

// seedPropNode lands one repo + folder-ledger + blob + node row the
// property tests address (the composite FK refuses annotations on nodes
// that do not exist, so every test needs the real chain).
func seedPropNode(t *testing.T, st metadata.Store, repoKey, path string) {
	t.Helper()
	ctx := context.Background()
	now := metadata.Now()
	if _, err := st.Repos().Get(ctx, repoKey); err != nil {
		if err := st.Repos().Create(ctx, &metadata.Repo{
			RepoKey: repoKey, Type: "local", PackageType: "generic", Config: "{}",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed repo %s: %v", repoKey, err)
		}
	}
	sha := strings.Repeat("a", 64)
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: 3, CreatedAt: now}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	if err := st.Nodes().Put(ctx, &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: sha, Size: 3,
		Mime: "application/octet-stream", CreatedBy: "t", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed node %s/%s: %v", repoKey, path, err)
	}
}

func TestNodePropsMergeListDelete(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	seedPropNode(t, st, "props-local", "ci/app.bin")

	np := st.NodeProps()

	got, err := np.List(ctx, "props-local", "ci/app.bin")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("fresh node carries properties: %+v", got)
	}

	// First write: two keys, one multi-valued.
	if err := np.Merge(ctx, "props-local", "ci/app.bin", map[string][]string{
		"build": {"77"},
		"env":   {"prod", "dev"},
	}); err != nil {
		t.Fatalf("merge 1: %v", err)
	}
	got, err = np.List(ctx, "props-local", "ci/app.bin")
	if err != nil {
		t.Fatalf("list 1: %v", err)
	}
	if len(got) != 2 || len(got["build"]) != 1 || got["build"][0] != "77" ||
		len(got["env"]) != 2 || got["env"][0] != "dev" || got["env"][1] != "prod" {
		t.Fatalf("merge 1 result = %+v (env must be the sorted set {dev,prod})", got)
	}

	// The section-11.40 merge law: same-key value-set replace, other keys
	// kept — env is rewritten to one value, build survives untouched.
	if err := np.Merge(ctx, "props-local", "ci/app.bin", map[string][]string{
		"env": {"staging"},
	}); err != nil {
		t.Fatalf("merge 2: %v", err)
	}
	got, err = np.List(ctx, "props-local", "ci/app.bin")
	if err != nil {
		t.Fatalf("list 2: %v", err)
	}
	if len(got) != 2 || len(got["env"]) != 1 || got["env"][0] != "staging" || len(got["build"]) != 1 {
		t.Fatalf("merge 2 result = %+v (build must survive, env replaced)", got)
	}

	// Selective delete: one key dropped, the other survives; an absent key
	// is idempotent (no error).
	if err := np.Delete(ctx, "props-local", "ci/app.bin", []string{"build", "nope"}); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	got, err = np.List(ctx, "props-local", "ci/app.bin")
	if err != nil {
		t.Fatalf("list 3: %v", err)
	}
	if len(got) != 1 || got["env"][0] != "staging" {
		t.Fatalf("delete key result = %+v", got)
	}

	// Delete-all (nil keys, the properties=* form).
	if err := np.Delete(ctx, "props-local", "ci/app.bin", nil); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	got, err = np.List(ctx, "props-local", "ci/app.bin")
	if err != nil {
		t.Fatalf("list 4: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("delete-all result = %+v", got)
	}

	// Node isolation: properties never leak across paths or repos.
	if err := np.Merge(ctx, "props-local", "ci/app.bin", map[string][]string{"k": {"v"}}); err != nil {
		t.Fatalf("merge 3: %v", err)
	}
	other, err := np.List(ctx, "props-local", "ci/other.bin")
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("properties leaked across paths: %+v", other)
	}
}

func TestNodePropsOrphanRefused(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	seedPropNode(t, st, "props-local", "ci/app.bin")

	// The composite FK is the orphan-annotation guard: a node row that
	// does not exist cannot carry properties.
	if err := st.NodeProps().Merge(ctx, "props-local", "ci/ghost.bin",
		map[string][]string{"k": {"v"}}); err == nil {
		t.Fatal("merging onto a missing node must fail on the FK")
	}
	// A repo that does not exist likewise.
	if err := st.NodeProps().Merge(ctx, "no-such-repo", "ci/app.bin",
		map[string][]string{"k": {"v"}}); err == nil {
		t.Fatal("merging onto a missing repository must fail on the FK")
	}
}

func TestNodePropsCascadeWithNodeDelete(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	seedPropNode(t, st, "props-local", "ci/app.bin")
	seedPropNode(t, st, "props-local", "ci/keep.bin")

	np := st.NodeProps()
	for _, p := range []string{"ci/app.bin", "ci/keep.bin"} {
		if err := np.Merge(ctx, "props-local", p, map[string][]string{"k": {"v"}}); err != nil {
			t.Fatalf("merge %s: %v", p, err)
		}
	}

	// Single-node delete cascades in the same statement (section 15.3.2's
	// "node 删除同事务清 node_props" by construction).
	if err := st.Nodes().Delete(ctx, "props-local", "ci/app.bin"); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	got, err := np.List(ctx, "props-local", "ci/app.bin")
	if err != nil || len(got) != 0 {
		t.Fatalf("deleted node still carries properties: %+v err=%v", got, err)
	}
	kept, err := np.List(ctx, "props-local", "ci/keep.bin")
	if err != nil || len(kept) != 1 {
		t.Fatalf("sibling lost its properties: %+v err=%v", kept, err)
	}

	// Prefix delete cascades the same way.
	if _, err := st.Nodes().DeleteByPrefix(ctx, "props-local", "ci"); err != nil {
		t.Fatalf("delete by prefix: %v", err)
	}
	kept, err = np.List(ctx, "props-local", "ci/keep.bin")
	if err != nil || len(kept) != 0 {
		t.Fatalf("prefix delete left properties behind: %+v err=%v", kept, err)
	}
}

func TestNodePropsCascadeWithRepoDelete(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	seedPropNode(t, st, "props-a", "x.bin")
	seedPropNode(t, st, "props-b", "x.bin")

	for _, rk := range []string{"props-a", "props-b"} {
		if err := st.NodeProps().Merge(ctx, rk, "x.bin", map[string][]string{"k": {"v"}}); err != nil {
			t.Fatalf("merge %s: %v", rk, err)
		}
	}
	if err := st.Repos().Delete(ctx, "props-a"); err != nil {
		t.Fatalf("delete repo: %v", err)
	}
	gone, err := st.NodeProps().List(ctx, "props-a", "x.bin")
	if err != nil || len(gone) != 0 {
		t.Fatalf("torn-down repo still carries properties: %+v err=%v", gone, err)
	}
	survivor, err := st.NodeProps().List(ctx, "props-b", "x.bin")
	if err != nil || len(survivor) != 1 {
		t.Fatalf("sibling repo lost its properties: %+v err=%v", survivor, err)
	}
}

func TestNodePropsMigrationIdempotent(t *testing.T) {
	// 013 lands through the ordinary migrator chain: opening the same file
	// twice applies it once (reopen-skips-applied, ADR-0007) and the table
	// keeps serving.
	st := open(t)
	ctx := context.Background()
	path := st.(interface{ DBPath() string }).DBPath()
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	seedPropNode(t, st2, "props-re", "a.bin")
	if err := st2.NodeProps().Merge(ctx, "props-re", "a.bin", map[string][]string{"k": {"v"}}); err != nil {
		t.Fatalf("merge after reopen: %v", err)
	}
	got, err := st2.NodeProps().List(ctx, "props-re", "a.bin")
	if err != nil || len(got) != 1 {
		t.Fatalf("list after reopen: %+v err=%v", got, err)
	}
}
