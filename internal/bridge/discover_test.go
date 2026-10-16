package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestYAMLLine(t *testing.T) {
	cases := []struct {
		line   string
		key    string
		val    string
		parsed bool
	}{
		{"meat-mode.sync: true", "meat-mode.sync", "true", true},
		{`"meat-mode.sync": "true"`, "meat-mode.sync", "true", true},
		{`'meat-mode.sync' : 'true'  # keep syncing`, "meat-mode.sync", "true", true},
		{"meat-mode.sync: true# not a comment", "meat-mode.sync", "true# not a comment", true},
		{`meat-mode.sync: "a # b"`, "meat-mode.sync", "a # b", true},
		{"# meat-mode.sync: true", "", "", false},
		{"  meat-mode.sync: true", "", "", false},
		{"no colon here", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := yamlLine(c.line)
		if ok != c.parsed || k != c.key || v != c.val {
			t.Errorf("yamlLine(%q) = %q, %q, %v", c.line, k, v, ok)
		}
	}
}

func writeRepo(t *testing.T, dir, config string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	writeRepo(t, filepath.Join(root, "garden"), "# settings\nmeat-mode.sync: true # on\n")
	writeRepo(t, filepath.Join(root, "kitchen"), "meat-mode.sync: false\n")
	writeRepo(t, filepath.Join(root, "attic"), "nested:\n  meat-mode.sync: true\n")
	writeRepo(t, filepath.Join(root, "quoted"), "\"meat-mode.sync\": 'true'\n")
	writeRepo(t, filepath.Join(root, "-shed"), "meat-mode.sync: true\n") // the hub refuses the name
	writeRepo(t, filepath.Join(root, "garden-wt"), "meat-mode.sync: true\n")
	if err := os.WriteFile(filepath.Join(root, "garden-wt", ".git"), []byte("gitdir: ../garden/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(t.TempDir(), "me")
	writeRepo(t, personal, "")

	got := Discover([]string{root, filepath.Join(root, "missing")}, personal, quiet)
	want := []Repo{
		{Name: "attic", Dir: filepath.Join(root, "attic")},
		{Name: "garden", Dir: filepath.Join(root, "garden"), OptedIn: true},
		{Name: "kitchen", Dir: filepath.Join(root, "kitchen")},
		{Name: "personal", Dir: personal, Personal: true, OptedIn: true},
		{Name: "quoted", Dir: filepath.Join(root, "quoted"), OptedIn: true},
	}
	if len(got) != len(want) {
		t.Fatalf("Discover = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("repo %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDiscoverSkipsPersonalThroughSymlink(t *testing.T) {
	root := t.TempDir()
	personal := filepath.Join(root, "me")
	writeRepo(t, personal, "")
	// The root is reached through a symlink, so the personal repo's absolute
	// path under the link differs from the one given as --personal.
	link := filepath.Join(t.TempDir(), "projects-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("cannot symlink: %v", err)
	}
	got := Discover([]string{link}, personal, quiet)
	if len(got) != 1 || got[0].Name != PersonalRepo || !got[0].Personal {
		t.Fatalf("Discover = %+v, want the personal repo once", got)
	}
}
