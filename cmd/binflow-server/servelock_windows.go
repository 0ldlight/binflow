//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// The stdlib syscall package for windows exports the simple blocking
// LockFile only; the exclusive + fail-immediately semantics the heartbeat
// lock needs come from LockFileEx/UnlockFileEx, reached through kernel32
// directly (the same posture as internal/storage's maintenance lock, and
// for the same reason: not promoting golang.org/x/sys to a direct
// dependency, ADR-0005).
var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procServeLockFileEx   = kernel32.NewProc("LockFileEx")
	procServeUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

// LockFileEx flag values (winbase.h).
const (
	serveLockExclusiveLock   = 0x00000002 // LOCKFILE_EXCLUSIVE_LOCK
	serveLockFailImmediately = 0x00000001 // LOCKFILE_FAIL_IMMEDIATELY
)

// serveErrnumLockViolation is ERROR_LOCK_VIOLATION (33): the errno
// LockFileEx reports when the range is held and FAIL_IMMEDIATELY is set.
const serveErrnumLockViolation = 33

// lockServeFile takes an exclusive non-blocking lock on byte 0 of f — the
// windows-native equivalent of flock. Byte-range locks are per file handle,
// so both cross-process and cross-thread exclusion hold.
func lockServeFile(f *os.File) error {
	ol := syscall.Overlapped{} // zero offset: the lock covers byte 0
	r1, _, errno := syscall.SyscallN(
		procServeLockFileEx.Addr(),
		f.Fd(),
		serveLockExclusiveLock|serveLockFailImmediately,
		0,
		1, 0, // lock one byte (low, high)
		uintptr(unsafe.Pointer(&ol)), //nolint:gosec // G103: the Overlapped record is the API-mandated carrier for the call's offset/length
	)
	if r1 == 0 {
		if errno == serveErrnumLockViolation {
			return errServeLockHeld
		}
		if errno == 0 {
			errno = syscall.EINVAL
		}
		return fmt.Errorf("LockFileEx: %w", errno)
	}
	return nil
}

// unlockServeFile drops the byte-range lock taken by lockServeFile.
func unlockServeFile(f *os.File) error {
	ol := syscall.Overlapped{}
	r1, _, errno := syscall.SyscallN(
		procServeUnlockFileEx.Addr(),
		f.Fd(),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&ol)), //nolint:gosec // G103: see lockServeFile
	)
	if r1 == 0 {
		if errno == 0 {
			errno = syscall.EINVAL
		}
		return fmt.Errorf("UnlockFileEx: %w", errno)
	}
	return nil
}
