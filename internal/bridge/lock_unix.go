//go:build unix

package bridge

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes an exclusive lock on f without waiting.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errLocked
	}
	return err
}

// lockFileWait takes an exclusive lock on f, waiting for its holder.
func lockFileWait(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
