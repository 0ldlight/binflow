//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockServeFile takes an exclusive non-blocking advisory lock on f (flock).
// The per-open-file-description semantics give the lock both cross-process
// and cross-thread exclusion — the probe in serveRunning relies on the
// cross-process half, a same-process second serve on the same data
// directory relies on the cross-thread half.
func lockServeFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return errServeLockHeld
		}
		return fmt.Errorf("flock: %w", err)
	}
	return nil
}

// unlockServeFile drops the advisory lock taken by lockServeFile.
func unlockServeFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("flock unlock: %w", err)
	}
	return nil
}
