//go:build !unix

package tracker

import "os"

// openNonBlocking opens path for reading; ReadRegular's checks before and
// after stand in for O_NONBLOCK here.
func openNonBlocking(path string) (*os.File, error) {
	return os.Open(path)
}
