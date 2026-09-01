package search_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/search"
)

// T-411 AC1 end-to-end: AQL text → Parse → PlanQuery →
// metadata.NodeQueryer.QueryNodes against a real store. The snapshot legs
// pin the planner and the compiler separately; this chain test pins that
// they meet on the same IR — every planner decision is asserted through
// the rows it actually returns.

const (
	chainMarker = "0000000000000000000000000000000000000000000000000000000000000000"
	blobAAA     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	blobBBB     = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// openChainStore seeds the chain corpus through the PUBLIC store API (the
// FK chain repositories → blobs → nodes → node_props, exactly as the
// deploy path writes it).
func openChainStore(t *testing.T) metadata.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := metadata.Open(context.Background(), metadata.Options{
		Driver: "sqlite", Path: dir + "/chain.db", AdminPassword: "chain-pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	now := metadata.Now()
	for _, rk := range []string{"libs", "libs-cache", "other"} {
		if err := st.Repos().Create(ctx, &metadata.Repo{
			RepoKey: rk, Type: "local", PackageType: "generic",
			Config: "{}", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("repo %s: %v", rk, err)
		}
	}
	// Three blobs incl. the folder sentinel; sha1/md5 discriminate the
	// lazy ledger join.
	for _, b := range []metadata.Blob{
		{Sha256: blobAAA, Sha1: "sha1-aaa", Md5: "md5-aaa", Size: 10, CreatedAt: now},
		{Sha256: blobBBB, Sha1: "sha1-bbb", Md5: "md5-bbb", Size: 20, CreatedAt: now},
		{Sha256: chainMarker, Size: 0, CreatedAt: now},
	} {
		if err := st.Blobs().Put(ctx, &b); err != nil {
			t.Fatalf("blob: %v", err)
		}
	}
	nodes := []struct {
		repo, path, sha, by, at string
	}{
		{"libs", "root.bin", blobAAA, "alice", "2026-08-01T10:00:00Z"},
		{"libs", "org/lib-1.0.jar", blobAAA, "alice", "2026-08-02T10:00:00Z"},
		{"libs", "org/sub/lib-2.0.jar", blobBBB, "bob", "2026-08-03T10:00:30.500Z"},
		{"libs", "日本語/資料-01.txt", blobAAA, "alice", "2026-08-04T10:00:00Z"},
		{"libs-cache", "remote/replicated.bin", blobBBB, "carol", "2026-08-05T10:00:00Z"},
		{"other", "unrelated.bin", blobAAA, "dave", "2026-08-06T10:00:00Z"},
		{"libs", "org/", chainMarker, "alice", "2026-08-01T00:00:00Z"},
		{"libs", "org/sub/", chainMarker, "alice", "2026-08-01T00:00:00Z"},
	}
	for _, n := range nodes {
		if err := st.Nodes().Put(ctx, &metadata.Node{
			RepoKey: n.repo, Path: n.path, Sha256: n.sha, Size: 7,
			Mime: "application/octet-stream", CreatedBy: n.by,
			CreatedAt: n.at, UpdatedAt: n.at,
		}); err != nil {
			t.Fatalf("node %s/%s: %v", n.repo, n.path, err)
		}
	}
	props := []struct {
		repo, path string
		m          map[string][]string
	}{
		{"libs", "org/lib-1.0.jar", map[string][]string{"license": {"Apache-2.0"}}},
		// env is multi-valued here: the any-of and $msp semantics
		// discriminate exactly on this node.
		{"libs", "日本語/資料-01.txt", map[string][]string{"env": {"prod", "dev"}}},
		{"libs-cache", "remote/replicated.bin", map[string][]string{"license": {"MIT"}, "env": {"prod"}}},
	}
	for _, p := range props {
		if err := st.NodeProps().Merge(ctx, p.repo, p.path, p.m); err != nil {
			t.Fatalf("props %s/%s: %v", p.repo, p.path, err)
		}
	}
	return st
}

// runQuery compiles and executes one AQL text end to end.
func runQuery(t *testing.T, st metadata.Store, aql string, opt search.PlanOptions) ([]*metadata.NodeQueryRow, *search.Plan) {
	t.Helper()
	ast, err := search.Parse(aql)
	if err != nil {
		t.Fatalf("Parse(%q): %v", aql, err)
	}
	plan, err := search.PlanQuery(context.Background(), ast, opt)
	if err != nil {
		t.Fatalf("PlanQuery(%q): %v", aql, err)
	}
	q, ok := st.Nodes().(metadata.NodeQueryer)
	if !ok {
		t.Fatal("store does not satisfy NodeQueryer")
	}
	rows, err := q.QueryNodes(context.Background(), plan.Query)
	if err != nil {
		t.Fatalf("QueryNodes(%q): %v", aql, err)
	}
	return rows, plan
}

func paths(rows []*metadata.NodeQueryRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.RepoKey+"/"+r.Path)
	}
	return out
}

func TestChainDefaultFileSemanticsAndDerivations(t *testing.T) {
	st := openChainStore(t)
	opt := search.PlanOptions{Now: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}

	rows, plan := runQuery(t, st, `items.find({"repo":"libs"})`, opt)
	// The implicit file predicate keeps the marker rows out of the default
	// result (aql.md §2.2, v09).
	if got := strings.Join(paths(rows), ","); got != "libs/org/lib-1.0.jar,libs/org/sub/lib-2.0.jar,libs/root.bin,libs/日本語/資料-01.txt" {
		t.Fatalf("rows = %v", got)
	}
	// Default output: 9 echo fields, no modified_by anywhere.
	if n := len(plan.Output); n != 9 {
		t.Fatalf("default output = %d fields, want 9", n)
	}
	byPath := map[string]*metadata.NodeQueryRow{}
	for _, r := range rows {
		byPath[r.Path] = r
	}
	if r := byPath["root.bin"]; r.ParentPath != "" || r.Name != "root.bin" || r.Depth != 1 || r.Type != "file" {
		t.Fatalf("root row derived columns: %+v", r)
	}
	if r := byPath["org/sub/lib-2.0.jar"]; r.ParentPath != "org/sub" || r.Name != "lib-2.0.jar" || r.Depth != 3 {
		t.Fatalf("nested row derived columns: %+v", r)
	}
	// The explicit folder arm returns only markers — the folder derivation
	// check rides its rows; "any" disables the file default entirely.
	folders, _ := runQuery(t, st, `items.find({"repo":"libs","type":"folder"})`, opt)
	if len(folders) != 2 || folders[0].Type != "folder" {
		t.Fatalf("type folder rows = %v", paths(folders))
	}
	if f := folders[0]; f.Path != "org/" || f.ParentPath != "org" || f.Name != "" || f.Depth != 1 {
		t.Fatalf("folder row derived columns: %+v", f)
	}
	anyRows, _ := runQuery(t, st, `items.find({"repo":"libs","type":"any"})`, opt)
	if len(anyRows) != len(rows)+len(folders) {
		t.Fatalf("type any must disable the file default: %d vs %d", len(anyRows), len(rows)+len(folders))
	}
}

func TestChainCriteriaLegs(t *testing.T) {
	st := openChainStore(t)
	opt := search.PlanOptions{Now: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}

	t.Run("name wildcard", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"name":{"$match":"lib-*.jar"}})`, opt)
		if len(rows) != 2 {
			t.Fatalf("wildcard rows = %v", paths(rows))
		}
	})
	t.Run("parent path equality", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"path":"org"})`, opt)
		// Only the file under org (the marker's parent is also "org", but
		// the implicit file predicate keeps it out of this query).
		if len(rows) != 1 || rows[0].Name != "lib-1.0.jar" {
			t.Fatalf("path rows = %v", paths(rows))
		}
	})
	t.Run("unicode name match", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"name":{"$match":"資料-*"}})`, opt)
		if len(rows) != 1 || rows[0].Name != "資料-01.txt" {
			t.Fatalf("unicode rows = %v", paths(rows))
		}
	})
	t.Run("escaped wildcard literals", func(t *testing.T) {
		// "%" and "_" are literal outside the wildcard translator's * and ?;
		// a name containing a literal percent must not widen the match.
		rows, _ := runQuery(t, st, `items.find({"name":{"$match":"100%_done*"}})`, opt)
		if len(rows) != 0 {
			t.Fatalf("literal %%/_ must not act as wildcards: %v", paths(rows))
		}
	})
	t.Run("depth range", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"depth":{"$gte":3}})`, opt)
		if len(rows) != 1 || rows[0].Path != "org/sub/lib-2.0.jar" {
			t.Fatalf("depth rows = %v", paths(rows))
		}
	})
	t.Run("date window with partial days", func(t *testing.T) {
		// The T-409 grammar takes one comparator per field pair, so the
		// window is a $and of two created conditions.
		rows, _ := runQuery(t, st,
			`items.find({"$and":[{"created":{"$gte":"2026-08-03"}},{"created":{"$lte":"2026-08-05"}}]})`, opt)
		if len(rows) != 3 {
			t.Fatalf("window rows = %v", paths(rows))
		}
	})
	t.Run("relative last window", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"created":{"$last":"28 days"}})`, opt)
		// Boundary Aug 4 00:00 UTC: the three rows created strictly after.
		if len(rows) != 3 {
			t.Fatalf("$last rows = %v", paths(rows))
		}
	})
	t.Run("checksums through the lazy ledger join", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"actual_sha1":"sha1-bbb"}).include("sha256","actual_sha1","actual_md5")`, opt)
		if len(rows) != 2 {
			t.Fatalf("sha1 rows = %v", paths(rows))
		}
		for _, r := range rows {
			if r.Sha1 != "sha1-bbb" || r.Md5 != "md5-bbb" {
				t.Fatalf("ledger not joined: %+v", r)
			}
		}
	})
	t.Run("sort paging", func(t *testing.T) {
		all, _ := runQuery(t, st, `items.find({}).sort({"$asc":["created"]})`, opt)
		page, _ := runQuery(t, st, `items.find({}).sort({"$asc":["created"]}).offset(1).limit(2)`, opt)
		if len(page) != 2 || page[0].Path != all[1].Path || page[1].Path != all[2].Path {
			t.Fatalf("page = %v, want %v", paths(page), paths(all[1:3]))
		}
	})
}

func TestChainPropertyLegs(t *testing.T) {
	st := openChainStore(t)
	opt := search.PlanOptions{Now: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}

	t.Run("key equality", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"@license":"Apache-2.0"})`, opt)
		if len(rows) != 1 || rows[0].Path != "org/lib-1.0.jar" {
			t.Fatalf("license rows = %v", paths(rows))
		}
	})
	t.Run("key existence via star value", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"@env":"*"})`, opt)
		if len(rows) != 2 {
			t.Fatalf("env-existence rows = %v", paths(rows))
		}
	})
	t.Run("catch-all key with value", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"@*":"prod"})`, opt)
		if len(rows) != 2 {
			t.Fatalf("catch-all rows = %v", paths(rows))
		}
	})
	t.Run("any-of vs $msp on a multi-valued property", func(t *testing.T) {
		anyOf, _ := runQuery(t, st,
			`items.find({"$and":[{"@env":"prod"},{"@env":"dev"}]})`, opt)
		if len(anyOf) != 1 || anyOf[0].Name != "資料-01.txt" {
			t.Fatalf("any-of rows = %v", paths(anyOf))
		}
		same, _ := runQuery(t, st,
			`items.find({"$msp":[{"@env":"prod"},{"@env":"dev"}]})`, opt)
		if len(same) != 0 {
			t.Fatalf("$msp must bind one property instance: %v", paths(same))
		}
	})
	t.Run("projection carries property values", func(t *testing.T) {
		rows, plan := runQuery(t, st, `items.find({"@license":"*"}).include("name","@license")`, opt)
		if len(rows) != 2 || len(plan.Query.Props) != 1 {
			t.Fatalf("projection rows = %v", paths(rows))
		}
		for _, r := range rows {
			if r.Props["license"] == nil {
				t.Fatalf("props not projected for %s: %v", r.Path, r.Props)
			}
		}
	})
	t.Run("property long form", func(t *testing.T) {
		rows, _ := runQuery(t, st, `items.find({"property.key":"license","property.value":"MIT"})`, opt)
		if len(rows) != 1 || rows[0].RepoKey != "libs-cache" {
			t.Fatalf("long-form rows = %v", paths(rows))
		}
	})
}

// chainVirtual is the T-413 stand-in resolver: "dist" aggregates the two
// member repos of the corpus.
type chainVirtual struct{}

func (chainVirtual) Members(_ context.Context, key string) ([]string, error) {
	if key == "dist" {
		return []string{"libs", "libs-cache"}, nil
	}
	return nil, nil
}

func TestChainVirtualRepoExpansion(t *testing.T) {
	st := openChainStore(t)
	opt := search.PlanOptions{Now: time.Now().UTC(), Virtual: chainVirtual{}}

	// $eq on the virtual key answers member storage rows; "other" never
	// appears even though it exists.
	rows, _ := runQuery(t, st, `items.find({"repo":"dist"})`, opt)
	if len(rows) != 5 {
		t.Fatalf("virtual rows = %v", paths(rows))
	}
	for _, r := range rows {
		if r.RepoKey == "other" || r.RepoKey == "dist" {
			t.Fatalf("row escaped the expansion: %+v", r)
		}
	}
	// A plain unknown key stays the honest empty set (aql.md §7-4).
	none, _ := runQuery(t, st, `items.find({"repo":"ghost-repo"})`, opt)
	if len(none) != 0 {
		t.Fatalf("unknown repo must be empty: %v", paths(none))
	}
	// $ne on the virtual excludes its members but keeps everything else.
	rest, _ := runQuery(t, st, `items.find({"repo":{"$ne":"dist"}})`, opt)
	if len(rest) != 1 || rest[0].RepoKey != "other" {
		t.Fatalf("$ne virtual rows = %v", paths(rest))
	}
}

func TestChainIncludeStarCoversStorageFields(t *testing.T) {
	st := openChainStore(t)
	opt := search.PlanOptions{Now: time.Now().UTC()}
	rows, plan := runQuery(t, st, `items.find({"repo":"libs","name":"root.bin"}).include("*")`, opt)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", paths(rows))
	}
	r := rows[0]
	if r.Sha1 != "sha1-aaa" || r.Md5 != "md5-aaa" || r.Depth != 1 || r.Sha256 != blobAAA {
		t.Fatalf("star projection must fill every storage field: %+v", r)
	}
	kinds := map[search.OutputKind]bool{}
	for _, o := range plan.Output {
		kinds[o.Kind] = true
	}
	if !kinds[search.OutputVirtualRepos] {
		t.Fatalf("star must echo virtual_repos: %v", plan.Output)
	}
	if fmt.Sprint(plan.Output[0].Key) != "repo" {
		t.Fatalf("star echo order starts at repo: %v", plan.Output)
	}
}
