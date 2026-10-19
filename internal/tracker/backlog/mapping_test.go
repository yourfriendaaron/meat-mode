package backlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// viewFixture is invented `backlog task view --json` output.
const viewFixture = `{
  "schemaVersion": 1,
  "kind": "task-view",
  "task": {
    "id": "TASK-7.1",
    "title": "Book the train",
    "status": "Done",
    "archived": false,
    "priority": "high",
    "reporter": "@alex",
    "labels": ["human", "dismissed", "travel"],
    "parentTaskId": "TASK-7",
    "references": ["meat-mode:action/act-9"],
    "createdAt": "2026-03-01T08:15:00Z",
    "updatedAt": "2026-03-02T17:40:00Z",
    "dueDate": "2026-03-05",
    "isReady": true,
    "path": "backlog/tasks/task-7.1 - Book-the-train.md",
    "description": "Before the 10th.",
    "comments": [
      {"index": 1, "body": "Seats by the window.", "createdAt": "2026-03-01T09:00:00Z", "author": "sam-app"},
      {"index": 2, "body": "Booked.", "createdAt": "2026-03-02T17:40:00Z", "author": null}
    ]
  }
}`

func TestToIssue(t *testing.T) {
	var v viewJSON
	if err := json.Unmarshal([]byte(viewFixture), &v); err != nil {
		t.Fatal(err)
	}
	got := toIssue(v.Task, defaultStatuses)
	updated := time.Date(2026, 3, 2, 17, 40, 0, 0, time.UTC)
	due := time.Date(2026, 3, 5, dueHour, 0, 0, 0, time.Local)
	switch {
	case got.ID != "TASK-7.1" || got.Title != "Book the train" || got.Description != "Before the 10th.":
		t.Fatalf("fields: %+v", got)
	case got.Status != tracker.StatusClosed || got.CloseReason != "Dismissed":
		t.Fatalf("status %q, reason %q", got.Status, got.CloseReason)
	case got.ClosedAt == nil || !got.ClosedAt.Equal(updated) || !got.UpdatedAt.Equal(updated):
		t.Fatalf("closed at %v, updated at %v", got.ClosedAt, got.UpdatedAt)
	case got.DueAt == nil || !got.DueAt.Equal(due):
		t.Fatalf("due %v; want %v", got.DueAt, due)
	case got.Priority != 1 || got.CreatedBy != "@alex" || got.Parent != "TASK-7":
		t.Fatalf("priority %d, by %q, parent %q", got.Priority, got.CreatedBy, got.Parent)
	case strings.Join(got.Labels, ",") != "human,travel":
		t.Fatalf("labels %v", got.Labels)
	case len(got.Comments) != 2 || got.Comments[0].Author != "sam-app" || got.Comments[0].Text != "Seats by the window." ||
		got.Comments[0].ID != "1" || got.Comments[1].Author != "":
		t.Fatalf("comments %+v", got.Comments)
	}
}

func TestMapStatus(t *testing.T) {
	board := []string{"Inbox", "To Do", "Doing", "Shipped"}
	for _, c := range []struct {
		status string
		ready  bool
		want   string
	}{
		{"Inbox", true, tracker.StatusOpen},
		{"inbox", false, tracker.StatusBlocked},
		{"To Do", true, tracker.StatusInProgress},
		{"doing", true, tracker.StatusInProgress},
		{"Doing", false, tracker.StatusInProgress},
		{"SHIPPED", true, tracker.StatusClosed},
		{"Gone", true, tracker.StatusOpen},
	} {
		if got := mapStatus(c.status, board, c.ready); got != c.want {
			t.Errorf("mapStatus(%q, ready %v) = %q; want %q", c.status, c.ready, got, c.want)
		}
	}
	if got := mapStatus("In Progress", defaultStatuses, true); got != tracker.StatusInProgress {
		t.Errorf("In Progress = %q", got)
	}
	if got := mapStatus("Done", defaultStatuses, true); got != tracker.StatusClosed {
		t.Errorf("Done = %q", got)
	}
}

func TestParseStatuses(t *testing.T) {
	if got := parseStatuses("To Do, In Progress, Done\n"); strings.Join(got, "|") != "To Do|In Progress|Done" {
		t.Fatalf("got %q", got)
	}
	if got := parseStatuses("\n"); len(got) != 3 {
		t.Fatalf("empty list = %q; want the defaults", got)
	}
}

func TestPriorities(t *testing.T) {
	for in, want := range map[string]int{"High": 1, "medium": 2, "low": 3, "Critical": 0, "someday": 2} {
		if got := mapPriority(&in); got != want {
			t.Errorf("%q = %d; want %d", in, got, want)
		}
	}
	if mapPriority(nil) != 2 {
		t.Error("no priority is not medium")
	}
}

func TestParseTime(t *testing.T) {
	for _, s := range []string{"2026-03-01T08:15:00Z", "2026-03-01"} {
		if parseTime(&s) == nil {
			t.Errorf("cannot read %q", s)
		}
	}
	bad := "soon"
	if parseTime(&bad) != nil || parseDue(&bad) != nil {
		t.Error("read a date from soon")
	}
}

// The version follows the file, so a new comment changes it, and the status
// and parent title, which come from outside it.
func TestFingerprint(t *testing.T) {
	is := tracker.Issue{ID: "TASK-1", Title: "Water the plants", Status: tracker.StatusOpen}
	file := []byte("---\nid: TASK-1\n---\n")
	base := fingerprint(is, file)
	if fingerprint(is, append(file, "## Comments\n"...)) == base {
		t.Error("a change to the file kept the version")
	}
	closed := is
	closed.Status = tracker.StatusClosed
	if fingerprint(closed, file) == base {
		t.Error("a change of status kept the version")
	}
	if fingerprint(is, nil) == fingerprint(closed, nil) {
		t.Error("without a file, a change of status kept the version")
	}
}

func TestListJSONRefusesPart(t *testing.T) {
	var l listJSON
	if err := json.Unmarshal([]byte(`{"schemaVersion":1,"kind":"task-list","tasks":[],"total":9,"nextSkip":5}`), &l); err != nil {
		t.Fatal(err)
	}
	if l.NextSkip == nil {
		t.Fatal("nextSkip not read")
	}
}

func TestDetect(t *testing.T) {
	write := func(t *testing.T, dir, name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b := &Tracker{}
	for _, c := range []struct {
		name  string
		files map[string]string
		want  bool
		store string
	}{
		{"folder config", map[string]string{"backlog/config.yml": "project_name: Example\n"}, true, "backlog"},
		{"hidden folder", map[string]string{".backlog/config.yaml": "project_name: Example\n"}, true, ".backlog"},
		{"root config", map[string]string{"backlog.config.yml": "project_name: \"Example\"\nbacklog_directory: \"planning\"\n"}, true, "planning"},
		{"root config without a name", map[string]string{"backlog.config.yml": "backlog_directory: planning\n"}, false, "backlog"},
		{"bare folder", map[string]string{"backlog/notes.md": "Ideas\n"}, false, "backlog"},
		{"nothing", map[string]string{"README.md": "Hello\n"}, false, "backlog"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range c.files {
				write(t, dir, name, body)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := b.Detect(dir, entries); got != c.want {
				t.Fatalf("Detect = %v; want %v", got, c.want)
			}
			if got := backlogDir(dir); got != c.store {
				t.Fatalf("backlogDir = %q; want %q", got, c.store)
			}
		})
	}
}

func TestTaskFiles(t *testing.T) {
	dir := t.TempDir()
	tasks := filepath.Join(dir, "backlog", "tasks")
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"task-1 - Water-the-plants.md", "task-1.1 - Buy-soil.md", "notes.md"} {
		if err := os.WriteFile(filepath.Join(tasks, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := taskFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || filepath.Base(got[idKey("TASK-1")]) != "task-1 - Water-the-plants.md" ||
		filepath.Base(got[idKey("TASK-1.1")]) != "task-1.1 - Buy-soil.md" {
		t.Fatalf("taskFiles = %v", got)
	}
}

func TestCheckVersion(t *testing.T) {
	for v, ok := range map[string]bool{"1.54.0": true, "1.51.0": true, "v2.0": true, "1.50.1": false, "1.51.0-beta.1": false, "soon": false} {
		if err := checkVersion(v); (err == nil) != ok {
			t.Errorf("checkVersion(%q) = %v", v, err)
		}
	}
}

func TestCheckID(t *testing.T) {
	for _, id := range []string{"TASK-1", "back-760.1", "task-001"} {
		if err := checkID(id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	for _, id := range []string{"", "--status=Done", "no-such task", "7"} {
		if checkID(id) == nil {
			t.Errorf("%q accepted", id)
		}
	}
}

func TestDescribe(t *testing.T) {
	b := &Tracker{}
	for cmd, want := range map[string]string{
		"backlog task view TASK-1 --plain":                       "Reading the task",
		"backlog task edit TASK-1 --comment hi --comment-author": "Writing a note",
		"backlog task edit TASK-1 --status Done":                 "Changing the task's status",
		"backlog task edit TASK-1 -s Done":                       "Changing the task's status",
		"backlog task create Buy soil":                           "Adding a task",
		"backlog config get statuses":                            "Reading the settings",
		"backlog task archive TASK-1":                            "Updating the task",
		"/usr/local/bin/backlog task list":                       "Reading the task",
		"bd show x":                                              "",
	} {
		if got := b.Describe(strings.Fields(cmd)); got != want {
			t.Errorf("Describe(%q) = %q; want %q", cmd, got, want)
		}
	}
}

func TestPromptNamesCommands(t *testing.T) {
	b := &Tracker{}
	p := b.Prompt("TASK-3")
	for _, want := range []string{"backlog task view TASK-3", "backlog task edit TASK-3 --status Done", "--comment-author \"$" + AuthorEnv + "\""} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if env := b.Env("claude-app"); len(env) != 1 || env[0] != AuthorEnv+"=claude-app" {
		t.Errorf("Env = %v", env)
	}
}
