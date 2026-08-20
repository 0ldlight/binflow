//go:build unix

package storage

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFile takes an exclusive non-blocking advisory lock on f (flock). The
// per-open-file-description semantics of flock give the lock both
// cross-process and cross-thread exclusion: a second OpenFile in the same
// process yields a second description and therefore contends honestly.
func lockFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrDataLockHeld
		}
		return fmt.Errorf("flock: %w", err)
	}
	return nil
}

// unlockFile drops the advisory lock taken by lockFile. The kernel drops it
// on close anyway; the explicit unlock keeps Release honest even if the fd
// were ever reused through a pooled wrapper.
func unlockFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("flock unlock: %w", err)
	}
	return nil
}
