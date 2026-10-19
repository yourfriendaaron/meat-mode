package beads_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/beads"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/beads/beadstest"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/trackertest"
)

func TestConformance(t *testing.T) {
	trackertest.Run(t, "beads", func(t *testing.T) (tracker.Tracker, string) {
		dir := beadstest.NewRepo(t, "", "garden")
		tr, err := beads.New(tracker.Env{})
		if err != nil {
			t.Fatal(err)
		}
		return tr, dir
	})
}

func TestRegistered(t *testing.T) {
	for _, f := range tracker.Factories() {
		if f.Name == "beads" {
			return
		}
	}
	t.Fatal("beads is not registered")
}

func TestNewFindsBD(t *testing.T) {
	tr, _ := beads.New(tracker.Env{LookPath: func(string) (string, error) { return "/opt/tools/bin/bd", nil }})
	if c := tr.(*beads.CLI); c.Bin != "/opt/tools/bin/bd" || c.Missing {
		t.Fatalf("bd from PATH: %+v", c)
	}
	tr, _ = beads.New(tracker.Env{Bin: "/srv/bd", LookPath: func(string) (string, error) { return "", errors.New("unused") }})
	if c := tr.(*beads.CLI); c.Bin != "/srv/bd" {
		t.Fatalf("bd from the config: %+v", c)
	}
	tr, _ = beads.New(tracker.Env{LookPath: func(string) (string, error) { return "", errors.New("not found") }})
	if _, err := tr.Check(t.Context()); err == nil || !strings.Contains(err.Error(), "bd is not on PATH") {
		t.Fatalf("Check without bd = %v", err)
	}
}

func TestDetectAndOptIn(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("garden/.beads/metadata.json", "{}\n")
	mk("garden/.beads/config.yaml", "meat-mode.sync: true\n")
	mk("old/.beads/beads.db", "")
	mk("old/.meat-mode.yaml", "sync: true\n")
	mk("empty/.beads/config.yaml", "")
	tr := &beads.CLI{}
	for name, want := range map[string]bool{"garden": true, "old": true, "empty": false} {
		dir := filepath.Join(root, name)
		entries, _ := os.ReadDir(dir)
		if got := tr.Detect(dir, entries); got != want {
			t.Errorf("Detect(%s) = %v", name, got)
		}
	}
	for name, want := range map[string]bool{"garden": true, "old": true, "empty": false} {
		if got := tr.OptedIn(filepath.Join(root, name)); got != want {
			t.Errorf("OptedIn(%s) = %v", name, got)
		}
	}
}

func TestDescribe(t *testing.T) {
	tr := &beads.CLI{}
	for cmd, want := range map[string]string{
		"bd show tst-1":              "Reading the bead",
		"/usr/local/bin/bd close x":  "Closing the bead",
		"bd comments add tst-1 hi":   "Writing a note",
		"bd comments tst-1":          "Reading the bead",
		"bd update tst-1 --status x": "Updating the bead",
		"go test ./...":              "",
	} {
		if got := tr.Describe(strings.Fields(cmd)); got != want {
			t.Errorf("Describe(%q) = %q, want %q", cmd, got, want)
		}
	}
	if env := tr.Env("planner"); len(env) != 1 || env[0] != "BEADS_ACTOR=planner" {
		t.Errorf("Env = %v", env)
	}
}

// TestOptInLines reads the opt-in line in .beads/config.yaml as YAML does.
func TestOptInLines(t *testing.T) {
	tr := &beads.CLI{}
	for config, want := range map[string]bool{
		"meat-mode.sync: true":                      true,
		`"meat-mode.sync": "true"`:                  true,
		`'meat-mode.sync' : 'true'  # keep syncing`: true,
		"# settings\nmeat-mode.sync: true # on":     true,
		"meat-mode.sync: true\nmeat-mode.sync: no":  false,
		"meat-mode.sync: true# not a comment":       false,
		"# meat-mode.sync: true":                    false,
		"nested:\n  meat-mode.sync: true":           false,
		"meat-mode.sync: false":                     false,
	} {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte(config+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := tr.OptedIn(dir); got != want {
			t.Errorf("OptedIn with %q = %v, want %v", config, got, want)
		}
	}
}
