package metadata_test

// T-256: Store.IsReferenced — the single-point Live oracle of ADR-0031
// mechanism A (architecture section 14.2 point 3). The contract under test:
// one bounded query over nodes ∪ docker_refs, answering "referenced RIGHT
// NOW", so the GC sweep's pre-delete recheck can close the stale-snapshot
// window (W-2). The GC-facing composition (GCMarker over Mark+Live) lives
// with the callers; here the store seam itself is pinned: every reference
// shape flips the answer, and deleting the last reference flips it back.

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestIsReferenced(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	putRepo(t, st, "live-r")
	sha, size := fakeBlob(9001)
	otherSha, _ := fakeBlob(9002)

	nodeRef := &metadata.Node{
		RepoKey: "live-r", Path: "a/b.bin", Sha256: sha, Size: size,
		Mime: "application/octet-stream", CreatedAt: now, UpdatedAt: now,
	}

	// One docker manifest whose ref ledger points at otherSha and nothing
	// else — the reference shape no node row can express.
	dgst := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := st.Docker().PutManifest(ctx, &metadata.DockerManifest{
		RepoKey: "live-r", Image: "app", Digest: dgst, Size: 1, CreatedAt: now,
	}); err != nil {
		t.Fatalf("put manifest: %v", err)
	}
	if err := st.Docker().PutRefs(ctx, "live-r", "app", dgst, []*metadata.DockerRef{{
		RepoKey: "live-r", Image: "app", ManifestDigest: dgst, BlobDigest: otherSha,
	}}); err != nil {
		t.Fatalf("put refs: %v", err)
	}

	cases := []struct {
		name string
		sha  string
		want bool
	}{
		{"unknown sha", fakeBlobHex(t, 9999), false},
		{"docker-ref-only sha is referenced (the union half)", otherSha, true},
	}
	for _, tc := range cases {
		if got, err := st.IsReferenced(ctx, tc.sha); err != nil || got != tc.want {
			t.Fatalf("%s: IsReferenced = (%v, %v), want (%v, nil)", tc.name, got, err, tc.want)
		}
	}

	// The node half, plus the "last reference removed" flip the delete gate
	// depends on: a sha that stops being referenced must answer false on the
	// very next call (no caching, no snapshot semantics). The ledger row
	// lands first (blob-first, the nodes.sha256 FK).
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
		t.Fatalf("blobs put: %v", err)
	}
	if err := st.Nodes().Put(ctx, nodeRef); err != nil {
		t.Fatalf("node put: %v", err)
	}
	if got, err := st.IsReferenced(ctx, sha); err != nil || !got {
		t.Fatalf("node-referenced sha: IsReferenced = (%v, %v), want (true, nil)", got, err)
	}
	if err := st.Nodes().Delete(ctx, "live-r", "a/b.bin"); err != nil {
		t.Fatalf("node delete: %v", err)
	}
	if got, err := st.IsReferenced(ctx, sha); err != nil || got {
		t.Fatalf("after the last node reference is gone: IsReferenced = (%v, %v), want (false, nil)", got, err)
	}

	// The docker half flips the same way (the sweep deletes ref rows with
	// their manifest in one transaction).
	if err := st.Docker().DeleteManifest(ctx, "live-r", "app", dgst); err != nil {
		t.Fatalf("delete manifest: %v", err)
	}
	if got, err := st.IsReferenced(ctx, otherSha); err != nil || got {
		t.Fatalf("after the manifest cascade removed the refs: IsReferenced = (%v, %v), want (false, nil)", got, err)
	}
}

// fakeBlobHex returns just the hex sha of fakeBlob(i) (the size is irrelevant
// to the existence probe).
func fakeBlobHex(t *testing.T, i int) string {
	t.Helper()
	sha, _ := fakeBlob(i)
	return sha
}
