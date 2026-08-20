package metadata_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// T-95/GE-05: the repo_usage accounting seam — same-transaction node+counter
// writes, delta semantics (fresh, overwrite, idempotent, delete), the
// not-found contract. The 005 backfill migration's own test lives in
// usage_internal_test.go (it needs the raw database handle to rewind the
// migration ledger, which the public Store interface does not expose).

func newUsageStore(t *testing.T) (metadata.UsageStore, metadata.NodeStore, metadata.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", Path: filepath.Join(t.TempDir(), "binflow.db"), AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "meter", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: "2026-08-19T00:00:00Z", UpdatedAt: "2026-08-19T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	return st.Usage(), st.Nodes(), st
}

func TestUsageStoreDeltaMatrix(t *testing.T) {
	ctx := context.Background()
	usage, nodes, st := newUsageStore(t)
	now := "2026-08-19T01:00:00Z"

	// The blobs-ledger rows the nodes.sha256 FK demands (blob-first, the
	// standing write order the service layer upholds).
	for _, sha := range []string{"aa", "bb", "cc", "dd", "00"} {
		if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, CreatedAt: now}); err != nil {
			t.Fatalf("seed blob %s: %v", sha, err)
		}
	}

	// No row yet: zero total, never an error.
	if u, err := usage.Get(ctx, "meter"); err != nil || u.LogicalBytes != 0 || u.RepoKey != "meter" {
		t.Fatalf("fresh usage = %+v (%v), want zero", u, err)
	}

	steps := []struct {
		name string
		node *metadata.Node
		want int64
	}{
		{"fresh node", &metadata.Node{RepoKey: "meter", Path: "a.bin", Sha256: "aa", Size: 10, CreatedAt: now, UpdatedAt: now}, 10},
		{"second node", &metadata.Node{RepoKey: "meter", Path: "b.bin", Sha256: "bb", Size: 20, CreatedAt: now, UpdatedAt: now}, 30},
		{"overwrite larger", &metadata.Node{RepoKey: "meter", Path: "a.bin", Sha256: "cc", Size: 25, CreatedAt: now, UpdatedAt: now}, 45},
		{"overwrite smaller", &metadata.Node{RepoKey: "meter", Path: "a.bin", Sha256: "dd", Size: 5, CreatedAt: now, UpdatedAt: now}, 25},
		{"idempotent same content", &metadata.Node{RepoKey: "meter", Path: "a.bin", Sha256: "dd", Size: 5, CreatedAt: now, UpdatedAt: now}, 25},
		{"folder marker is zero", &metadata.Node{RepoKey: "meter", Path: "d/", Sha256: "00", Size: 0, CreatedAt: now, UpdatedAt: now}, 25},
	}
	for _, tt := range steps {
		if err := usage.PutNodeWithUsage(ctx, tt.node, now); err != nil {
			t.Fatalf("%s: PutNodeWithUsage: %v", tt.name, err)
		}
		if u, err := usage.Get(ctx, "meter"); err != nil || u.LogicalBytes != tt.want {
			t.Fatalf("%s: usage = %d (%v), want %d", tt.name, u.LogicalBytes, err, tt.want)
		}
	}

	// The node rows themselves are the plain store's exact upserts.
	if n, err := nodes.Get(ctx, "meter", "a.bin"); err != nil || n.Size != 5 || n.Sha256 != "dd" {
		t.Fatalf("node after matrix = %+v (%v)", n, err)
	}

	// Deletes subtract; an absent row keeps NodeStore.Delete's contract.
	if err := usage.DeleteNodeWithUsage(ctx, "meter", "b.bin", now); err != nil {
		t.Fatalf("delete b.bin: %v", err)
	}
	if u, _ := usage.Get(ctx, "meter"); u.LogicalBytes != 5 {
		t.Fatalf("usage after delete = %d, want 5", u.LogicalBytes)
	}
	if err := usage.DeleteNodeWithUsage(ctx, "meter", "nope.bin", now); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("absent delete error = %v, want ErrNodeNotFound", err)
	}
	if u, _ := usage.Get(ctx, "meter"); u.LogicalBytes != 5 {
		t.Fatalf("usage after absent delete = %d, want 5 (unchanged)", u.LogicalBytes)
	}
}
