package generic

import (
	"context"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Protocol is the package-type identifier this adapter serves.
const Protocol = "generic"

// Handler is the generic content-path adapter (architecture section 5.2).
type Handler struct {
	svc repo.Service
	md  BlobLedger
	now func() string
}

// BlobLedger is the read-only digest ledger the adapter consults for
// sha1/md5 (the ancillary digests of a node's blob). The physical fact
// source is metadata's blobs table (ADR-0006 keeps no sidecar files), so
// downloads and FileInfo need this lookup; satisfied by
// metadata.Store.Blobs().
//
// Dependency-direction note (T-13 review B1): this is a READ-only digest
// lookup on a type that already flows through repo.Service's signatures
// (*metadata.Node carries the same data); the functional path to storage —
// checksum deploy — went back behind repo.Service.PutFromBlob, so the
// adapter holds no storage engine seam at all.
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// New wires the handler. svc is required; md serves the sha1/md5 download
// headers and FileInfo digests.
func New(svc repo.Service, md BlobLedger) *Handler {
	return &Handler{svc: svc, md: md, now: func() string {
		return time.Now().UTC().Format(time.RFC3339)
	}}
}

// NewWithClock is New with an injected RFC3339 timestamp source (tests).
func NewWithClock(svc repo.Service, md BlobLedger, now func() string) *Handler {
	return &Handler{svc: svc, md: md, now: now}
}
