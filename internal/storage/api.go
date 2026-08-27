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
	// persisted session row exists for the id, or when the row is already
	// expired but not yet swept (expired = unknown, architecture section
	// 5.3.1 contract 3: the sweep is a row's only legitimate reclamation
	// path, so a resume must fail closed rather than race it). A surviving
	// unexpired row whose data file vanished instead resumes from offset 0.
	// Callers should restart the upload from zero.
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
	// and must be discarded with Abort (the startup sweep is the backstop —
	// engine Close no longer deletes sessions, ADR-0028). The wrapped chain
	// carries the original cause.
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
// safe for concurrent use of distinct sessions; a single session is serial
// for its mutating methods — Offset is the one reader that must stay safe to
// call while another goroutine holds the session in Append/Commit
// (architecture section 3.1 [M7]).
type Session interface {
	ID() string
	// Offset returns the cumulative bytes the session has received so far.
	// For the disk backend it is the uploads/<id>/data file length: after a
	// ResumeSession re-hash recovery it is the re-derived file length, which
	// makes it the authoritative offset source for REST resume (architecture
	// sections 3.1 [M7] and 5.3.1 contract 1) — callers must treat this
	// value, not a cached mirror, as the basis for Content-Range alignment
	// checks and 416/Range responses. Reads serialize against Append/Commit.
	Offset() int64
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

// MultipartUploads is the optional capability an engine may carry for the
// /api/v1/uploads REST plane (T-289, FR-90.1 / architecture section 15.4):
// beginning an upload session with an EXPLICIT per-session part size, the
// knob the REST create/config verbs expose. The S3 engine implements it
// (multipart is its native session form); the disk engine deliberately does
// not — a filestore instance discovers no seam and the REST plane answers
// its honest 501 (FR-90-AC3: no inert face). This is a capability DISCOVERY
// type, not a second session API: the returned Session is the same
// interface every other upload path drives (Append/Commit/Abort), and
// ResumeSession semantics are the Engine's own — the S3 arm's restart
// resume landed with T-323 (upload id rows + ListParts rebuild; see
// s3_resume.go), so both backends now resume wherever a Sessions store is
// wired.
type MultipartUploads interface {
	// BeginMultipartSession creates a new upload session whose Append
	// flushes a part every partSize bytes. partSize <= 0 takes the engine
	// default; values below the S3 multipart minimum (MinS3PartSize) clamp
	// up — the resolved value is observable through the REST plane's echo
	// of the effective size, never silently guessed at complete time.
	BeginMultipartSession(ctx context.Context, partSize int64) (Session, error)
}

// SessionSweeper is the optional capability an engine may carry to rerun
// its open-time expired-session reclamation on demand (T-324): the
// unused-cleanup engine's periodic driver invokes it so a long-running
// serve process reclaims expired upload-session rows and their temp files
// without waiting for a restart (the startup sweep + TTL remained the only
// reclamation path through M10 — architecture section 5.3.1 contract 7's
// "startup sweep" wording widens from "at Open" to "at Open and on the
// maintenance clock", the same reclamation, no new caller). Both real
// engines implement it; test fakes need not — the capability is discovered
// by type assertion and its absence simply skips the leg.
type SessionSweeper interface {
	// SweepExpiredSessions reclaims expired upload sessions (rows plus
	// backend state: uploads/<id>/ dirs on disk, orphaned multipart uploads
	// on S3) and returns the number of session ROWS removed. Live sessions
	// of this process are never reclaimed.
	SweepExpiredSessions(ctx context.Context) (int, error)
}

// GCMarker is the two-method reference oracle behind Engine.GCSweep
// ([M9] ADR-0031, architecture section 14.2 point 3). Mark is the sweep's
// snapshot; Live is the per-candidate, pre-delete recheck that closes the
// stale-snapshot window (W-2).
//
// Contract for implementors:
//
//   - Mark must return every sha256 currently referenced by metadata
//     (nodes ∪ docker_refs). It may be invoked several times per sweep —
//     once for the candidacy snapshot and, for markers without a real
//     single-point Live, once more as the apply-phase refresh.
//   - Live must answer "is this sha referenced RIGHT NOW" with a bounded
//     single-point query (metadata's indexed existence probe), NOT a full
//     set rebuild. The pre-delete gate relies on Live reflecting every
//     reference committed before the call: it is the freshness boundary of
//     the whole soundness argument (see Engine.GCSweep).
//
// ReferencedFunc adapts the legacy single-snapshot callback to this
// interface; engines detect it and substitute one apply-phase re-snapshot
// for its Live (see gcRecheck).
type GCMarker interface {
	// Mark returns the snapshot of all referenced sha256 values.
	Mark() (map[string]struct{}, error)
	// Live reports whether sha256 is referenced as of this call.
	Live(sha256 string) (bool, error)
}

// ReferencedFunc adapts the pre-M9 single-snapshot GC callback to GCMarker.
// Its Live re-runs the callback and checks membership: correct standalone,
// but O(referenced-set) per call — callers routing through Engine.GCSweep
// never pay that, because the engine recognizes the legacy form and serves
// its delete gate from one refreshed snapshot per apply pass instead.
type ReferencedFunc func() (map[string]struct{}, error)

// Mark implements GCMarker.
func (f ReferencedFunc) Mark() (map[string]struct{}, error) { return f() }

// Live implements GCMarker by re-snapshoting (see the type doc).
func (f ReferencedFunc) Live(sha256 string) (bool, error) {
	set, err := f()
	if err != nil {
		return false, err
	}
	_, ok := set[sha256]
	return ok, nil
}

// Engine is the checksum-addressed blob engine (architecture section 3.1).
//
// GC deviation from architecture section 3.1: the referenced callback takes
// the full referenced set (map) instead of being invoked per checksum. The
// ticket T-9 contract specifies the set form, which also avoids one query
// per blob; recorded for architect write-back.
//
// [M9] ADR-0031: the two-method form of that callback is GCMarker, carried
// by GCSweep; the original GC signature stays for the M4~M8 callers (CLI,
// REST seam, tests) and delegates to GCSweep via ReferencedFunc, so every
// caller gets the hold-set and delete-recheck protection regardless of
// which face it calls.
type Engine interface {
	// BeginSession creates a new upload session; directories are created as
	// needed.
	BeginSession(ctx context.Context) (Session, error)
	// ResumeSession re-materializes an in-progress session from its persisted
	// row and backend state: the partial bytes are re-processed to rebuild
	// the digest chain and the session is returned ready for further Append,
	// with Offset reporting the re-derived durable byte count. A missing row —
	// or one already expired but not yet swept — yields ErrSessionNotFound
	// (fail-closed, architecture sections 3.1 [M7] and 5.3.1 contract 3: an
	// expired session is unknown, and the sweep is the row's only legitimate
	// reclamation path). A missing data file under a surviving unexpired row
	// is recreated empty and the session resumes from offset 0. Requires a
	// Sessions store in Options; without one this always yields
	// ErrSessionNotFound. The S3 engine resumes the same way since T-323 paid
	// the section 11.31 debt: the row carries the multipart upload id, the
	// parts and durable offset are rebuilt via ListParts, and (an S3 fact:
	// in-progress parts are unreadable) a rebuilt session's Commit verifies
	// through a streaming readback of the assembled object instead of the
	// in-memory digest chain — see s3_resume.go.
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
	//
	// [M9] ADR-0031: this is the legacy face. It delegates to GCSweep with a
	// ReferencedFunc adapter, so hold-set exclusion and the apply-phase
	// delete recheck apply here too; new callers should prefer GCSweep with
	// a marker whose Live is a true single-point query (the REST face's
	// full W-2 closure needs it).
	GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error)
	// GCSweep is the [M9] mark-sweep face (ADR-0031): the referenced
	// callback upgraded into the two-method GCMarker. Candidacy gained a
	// hold-set gate (an in-flight sha is never a candidate, grace is not
	// consulted for it), and apply gained per-candidate delete gates — the
	// hold set re-checked immediately before the delete, then Live. Dry-run
	// and apply semantics are otherwise those of GC; see the implementing
	// engines for the full happens-before argument.
	GCSweep(ctx context.Context, m GCMarker, grace time.Duration, apply bool) ([]string, error)
	// ReleaseGCHold drops one in-flight registration for sha256 — the seam
	// repo.Service calls after the metadata transaction referencing the
	// blob has committed ([M9] ADR-0031; the five landing paths are wired
	// outside this package). Commits acquire the hold themselves; this
	// method only ever releases. It cannot fail the caller's operation:
	// unknown shas, double releases, post-TTL releases and post-Close
	// releases are all no-ops returning nil (a hold is acceleration for
	// reclamation, never a correctness obligation on the caller — the TTL
	// backstops anything missed).
	ReleaseGCHold(sha256 string) error
	// Close shuts the engine down: it stops accepting mutations (BeginSession,
	// Delete, GC fail with ErrEngineClosed), drains the in-memory session
	// registry (closing live sessions' data fds — no leaks) and PRESERVES
	// unexpired upload sessions — rows and uploads/<id>/ data files alike —
	// so a clean shutdown (SIGTERM, compose restart) resumes exactly like a
	// crash (kill -9). Close deletes no session and runs no expiry pass: the
	// startup sweep + TTL is the only reclamation path, and one INFO line
	// (opts.Logger or slog.Default) reports the preserved count + ids
	// (ADR-0028, architecture section 5.3.1 contract 7). Open/Stat keep
	// serving committed blobs. Idempotent.
	Close() error
}
