package main

// The serve heartbeat lock ([M9] ADR-0031 / architecture section 14.2 point
// 4): <data>/serve.lock is a process-lifetime advisory flock the serve
// process holds from boot to exit. It carries no state and guards nothing
// by itself — its single consumer is the gc CLI's cross-process gate:
//
//	CLI gc --apply with an explicit grace < 60s + serve.lock held
//	→ refuse the run (a sweep in another process cannot see serve's
//	in-flight upload hold set, so a no-window apply could delete a blob
//	whose reference is still being written — the T-232 race, W-1 half).
//
// Dry-run runs are never refused (no destructive face); they annotate the
// report instead (the K22 wording: candidates may over-report while serve
// runs, because the cross-process sweep cannot see in-flight uploads).
// Default-grace runs are never refused either (the mtime window covers the
// millisecond-scale pre-reference gap).
//
// The lock primitive mirrors internal/storage's maintenance lock shape
// (flock on unix, LockFileEx on windows) but is a SEPARATE file: the
// maintenance lock must stay free for gc/export to take while serve runs —
// serve holding .maintenance.lock would refuse every online maintenance
// operation, which is exactly backwards.
//
// A second serve process on the same data directory fails its boot on this
// lock: multi-instance on one data directory is already forbidden
// (architecture section 9); the lock turns that from silent SQLite
// contention into a pointed refusal.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// serveLockName is the lock file's name inside the data directory.
const serveLockName = "serve.lock"

// errServeLockHeld names the contention the acquirer can see.
var errServeLockHeld = errors.New("serve.lock is held by another process")

// serveLock is the held serve heartbeat lock. release is idempotent and
// nil-safe (the defer-friendly zero-value shape storage.DataLock uses); the
// kernel drops the flock on process death either way — the lock IS the
// heartbeat, no cleanup path is needed.
type serveLock struct {
	f *os.File
}

// acquireServeLock takes the serve heartbeat lock for dataDir. The data
// directory is created when missing (the same posture as the engine open
// and the maintenance lock) so a fresh instance boots cleanly.
func acquireServeLock(dataDir string) (*serveLock, error) {
	if dataDir == "" {
		return nil, errors.New("serve lock: data directory path is empty")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("serve lock: create %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, serveLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: engine-owned data dir + constant file name
	if err != nil {
		return nil, fmt.Errorf("serve lock: open %s: %w", path, err)
	}
	if err := lockServeFile(f); err != nil {
		_ = f.Close()
		if errors.Is(err, errServeLockHeld) {
			return nil, fmt.Errorf("serve lock %s: %w (another serve process owns this data directory; multiple instances on one data directory are not supported)", path, errServeLockHeld)
		}
		return nil, fmt.Errorf("serve lock: locking %s: %w", path, err)
	}
	return &serveLock{f: f}, nil
}

// release drops the heartbeat lock. Idempotent; nil-safe.
func (l *serveLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlockServeFile(l.f)
	cerr := l.f.Close()
	l.f = nil
	if err != nil {
		return fmt.Errorf("serve lock: unlock: %w", err)
	}
	if cerr != nil {
		return fmt.Errorf("serve lock: close: %w", cerr)
	}
	return nil
}

// serveRunning reports whether some process currently holds the serve
// heartbeat lock of dataDir. The probe never creates the file and never
// leaves a lock behind: open without O_CREATE, try the non-blocking lock,
// and drop it immediately when it succeeds. A missing file, an unreadable
// one or any other error reads as "not running" — the gate this feeds only
// ever refuses work, so a false negative is the conservative direction for
// availability (the REST face carries the real protection).
func serveRunning(dataDir string) bool {
	if dataDir == "" {
		return false
	}
	f, err := os.OpenFile(filepath.Join(dataDir, serveLockName), os.O_RDWR, 0o600) //nolint:gosec // constant name inside the engine-owned data dir
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // probe-only handle
	if err := lockServeFile(f); err != nil {
		return errors.Is(err, errServeLockHeld)
	}
	// We took it — no serve process holds it. Put it straight back.
	_ = unlockServeFile(f) //nolint:errcheck // probe-only lock, dropped immediately
	return false
}
