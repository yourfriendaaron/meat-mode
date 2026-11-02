//go:build unix

package ticket

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReadSkipsFIFO: a ticket file that links to a pipe gives an error at
// once instead of hanging the sync.
func TestReadSkipsFIFO(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, storeDir)
	if err := os.Mkdir(store, 0o755); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("cannot make a FIFO:", err)
	}
	if err := os.Symlink(fifo, filepath.Join(store, "t-1.md")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() { _, err := read(dir, "t-1"); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("read of a FIFO gave no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read blocked on a FIFO")
	}
}
