package metadata

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// TestBlobGetBySha1: the sha1-keyed ledger lookup (idx_blobs_sha1, T-73's
// checksum-deploy seam) — hit resolves the full row, miss answers ErrNotFound,
// and the empty string never matches anything (the folder-marker row carries
// an empty sha1; a caller that failed its both-empty guard must not resolve
// the marker).
func TestBlobGetBySha1(t *testing.T) {
	ctx := context.Background()
	md, err := Open(ctx, Options{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "binflow.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	seed := []*Blob{
		{Sha256: "aa00000000000000000000000000000000000000000000000000000000000000",
			Sha1: "bb00000000000000000000000000000000000000", Md5: "c0000000000000000000000000000000", Size: 3,
			CreatedAt: "2026-08-20T00:00:00Z"},
		{Sha256: emptyFolderMarkerSHAForTest(),
			Sha1: "", Size: 0, CreatedAt: "2026-08-20T00:00:00Z"},
	}
	for _, b := range seed {
		if err := md.Blobs().Put(ctx, b); err != nil {
			t.Fatalf("seed %s: %v", b.Sha256, err)
		}
	}

	got, err := md.Blobs().GetBySha1(ctx, seed[0].Sha1)
	if err != nil {
		t.Fatalf("GetBySha1(hit): %v", err)
	}
	if got.Sha256 != seed[0].Sha256 || got.Sha1 != seed[0].Sha1 || got.Md5 != seed[0].Md5 || got.Size != 3 {
		t.Fatalf("resolved row = %+v, want %+v", got, seed[0])
	}

	if _, err := md.Blobs().GetBySha1(ctx, "ff00000000000000000000000000000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBySha1(miss) error = %v, want ErrNotFound", err)
	}
	if _, err := md.Blobs().GetBySha1(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBySha1(empty) error = %v, want ErrNotFound (the marker row must not resolve)", err)
	}
}

// emptyFolderMarkerSHAForTest is the shared folder-marker sentinel's digest
// spelling (the repo layer owns the constant; here only uniqueness of a row
// with an EMPTY sha1 matters).
func emptyFolderMarkerSHAForTest() string {
	return "0000000000000000000000000000000000000000000000000000000000000000"
}
