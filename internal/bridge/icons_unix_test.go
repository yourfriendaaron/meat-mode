//go:build unix

package bridge

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
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
