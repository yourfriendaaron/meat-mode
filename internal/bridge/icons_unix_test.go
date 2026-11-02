//go:build unix

package bridge

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestFindIconSkipsFIFO: a FIFO named favicon.svg is skipped without being
// opened, which would block the bridge.
func TestFindIconSkipsFIFO(t *testing.T) {
	repo := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(repo, "favicon.svg"), 0o644); err != nil {
		t.Skip("cannot make a FIFO:", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "icon.png"), pngIcon, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindIcon(repo); string(got) != string(pngIcon) {
		t.Errorf("FindIcon = %q; want icon.png", got)
	}
}

// TestOpenNoFollow: the open read uses neither blocks on a FIFO nor
// follows a link, so a file swapped in after the checks cannot hang the
// icon pass or lead it elsewhere.
func TestOpenNoFollow(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("cannot make a FIFO:", err)
	}
	f, err := openNoFollow(fifo)
	if err != nil {
		t.Fatalf("open a FIFO: %v", err)
	}
	if fi, err := f.Stat(); err != nil || fi.Mode().IsRegular() {
		t.Errorf("FIFO reads as a regular file: %v %v", fi, err)
	}
	f.Close()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, pngIcon, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openNoFollow(link); err == nil {
		f.Close()
		t.Error("opened through a link")
	}
}

// TestReadSpaceColoursFIFO: a pipe at the workspace colours path, or a link
// to one, gives no colours at once with an error, instead of hanging the
// bridge's loop.
func TestReadSpaceColoursFIFO(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "space-colours.json")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("cannot make a FIFO:", err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, p := range []string{fifo, link} {
			if got, err := ReadSpaceColours(p); err == nil || got != nil {
				t.Errorf("ReadSpaceColours(%s) = %v, %v; want an error", filepath.Base(p), got, err)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ReadSpaceColours blocked on a FIFO")
	}
}
