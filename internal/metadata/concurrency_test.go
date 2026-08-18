package metadata_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Concurrent readers plus writers must not trip SQLITE_BUSY or the race
// detector: WAL + busy_timeout (BusyTimeoutMs) + a one-connection pool
// serialize access.
func TestConcurrentMixedWorkload(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putRepo(t, st, "conc")
	now := metadata.Now()

	const writers = 4
	const perWriter = 15
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				sha, size := fakeBlob(w*1000 + i)
				if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
					errs <- fmt.Errorf("blob put w%d i%d: %w", w, i, err)
					return
				}
				path := fmt.Sprintf("w%d/i%03d.bin", w, i)
				err := st.Nodes().Put(ctx, &metadata.Node{
					RepoKey: "conc", Path: path, Sha256: sha, Size: size, CreatedBy: "t",
					CreatedAt: now, UpdatedAt: now,
				})
				if err != nil {
					errs <- fmt.Errorf("node put %s: %w", path, err)
					return
				}
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if _, err := st.Nodes().ListByPrefix(ctx, "conc", ""); err != nil {
					errs <- fmt.Errorf("list: %w", err)
					return
				}
				if err := st.Ping(ctx); err != nil {
					errs <- fmt.Errorf("ping: %w", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	nodes, err := st.Nodes().ListByPrefix(ctx, "conc", "")
	if err != nil {
		t.Fatalf("final list: %v", err)
	}
	if len(nodes) != writers*perWriter {
		t.Fatalf("nodes = %d, want %d", len(nodes), writers*perWriter)
	}
}

// FilterUnreferenced streams pages without loading the whole table: reading
// a table larger than one page must terminate and return every row.
func TestFilterUnreferencedPagesThroughLargeTable(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	const total = 120
	for i := 0; i < total; i++ {
		sha, size := fakeBlob(i + 500)
		if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
			t.Fatalf("blob put: %v", err)
		}
	}
	var got int
	if err := st.Blobs().FilterUnreferenced(ctx, 7, func(string) error { got++; return nil }); err != nil {
		t.Fatalf("FilterUnreferenced: %v", err)
	}
	if got != total {
		t.Fatalf("streamed %d rows, want %d", got, total)
	}
}
