package beads_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
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

func TestEpicsConformance(t *testing.T) {
	trackertest.RunEpics(t, func(t *testing.T) (tracker.Epics, string, trackertest.EpicSeed) {
		dir := beadstest.NewRepo(t, "", "garden")
		tr, err := beads.New(tracker.Env{})
		if err != nil {
			t.Fatal(err)
		}
		return tr.(tracker.Epics), dir, trackertest.EpicSeed{
			Add: func(title, desc, typ, parent string, human bool, assignee string) string {
				args := []string{"create", "--title=" + title, "--description=" + desc, "-t", typ, "--json"}
				if parent != "" {
					args = append(args, "--parent="+parent)
				}
				if human {
					args = append(args, "-l", tracker.HumanLabel)
				}
				if assignee != "" {
					args = append(args, "-a", assignee)
				}
				var b beads.Bead
				out := beadstest.Run(t, dir, "bd", args...)
				if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &b); err != nil || b.ID == "" {
					t.Fatalf("bd create printed %q: %v", out, err)
				}
				return b.ID
			},
			Close: func(id string) { beadstest.Run(t, dir, "bd", "close", "--reason=done", "--json", "--", id) },
		}
	})
}

// TestEpicRowsWaitsDatesAndComments checks, against real bd, that a bead
// held by an open one names it, that a closed blocker frees it, and that a
// deferred bead brings its date and a commented one its count.
func TestEpicRowsWaitsDatesAndComments(t *testing.T) {
	dir := beadstest.NewRepo(t, "", "garden")
	tr, err := beads.New(tracker.Env{})
	if err != nil {
		t.Fatal(err)
	}
	add := func(title string) string {
		out := beadstest.Run(t, dir, "bd", "create", "--title="+title, "-t", "task", "--json")
		var b beads.Bead
		if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &b); err != nil || b.ID == "" {
			t.Fatalf("bd create printed %q: %v", out, err)
		}
		return b.ID
	}
	seeds, beds, done, fence, later := add("Buy the seeds"), add("Dig the beds"), add("Rake the soil"), add("Paint the fence"), add("Prune the roses")
	beadstest.Run(t, dir, "bd", "dep", "add", fence, seeds)
	beadstest.Run(t, dir, "bd", "dep", "add", fence, beds)
	beadstest.Run(t, dir, "bd", "dep", "add", seeds, done)
	beadstest.Run(t, dir, "bd", "close", "--reason=done", "--json", "--", done)
	beadstest.Run(t, dir, "bd", "update", "--defer=2099-06-01", "--json", "--", later)
	beadstest.Run(t, dir, "bd", "comments", "add", beds, "Check the soil first.")
	beadstest.Run(t, dir, "bd", "comments", "add", beds, "Use the long spade.")

	rows, err := tr.(tracker.Epics).EpicRows(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]api.EpicRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	want := []string{seeds, beds}
	slices.Sort(want)
	if got := byID[fence].BlockedBy; !slices.Equal(got, want) {
		t.Errorf("fence blockedBy = %v; want %v", got, want)
	}
	if got := byID[seeds].BlockedBy; len(got) != 0 {
		t.Errorf("seeds waits on %v after its blocker closed", got)
	}
	if r := byID[later]; r.Status != "deferred" || r.DeferUntil == nil || r.DeferUntil.Year() != 2099 {
		t.Errorf("deferred row = status %q, deferUntil %v", r.Status, r.DeferUntil)
	}
	if got := byID[beds].CommentCount; got != 2 {
		t.Errorf("beds commentCount = %d; want 2", got)
	}
}
