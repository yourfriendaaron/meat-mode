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
