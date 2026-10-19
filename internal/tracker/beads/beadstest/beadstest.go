// Package beadstest makes throwaway beads repos for tests that call real bd.
package beadstest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// NeedBD skips the test when bd is not on PATH.
func NeedBD(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// NewRepo makes a Git repo with an initialised beads database at parent/name
// (parent defaults to a temp dir) and returns its path.
func NewRepo(t testing.TB, parent, name string) string {
	t.Helper()
	NeedBD(t)
	if parent == "" {
		parent = t.TempDir()
	}
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "init", "-q")
	run(t, dir, "bd", "init", "-q", "--non-interactive", "--skip-agents", "--skip-hooks", "-p", "tst")
	return dir
}

// OptIn appends the sync opt-in line to the repo's .beads/config.yaml.
func OptIn(t testing.TB, dir string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, ".beads", "config.yaml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("\nmeat-mode.sync: true\n"); err != nil {
		t.Fatal(err)
	}
}

// Run runs a command in dir and fails the test on error.
func Run(t testing.TB, dir string, name string, args ...string) string {
	t.Helper()
	return run(t, dir, name, args...)
}

func run(t testing.TB, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BD_NON_INTERACTIVE=1", "GIT_AUTHOR_NAME=Alex", "GIT_COMMITTER_NAME=Alex",
		"GIT_AUTHOR_EMAIL=alex@example.com", "GIT_COMMITTER_EMAIL=alex@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
