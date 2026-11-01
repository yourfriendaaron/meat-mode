//go:build !unix

package bridge

import "os"

// openNoFollow opens path for reading; the caller's checks before and after
// stand in for O_NOFOLLOW and O_NONBLOCK here.
func openNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}
