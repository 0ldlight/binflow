package generic

import (
	"context"
	"io"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Protocol is the package-type identifier this adapter serves.
const Protocol = "generic"

// BlobOpener opens an already-committed blob for re-streaming. It is the
// narrow seam checksum-deploy (X-Checksum-Deploy: true) needs: the service
// API takes a content reader, so an "upload without transfer" is expressed
// as re-feeding the existing blob through the ordinary Put pipeline. The
// adapter still never touches the filesystem or SQL — it holds the storage
// contract, which is what repo.Service itself holds (architecture section
// 5.1's adapter -> repo boundary; both are internal packages of the same
// binary and the dependency is storage's read-side only).
//
// Satisfied by storage.Engine.Open.
type BlobOpener func(ctx context.Context, sha256 string) (io.ReadSeekCloser, storage.BlobRef, error)

// Handler is the generic content-path adapter (architecture section 5.2).
type Handler struct {
	svc    repo.Service
	md     BlobLedger
	opener BlobOpener
	now    func() string
}

// BlobLedger is the read-only digest ledger the adapter consults for
// sha1/md5 (the ancillary digests of a node's blob). The physical fact
// source is metadata's blobs table (ADR-0006 keeps no sidecar files), so
// downloads and FileInfo need this lookup; satisfied by
// metadata.Store.Blobs().
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// compile-time interface check.
var _ BlobLedger = (metadata.BlobStore)(nil)

// New wires the handler. svc is required; md and opener are required for
// the full download/deploy surface (sha1/md5 headers, checksum deploy).
func New(svc repo.Service, md BlobLedger, opener BlobOpener) *Handler {
	return &Handler{svc: svc, md: md, opener: opener, now: func() string {
		return time.Now().UTC().Format(time.RFC3339)
	}}
}

// NewWithClock is New with an injected RFC3339 timestamp source (tests).
func NewWithClock(svc repo.Service, md BlobLedger, opener BlobOpener, now func() string) *Handler {
	return &Handler{svc: svc, md: md, opener: opener, now: now}
}
