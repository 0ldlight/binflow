package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maintenanceLockName is the advisory lock file every data-directory-wide
// maintenance operation (GC, export) holds while it runs (ADR-0015 erratum
// 3). It lives inside the data directory itself so that every process that
// can see the blobs can also see the lock — there is no out-of-band
// coordination channel in any of the ADR-0004 deployment shapes.
//
// The file never carries state: only the advisory lock on it matters. Its
// first line records the current holder ("pid=<n> op=<gc|export|...>") as
// diagnostics for the process that loses the race, so an operator staring at
// a refused export sees who to wait for (the REST gc face maps the same
// content to its 409 "export in progress" body, T-94).
const maintenanceLockName = ".maintenance.lock"

// ErrDataLockHeld is returned (wrapped) by AcquireDataLock when another
// maintenance operation already holds the data-directory lock. Callers must
// fail fast, never queue: GC deletes blobs while export copies them, so the
// two operations are mutually exclusive by design, and a queued export would
// hold its output directory hostage for an unbounded time.
var ErrDataLockHeld = errors.New("data directory is locked by another maintenance operation")

// DataLock is the held data-directory maintenance lock. Release returns the
// directory to the shared state; the lock file itself stays behind (removing
// it would race the next acquirer — unlink-while-waiting is the classic
// flock lifecycle bug). Safe to Release at most once; a nil or released
// DataLock is a no-op.
type DataLock struct {
	f *os.File
}

// AcquireDataLock takes the exclusive maintenance lock for dataDir on behalf
// of the named operation (op is free-form diagnostics: "gc", "export", ...).
// The lock is cross-process (flock on unix, LockFileEx on windows) and
// cross-thread: two connections in the same process also exclude each other,
// which is what lets a serve-process REST gc contend with a CLI export
// process on the same data directory.
//
// The data directory is created when missing (0700) so the lock can be taken
// on a fresh instance; this mirrors OpenEngine's posture and keeps the lock
// file co-located with the data it guards.
func AcquireDataLock(dataDir, op string) (*DataLock, error) {
	if dataDir == "" {
		return nil, errors.New("storage: data lock: data directory path is empty")
	}
	// 0700: the data dir holds opaque artifact bytes (same posture as
	// OpenEngine; gosec G301 wants 0750, we go stricter).
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("storage: data lock: create %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, maintenanceLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: engine-owned data dir + constant file name; nothing operator-supplied reaches the path
	if err != nil {
		return nil, fmt.Errorf("storage: data lock: open %s: %w", path, err)
	}
	if err := lockFile(f); err != nil {
		holder := lockHolder(f)
		_ = f.Close()
		if errors.Is(err, ErrDataLockHeld) {
			return nil, fmt.Errorf("storage: data lock %s is held (last holder: %s): %w", path, holder, ErrDataLockHeld)
		}
		return nil, fmt.Errorf("storage: data lock: locking %s: %w", path, err)
	}
	// Best-effort holder record: only the lock holder writes while holding,
	// so the content is current for as long as the lock is ours. Write
	// failures degrade diagnostics only, never the lock itself.
	record := []byte(fmt.Sprintf("pid=%d op=%s\n", os.Getpid(), op))
	_, _ = f.WriteAt(record, 0)        //nolint:errcheck // diagnostics only, see above
	_ = f.Truncate(int64(len(record))) //nolint:errcheck // a stale tail is harmless (one line is read)
	return &DataLock{f: f}, nil
}

// Data-lock operation vocabulary (architecture review N2, T-96 review):
// pinned so the 409 diagnostics face (T-94) and the holder records can
// never drift apart. op stays free-form diagnostics in AcquireDataLock,
// but every in-tree caller passes exactly one of these; a new maintenance
// operation extends this list first.
const (
	// DataLockOpGC is the op of both gc entrances (REST and CLI).
	DataLockOpGC = "gc"
	// DataLockOpExport is the op of the export CLI.
	DataLockOpExport = "export"
	// DataLockOpImport is the op of the import CLI (T-96 correctness
	// review B1: import is a data-directory-wide maintenance operation too —
	// it holds the lock for its whole run so a concurrent gc can never
	// build a database inside a target import is restoring into).
	DataLockOpImport = "import"
	// DataLockOpCleanup is the op of the unused-cleanup engine (T-324,
	// FR-102.2): its run deletes node rows AND drives a GCSweep apply over
	// the same blob tree the gc/export family guards, so it joins the one
	// mutual-exclusion set — a cleanup run refuses (409 on the REST face)
	// while a gc, export or import holds the lock, and vice versa.
	DataLockOpCleanup = "cleanup"
)

// MaintenanceLockName exposes the lock file's name to co-located maintenance
// tooling: an operation that clears a data directory back to empty (import's
// no-half-restore cleanup) must SKIP this file, never unlink it — removing a
// lock file another holder is holding is exactly the unlink-while-held race
// the type comment above forbids (T-96 correctness review B1).
const MaintenanceLockName = maintenanceLockName

// LockHolder reads the holder record of dataDir's maintenance lock file —
// the first line the current holder wrote ("pid=<n> op=<gc|export>") — for
// a contender that just lost the race (the 409 diagnostics face, T-94;
// architecture review N1). It returns "" when the file is missing,
// unreadable or carries no record: on Windows the exclusive byte-range
// lock makes the record unreadable while held, so "" is the documented
// degradation, not an error (N3). A record does NOT imply its holder is
// alive — a crashed process leaves its line behind; present it as "last
// holder", never a guarantee.
func LockHolder(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	buf, err := os.ReadFile(filepath.Join(dataDir, maintenanceLockName)) //nolint:gosec // constant name inside the engine-owned data dir, same posture as AcquireDataLock
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(buf), "\n")
	return strings.TrimSpace(line)
}

// HolderOp extracts the op token of a holder record ("pid=123 op=export"
// -> "export"; one of the DataLockOp* vocabulary). "" when the record is
// absent or carries no op token — callers word their generic branch on
// exactly that (N3's unknown fallback).
func HolderOp(holder string) string {
	for _, field := range strings.Fields(holder) {
		if op, ok := strings.CutPrefix(field, "op="); ok {
			return op
		}
	}
	return ""
}

// lockHolder reads the holder record of a lock file we failed to acquire.
// The record may be stale (a crashed holder leaves its line behind); the
// wording at the call site presents it as "last holder", not a guarantee.
func lockHolder(f *os.File) string {
	buf := make([]byte, 128)
	n, err := f.ReadAt(buf, 0)
	if err != nil && err != io.EOF || n == 0 {
		return "unknown"
	}
	line := string(buf[:n])
	for i, c := range line {
		if c == '\n' {
			line = line[:i]
			break
		}
	}
	if line == "" {
		return "unknown"
	}
	return line
}

// Release drops the lock. Idempotent; safe on a nil DataLock (the
// zero-value-friendly shape defer-based callers rely on).
func (l *DataLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlockFile(l.f)
	cerr := l.f.Close()
	l.f = nil
	if err != nil {
		return fmt.Errorf("storage: data lock: unlock: %w", err)
	}
	if cerr != nil {
		return fmt.Errorf("storage: data lock: close: %w", cerr)
	}
	return nil
}
