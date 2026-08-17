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
// under <root>/sessions/<uuid>/ plus running hash states. A session is
// single-threaded by contract (architecture section 3.1); mu guards against
// accidental concurrent use so state can never interleave.
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
	// Keep state.json truthful (received = bytes on disk) per section 4.1;
	// M1 has no reader for it, but the file must never lie. Best effort: a
	// failure here poisons the session too — the on-disk bookkeeping would
	// diverge from the data file.
	if err := writeSessionState(s.dir, s.state); err != nil {
		s.poison(s, err)
		return 0, fmt.Errorf("storage: append session %s: %w", s.id, err)
	}
	return s.state.Received, nil
}

// poison marks the session unusable after a write-path failure. It does NOT
// delete the directory: the caller gets to inspect the failure, and Abort
// (or engine Close / the startup sweep) performs the actual cleanup.
func (s *uploadSession) poison(_ *uploadSession, cause error) {
	if !s.poisoned {
		s.poisoned = true
		s.cause = cause
	}
}

// Commit finalizes the session. Protocol (ADR-0006, order fixed):
//
//  1. verify expected digests against the streamed content
//  2. fsync(session data)
//  3. singleflight on sha256: if the blob already exists, discard the
//     session and return the existing ref (idempotent dedup); else
//  4. rename(data -> blobs/<xx>/<sha256>) — atomic within one filesystem
//  5. fsync(blob shard dir)
//
// The metadata transaction is deliberately out of scope: this method's
// contract ends at "blob in place" (architecture section 3.3 makes the
// ordering blob-first a hard rule).
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
		if err := os.Rename(s.dataPath(), target); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", s.dataPath(), target, err)
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
	// close the fd and drop the session directory.
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
	return filepath.Join(s.dir, sessionDataFile)
}

// finishLocked marks the session done and removes its directory. Callers
// hold s.mu.
func (s *uploadSession) finishLocked() {
	s.done = true
	if s.file != nil {
		_ = s.file.Close()
	}
	_ = os.RemoveAll(s.dir)
	s.eng.forgetSession(s)
}

// failLocked is finishLocked for the error paths: same cleanup, the session
// is never reusable after a failed Commit or a poisoned Append.
func (s *uploadSession) failLocked() {
	s.poisoned = true
	s.finishLocked()
}

// cleanup is the engine-shutdown path (Close already drained the registry).
// A poisoned session cleans up exactly like a healthy one: the temp data is
// garbage either way.
func (s *uploadSession) cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}
	s.done = true
	s.poisoned = true
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			return fmt.Errorf("close data: %w", err)
		}
	}
	if err := os.RemoveAll(s.dir); err != nil {
		return fmt.Errorf("remove %s: %w", s.dir, err)
	}
	return nil
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
