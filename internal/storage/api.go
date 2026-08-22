package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// Sentinel errors. Callers must match with errors.Is; implementations wrap
// them with context (architecture section 3.1).
var (
	// ErrBlobNotFound is returned (wrapped) by Open, Stat and Delete when the
	// requested sha256 is not present in the blob store.
	ErrBlobNotFound = errors.New("blob not found")
	// ErrSessionNotFound is returned (wrapped) by ResumeSession when no
	// persisted session row exists for the id (a surviving row whose data
	// file vanished instead resumes from offset 0). Callers should restart
	// the upload from zero.
	ErrSessionNotFound = errors.New("session not found")
	// ErrChecksumMismatch is returned (wrapped) by Commit when an expected
	// digest supplied by the caller does not match the streamed content. No
	// blob is written in that case.
	ErrChecksumMismatch = errors.New("checksum mismatch")
	// ErrBlobCorrupt is returned (wrapped) by Stat when the content of a blob
	// does not hash to its own path; the file must be treated as damaged.
	ErrBlobCorrupt = errors.New("blob corrupt")
	// ErrEngineClosed is returned by the mutating operations (BeginSession,
	// Delete, GC) after Close. The read paths (Open, Stat) keep serving
	// already-committed blobs: they touch only immutable files.
	ErrEngineClosed = errors.New("storage engine closed")
	// ErrSessionPoisoned is reported by Append and Commit after an earlier
	// Append failed: the session can no longer produce a trustworthy blob
	// and must be discarded with Abort (engine Close and the startup sweep
	// are the backstops). The wrapped chain carries the original cause.
	ErrSessionPoisoned = errors.New("session poisoned by a failed append")
)

// BlobRef identifies a committed blob. Sha256 is the global primary key;
// Sha1 and Md5 are ancillary digests kept for protocol compatibility
// (Docker/Maven/PyPI clients verify against them).
type BlobRef struct {
	Sha256 string // hex, lowercase, 64 chars, primary key
	Sha1   string // hex, lowercase, 40 chars
	Md5    string // hex, lowercase, 32 chars
	Size   int64
}

// Session is one upload. Data is streamed to <data>/uploads/<id>/data while
// sha256+sha1+md5 are computed incrementally, and the session row is kept in
// the metadata store's upload_sessions table (T-209). Implementations must be
// safe for concurrent use of distinct sessions; a single session is serial.
type Session interface {
	ID() string
	// Append streams r into the session and returns the cumulative offset.
	// If Append fails for any reason the session is poisoned: every
	// subsequent Append or Commit fails with an error wrapping
	// ErrSessionPoisoned (plus the original cause), and Abort discards the
	// remains. A partially-written session file must never be renamed into
	// the blob store under a digest it does not match.
	Append(ctx context.Context, r io.Reader) (written int64, err error)
	// Commit finalizes the session: non-empty expected digests in expect must
	// match the streamed content, then the data file is fsynced and renamed
	// into the blob store (write -> fsync(data) -> rename -> fsync(dir),
	// ADR-0006; the order is not negotiable). If the target blob already
	// exists the session data is discarded and the existing blob is returned
	// (idempotent deduplication).
	Commit(ctx context.Context, expect BlobRef) (BlobRef, error)
	// Abort discards the session. Safe on a nil receiver and idempotent.
	Abort(ctx context.Context) error
}

// Engine is the checksum-addressed blob engine (architecture section 3.1).
//
// GC deviation from architecture section 3.1: the referenced callback takes
// the full referenced set (map) instead of being invoked per checksum. The
// ticket T-9 contract specifies the set form, which also avoids one query
// per blob; recorded for architect write-back.
type Engine interface {
	// BeginSession creates a new upload session; directories are created as
	// needed.
	BeginSession(ctx context.Context) (Session, error)
	// ResumeSession re-materializes an in-progress session from its persisted
	// row and on-disk data file: the partial bytes are re-hashed to rebuild
	// the digest chain and the session is returned ready for further Append.
	// A missing row yields ErrSessionNotFound; a missing data file is
	// recreated empty and the session resumes from offset 0. Requires a
	// Sessions store in Options; without one this always yields
	// ErrSessionNotFound.
	ResumeSession(ctx context.Context, id string) (Session, error)
	// Open opens a blob for reading; the caller must Close it. Missing blobs
	// yield ErrBlobNotFound wrapped. The returned BlobRef carries Sha256 and
	// Size; ancillary digests are the metadata store's source of truth, use
	// Stat for a full digest pass.
	//
	// The returned reader is an io.ReadCloser — the minimum contract every
	// backend must satisfy. The DiskEngine returns a concrete *os.File, which
	// also implements io.ReadSeekCloser; callers that need Seek (e.g. HTTP
	// Range requests) may type-assert to io.ReadSeekCloser. Other backends
	// (S3, memory) may only honor io.ReadCloser. ADR-0019.
	Open(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error)
	// Stat verifies a blob end to end: it streams the content once, returns
	// all three digests and fails with ErrBlobCorrupt when the content does
	// not hash to its own path. The pass is O(size); it is the consistency
	// check for GC and repairs, not a cheap existence probe.
	Stat(ctx context.Context, sha256 string) (BlobRef, error)
	// Delete physically removes a blob. Only GC may call it; runtime artifact
	// deletion removes metadata references instead. Returns an error wrapping
	// ErrEngineClosed after Close.
	Delete(ctx context.Context, sha256 string) error
	// GC is mark-sweep: referenced returns the set of sha256 values still
	// pointed at by nodes; blobs that are unreferenced and older than grace
	// are deletion candidates. With apply=false nothing is deleted and only
	// the candidate list is returned (dry-run is the default posture).
	// grace <= 0 means DefaultGCGrace (24h) — a zero grace is NOT an
	// immediate-collect request; callers wanting no grace must pass a
	// sub-second duration explicitly. Returns an error wrapping
	// ErrEngineClosed after Close.
	GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error)
	// Close shuts the engine down; further sessions are refused.
	Close() error
}
