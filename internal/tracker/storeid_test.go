package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormaliseRemote(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com/team/garden.git":        "example.com/team/garden",
		"https://sam@example.com/team/garden":        "example.com/team/garden",
		"https://Example.com/team/garden":            "example.com/team/garden",
		"git@example.com:team/garden.git":            "example.com/team/garden",
		"ssh://git@example.com:2222/team/garden.git": "example.com/team/garden",
		"git+https://example.com/team/garden.git":    "example.com/team/garden",
		"https://example.com/team/garden/":           "example.com/team/garden",
		"/srv/git/garden.git":                        "",
		"../garden":                                  "",
		`C:\repos\garden`:                            "",
		"C:/repos/garden":                            "",
		"file:///srv/git/garden.git":                 "",
		"file://localhost/srv/git/garden.git":        "",
		"":                                           "",
		"https://example.com":                        "",
	} {
		if got := normaliseRemote(in); got != want {
			t.Errorf("normaliseRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitStoreID(t *testing.T) {
	root := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo := filepath.Join(root, "garden")
	write(filepath.Join(repo, ".git", "config"), "[core]\n\tbare = false\n[remote \"upstream\"]\n\turl = https://example.com/other/garden.git\n[remote \"origin\"]\n\turl = git@example.com:team/garden.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n")
	if got := GitStoreID(repo); got != "example.com/team/garden#." {
		t.Errorf("at the root: %q", got)
	}
	sub := filepath.Join(repo, "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := GitStoreID(sub); got != "example.com/team/garden#web" {
		t.Errorf("in a subfolder: %q", got)
	}
	// No origin, a worktree's .git file, and no repo at all give none.
	bare := filepath.Join(root, "notes")
	write(filepath.Join(bare, ".git", "config"), "[core]\n\tbare = false\n")
	tree := filepath.Join(root, "tree")
	write(filepath.Join(tree, ".git"), "gitdir: ../garden/.git/worktrees/tree\n")
	for _, dir := range []string{bare, tree, filepath.Join(root, "loose")} {
		if got := GitStoreID(dir); got != "" {
			t.Errorf("GitStoreID(%s) = %q, want none", filepath.Base(dir), got)
		}
	}
}

func TestCleanStoreID(t *testing.T) {
	for in, want := range map[string]string{
		" b1ee-7fa4 ":            "b1ee-7fa4",
		"two words":              "",
		"line\nbreak":            "",
		strings.Repeat("a", 513): "",
		strings.Repeat("a", 512): strings.Repeat("a", 512),
	} {
		if got := CleanStoreID(in); got != want {
			t.Errorf("CleanStoreID(%q) = %q, want %q", in, got, want)
		}
	}
}
