package backlog_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/backlog"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/trackertest"
)

// newRepo makes a Git repo with a fresh Backlog.md project in a temp dir,
// skipping the test when backlog is not installed.
func newRepo(t *testing.T) (tracker.Tracker, string) {
	t.Helper()
	bin, err := exec.LookPath("backlog")
	if err != nil {
		t.Skip("backlog is not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", "-q"},
		{bin, "init", "Example", "--defaults", "--integration-mode", "none"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	tr, err := factory(t).New(tracker.Env{Bin: bin, Getenv: os.Getenv, LookPath: exec.LookPath})
	if err != nil {
		t.Fatal(err)
	}
	return tr, dir
}

func factory(t *testing.T) tracker.Factory {
	t.Helper()
	for _, f := range tracker.Factories() {
		if f.Name == backlog.Name {
			return f
		}
	}
	t.Fatal("backlog is not registered")
	return tracker.Factory{}
}

func TestConformance(t *testing.T) {
	trackertest.Run(t, backlog.Name, newRepo)
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("backlog", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("backlog %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// The action id is a reference, which Backlog.md keeps through later edits.
func TestActionIDSurvivesEdits(t *testing.T) {
	tr, dir := newRepo(t)
	ctx := context.Background()
	id, err := tr.Create(ctx, dir, tracker.NewIssue{Title: "Water the plants", ActionID: "act-1", Actor: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	run(t, dir, "task", "edit", id, "--description", "The ones on the balcony", "--priority", "high",
		"--add-label", "garden", "--add-ref", "https://example.com/plants", "--status", "In Progress")
	if got, err := tr.FindByActionID(ctx, dir, "act-1"); err != nil || got != id {
		t.Fatalf("FindByActionID after an edit = %q, %v; want %q", got, err, id)
	}
	got, err := tr.Show(ctx, dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tracker.StatusInProgress || got.Priority != 1 || got.Description != "The ones on the balcony" {
		t.Fatalf("Show = %+v", got)
	}
}

func TestDismissAndReopen(t *testing.T) {
	tr, dir := newRepo(t)
	ctx := context.Background()
	id, err := tr.Create(ctx, dir, tracker.NewIssue{Title: "Renew the library card", Actor: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Dismiss(ctx, dir, id, "alex"); err != nil {
		t.Fatal(err)
	}
	got, err := tr.Show(ctx, dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tracker.StatusClosed || got.CloseReason != "Dismissed" || got.ClosedAt == nil {
		t.Fatalf("after Dismiss: %+v", got)
	}
	for _, l := range got.Labels {
		if l == "dismissed" {
			t.Fatal("the dismissed label is shown as a label")
		}
	}
	if err := tr.Reopen(ctx, dir, id, "alex"); err != nil {
		t.Fatal(err)
	}
	if got, err = tr.Show(ctx, dir, id); err != nil || got.Status != tracker.StatusOpen || got.CloseReason != "" {
		t.Fatalf("after Reopen: %+v, %v", got, err)
	}
}

// Statuses come from the project's config: here a four-column board whose
// last column is Shipped.
func TestCustomStatuses(t *testing.T) {
	tr, dir := newRepo(t)
	ctx := context.Background()
	cfg := filepath.Join(dir, "backlog", "config.yml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `statuses: ["To Do", "In Progress", "Done"]`,
		`statuses: ["Inbox", "Doing", "Review", "Shipped"]`, 1))
	data = []byte(strings.Replace(string(data), `default_status: "To Do"`, `default_status: "Inbox"`, 1))
	if err := os.WriteFile(cfg, data, 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := tr.Create(ctx, dir, tracker.NewIssue{Title: "Call the plumber", Actor: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	run(t, dir, "task", "edit", id, "--status", "Review")
	if got, _ := tr.Show(ctx, dir, id); got.Status != tracker.StatusInProgress {
		t.Fatalf("Review is %q; want in progress", got.Status)
	}
	if err := tr.Close(ctx, dir, id, "", "alex"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run(t, dir, "task", "view", id, "--plain"), "Shipped") {
		t.Fatal("Close did not move the task to Shipped")
	}
	if err := tr.Reopen(ctx, dir, id, "alex"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run(t, dir, "task", "view", id, "--plain"), "Inbox") {
		t.Fatal("Reopen did not move the task to Inbox")
	}
}

// List holds only the tasks for the person, with their parents' titles.
func TestListOnlyHuman(t *testing.T) {
	tr, dir := newRepo(t)
	ctx := context.Background()
	run(t, dir, "task", "create", "Refactor the parser")
	parent := strings.Fields(run(t, dir, "task", "create", "Plan the trip"))[2]
	child, err := tr.Create(ctx, dir, tracker.NewIssue{Title: "Book the train", Parent: parent, Actor: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := tr.List(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != child || list[0].ParentTitle != "Plan the trip" {
		t.Fatalf("List = %+v; want only %s under Plan the trip", list, child)
	}
}

func TestNoteWithRule(t *testing.T) {
	tr, dir := newRepo(t)
	ctx := context.Background()
	id, err := tr.Create(ctx, dir, tracker.NewIssue{Title: "Pick up the parcel", Actor: "alex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.AddComment(ctx, dir, id, "sam-app", "Before\n---\nAfter"); err != nil {
		t.Fatal(err)
	}
	got, err := tr.Show(ctx, dir, id)
	if err != nil || len(got.Comments) != 1 || got.Comments[0].Text != "Before\n---\nAfter" {
		t.Fatalf("Show = %+v, %v", got.Comments, err)
	}
}
