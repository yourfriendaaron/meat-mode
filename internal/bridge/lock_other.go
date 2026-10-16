//go:build !unix

package bridge

import "os"

// lockFile is a no-op where flock is missing; the bridge ships for Linux and
// macOS.
func lockFile(*os.File) error { return nil }
