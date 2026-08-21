package storage

import (
	"context"
	"io"
)

// Backend is the pure blob CRUD surface — no sessions, no GC, no locks.
// It is a package-internal interface (ADR-0019): the Engine interface is the
// public face of the storage package; Backend is the implementation detail
// that lets DiskEngine and the future S3Engine (T-151) share the same blob
// path shape, digest validation and checksum semantics without duplicating
// the session/GC/lock machinery.
//
// Implementations must be safe for concurrent use.
type Backend interface {
	// Put stores a blob under sha256. The caller streams the bytes through r
	// with a known size; the implementation writes them to the content-addressed
	// path, verifies the streamed digest matches sha256 and returns the
	// committed BlobRef. An existing blob with the same sha256 is idempotent
	// (deduplication): the call succeeds and returns the existing BlobRef.
	Put(ctx context.Context, sha256 string, r io.Reader, size int64) (BlobRef, error)

	// Get opens a blob for reading. The caller must Close the returned reader.
	// Missing blobs yield ErrBlobNotFound wrapped.
	Get(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error)

	// Delete physically removes a blob. Callers must coordinate with GC — the
	// backend does not reference-count. Missing blobs yield ErrBlobNotFound
	// wrapped.
	Delete(ctx context.Context, sha256 string) error

	// Exists probes whether a blob is present on disk (or equivalent storage).
	// It is a cheap existence check — no digest verification.
	Exists(ctx context.Context, sha256 string) (bool, error)

	// List returns the sha256 values of every blob in the store. The result is
	// a snapshot; blobs added or deleted concurrently may or may not appear.
	// The caller must not assume the returned sha256 list is comprehensive.
	List(ctx context.Context) ([]string, error)
}
