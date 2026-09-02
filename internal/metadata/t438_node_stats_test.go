package metadata_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// T-438 (FR-146.2 / ADR-0044 K69): the nodes table's four counting columns —
// the store-level contract behind the download plane's single counting
// channel. Every case runs on the real sqlite store, i.e. on migration 020's
// as-landed schema.

// seedFileNode lands one file node row (repo + blob + node) and returns its
// coordinates.
func seedFileNode(t *testing.T, st metadata.Store, repoKey, path string) {
	t.Helper()
	ctx := context.Background()
	putRepo(t, st, repoKey)
	sha, size := fakeBlob(len(path))
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: metadata.Now()}); err != nil {
		t.Fatalf("blob put: %v", err)
	}
	now := metadata.Now()
	if err := st.Nodes().Put(ctx, &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: sha, Size: size,
		CreatedBy: "seeder", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("node put: %v", err)
	}
}

// seedFolderNode lands one folder row (the shared marker blob, migration
// 007's shape).
func seedFolderNode(t *testing.T, st metadata.Store, repoKey, path string) {
	t.Helper()
	ctx := context.Background()
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: metadata.FolderMarkerSHA, Size: 0, CreatedAt: metadata.Now()}); err != nil {
		t.Fatalf("marker blob put: %v", err)
	}
	now := metadata.Now()
	if err := st.Nodes().Put(ctx, &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: metadata.FolderMarkerSHA, Size: 0,
		CreatedBy: "seeder", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("folder put: %v", err)
	}
}

// TestT438CountDownloadArms pins the store-side counting contract: the
// self-increment, the stamping, the remote delta and the two structural
// guards (folder rows excluded, missing rows silent).
func TestT438CountDownloadArms(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	seedFileNode(t, st, "loc", "a/b.bin")

	direct := []struct {
		name        string
		remoteDelta bool
	}{
		{"local serving row bumps download_count only", false},
		{"remote serving row bumps both counters", true},
	}
	for _, tc := range direct {
		t.Run(tc.name, func(t *testing.T) {
			st := open(t)
			seedFileNode(t, st, "r", "x.bin")
			if err := st.Nodes().CountDownload(ctx, "r", "x.bin", "alice", "2026-09-03T10:00:00Z", tc.remoteDelta); err != nil {
				t.Fatalf("CountDownload: %v", err)
			}
			got, err := st.Nodes().Stats(ctx, "r", "x.bin")
			if err != nil {
				t.Fatalf("Stats: %v", err)
			}
			if got.DownloadCount != 1 {
				t.Fatalf("download_count = %d, want 1", got.DownloadCount)
			}
			wantRemote := int64(0)
			if tc.remoteDelta {
				wantRemote = 1
			}
			if got.RemoteDownloadCount != wantRemote {
				t.Fatalf("remote_download_count = %d, want %d", got.RemoteDownloadCount, wantRemote)
			}
			if got.LastDownloadedAt != "2026-09-03T10:00:00Z" {
				t.Fatalf("last_downloaded_at = %q", got.LastDownloadedAt)
			}
			if got.LastDownloadedBy != "alice" {
				t.Fatalf("last_downloaded_by = %q", got.LastDownloadedBy)
			}
			// A second landing refreshes the stamps and the counts: the
			// SQL self-increment is the accumulation contract.
			if err := st.Nodes().CountDownload(ctx, "r", "x.bin", "anonymous", "2026-09-03T11:00:00Z", tc.remoteDelta); err != nil {
				t.Fatalf("CountDownload #2: %v", err)
			}
			got, err = st.Nodes().Stats(ctx, "r", "x.bin")
			if err != nil {
				t.Fatalf("Stats #2: %v", err)
			}
			if got.DownloadCount != 2 || got.RemoteDownloadCount != wantRemote*2 {
				t.Fatalf("counts = %d/%d, want 2/%d", got.DownloadCount, got.RemoteDownloadCount, wantRemote*2)
			}
			if got.LastDownloadedBy != "anonymous" {
				t.Fatalf("last_downloaded_by = %q, want the latest downloader", got.LastDownloadedBy)
			}
		})
	}

	t.Run("folder rows stay at their structural zero", func(t *testing.T) {
		seedFolderNode(t, st, "loc", "a/")
		if err := st.Nodes().CountDownload(ctx, "loc", "a/", "alice", "2026-09-03T10:00:00Z", true); err != nil {
			t.Fatalf("CountDownload on folder: %v", err)
		}
		got, err := st.Nodes().Stats(ctx, "loc", "a/")
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		if got.DownloadCount != 0 || got.RemoteDownloadCount != 0 || got.LastDownloadedAt != "" || got.LastDownloadedBy != "" {
			t.Fatalf("folder row counted: %+v", got)
		}
	})

	t.Run("missing row is a silent no-op", func(t *testing.T) {
		if err := st.Nodes().CountDownload(ctx, "loc", "gone.bin", "alice", "2026-09-03T10:00:00Z", false); err != nil {
			t.Fatalf("CountDownload on missing row = %v, want nil", err)
		}
		if _, err := st.Nodes().Stats(ctx, "loc", "gone.bin"); err == nil {
			t.Fatal("Stats on missing row must fail")
		}
	})
}

// TestT438CountDownloadConcurrent pins the atomicity claim under the race
// detector: N goroutines each land one download; the row must end at
// exactly N with no lost increment (SQLite single writer + the SQL-side
// self-increment).
func TestT438CountDownloadConcurrent(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	seedFileNode(t, st, "loc", "c.bin")

	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = st.Nodes().CountDownload(ctx, "loc", "c.bin", "u", "2026-09-03T10:00:00Z", i%2 == 0)
		}(i)
	}
	wg.Wait()

	got, err := st.Nodes().Stats(ctx, "loc", "c.bin")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if got.DownloadCount != n {
		t.Fatalf("download_count = %d, want %d (lost increments)", got.DownloadCount, n)
	}
	if got.RemoteDownloadCount != n/2 {
		t.Fatalf("remote_download_count = %d, want %d", got.RemoteDownloadCount, n/2)
	}
}

// TestT438Migration020IdempotentReopen pins the startup-migration
// idempotence on a database that already carries the counting columns: the
// version ledger skips the applied ALTER family and a reopen answers the
// same zero-valued statistics (ADR-0007's reopen-skips-applied).
func TestT438Migration020IdempotentReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binflow.db")
	ctx := context.Background()

	st, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("open #1: %v", err)
	}
	seedFileNode(t, st, "loc", "a.bin")
	if err := st.Nodes().CountDownload(ctx, "loc", "a.bin", "alice", "2026-09-03T10:00:00Z", false); err != nil {
		t.Fatalf("CountDownload: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close #1: %v", err)
	}

	st2, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("reopen over the applied 020: %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	got, err := st2.Nodes().Stats(ctx, "loc", "a.bin")
	if err != nil {
		t.Fatalf("Stats after reopen: %v", err)
	}
	if got.DownloadCount != 1 || got.LastDownloadedBy != "alice" {
		t.Fatalf("stats after reopen = %+v", got)
	}
	// A fresh node on the reopened store starts at the zero defaults.
	seedFileNode(t, st2, "loc2", "fresh.bin")
	fresh, err := st2.Nodes().Stats(ctx, "loc2", "fresh.bin")
	if err != nil {
		t.Fatalf("Stats fresh: %v", err)
	}
	if fresh.DownloadCount != 0 || fresh.LastDownloadedAt != "" || fresh.RemoteDownloadCount != 0 {
		t.Fatalf("fresh row = %+v, want the zero defaults", fresh)
	}
}
