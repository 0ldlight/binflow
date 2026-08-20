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
