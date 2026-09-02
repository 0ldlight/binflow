package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// uploadSession is the live state of one upload: an append-only data file
// under <root>/uploads/<uuid>/ plus running hash states, mirrored by an
// upload_sessions row in the metadata store. A session is single-threaded by
// contract (architecture section 3.1); mu guards against accidental concurrent
// use so state can never interleave.
type uploadSession struct {
	eng      *engine
	id       string
	dir      string
	mu       sync.Mutex
	file     *os.File
	state    sessionState
	digests  *digesters
	done     bool  // Commit or Abort reached; further calls are no-ops
	cause    error // first Append failure; nil while the session is healthy
	poisoned bool  // true once cause is set: no further use is trustworthy
}

// ID returns the session uuid.
func (s *uploadSession) ID() string { return s.id }

// Offset returns the cumulative bytes received so far — the authoritative
// offset source for REST resume (architecture sections 3.1 [M7] and 5.3.1
// contract 1). For a live session it is the appended byte count, which equals
// the uploads/<id>/data file length (every Append updates it while holding
// s.mu); after a ResumeSession re-hash recovery it is the re-derived file
// length. Reads serialize against Append/Commit via s.mu, so a concurrent
// reader never observes a mid-write counter.
func (s *uploadSession) Offset() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Received
}

// Append streams r into the session data file while feeding the three hash
// states, returning the cumulative offset. The write is durable-visible to
// readers of the temp path but not to the blob store until Commit renames
// it into place, so a crash mid-Append can never expose a partial blob.
//
// Failure semantics: if any Append fails (I/O error, cancelled context,
// reader error), the session is poisoned — the file on disk and the running
// digests may have diverged (a partial write advances the file but not the
// hash chain), so every later Append or Commit reports an error wrapping
// ErrSessionPoisoned and the caller must Abort. Reuse-on-error would let a
// file whose content does not match its computed digest enter the blob
// store, corrupting future dedup hits; that must never happen.
func (s *uploadSession) Append(ctx context.Context, r io.Reader) (int64, error) {
	if s == nil {
		return 0, errors.New("storage: append: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return 0, fmt.Errorf("storage: append session %s: session already finalized", s.id)
	}
	if s.poisoned {
		return 0, fmt.Errorf("storage: append session %s: %w: %w", s.id, ErrSessionPoisoned, s.cause)
	}
	if err := ctx.Err(); err != nil {
		s.poison(s, err)
		return 0, fmt.Errorf("storage: append session %s: %w", s.id, err)
	}
	written, err := copyWithCtx(ctx, io.MultiWriter(s.file, s.digests.writer()), r)
	if err != nil {
		s.poison(s, err)
		return 0, fmt.Errorf("storage: append session %s: %w", s.id, err)
	}
	s.state.Received += written
	// Persist the updated received counter to the DB row (best-effort truth
	// bookkeeping; ResumeSession re-derives the offset from the file, so a
	// crash between the data write and this update is harmless). A failure
	// here poisons the session: on-disk bookkeeping diverged from the data
	// file, and a resumed session must not trust either.
	//
	// The bookkeeping write is detached from the caller's cancellation
	// (N3, T-220): the data bytes are already on disk once the copy reached
	// EOF, so a cancellation landing in the window between EOF and this
	// SetState — a client disconnecting right after the last byte — must not
	// fail the write and poison a complete session. Cancellation during the
	// copy itself still poisons (a partial write diverges file and digests).
	if err := s.persistStateLocked(context.WithoutCancel(ctx)); err != nil {
		s.poison(s, err)
		return 0, fmt.Errorf("storage: append session %s: %w", s.id, err)
	}
	return s.state.Received, nil
}

// persistStateLocked writes the session state to the metadata row. A nil
// Sessions store (blob-only GC path) makes it a no-op. Callers hold s.mu.
//
// The write rides the busy budget (T-423, T-377 D1): it is an idempotent
// overwrite of the row's state column with a value computed under s.mu (the
// retry re-marshals the identical state), so a busy-class escape re-runs
// instead of poisoning a session whose data bytes are already durable. The
// caller's detached context (Append passes WithoutCancel, N3/T-220) keeps
// the ladder running to its end for a client that disconnected between the
// last byte and this bookkeeping write.
func (s *uploadSession) persistStateLocked(ctx context.Context) error {
	ss := s.eng.opts.Sessions
	if ss == nil {
		return nil
	}
	if err := RetryOnBusy(ctx, s.eng.opts.Logger, s.eng.opts.BusyRetry, "upload-sessions set-state",
		func(ctx context.Context) error {
			return ss.SetState(ctx, s.id, marshalSessionState(s.state))
		}); err != nil {
		return fmt.Errorf("persist session state: %w", err)
	}
	return nil
}

// poison marks the session unusable after a write-path failure. It does NOT
// delete the directory: the caller gets to inspect the failure, and Abort
// (or the startup sweep once the row expires) performs the actual cleanup —
// engine Close no longer deletes anything (ADR-0028).
func (s *uploadSession) poison(_ *uploadSession, cause error) {
	if !s.poisoned {
		s.poisoned = true
		s.cause = cause
	}
}

// Commit finalizes the session. Protocol (ADR-0006, order fixed):
//
//  1. verify expected digests against the streamed content
//     1a. [M9] ADR-0031: acquire the GC hold for the sha256 — BEFORE any step
//     that could make the blob visible to a sweep (see below)
//  2. fsync(session data)
//  3. singleflight on sha256: if the blob already exists, discard the
//     session and return the existing ref (idempotent dedup); else
//  4. rename(data -> blobs/<xx>/<sha256>) — atomic within one filesystem
//  5. fsync(blob shard dir)
//
// The metadata transaction is deliberately out of scope: this method's
// contract ends at "blob in place" (architecture section 3.3 makes the
// ordering blob-first a hard rule). The session row is deleted on success.
//
// [M9] The hold acquired at step 1a is what closes the pre-reference window
// (W-1): between the rename (4) and the caller's metadata commit the blob is
// on disk but referenced nowhere, and an explicit grace=0 sweep would
// otherwise collect it (the T-232 t134 race). Acquire-before-rename is the
// anchoring invariant — a scan that can see the blob necessarily started
// after the acquire, so the hold gate of gcDeleteGate cannot miss it. The
// hold is kept on success for the caller to release via
// Engine.ReleaseGCHold once its reference has landed; failed Commits
// release it immediately. A dedup hit (step 3 early return) also keeps the
// hold: the winner's registration may already be gone, and this Commit's
// caller has its own metadata transaction ahead of it.
func (s *uploadSession) Commit(ctx context.Context, expect BlobRef) (BlobRef, error) {
	if s == nil {
		return BlobRef{}, errors.New("storage: commit: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return BlobRef{}, fmt.Errorf("storage: commit session %s: session already finalized", s.id)
	}
	if s.poisoned {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: commit session %s: %w: %w", s.id, ErrSessionPoisoned, s.cause)
	}
	if err := ctx.Err(); err != nil {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: commit session %s: %w", s.id, err)
	}

	sums := s.digests.sums()
	actual := BlobRef{Sha256: sums.sha256, Sha1: sums.sha1, Md5: sums.md5, Size: s.state.Received}

	// Step 1: every non-empty expected digest must match. Uppercase hex is
	// tolerated; any mismatch aborts with ErrChecksumMismatch and nothing
	// reaches the blob store.
	for _, chk := range []struct {
		name   string
		got    string
		want   string
		hexLen int
	}{
		{"sha256", actual.Sha256, expect.Sha256, sha256HexLen},
		{"sha1", actual.Sha1, expect.Sha1, sha1HexLen},
		{"md5", actual.Md5, expect.Md5, md5HexLen},
	} {
		want, err := normalizeHex(chk.want, chk.hexLen)
		if err != nil {
			// A malformed client-supplied digest is a rejection, same class
			// as a mismatch: nothing reaches the blob store.
			s.failLocked()
			bad := fmt.Errorf("%s: %w", chk.name, err)
			return BlobRef{}, fmt.Errorf("storage: commit session %s: %w: %w", s.id, ErrChecksumMismatch, bad)
		}
		if want != "" && want != chk.got {
			s.failLocked()
			return BlobRef{}, fmt.Errorf("storage: commit session %s: %w: %s received %s, actual %s",
				s.id, ErrChecksumMismatch, chk.name, want, chk.got)
		}
	}

	// Step 1a [M9]: acquire the in-flight hold. It must be in place before
	// the rename can publish the blob (step 4); a failed Commit releases it
	// via the deferred complement, a successful one keeps it for the
	// caller's ReleaseGCHold. See the method godoc and hold.go.
	s.eng.holds.acquire(actual.Sha256)
	holdKept := false
	defer func() {
		if !holdKept {
			s.eng.holds.release(actual.Sha256)
		}
	}()

	// Step 2: make the temp bytes durable before the rename publishes them.
	if err := s.file.Sync(); err != nil {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: commit session %s: fsync data: %w", s.id, err)
	}

	target, err := s.eng.blobPath(actual.Sha256)
	if err != nil {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: commit session %s: %w", s.id, err)
	}

	// Steps 3-5 under the per-checksum singleflight: exactly one concurrent
	// uploader of this blob performs the physical publish; the rest wake up,
	// see the blob present and discard their own session data.
	commitErr := s.eng.sf.do(actual.Sha256, func() error {
		fp := s.dataPath()
		if _, err := os.Stat(target); err == nil {
			return nil // blob already present; idempotent hit
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", target, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil { // stricter than gosec G301's 0750
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
		}
		// rename is atomic: readers see either the old file or the complete
		// new one, never a half-written blob.
		if err := os.Rename(fp, target); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", fp, target, err)
		}
		if err := syncDir(filepath.Dir(target)); err != nil {
			return fmt.Errorf("fsync %s: %w", filepath.Dir(target), err)
		}
		return nil
	})
	if commitErr != nil {
		// The rename may have succeeded while a later fsync failed; either
		// way this session is dead. The blob (if it landed) is complete and
		// unreferenced — GC's grace period is the designed recovery path.
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: commit session %s: %w", s.id, commitErr)
	}

	// Success: the data file is gone (renamed) or obsolete (dedup hit);
	// close the fd and drop the session directory and DB row. The GC hold
	// stays registered — releasing it is the caller's step, after the
	// metadata transaction that makes this blob referenced.
	holdKept = true
	s.finishLocked()
	return actual, nil
}

// Abort discards the session. Idempotent and nil-receiver safe. Cleanup is
// purely local, so a cancelled context does not prevent the discard.
func (s *uploadSession) Abort(ctx context.Context) error {
	if s == nil {
		return nil
	}
	_ = ctx.Err() // accepted: Abort must clean up regardless of cancellation
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}
	s.finishLocked()
	return nil
}

// dataPath is the temp file location; only valid while the session lives.
func (s *uploadSession) dataPath() string {
	return filepath.Join(s.dir, dataFileName)
}

// deleteRowLocked removes the metadata row (best-effort: a replayed Commit
// or Abort is a no-op and the startup sweep reclaims any orphan row via
// expiry). A nil Sessions store makes it a no-op. Callers hold s.mu.
func (s *uploadSession) deleteRowLocked() {
	if ss := s.eng.opts.Sessions; ss != nil {
		_ = ss.Delete(context.Background(), s.id)
	}
}

// finishLocked marks the session done and removes its directory and row.
// Callers hold s.mu.
func (s *uploadSession) finishLocked() {
	s.done = true
	if s.file != nil {
		_ = s.file.Close()
	}
	_ = os.RemoveAll(s.dir)
	s.deleteRowLocked()
	s.eng.forgetSession(s)
}

// failLocked is finishLocked for the error paths: same cleanup, the session
// is never reusable after a failed Commit or a poisoned Append.
func (s *uploadSession) failLocked() {
	s.poisoned = true
	s.finishLocked()
}

// detach unloads a session without deleting anything. Callers: a repeated
// ResumeSession for the same id (the replacement session owns the shared
// directory and row), and engine Close (ADR-0028 — a clean shutdown detaches
// every live session's fd but preserves its uploads/<id>/ directory and DB
// row, so the restart resumes exactly like a crash would). It closes the fd
// and marks the session finalized so every later Append/Commit/Abort is a
// no-op. The caller must NOT hold e.mu — acquiring s.mu while holding e.mu
// would invert the s.mu -> e.mu order finishLocked/forgetSession establish.
func (s *uploadSession) detach() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
}

// copyWithCtx is io.Copy with context cancellation checked between buffer
// chunks, so a cancelled upload stops consuming the request body.
func copyWithCtx(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				total += int64(nw)
			}
			if ew != nil {
				return total, ew
			}
			if nr != nw {
				return total, io.ErrShortWrite
			}
		}
		if er != nil {
			if errors.Is(er, io.EOF) {
				return total, nil
			}
			return total, er
		}
	}
}
