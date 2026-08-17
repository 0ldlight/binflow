package metadata_test

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Regression guard: pages must be keyset-based (created_at, sha256), not
// OFFSET-based — the GC callback deletes rows between pages and OFFSET would
// skip every pageSize-th blob.
func TestFilterUnreferencedWhileDeleting(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()
	const total = 50
	for i := 0; i < total; i++ {
		sha, size := fakeBlob(i + 900)
		if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
			t.Fatalf("blob put: %v", err)
		}
	}
	var seen int
	err := st.Blobs().FilterUnreferenced(ctx, 10, func(sha string) error {
		seen++
		return st.Blobs().Delete(ctx, sha)
	})
	if err != nil {
		t.Fatalf("FilterUnreferenced while deleting: %v", err)
	}
	if seen != total {
		t.Fatalf("streamed %d rows while deleting, want %d (keyset pagination must not skip)", seen, total)
	}
	n, err := st.Blobs().Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("blobs remaining = %d, want 0", n)
	}
}
