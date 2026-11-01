//go:build unix

package bridge

import (
	"os"
	"syscall"
)

// openNoFollow opens path for reading without following a final symbolic
// link and without blocking on a FIFO.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
