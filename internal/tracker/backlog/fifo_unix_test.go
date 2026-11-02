//go:build unix

package backlog

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReadRootConfigSkipsFIFO: a repo whose backlog.config.yml links to a
// pipe is not taken for a Backlog.md repo, and the scan does not hang.
func TestReadRootConfigSkipsFIFO(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("cannot make a FIFO:", err)
	}
	path := filepath.Join(dir, "backlog.config.yml")
	if err := os.Symlink(fifo, path); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool)
	go func() { _, ok := readRootConfig(path); done <- ok }()
	select {
	case ok := <-done:
		if ok {
			t.Error("readRootConfig took a FIFO for a config")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readRootConfig blocked on a FIFO")
	}
}
