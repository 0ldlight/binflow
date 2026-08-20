//go:build windows

package storage

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// The stdlib syscall package for windows exports the simple blocking
// LockFile only; the exclusive + fail-immediately semantics the maintenance
// lock needs come from LockFileEx/UnlockFileEx, reached here through
// kernel32 directly rather than by promoting golang.org/x/sys from an
// indirect to a direct dependency (ADR-0005 dependency gate; x/sys already
// rides the build via modernc.org/libc, and this file changes none of that).
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

// LockFileEx flag values (winbase.h).
const (
	lockFileExclusiveLock   = 0x00000002 // LOCKFILE_EXCLUSIVE_LOCK
	lockFileFailImmediately = 0x00000001 // LOCKFILE_FAIL_IMMEDIATELY
)

// errnumLockViolation is ERROR_LOCK_VIOLATION (33): the errno
// LockFileEx reports when the range is held and FAIL_IMMEDIATELY is set.
const errnumLockViolation = 33

// lockFile takes an exclusive non-blocking lock on byte 0 of f — the
// windows-native equivalent of flock. Byte-range locks are per file handle,
// so both cross-process and cross-thread exclusion hold.
func lockFile(f *os.File) error {
	ol := syscall.Overlapped{} // zero offset: the lock covers byte 0
	r1, _, errno := syscall.SyscallN(
		procLockFileEx.Addr(),
		f.Fd(),
		lockFileExclusiveLock|lockFileFailImmediately,
		0,
		1, 0, // lock one byte (low, high)
		uintptr(unsafe.Pointer(&ol)), //nolint:gosec // G103: the Overlapped record is the API-mandated carrier for the call's offset/length
	)
	if r1 == 0 {
		if errno == errnumLockViolation {
			return ErrDataLockHeld
		}
		if errno == 0 {
			errno = syscall.EINVAL
		}
		return fmt.Errorf("LockFileEx: %w", errno)
	}
	return nil
}

// unlockFile drops the byte-range lock taken by lockFile.
func unlockFile(f *os.File) error {
	ol := syscall.Overlapped{}
	r1, _, errno := syscall.SyscallN(
		procUnlockFileEx.Addr(),
		f.Fd(),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&ol)), //nolint:gosec // G103: see lockFile
	)
	if r1 == 0 {
		if errno == 0 {
			errno = syscall.EINVAL
		}
		return fmt.Errorf("UnlockFileEx: %w", errno)
	}
	return nil
}
