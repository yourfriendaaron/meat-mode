//go:build unix

package tracker

import (
	"os"
	"syscall"
)

// openNonBlocking opens path for reading without blocking on a FIFO.
func openNonBlocking(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
