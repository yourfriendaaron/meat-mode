package tracker

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrTooLarge is the error ReadFile gives for a file over MaxFileBytes.
var ErrTooLarge = errors.New("file too large")

// MaxFileBytes is the most ReadFile reads of a tracker's file.
const MaxFileBytes = 8 << 20

// ReadFile reads a file a repo holds, as os.ReadFile does, through
// ReadRegular: a pipe or device there, or a link to one, gives
// ErrNotRegular instead of hanging the bridge, and a file over MaxFileBytes
// gives ErrTooLarge.
func ReadFile(path string) ([]byte, error) {
	data, err := ReadRegular(path, MaxFileBytes)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%s: %w", path, ErrTooLarge)
	}
	return data, nil
}

// ErrNotRegular is the error ReadRegular gives for a path that is not a
// regular file: a folder, a pipe, a device or a socket.
var ErrNotRegular = errors.New("not a regular file")

// ReadRegular reads at most limit+1 bytes of the regular file at path,
// following symbolic links, so a caller can tell a file over limit. A pipe,
// device or other file that is not regular is refused before it is opened
// and is never opened in a way that blocks, so a link a repo or another
// program puts there cannot hang the bridge. A missing file gives an error
// matching fs.ErrNotExist.
func ReadRegular(path string, limit int64) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	f, err := openNonBlocking(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Checked again on the open file, so one swapped in since the check is
	// still a regular file. A file renamed over in between (how writers
	// replace one whole) is read as it is now.
	if now, err := f.Stat(); err != nil || !now.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	return io.ReadAll(io.LimitReader(f, limit+1))
}
