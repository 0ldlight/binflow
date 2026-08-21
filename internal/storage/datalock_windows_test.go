//go:build windows

package storage

// The windows runtime leg of the data-directory maintenance lock (G05,
// T-169). lockFile/unlockFile below AcquireDataLock run through
// LockFileEx/UnlockFileEx on this platform while every other platform runs
// the flock twin (datalock_unix.go); the untagged TestDataLockMutualExclusion
// in backup_test.go covers the platform-neutral contract on unix CI. These
// tests pin the windows-specific byte-range semantics:
//
//   - a second handle in the same process contends (locks are per handle,
//     so cross-thread exclusion holds, not just cross-process);
//   - while the lock is held the contender cannot read the holder record —
//     the contention error degrades to "last holder: unknown" and LockHolder
//     returns "" (the documented N3 degradation, not an error);
//   - after Release the byte-range lock is really gone: the stale record
//     becomes readable again and a fresh handle re-acquires the lock.
//
// Run on a windows host with: go test -race -run TestDataLock ./internal/storage/
// (the deferred FR-34-AC5 real-machine leg, PRD milestone-5 Q3).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDataLockWindowsExclusion drives the acquire/contend/release/reacquire
// cycle for every op-pair the serve process can meet: the REST gc of a
// running serve process against a CLI export/import on the same data
// directory is exactly the second-acquire shape.
func TestDataLockWindowsExclusion(t *testing.T) {
	cases := []struct {
		name     string
		firstOp  string
		secondOp string
	}{
		{"gc vs export", DataLockOpGC, DataLockOpExport},
		{"export vs gc", DataLockOpExport, DataLockOpGC},
		{"import vs gc", DataLockOpImport, DataLockOpGC},
		{"same op twice", DataLockOpGC, DataLockOpGC},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			first, err := AcquireDataLock(dir, tc.firstOp)
			if err != nil {
				t.Fatalf("first acquire: %v", err)
			}

			// A second acquisition through a second handle must fail fast
			// with the documented error chain and the degraded holder
			// diagnostics: the exclusive byte-range lock on byte 0 makes
			// the record unreadable for the contender (N3).
			second, err := AcquireDataLock(dir, tc.secondOp)
			if err == nil {
				_ = second.Release()
				t.Fatal("second AcquireDataLock succeeded while the lock was held")
			}
			if !errors.Is(err, ErrDataLockHeld) {
				t.Fatalf("second acquire error = %v, want ErrDataLockHeld in the chain", err)
			}
			if want := "last holder: unknown"; !strings.Contains(err.Error(), want) {
				t.Fatalf("contention error = %q, want the degraded %q diagnostics", err.Error(), want)
			}

			// The 409 diagnostics face reads the record through this helper;
			// while held it must degrade to "" (never an error, never a
			// partial record).
			if got := LockHolder(dir); got != "" {
				t.Fatalf("LockHolder while held = %q, want the documented empty degradation", got)
			}

			if err := first.Release(); err != nil {
				t.Fatalf("release: %v", err)
			}

			// Release must have really dropped the byte-range lock: a fresh
			// handle re-acquires, and the previous holder's record — stale,
			// the process may long be gone — becomes readable again.
			again, err := AcquireDataLock(dir, tc.secondOp)
			if err != nil {
				t.Fatalf("acquire after release: %v", err)
			}
			if holder := LockHolder(dir); HolderOp(holder) != tc.firstOp {
				t.Fatalf("stale record after release = %q, want previous op %q", holder, tc.firstOp)
			}
			if err := again.Release(); err != nil {
				t.Fatalf("second release: %v", err)
			}
		})
	}
}

// TestDataLockWindowsRecordPersists pins the lifecycle of the on-disk record
// itself: the lock file survives Release (removing it would race the next
// acquirer) and carries the last holder's line for the next contender.
func TestDataLockWindowsRecordPersists(t *testing.T) {
	dir := t.TempDir()
	lock, err := AcquireDataLock(dir, DataLockOpExport)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, maintenanceLockName)); err != nil {
		t.Fatalf("lock file after release: %v (it must stay behind)", err)
	}
	if holder := LockHolder(dir); HolderOp(holder) != DataLockOpExport {
		t.Fatalf("record after release = %q, want op %q", holder, DataLockOpExport)
	}
}
