package metadata_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// searcherOf asserts the store's Nodes() carries the NodeSearcher seam and
// returns it (T-92's wiring contract: the production sqlite store always
// satisfies it).
func searcherOf(t *testing.T, st metadata.Store) metadata.NodeSearcher {
	t.Helper()
	ns, ok := st.Nodes().(metadata.NodeSearcher)
	if !ok {
		t.Fatalf("Nodes() does not implement NodeSearcher")
	}
	return ns
}

// seedSearchNodes seeds file nodes (path -> blob index via fakeBlob) in
// repo; seedNodes creates the repo row itself.
func seedSearchNodes(t *testing.T, st metadata.Store, key string, paths ...string) {
	t.Helper()
	seedNodes(t, st, key, paths...)
}

// seedFolderRow writes one trailing-slash folder marker row (the shared
// empty-folder sentinel blob) — the rows search must never surface.
func seedFolderRow(t *testing.T, st metadata.Store, key, path string) {
	t.Helper()
	ctx := context.Background()
	now := metadata.Now()
	sentinel := "0000000000000000000000000000000000000000000000000000000000000000"
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sentinel, Size: 0, CreatedAt: now}); err != nil {
		t.Fatalf("folder ledger row: %v", err)
	}
	if err := st.Nodes().Put(ctx, &metadata.Node{
		RepoKey: key, Path: path, Sha256: sentinel, Size: 0, CreatedBy: "t",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("folder node %s: %v", path, err)
	}
}

// TestSearchByName pins SR-01's SQL semantics under K64's calibration
// (aql.md section 0-5, T-417): a literal path substring matched
// case-INSENSITIVELY — the fragment folds and the column wraps in LOWER(),
// so the store's case_sensitive_like DSN pragma is irrelevant on this arm.
// The fragment escaping keeps %/_/\ literal, and the fold is ASCII (the
// registered limitation).
func TestSearchByName(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	ns := searcherOf(t, st)
	seedSearchNodes(t, st, "alpha",
		"acme/artifact.bin",       // the W14 target
		"acme/ARTIFACT.bin",       // the K64 twin: folds onto the same match
		"other/lib.jar",           // no match below
		"deep/acme/readme.txt",    // fragment in a DIRECTORY name matches too
		"my_lib/artifact.bin",     // underscore must stay literal
		"myXlib/artifact.bin",     // would match my_lib if _ were a wildcard
		"100pct/artifact.bin",     // percent must stay literal
		"100Xct/artifact.bin",     // would match 100pct if % were a wildcard
		`back\slash/artifact.bin`, // backslash must stay literal
	)
	seedFolderRow(t, st, "alpha", "acme/")

	tests := []struct {
		name string
		frag string
		want []string
	}{
		{"filename substring (W14)", "artifact", []string{
			"100Xct/artifact.bin", "100pct/artifact.bin", "acme/ARTIFACT.bin",
			"acme/artifact.bin", `back\slash/artifact.bin`, "myXlib/artifact.bin",
			"my_lib/artifact.bin",
		}},
		{"case-insensitive: the uppercase twin joins (K64)", "artifact.bin", []string{
			"100Xct/artifact.bin", "100pct/artifact.bin", "acme/ARTIFACT.bin",
			"acme/artifact.bin", `back\slash/artifact.bin`, "myXlib/artifact.bin",
			"my_lib/artifact.bin",
		}},
		{"uppercase fragment folds onto every twin (K64)", "ARTIFACT", []string{
			"100Xct/artifact.bin", "100pct/artifact.bin", "acme/ARTIFACT.bin",
			"acme/artifact.bin", `back\slash/artifact.bin`, "myXlib/artifact.bin",
			"my_lib/artifact.bin",
		}},
		{"directory fragment folds too (K64)", "DEEP/ACME", []string{"deep/acme/readme.txt"}},
		{"directory name substring matches its files", "deep/acme", []string{"deep/acme/readme.txt"}},
		{"underscore stays literal", "my_lib", []string{"my_lib/artifact.bin"}},
		{"percent stays literal", "100pct", []string{"100pct/artifact.bin"}},
		{"backslash stays literal", `back\slash`, []string{`back\slash/artifact.bin`}},
		{"extension fragment", ".jar", []string{"other/lib.jar"}},
		{"no hit is an empty (nil) page", "does-not-exist", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ns.SearchByName(ctx, tt.frag, nil)
			if err != nil {
				t.Fatalf("SearchByName(%q): %v", tt.frag, err)
			}
			if !equalPaths(pathsOf(t, got), tt.want) {
				t.Fatalf("SearchByName(%q) = %v, want %v", tt.frag, pathsOf(t, got), tt.want)
			}
		})
	}

	// The folder row seeded above must never surface even when the fragment
	// addresses it directly (search answers artifacts only).
	got, err := ns.SearchByName(ctx, "acme", nil)
	if err != nil {
		t.Fatalf("SearchByName(acme): %v", err)
	}
	for _, p := range pathsOf(t, got) {
		if strings.HasSuffix(p, "/") {
			t.Fatalf("folder row %q surfaced in search results", p)
		}
	}
}

// TestSearchByNameReposFilter pins the repos IN predicate: the filter is
// SQL-side narrowing, unknown keys match nothing and never error (the E-04
// filter posture).
func TestSearchByNameReposFilter(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	ns := searcherOf(t, st)
	seedSearchNodes(t, st, "generic-local", "acme/artifact.bin")
	seedSearchNodes(t, st, "other-local", "acme/artifact.bin")
	seedSearchNodes(t, st, "docker-local", "acme/artifact.bin")

	tests := []struct {
		name  string
		repos []string
		want  []string // repo keys of the results, ordered
	}{
		{"no filter reaches every repo", nil, []string{"docker-local", "generic-local", "other-local"}},
		{"empty filter means every repo", []string{}, []string{"docker-local", "generic-local", "other-local"}},
		{"one repo narrows to it", []string{"other-local"}, []string{"other-local"}},
		{"two repos narrow to both", []string{"generic-local", "other-local"}, []string{"generic-local", "other-local"}},
		{"unknown key matches nothing (no error)", []string{"ghost-local"}, nil},
		{"mixed known and unknown", []string{"ghost-local", "generic-local"}, []string{"generic-local"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ns.SearchByName(ctx, "artifact", tt.repos)
			if err != nil {
				t.Fatalf("SearchByName: %v", err)
			}
			repoKeys := make([]string, 0, len(got))
			for _, n := range got {
				repoKeys = append(repoKeys, n.RepoKey)
			}
			if !equalPaths(repoKeys, tt.want) {
				t.Fatalf("repos filter %v = %v, want %v", tt.repos, repoKeys, tt.want)
			}
		})
	}
}

// seedChecksumBlob writes one ledger row carrying an explicit triple and one
// node per (repo, path) referencing it — the C07 shared-blob shape.
func seedChecksumBlob(t *testing.T, st metadata.Store, sha256hex, sha1hex, md5hex string, refs ...[2]string) {
	t.Helper()
	ctx := context.Background()
	now := metadata.Now()
	if err := st.Blobs().Put(ctx, &metadata.Blob{
		Sha256: sha256hex, Sha1: sha1hex, Md5: md5hex, Size: 7, CreatedAt: now,
	}); err != nil {
		t.Fatalf("blob put %s: %v", sha256hex, err)
	}
	for _, ref := range refs {
		if err := st.Nodes().Put(ctx, &metadata.Node{
			RepoKey: ref[0], Path: ref[1], Sha256: sha256hex, Size: 7, CreatedBy: "t",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("node put %s/%s: %v", ref[0], ref[1], err)
		}
	}
}

// hexOf builds a deterministic lowercase hex digest of the wanted length
// from a seed — distinct seeds give distinct digests, and the value is only
// ledger text (the store never re-derives it).
func hexOf(seed string, want int) string {
	sum := sha256.Sum256([]byte("t92-digest:" + seed))
	out := hex.EncodeToString(sum[:])
	for len(out) < want {
		next := sha256.Sum256([]byte(out))
		out += hex.EncodeToString(next[:])
	}
	return out[:want]
}

// TestSearchByChecksum pins SR-02's mechanics: exact digest matching, the
// ledger resolution of sha1/md5, the union of several digests and the
// cross-repository completeness (every referencing node surfaces).
func TestSearchByChecksum(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	ns := searcherOf(t, st)
	putRepo(t, st, "generic-local")
	putRepo(t, st, "other-local")

	shaA := hexOf("a", 64)
	shaB := hexOf("b", 64)
	seedChecksumBlob(t, st, shaA, hexOf("a1", 40), hexOf("a2", 32),
		[2]string{"generic-local", "acme/artifact.bin"},
		[2]string{"other-local", "mirror/artifact.bin"}) // C07: two paths, one blob
	seedChecksumBlob(t, st, shaB, hexOf("b1", 40), hexOf("b2", 32),
		[2]string{"generic-local", "unrelated/tool.bin"})
	seedFolderRow(t, st, "generic-local", "acme/")

	t.Run("sha256 returns every referencing node across repos (W15)", func(t *testing.T) {
		got, err := ns.SearchByChecksum(ctx, shaA, "", "", nil)
		if err != nil {
			t.Fatalf("SearchByChecksum: %v", err)
		}
		want := []string{"generic-local/acme/artifact.bin", "other-local/mirror/artifact.bin"}
		gotKeys := make([]string, 0, len(got))
		for _, n := range got {
			gotKeys = append(gotKeys, n.RepoKey+"/"+n.Path)
		}
		if !equalPaths(gotKeys, want) {
			t.Fatalf("sha256 search = %v, want %v", gotKeys, want)
		}
	})

	t.Run("sha1 resolves through the ledger", func(t *testing.T) {
		got, err := ns.SearchByChecksum(ctx, "", hexOf("b1", 40), "", nil)
		if err != nil {
			t.Fatalf("SearchByChecksum: %v", err)
		}
		if len(got) != 1 || got[0].Path != "unrelated/tool.bin" {
			t.Fatalf("sha1 search = %v", pathsOf(t, got))
		}
	})

	t.Run("md5 resolves through the ledger", func(t *testing.T) {
		got, err := ns.SearchByChecksum(ctx, "", "", hexOf("a2", 32), nil)
		if err != nil {
			t.Fatalf("SearchByChecksum: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("md5 search returned %d nodes, want 2: %v", len(got), pathsOf(t, got))
		}
	})

	t.Run("two digests union their blob sets", func(t *testing.T) {
		got, err := ns.SearchByChecksum(ctx, shaA, hexOf("b1", 40), "", nil)
		if err != nil {
			t.Fatalf("SearchByChecksum: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("union search returned %d nodes, want 3: %v", len(got), pathsOf(t, got))
		}
	})

	t.Run("repos filter narrows checksum results", func(t *testing.T) {
		got, err := ns.SearchByChecksum(ctx, shaA, "", "", []string{"other-local"})
		if err != nil {
			t.Fatalf("SearchByChecksum: %v", err)
		}
		if len(got) != 1 || got[0].RepoKey != "other-local" {
			t.Fatalf("repos-filtered sha256 search = %v", pathsOf(t, got))
		}
	})

	t.Run("miss answers an empty page, no error", func(t *testing.T) {
		got, err := ns.SearchByChecksum(ctx, hexOf("zz", 64), "", "", nil)
		if err != nil {
			t.Fatalf("SearchByChecksum: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("miss search = %v", pathsOf(t, got))
		}
	})

	t.Run("all-empty digests address no blob", func(t *testing.T) {
		got, err := ns.SearchByChecksum(ctx, "", "", "", nil)
		if err != nil {
			t.Fatalf("SearchByChecksum: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("empty-digest search = %v", pathsOf(t, got))
		}
	})
}

// seedProps writes properties onto a seeded node through the real
// NodePropStore face (the M10 write arm — the search arm's sibling).
func seedProps(t *testing.T, st metadata.Store, key, path string, props map[string]string) {
	t.Helper()
	set := make(map[string][]string, len(props))
	for k, v := range props {
		set[k] = []string{v}
	}
	if err := st.NodeProps().Merge(context.Background(), key, path, set); err != nil {
		t.Fatalf("props merge %s/%s: %v", key, path, err)
	}
}

// TestSearchByPath pins the gavc arm's SQL semantics (T-417, FR-134.1): a
// literal leading prefix plus literal infix substrings, case-SENSITIVE (a
// maven coordinate is), wildcard bytes in the pieces escaped to themselves,
// folder rows never surfacing, the repos filter narrowing, and limit > 0
// capping the page SQL-side.
func TestSearchByPath(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	ns := searcherOf(t, st)
	seedSearchNodes(t, st, "maven-local",
		"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
		"com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
		"com/acme/demo-app/2.0.0/demo-app-2.0.0.jar",
		"com/acme/other-lib/1.0.0/other-lib-1.0.0.jar",
		"COM/ACME/demo-app/1.0.0/demo-app-1.0.0.jar",  // case twin: gavc stays case-sensitive
		"org/other/demo-app/3.0.0/demo-app-3.0.0.jar", // the module at another org
		"com/acme/100pct/demo/100pct-1.0.jar",         // literal % inside the path text
	)
	seedSearchNodes(t, st, "maven-other", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar")
	seedFolderRow(t, st, "maven-local", "com/acme/")

	tests := []struct {
		name  string
		f     metadata.PathFilter
		repos []string
		want  []string
	}{
		{"group+artifact+version prefix", metadata.PathFilter{
			Prefix: "com/acme/demo-app/1.0.0"}, nil, []string{
			"com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", // maven-other's twin
		}},
		{"group prefix reaches every version", metadata.PathFilter{
			Prefix: "com/acme/demo-app"}, nil, []string{
			"com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			"com/acme/demo-app/2.0.0/demo-app-2.0.0.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", // maven-other's twin
		}},
		{"prefix is case-sensitive", metadata.PathFilter{
			Prefix: "com/acme/DEMO-APP"}, nil, nil},
		{"prefix anchors at the head (a suffix is not a prefix)", metadata.PathFilter{
			Prefix: "acme/demo-app"}, nil, nil},
		// Raw-head semantics: this layer implies NO segment boundary — the
		// gavc caller's trailing slash is what makes a prefix a directory.
		{"prefix is raw head bytes, boundary or not", metadata.PathFilter{
			Prefix: "com/acme/demo-app/1.0"}, nil, []string{
			"com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", // maven-other's twin
		}},
		{"artifact infix without group", metadata.PathFilter{
			Infixes: []string{"/demo-app/"}}, nil, []string{
			"COM/ACME/demo-app/1.0.0/demo-app-1.0.0.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			"com/acme/demo-app/2.0.0/demo-app-2.0.0.jar",
			"org/other/demo-app/3.0.0/demo-app-3.0.0.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", // maven-other's twin
		}},
		{"classifier infix", metadata.PathFilter{
			Infixes: []string{"-sources."}}, nil, []string{
			"com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
		}},
		{"prefix plus classifier infix compose", metadata.PathFilter{
			Prefix: "com/acme", Infixes: []string{"-sources."}}, nil, []string{
			"com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
		}},
		{"repos filter narrows", metadata.PathFilter{
			Prefix: "com/acme/demo-app/1.0.0"}, []string{"maven-other"}, []string{
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
		}},
		{"unknown repo matches nothing", metadata.PathFilter{
			Prefix: "com/acme"}, []string{"ghost"}, nil},
		{"empty filter matches every file", metadata.PathFilter{}, nil, []string{
			"COM/ACME/demo-app/1.0.0/demo-app-1.0.0.jar",
			"com/acme/100pct/demo/100pct-1.0.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
			"com/acme/demo-app/2.0.0/demo-app-2.0.0.jar",
			"com/acme/other-lib/1.0.0/other-lib-1.0.0.jar",
			"org/other/demo-app/3.0.0/demo-app-3.0.0.jar",
			"com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", // maven-other's twin
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ns.SearchByPath(ctx, tt.f, 0, tt.repos)
			if err != nil {
				t.Fatalf("SearchByPath: %v", err)
			}
			if !equalPaths(pathsOf(t, got), tt.want) {
				t.Fatalf("SearchByPath(%+v) = %v, want %v", tt.f, pathsOf(t, got), tt.want)
			}
		})
	}

	// The limit cap trims SQL-side (the K63 +1 probe rides this parameter).
	got, err := ns.SearchByPath(ctx, metadata.PathFilter{Prefix: "com/acme"}, 2, nil)
	if err != nil {
		t.Fatalf("SearchByPath limit: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("limited search returned %d rows, want 2", len(got))
	}

	// The folder row never surfaces even when the prefix addresses it.
	got, err = ns.SearchByPath(ctx, metadata.PathFilter{Prefix: "com/acme"}, 0, nil)
	if err != nil {
		t.Fatalf("SearchByPath: %v", err)
	}
	for _, p := range pathsOf(t, got) {
		if strings.HasSuffix(p, "/") {
			t.Fatalf("folder row %q surfaced in search results", p)
		}
	}
}

// TestSearchByProps pins the prop arm's SQL semantics (T-417, FR-134.2): a
// key-without-value condition is bare existence, a value-pinned one is an
// exact match, constraints AND across keys, multi-valued keys match when any
// value does, and unknown keys answer the honest empty page.
func TestSearchByProps(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	ns := searcherOf(t, st)
	seedSearchNodes(t, st, "generic-local",
		"acme/artifact.bin", "acme/other.bin", "acme/multi.bin", "acme/bare.bin")
	seedSearchNodes(t, st, "other-local", "acme/artifact.bin")
	seedProps(t, st, "generic-local", "acme/artifact.bin",
		map[string]string{"license": "Apache-2.0", "build.name": "demo"})
	seedProps(t, st, "generic-local", "acme/other.bin",
		map[string]string{"license": "MIT"})
	seedProps(t, st, "generic-local", "acme/multi.bin",
		map[string]string{"license": "GPL-3.0-only"})
	if err := st.NodeProps().Merge(ctx, "generic-local", "acme/multi.bin",
		map[string][]string{"license": {"Apache-2.0"}}); err != nil {
		t.Fatalf("second value: %v", err)
	}
	seedProps(t, st, "generic-local", "acme/bare.bin",
		map[string]string{"stage": "gold"})

	tests := []struct {
		name  string
		conds []metadata.PropFilter
		repos []string
		want  []string
	}{
		{"key=value pins the value", []metadata.PropFilter{{Key: "license", Value: "Apache-2.0"}}, nil, []string{
			"acme/artifact.bin", "acme/multi.bin",
		}},
		{"key without value is bare existence", []metadata.PropFilter{{Key: "license"}}, nil, []string{
			"acme/artifact.bin", "acme/multi.bin", "acme/other.bin",
		}},
		{"constraints AND across keys", []metadata.PropFilter{
			{Key: "license", Value: "Apache-2.0"}, {Key: "build.name"}}, nil, []string{
			"acme/artifact.bin",
		}},
		{"both values pinned intersects", []metadata.PropFilter{
			{Key: "license", Value: "MIT"}, {Key: "build.name", Value: "demo"}}, nil, nil},
		{"unknown key is the honest empty page", []metadata.PropFilter{{Key: "ghost"}}, nil, nil},
		{"repos filter narrows", []metadata.PropFilter{{Key: "license"}}, []string{"other-local"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ns.SearchByProps(ctx, tt.conds, 0, tt.repos)
			if err != nil {
				t.Fatalf("SearchByProps: %v", err)
			}
			if !equalPaths(pathsOf(t, got), tt.want) {
				t.Fatalf("SearchByProps(%v) = %v, want %v", tt.conds, pathsOf(t, got), tt.want)
			}
		})
	}
}

// TestSearchByPattern pins the pattern arm's SQL semantics (T-417,
// FR-134.3): the repo and path patterns arrive PRE-TRANSLATED (the kernel
// lives in internal/search and has its own suite — match_test.go); this
// layer adds only the ESCAPE clause, so a translated literal wildcard stays
// literal and the path pattern anchors at the head.
func TestSearchByPattern(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	ns := searcherOf(t, st)
	seedSearchNodes(t, st, "maven-local",
		"com/acme/lib/a.jar",
		"com/acme/lib/deep/b.jar",
		"com/acme/root.jar",
		"com/acme/lib/x.pom",
		"100%/lib/c.jar",
	)
	seedSearchNodes(t, st, "other-local", "com/acme/lib/a.jar")

	tests := []struct {
		name               string
		repoLike, pathLike string
		want               []string
	}{
		{"literal repo and path head", "maven-local", "com/acme/root.jar", []string{
			"maven-local/com/acme/root.jar",
		}},
		{"translated star crosses segments (SQL % semantics)", "maven-local", "com/acme/%.jar", []string{
			"maven-local/com/acme/lib/a.jar",
			"maven-local/com/acme/lib/deep/b.jar",
			"maven-local/com/acme/root.jar",
		}},
		{"translated double-star behaves as one star (depth floor: one slash)", "maven-local", "com/acme/%%/%.jar", []string{
			"maven-local/com/acme/lib/a.jar",
			"maven-local/com/acme/lib/deep/b.jar",
		}},
		{"repo pattern spans repositories", "%-local", "com/acme/lib/a.jar", []string{
			"maven-local/com/acme/lib/a.jar", "other-local/com/acme/lib/a.jar",
		}},
		{"path pattern anchors at the head", "maven-local", "acme/%", nil},
		{"translated literal percent stays literal", "maven-local", `100\%/lib/%`, []string{
			"maven-local/100%/lib/c.jar",
		}},
		{"empty path pattern leaves the repo constraint alone", "other-local", "", []string{
			"other-local/com/acme/lib/a.jar",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ns.SearchByPattern(ctx, tt.repoLike, tt.pathLike, 0)
			if err != nil {
				t.Fatalf("SearchByPattern: %v", err)
			}
			keys := make([]string, 0, len(got))
			for _, n := range got {
				keys = append(keys, n.RepoKey+"/"+n.Path)
			}
			if !equalPaths(keys, tt.want) {
				t.Fatalf("SearchByPattern(%q,%q) = %v, want %v", tt.repoLike, tt.pathLike, keys, tt.want)
			}
		})
	}
}
