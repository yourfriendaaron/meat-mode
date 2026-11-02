//go:build unix

package tracker

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReadRegular: a pipe, or a link to one, is refused at once instead of
// blocking; a link to a regular file is read; the read stops one byte past
// the limit.
func TestReadRegular(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("cannot make a FIFO:", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("sync: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	toFifo, toFile := filepath.Join(dir, "to-fifo"), filepath.Join(dir, "to-file")
	if err := os.Symlink(fifo, toFifo); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, toFile); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, p := range []string{fifo, toFifo, dir} {
			if _, err := ReadRegular(p, 64); !errors.Is(err, ErrNotRegular) {
				t.Errorf("ReadRegular(%s) error = %v; want ErrNotRegular", filepath.Base(p), err)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ReadRegular blocked on a FIFO")
	}
	if got, err := ReadRegular(toFile, 64); err != nil || string(got) != "sync: true\n" {
		t.Errorf("ReadRegular(link to file) = %q, %v", got, err)
	}
	if got, err := ReadRegular(file, 4); err != nil || len(got) != 5 {
		t.Errorf("ReadRegular(file, 4) read %d bytes, %v; want 5", len(got), err)
	}
	if _, err := ReadRegular(filepath.Join(dir, "missing"), 64); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadRegular(missing) error = %v; want fs.ErrNotExist", err)
	}
}

// TestRepoOptInSkipsFIFO: a repo whose .meat-mode.yaml links to a pipe is
// not opted in, and the check does not hang the scan.
func TestRepoOptInSkipsFIFO(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("cannot make a FIFO:", err)
	}
	if err := os.Symlink(fifo, filepath.Join(dir, OptInFile)); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool)
	go func() { done <- RepoOptIn(dir) }()
	select {
	case on := <-done:
		if on {
			t.Error("RepoOptIn = true for a FIFO")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RepoOptIn blocked on a FIFO")
	}
}

// TestOpenNonBlocking: the open itself does not block on a FIFO, so a pipe
// swapped in after ReadRegular's check cannot hang it, and the check on the
// open file refuses it.
func TestOpenNonBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("cannot make a FIFO:", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f, err := openNonBlocking(fifo)
		if err != nil {
			t.Errorf("open a FIFO: %v", err)
			return
		}
		defer f.Close()
		if fi, err := f.Stat(); err != nil || fi.Mode().IsRegular() {
			t.Errorf("a FIFO stats as regular: %v, %v", fi, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("openNonBlocking blocked on a FIFO")
	}
}

// TestReadFile: a file over MaxFileBytes gives ErrTooLarge; one at the limit
// is read whole.
func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	at, over := filepath.Join(dir, "at"), filepath.Join(dir, "over")
	if err := os.WriteFile(at, make([]byte, MaxFileBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(over, make([]byte, MaxFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadFile(at); err != nil || len(got) != MaxFileBytes {
		t.Errorf("ReadFile(at the limit) read %d bytes, %v", len(got), err)
	}
	if _, err := ReadFile(over); !errors.Is(err, ErrTooLarge) {
		t.Errorf("ReadFile(over the limit) error = %v; want ErrTooLarge", err)
	}
}
