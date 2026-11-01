package ticket

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/trackertest"
)

// TestConformance runs the shared suite against the real tk.
func TestConformance(t *testing.T) {
	trackertest.Run(t, Name, func(t *testing.T) (tracker.Tracker, string) {
		if _, err := exec.LookPath("tk"); err != nil {
			t.Skip("tk is not installed")
		}
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, storeDir), 0o755); err != nil {
			t.Fatal(err)
		}
		return &Ticket{bin: "tk", look: exec.LookPath}, dir
	})
}

const sample = `---
id: wp-1a2b
status: in_progress
deps: []
links: []
created: 2026-01-19T03:10:30Z
type: task
priority: 1
assignee: Alex
external-ref: meat-mode:abc
parent: wp-0000
tags: [human, garden]
due: 2026-02-01T09:00:00Z
---
# Water the plants

Every other day.
status: this line is body text

## Design

Use the can.

## Notes

**2026-01-20T08:00:00Z**

by sam-app:

Done once, still needed.

**2026-01-21T08:00:00Z**

An agent wrote this one.
`

func TestParseTicket(t *testing.T) {
	is := parseTicket([]byte(sample)).issue("wp-1a2b", "v", time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC))
	if is.Status != tracker.StatusInProgress {
		t.Errorf("status %q; a body line starting status: must not count", is.Status)
	}
	if is.Title != "Water the plants" || is.Description != "Every other day.\nstatus: this line is body text" {
		t.Errorf("title %q, description %q", is.Title, is.Description)
	}
	if len(is.Labels) != 2 || is.Labels[0] != "human" || is.Labels[1] != "garden" {
		t.Errorf("labels %v", is.Labels)
	}
	if is.Priority != 1 || is.Parent != "wp-0000" || is.CreatedBy != "Alex" {
		t.Errorf("got %+v", is)
	}
	if is.DueAt == nil || !is.DueAt.Equal(time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("due %v", is.DueAt)
	}
	if len(is.Comments) != 2 {
		t.Fatalf("comments %+v", is.Comments)
	}
	if c := is.Comments[0]; c.Author != "sam-app" || c.Text != "Done once, still needed." {
		t.Errorf("first note %+v", c)
	}
	if c := is.Comments[1]; c.Author != "agent" || c.Text != "An agent wrote this one." {
		t.Errorf("second note %+v", c)
	}
}

func TestParseWithoutNotes(t *testing.T) {
	data := "---\nid: a-1\nstatus: done\ntags: [human]\n---\n# Sort the post\n"
	mtime := time.Now()
	is := parseTicket([]byte(data)).issue("a-1", "v", mtime)
	if is.Status != tracker.StatusClosed || is.ClosedAt == nil || len(is.Comments) != 0 || is.Description != "" {
		t.Errorf("got %+v", is)
	}
}

func TestDismissReason(t *testing.T) {
	data := "---\nid: a-1\nstatus: closed\n---\n# Renew the card\n\n## Notes\n\n**2026-01-20T08:00:00Z**\n\nby alex:\n\n" + dismissedNote + "\n"
	if is := parseTicket([]byte(data)).issue("a-1", "v", time.Now()); is.CloseReason != "dismissed" {
		t.Errorf("close reason %q", is.CloseReason)
	}
}

// A name with a space is the author as written, not a dashed form of it.
func TestParseAuthorWithASpace(t *testing.T) {
	data := "---\nid: a-1\nstatus: open\n---\n# Renew the card\n\n## Notes\n\n**2026-01-20T08:00:00Z**\n\nby Alex Smith:\n\nDone once.\n"
	is := parseTicket([]byte(data)).issue("a-1", "v", time.Now())
	if len(is.Comments) != 1 || is.Comments[0].Author != "Alex Smith" || is.Comments[0].Text != "Done once." {
		t.Fatalf("comments %+v", is.Comments)
	}
}

// An author that is only blanks is no author, so a note never opens with an
// empty "by  :" line.
func TestNoteAuthorBlankIsNoAuthor(t *testing.T) {
	for _, a := range []string{"", " ", "\t\n", " \x00 "} {
		if got := noteAuthor(a); got != "" {
			t.Errorf("noteAuthor(%q) = %q, want no author", a, got)
		}
	}
	for _, a := range []string{"Alex", "Alex Smith", "  padded  "} {
		if got := noteAuthor(a); got == "" {
			t.Errorf("noteAuthor(%q) is no author", a)
		}
	}
}

// A note of an agent's own that opens like an author line keeps its text:
// Meat Mode's author line always has a blank line after it.
func TestParseNoteThatOpensWithBy(t *testing.T) {
	data := "---\nid: a-1\nstatus: open\n---\n# Renew the card\n\n## Notes\n\n**2026-01-20T08:00:00Z**\n\nby Friday we need these:\n- the card\n"
	is := parseTicket([]byte(data)).issue("a-1", "v", time.Now())
	if len(is.Comments) != 1 || is.Comments[0].Author != defaultAuthor || is.Comments[0].Text != "by Friday we need these:\n- the card" {
		t.Fatalf("comments %+v", is.Comments)
	}
}

// The note's author line survives a round trip through tk.
func TestCommentKeepsAnAuthorWithASpace(t *testing.T) {
	if _, err := exec.LookPath("tk"); err != nil {
		t.Skip("tk is not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	tr := &Ticket{bin: "tk", look: exec.LookPath}
	id, err := tr.Create(ctx, dir, tracker.NewIssue{Title: "Renew the library card", Actor: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.AddComment(ctx, dir, id, "Alex Smith", "Done once."); err != nil {
		t.Fatal(err)
	}
	is, err := tr.Show(ctx, dir, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range is.Comments {
		if c.Text == "Done once." {
			if c.Author != "Alex Smith" {
				t.Fatalf("author %q, want %q", c.Author, "Alex Smith")
			}
			return
		}
	}
	t.Fatalf("note not found in %+v", is.Comments)
}

// A first Dismiss attempt that wrote its note but did not close must not
// write the note again on the next attempt.
func TestDismissAfterANoteOnlyAttempt(t *testing.T) {
	if _, err := exec.LookPath("tk"); err != nil {
		t.Skip("tk is not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	tr := &Ticket{bin: "tk", look: exec.LookPath}
	id, err := tr.Create(ctx, dir, tracker.NewIssue{Title: "Renew the library card", Actor: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.AddComment(ctx, dir, id, "Alex Smith", dismissedNote); err != nil {
		t.Fatal(err)
	}
	if err := tr.Dismiss(ctx, dir, id, "Alex Smith"); err != nil {
		t.Fatal(err)
	}
	is, err := tr.Show(ctx, dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if is.Status != tracker.StatusClosed || is.CloseReason != "dismissed" {
		t.Errorf("status %q, close reason %q; want closed and dismissed", is.Status, is.CloseReason)
	}
	notes := 0
	for _, c := range is.Comments {
		if c.Text == dismissedNote {
			notes++
		}
	}
	if notes != 1 {
		t.Errorf("%d dismissed notes, want 1: %+v", notes, is.Comments)
	}
}

// A file whose name cannot be an id is not a ticket, and must not fail the
// whole list.
func TestListSkipsAnInvalidFileName(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, storeDir)
	if err := os.Mkdir(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, ".hidden.md"), []byte("not a ticket"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "a-1.md"), []byte("---\nid: a-1\nstatus: open\ntags: [human]\n---\n# Water the plants\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	is, err := (&Ticket{}).List(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(is) != 1 || is[0].ID != "a-1" {
		t.Fatalf("listed %+v", is)
	}
}

func TestDetect(t *testing.T) {
	tr := &Ticket{}
	detect := func(setup func(dir string)) bool {
		dir := t.TempDir()
		setup(dir)
		entries, _ := os.ReadDir(dir)
		return tr.Detect(dir, entries)
	}
	if detect(func(string) {}) {
		t.Error("matched a folder with no .tickets")
	}
	if !detect(func(d string) { os.Mkdir(filepath.Join(d, storeDir), 0o755) }) {
		t.Error("an empty .tickets should match")
	}
	if !detect(func(d string) {
		os.Mkdir(filepath.Join(d, storeDir), 0o755)
		os.WriteFile(filepath.Join(d, storeDir, "a-1.md"), []byte("---\n---\n"), 0o644)
	}) {
		t.Error(".tickets with a ticket should match")
	}
	if detect(func(d string) {
		os.Mkdir(filepath.Join(d, storeDir), 0o755)
		os.WriteFile(filepath.Join(d, storeDir, "notes.txt"), nil, 0o644)
	}) {
		t.Error(".tickets with only other files should not match")
	}
}

// TestCreateDashTitle checks the file itself: tk keeps the space the title
// went in behind, and Create takes it off again.
func TestCreateDashTitle(t *testing.T) {
	if _, err := exec.LookPath("tk"); err != nil {
		t.Skip("tk is not installed")
	}
	dir := t.TempDir()
	tr := &Ticket{bin: "tk", look: exec.LookPath}
	id, err := tr.Create(context.Background(), dir, tracker.NewIssue{Title: "-v Water the plants", Actor: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, storeDir, id+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n# -v Water the plants\n") {
		t.Errorf("file:\n%s", data)
	}
}

// TestExecHidesBridgeEnvironment keeps the bridge's own MEAT_MODE_*
// settings, the hub token above all, out of the tk child.
func TestExecHidesBridgeEnvironment(t *testing.T) {
	t.Setenv("MEAT_MODE_BRIDGE_TOKEN", "example-secret")
	t.Setenv("MEAT_MODE_HUB_URL", "https://hub.example.com")
	t.Setenv("TICKETS_DIR", "/elsewhere")
	dir := t.TempDir()
	seen := filepath.Join(dir, "env")
	bin := filepath.Join(dir, "tk")
	script := "#!/bin/sh\nenv > '" + seen + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	tr := &Ticket{bin: bin, look: exec.LookPath}
	if _, err := tr.exec(context.Background(), repo, "ls"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, "MEAT_MODE_") || strings.Contains(got, "example-secret") {
		t.Errorf("tk saw a MEAT_MODE_ variable:\n%s", got)
	}
	if !strings.Contains(got, "PATH=") || !strings.Contains(got, "TICKETS_DIR="+filepath.Join(repo, storeDir)+"\n") ||
		strings.Contains(got, "/elsewhere") {
		t.Errorf("tk got the wrong environment:\n%s", got)
	}
}

// Check runs tk too, and gives it the same stripped environment.
func TestCheckHidesBridgeEnvironment(t *testing.T) {
	t.Setenv("MEAT_MODE_BRIDGE_TOKEN", "example-secret")
	t.Setenv("TICKETS_DIR", "/elsewhere")
	dir := t.TempDir()
	seen := filepath.Join(dir, "env")
	bin := filepath.Join(dir, "tk")
	script := "#!/bin/sh\nenv > '" + seen + "'\necho 'tk 1.2.3'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	tr := &Ticket{bin: bin, look: exec.LookPath}
	v, err := tr.Check(context.Background())
	if err != nil || v != "1.2.3" {
		t.Fatalf("Check = %q, %v", v, err)
	}
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); strings.Contains(got, "MEAT_MODE_") || strings.Contains(got, "example-secret") ||
		strings.Contains(got, "/elsewhere") {
		t.Errorf("tk --version saw the bridge environment:\n%s", got)
	}
}

func TestEpicsConformance(t *testing.T) {
	trackertest.RunEpics(t, func(t *testing.T) (tracker.Epics, string, trackertest.EpicSeed) {
		bin, err := exec.LookPath("tk")
		if err != nil {
			t.Skip("tk is not installed")
		}
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, storeDir), 0o755); err != nil {
			t.Fatal(err)
		}
		tk := func(args ...string) string {
			cmd := exec.Command(bin, args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("tk %s: %v\n%s", strings.Join(args, " "), err, out)
			}
			return strings.TrimSpace(string(out))
		}
		return &Ticket{bin: bin, look: exec.LookPath}, dir, trackertest.EpicSeed{
			Add: func(title, desc, typ, parent string, human bool, assignee string) string {
				args := []string{"create", title, "-d", desc, "-t", typ}
				if parent != "" {
					args = append(args, "--parent", parent)
				}
				if human {
					args = append(args, "--tags", tracker.HumanLabel)
				}
				if assignee != "" {
					args = append(args, "-a", assignee)
				}
				return tk(args...)
			},
			Close: func(id string) { tk("close", id) },
		}
	})
}
