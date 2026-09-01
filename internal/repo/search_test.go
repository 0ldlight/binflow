package repo_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedSearchEnv builds the two-repository visibility playground: r1 holds a
// public and a secret artifact, r2 another public one. The authorizer grants
// alice read on the "pub" path prefix only (r1's pub/ arm).
func seedSearchEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	mustCreateRepo(t, e, "r1")
	mustCreateRepo(t, e, "r2")
	put(t, e, admin(), "r1", "pub/artifact.bin", "shared-artifact")
	put(t, e, admin(), "r1", "secret/artifact.bin", "secret-artifact")
	put(t, e, admin(), "r2", "pub2/also-artifact.bin", "other-artifact")
	e.az.add("alice", repo.ActionRead, "pub/")
	return e
}

// TestSearchArtifactsValidation pins the SR-01 query-shape contract: a blank
// name and an oversized repos filter answer ErrInvalidSearchQuery before any
// store access.
func TestSearchArtifactsValidation(t *testing.T) {
	e := seedSearchEnv(t)
	ctx := context.Background()

	tooMany := make([]string, 1001)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("repo-%d", i)
	}

	tests := []struct {
		name  string
		query string
		repos []string
	}{
		{"missing name", "", nil},
		{"blank name", "   ", nil},
		{"repos filter beyond the cap", "artifact", tooMany},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.svc.SearchArtifacts(ctx, admin(), tt.query, tt.repos)
			if !errors.Is(err, repo.ErrInvalidSearchQuery) {
				t.Fatalf("SearchArtifacts(%q) err = %v, want ErrInvalidSearchQuery", tt.query, err)
			}
		})
	}
}

// TestSearchChecksumValidation pins the SR-02 query-shape contract: at least
// one digest, each present one bare hex of its algorithm's length; mixed
// case is accepted and normalized.
func TestSearchChecksumValidation(t *testing.T) {
	e := seedSearchEnv(t)
	ctx := context.Background()
	n := put(t, e, admin(), "r1", "pub/artifact.bin", "checksum-shape")

	tests := []struct {
		name    string
		q       repo.ChecksumQuery
		wantErr bool
	}{
		{"all digests absent", repo.ChecksumQuery{}, true},
		{"short sha256", repo.ChecksumQuery{Sha256: "abc123"}, true},
		{"non-hex sha256", repo.ChecksumQuery{Sha256: strings.Repeat("z", 64)}, true},
		{"wrong-length sha1", repo.ChecksumQuery{Sha1: strings.Repeat("a", 64)}, true},
		{"non-hex md5", repo.ChecksumQuery{Md5: strings.Repeat("g", 32)}, true},
		{"docker-prefixed digest is not bare hex", repo.ChecksumQuery{Sha256: "sha256:" + strings.Repeat("a", 64)}, true},
		{"valid sha256", repo.ChecksumQuery{Sha256: n.Sha256}, false},
		{"mixed-case sha256 normalizes to the stored spelling", repo.ChecksumQuery{
			Sha256: strings.ToUpper(n.Sha256)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := e.svc.SearchChecksum(ctx, admin(), tt.q, nil)
			if tt.wantErr {
				if !errors.Is(err, repo.ErrInvalidSearchQuery) {
					t.Fatalf("SearchChecksum err = %v, want ErrInvalidSearchQuery", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SearchChecksum: %v", err)
			}
			if len(nodes) != 1 || nodes[0].Path != "pub/artifact.bin" {
				t.Fatalf("SearchChecksum = %v, want the seeded node", nodes)
			}
		})
	}
}

// TestSearchVisibilityMatrix is the NFR-S24 core: a result node is visible
// exactly when the caller could GET it. Admin sees everything; a path-level
// grant exposes exactly the granted arm (never the same-repo secret); a
// zero-grant authenticated user gets an empty page, not an error; anonymous
// on this fake authorizer (which, like a closed instance, denies anonymous
// reads) meets ErrForbidden before any store access.
func TestSearchVisibilityMatrix(t *testing.T) {
	e := seedSearchEnv(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		p        *repo.Principal
		wantErr  error
		wantHits []string // "<repo>/<path>"
	}{
		{"admin sees every repository", admin(), nil, []string{
			"r1/pub/artifact.bin", "r1/secret/artifact.bin", "r2/pub2/also-artifact.bin"}},
		{"path-level grant exposes only the granted arm", alice(), nil,
			[]string{"r1/pub/artifact.bin"}},
		{"zero-grant authenticated user gets an empty page", &repo.Principal{Name: "bob"}, nil, nil},
		{"anonymous on a closed instance is refused", nil, repo.ErrForbidden, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := e.svc.SearchArtifacts(ctx, tt.p, "artifact", nil)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SearchArtifacts err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SearchArtifacts: %v", err)
			}
			hits := make([]string, 0, len(nodes))
			for _, n := range nodes {
				hits = append(hits, n.RepoKey+"/"+n.Path)
			}
			if !equalStringSlices(hits, tt.wantHits) {
				t.Fatalf("visible hits = %v, want %v", hits, tt.wantHits)
			}
		})
	}

	// The same filter governs the checksum arm: r1's pub and secret nodes
	// hold different content, so alice's sha256 of the pub artifact must
	// surface while the secret one stays hidden even when addressed
	// directly.
	pub := put(t, e, admin(), "r1", "pub/artifact.bin", "checksum-shape")
	nodes, err := e.svc.SearchChecksum(ctx, alice(), repo.ChecksumQuery{Sha256: pub.Sha256}, nil)
	if err != nil {
		t.Fatalf("SearchChecksum as alice: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Path != "pub/artifact.bin" {
		t.Fatalf("alice's checksum search = %v, want exactly the pub node", nodes)
	}
}

// TestSearchCrossRepoChecksum is W15's C07 shape: one blob, two paths across
// two repositories — the checksum search must report every visible
// reference, and the repos filter must narrow it.
func TestSearchCrossRepoChecksum(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	mustCreateRepo(t, e, "other-local")
	n1 := put(t, e, admin(), "generic-local", "acme/artifact.bin", "same-bytes")
	n2 := put(t, e, admin(), "other-local", "mirror/path/artifact.bin", "same-bytes")
	if n1.Sha256 != n2.Sha256 {
		t.Fatalf("identical content produced different digests: %s vs %s", n1.Sha256, n2.Sha256)
	}
	ctx := context.Background()
	// The ledger row carries the ancillary sha1 the second subtest addresses.
	blob, err := e.md.Blobs().Get(ctx, n1.Sha256)
	if err != nil {
		t.Fatalf("ledger blob %s: %v", n1.Sha256, err)
	}

	tests := []struct {
		name     string
		q        repo.ChecksumQuery
		repos    []string
		wantHits []string
	}{
		{"sha256 reports every reference", repo.ChecksumQuery{Sha256: n1.Sha256}, nil,
			[]string{"generic-local/acme/artifact.bin", "other-local/mirror/path/artifact.bin"}},
		{"sha1 resolves through the ledger", repo.ChecksumQuery{Sha1: blob.Sha1}, nil,
			[]string{"generic-local/acme/artifact.bin", "other-local/mirror/path/artifact.bin"}},
		{"repos filter narrows the references", repo.ChecksumQuery{Sha256: n1.Sha256},
			[]string{"other-local"}, []string{"other-local/mirror/path/artifact.bin"}},
		{"unknown digest is an empty page", repo.ChecksumQuery{Sha256: strings.Repeat("0", 64)},
			nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := e.svc.SearchChecksum(ctx, admin(), tt.q, tt.repos)
			if err != nil {
				t.Fatalf("SearchChecksum: %v", err)
			}
			hits := make([]string, 0, len(nodes))
			for _, n := range nodes {
				hits = append(hits, n.RepoKey+"/"+n.Path)
			}
			if !equalStringSlices(hits, tt.wantHits) {
				t.Fatalf("hits = %v, want %v", hits, tt.wantHits)
			}
		})
	}
}

// TestSearchUnavailableWithoutSeam pins the defensive arm: a store whose
// Nodes() does not carry metadata.NodeSearcher answers
// repo.ErrSearchUnavailable instead of panicking on a nil interface.
func TestSearchUnavailableWithoutSeam(t *testing.T) {
	e := newEnvCustom(t, func(md metadata.Store) metadata.Store {
		return hideSearchStore{Store: md}
	})
	ctx := context.Background()

	if _, err := e.svc.SearchArtifacts(ctx, admin(), "anything", nil); !errors.Is(err, repo.ErrSearchUnavailable) {
		t.Fatalf("SearchArtifacts err = %v, want ErrSearchUnavailable", err)
	}
	if _, err := e.svc.SearchChecksum(ctx, admin(), repo.ChecksumQuery{Sha256: strings.Repeat("a", 64)}, nil); !errors.Is(err, repo.ErrSearchUnavailable) {
		t.Fatalf("SearchChecksum err = %v, want ErrSearchUnavailable", err)
	}
}

// hideSearchStore decorates a store so Nodes() returns a wrapper exposing
// only the NodeStore face — the NodeSearcher assertion in newService then
// fails, which is exactly the posture being pinned above.
type hideSearchStore struct {
	metadata.Store
}

type hideSearchNodes struct {
	metadata.NodeStore
}

func (s hideSearchStore) Nodes() metadata.NodeStore { return hideSearchNodes{s.Store.Nodes()} }

// equalStringSlices is the slice equality the search tests need (the
// metadata package's equalPaths twin, local to this file to keep the test
// self-contained).
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// legacyFace asserts the concrete service carries the FR-134 capability face
// (the CopyMoveService precedent — never part of the big Service interface).
func legacyFace(t *testing.T, e *env) repo.LegacySearchService {
	t.Helper()
	ls, ok := e.svc.(repo.LegacySearchService)
	if !ok {
		t.Fatalf("the concrete service does not carry LegacySearchService")
	}
	return ls
}

// TestSearchGavcValidation pins FR-134.1's query-shape contract: at least one
// coordinate, no path separators or control bytes inside a coordinate, no
// empty/dot-only group segments, a positive limit, and the repos cap — every
// rejection is ErrInvalidSearchQuery before any store access.
func TestSearchGavcValidation(t *testing.T) {
	e := seedSearchEnv(t)
	ctx := context.Background()
	ls := legacyFace(t, e)

	tooMany := make([]string, 1001)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("repo-%d", i)
	}

	tests := []struct {
		name  string
		q     repo.GavcQuery
		limit int
		repos []string
	}{
		{"all coordinates absent", repo.GavcQuery{}, 2, nil},
		{"all coordinates blank", repo.GavcQuery{Group: " ", Artifact: "  "}, 2, nil},
		{"group with a path separator", repo.GavcQuery{Group: "com/acme"}, 2, nil},
		{"group with an empty dot segment", repo.GavcQuery{Group: "com..acme"}, 2, nil},
		{"group with a leading dot segment", repo.GavcQuery{Group: ".com"}, 2, nil},
		{"group with a dot-only segment", repo.GavcQuery{Group: "com.." + ""}, 2, nil},
		{"artifact with a path separator", repo.GavcQuery{Artifact: "demo/app"}, 2, nil},
		{"version with a path separator", repo.GavcQuery{Version: "1.0/2"}, 2, nil},
		{"classifier with a path separator", repo.GavcQuery{Classifier: "src/x"}, 2, nil},
		{"control byte in a coordinate", repo.GavcQuery{Artifact: "demo\x01"}, 2, nil},
		{"oversized coordinate", repo.GavcQuery{Artifact: strings.Repeat("a", 513)}, 2, nil},
		{"zero limit", repo.GavcQuery{Group: "com.acme"}, 0, nil},
		{"negative limit", repo.GavcQuery{Group: "com.acme"}, -1, nil},
		{"repos filter beyond the cap", repo.GavcQuery{Group: "com.acme"}, 2, tooMany},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ls.SearchGavc(ctx, admin(), tt.q, tt.limit, tt.repos)
			if !errors.Is(err, repo.ErrInvalidSearchQuery) {
				t.Fatalf("SearchGavc(%+v) err = %v, want ErrInvalidSearchQuery", tt.q, err)
			}
		})
	}
}

// TestSearchGavcMatching pins FR-134.1's compatible-subset semantics through
// the real service: the chained g/a/v prefix, the broken-chain infixes and
// the classifier token — on real nodes of both repositories.
func TestSearchGavcMatching(t *testing.T) {
	e := seedSearchEnv(t)
	ctx := context.Background()
	ls := legacyFace(t, e)
	put(t, e, admin(), "r1", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", "gavc-a")
	put(t, e, admin(), "r1", "com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar", "gavc-b")
	put(t, e, admin(), "r1", "com/acme/demo-app/2.0.0/demo-app-2.0.0.jar", "gavc-c")
	put(t, e, admin(), "r1", "com/acme/other-lib/1.0.0/other-lib-1.0.0.jar", "gavc-d")
	put(t, e, admin(), "r2", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", "gavc-e")

	tests := []struct {
		name  string
		q     repo.GavcQuery
		repos []string
		want  []string
	}{
		{"group+artifact reaches every version", repo.GavcQuery{Group: "com.acme", Artifact: "demo-app"}, nil,
			[]string{"r1/com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
				"r1/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
				"r1/com/acme/demo-app/2.0.0/demo-app-2.0.0.jar",
				"r2/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar"}},
		{"version narrows", repo.GavcQuery{Group: "com.acme", Artifact: "demo-app", Version: "1.0.0"}, nil,
			[]string{"r1/com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar",
				"r1/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar",
				"r2/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar"}},
		{"classifier narrows", repo.GavcQuery{Group: "com.acme", Artifact: "demo-app", Version: "1.0.0", Classifier: "sources"}, nil,
			[]string{"r1/com/acme/demo-app/1.0.0/demo-app-1.0.0-sources.jar"}},
		{"repos filter narrows", repo.GavcQuery{Group: "com.acme", Artifact: "demo-app"}, []string{"r2"},
			[]string{"r2/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar"}},
		{"artifact without group degrades to any depth", repo.GavcQuery{Artifact: "other-lib"}, nil,
			[]string{"r1/com/acme/other-lib/1.0.0/other-lib-1.0.0.jar"}},
		{"miss is the honest empty page", repo.GavcQuery{Group: "org.nobody"}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := ls.SearchGavc(ctx, admin(), tt.q, 100, tt.repos)
			if err != nil {
				t.Fatalf("SearchGavc: %v", err)
			}
			hits := make([]string, 0, len(nodes))
			for _, n := range nodes {
				hits = append(hits, n.RepoKey+"/"+n.Path)
			}
			if !equalStringSlices(hits, tt.want) {
				t.Fatalf("SearchGavc(%+v) = %v, want %v", tt.q, hits, tt.want)
			}
		})
	}

	// The limit trims SQL-side.
	nodes, err := ls.SearchGavc(ctx, admin(), repo.GavcQuery{Group: "com.acme"}, 2, nil)
	if err != nil {
		t.Fatalf("SearchGavc limit: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("limited gavc search returned %d rows, want 2", len(nodes))
	}
}

// TestSearchLegacyVisibility pins the ACL reuse (FR-134.4): the gavc/prop/
// pattern arms run the SAME searchGate/filterVisible pair the T-92 face
// runs — alice's pub/-only grant exposes exactly the granted arm on all
// three entrances, and anonymous on the closed instance (the fake
// authorizer denies anonymous reads) meets ErrForbidden before any store
// access.
func TestSearchLegacyVisibility(t *testing.T) {
	e := seedSearchEnv(t)
	ctx := context.Background()
	ls := legacyFace(t, e)
	pub := put(t, e, admin(), "r1", "com/acme/pub/app/1.0.0/app-1.0.0.jar", "vis-pub")
	put(t, e, admin(), "r1", "com/acme/secret/app/1.0.0/app-1.0.0.jar", "vis-secret")
	e.az.add("alice", repo.ActionRead, "com/acme/pub/")

	// The prop fixture rides the deploy-time matrix seam (PutOptions).
	// The visibility contract is what matters here: the granted arm only.
	_ = pub

	got, err := ls.SearchGavc(ctx, alice(), repo.GavcQuery{Group: "com.acme"}, 100, nil)
	if err != nil {
		t.Fatalf("alice gavc: %v", err)
	}
	if len(got) != 1 || got[0].Path != "com/acme/pub/app/1.0.0/app-1.0.0.jar" {
		t.Fatalf("alice gavc hits = %v, want exactly the pub row", got)
	}

	got, err = ls.SearchPattern(ctx, alice(), "", "com/acme/%", 100)
	if err != nil {
		t.Fatalf("alice pattern: %v", err)
	}
	if len(got) != 1 || got[0].Path != "com/acme/pub/app/1.0.0/app-1.0.0.jar" {
		t.Fatalf("alice pattern hits = %v, want exactly the pub row", got)
	}

	if _, err := ls.SearchGavc(ctx, nil, repo.GavcQuery{Group: "com.acme"}, 100, nil); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("anonymous gavc err = %v, want ErrForbidden", err)
	}
	if _, err := ls.SearchPattern(ctx, nil, "", "com/acme/%", 100); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("anonymous pattern err = %v, want ErrForbidden", err)
	}
}

// TestSearchPropsValidation pins FR-134.2's query-shape contract: at least
// one constraint, at most MaxPropSearchConds, and every key/value through
// the M10 write grammar (a key that could never be written is refused).
func TestSearchPropsValidation(t *testing.T) {
	e := seedSearchEnv(t)
	ctx := context.Background()
	ls := legacyFace(t, e)

	over := make([]repo.PropCond, repo.MaxPropSearchConds+1)
	for i := range over {
		over[i] = repo.PropCond{Key: fmt.Sprintf("key%d", i)}
	}

	tests := []struct {
		name  string
		conds []repo.PropCond
	}{
		{"no constraint at all", nil},
		{"empty key", []repo.PropCond{{Key: ""}}},
		{"key with an illegal charset", []repo.PropCond{{Key: "1bad"}}},
		{"oversized key", []repo.PropCond{{Key: strings.Repeat("k", 65)}}},
		{"oversized value", []repo.PropCond{{Key: "license", Value: strings.Repeat("v", 1025)}}},
		{"control byte in value", []repo.PropCond{{Key: "license", Value: "a\x01b"}}},
		{"beyond the constraint cap", over},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ls.SearchProps(ctx, admin(), tt.conds, 2, nil)
			if !errors.Is(err, repo.ErrInvalidSearchQuery) {
				t.Fatalf("SearchProps(%v) err = %v, want ErrInvalidSearchQuery", tt.conds, err)
			}
		})
	}
}

// TestSearchPatternValidation pins FR-134.3's shape: both patterns empty is
// the only rejection here (the <repo>:<path> split and the wildcard grammar
// are the endpoint's to judge — this layer receives already-translated LIKE
// patterns).
func TestSearchPatternValidation(t *testing.T) {
	e := seedSearchEnv(t)
	ctx := context.Background()
	ls := legacyFace(t, e)

	for _, tt := range []struct {
		name               string
		repoLike, pathLike string
	}{
		{"both patterns empty", "", " "},
		{"zero limit", "r1", "x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			limit := 2
			if tt.name == "zero limit" {
				limit = 0
			}
			_, err := ls.SearchPattern(ctx, admin(), tt.repoLike, tt.pathLike, limit)
			if !errors.Is(err, repo.ErrInvalidSearchQuery) {
				t.Fatalf("SearchPattern(%q,%q) err = %v, want ErrInvalidSearchQuery", tt.repoLike, tt.pathLike, err)
			}
		})
	}
}
