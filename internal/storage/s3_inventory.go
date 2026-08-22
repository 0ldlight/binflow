package storage

// Read-only blob inventory seam over the S3 engine (T-201, T-173 D-1/D-2).
//
// This file deliberately lives apart from s3.go: the session/upload paths in
// that file are T-202's in-flight area. The seam here only LISTENS to the
// bucket — one ListObjectsV2 walk returning every blob's stored size and
// LastModified — so consumers (the /api/v1/storage/stats endpoint, the
// /metrics storage gauges, the GC candidate sizing and the export/import
// CLI faces) can size an S3-backed instance the way the disk faces walk
// blobs/. No upload, session or mutation path is touched.

import (
	"context"
	"fmt"
	"time"

	"github.com/minio/minio-go/v7"
)

// BlobStat is one stored blob's inventory entry: its physical size and the
// object's LastModified (the S3 stand-in for the disk blob's mtime — the
// GC grace clock's basis, ADR-0006 erratum 2).
type BlobStat struct {
	Size         int64
	LastModified time.Time
}

// BlobStats lists every blob object in the bucket with its size and
// LastModified. It is a read-only snapshot: objects added or deleted
// concurrently may or may not appear. Keys that do not match the blob key
// shape (<prefix>/blobs/<xx>/<sha256>) are skipped — the same filter the GC
// sweep applies, so foreign objects under other prefixes never inflate the
// inventory.
func (e *S3Engine) BlobStats(ctx context.Context) (map[string]BlobStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: s3: blob stats: %w", err)
	}
	stats := make(map[string]BlobStat)
	objCh := e.api().ListObjects(ctx, e.bucket, minio.ListObjectsOptions{
		Prefix:    e.objectKeyPrefix(),
		Recursive: true,
	})
	for obj := range objCh {
		if obj.Err != nil {
			return nil, fmt.Errorf("storage: s3: blob stats: list objects: %w", obj.Err)
		}
		sha := extractSha256FromKey(obj.Key)
		if sha == "" {
			continue
		}
		stats[sha] = BlobStat{Size: obj.Size, LastModified: obj.LastModified}
	}
	return stats, nil
}
