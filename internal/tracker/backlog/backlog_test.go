package backlog_test

import (
	"context"
	"errors"
	"github.com/yourfriendaaron/meat-mode/internal/api"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// newEpicRepo is newRepo with the task types the epics fixture uses.
func newEpicRepo(t *testing.T) (tracker.Epics, string, func(args ...string) string) {
	t.Helper()
	tr, dir := newRepo(t)
	cfg, err := os.OpenFile(filepath.Join(dir, "backlog", "config.yml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.WriteString("\ntypes: [\"epic\", \"task\"]\n"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Close(); err != nil {
		t.Fatal(err)
	}
	bin, _ := exec.LookPath("backlog")
	run := func(args ...string) string {
		cmd := exec.Command(bin, append([]string{"task"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("backlog task %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	return tr.(tracker.Epics), dir, run
}

var createdID = regexp.MustCompile(`(?m)^Task (\S+) - `)

func add(t *testing.T, run func(args ...string) string, title string, args ...string) string {
	t.Helper()
	m := createdID.FindStringSubmatch(run(append([]string{"create", title, "--plain"}, args...)...))
	if m == nil {
		t.Fatalf("backlog task create %q printed no id", title)
	}
	return m[1]
}

func TestEpicsConformance(t *testing.T) {
	trackertest.RunEpics(t, func(t *testing.T) (tracker.Epics, string, trackertest.EpicSeed) {
		tr, dir, run := newEpicRepo(t)
		return tr, dir, trackertest.EpicSeed{
			Add: func(title, desc, typ, parent string, human bool, assignee string) string {
				args := []string{"--type", typ, "-d", desc}
				if parent != "" {
					args = append(args, "--parent", parent)
				}
				if human {
					args = append(args, "--labels", tracker.HumanLabel)
				}
				if assignee != "" {
					args = append(args, "-a", assignee)
				}
				return add(t, run, title, args...)
			},
			Close: func(id string) { run("edit", id, "-s", "Done") },
		}
	})
}

// TestEpicRowsOffBoard checks that done tasks Backlog.md no longer lists,
// those in completed/ and archived ones, still count as closed children,
// and that an archived task that was not done does not.
func TestEpicRowsOffBoard(t *testing.T) {
	tr, dir, run := newEpicRepo(t)
	parent := add(t, run, "Plan the garden", "--type", "epic", "-d", "Beds and paths.")
	completed := add(t, run, "Dig the beds", "--parent", parent)
	humanDone := add(t, run, "Water the beds", "--parent", parent, "--labels", "human")
	archivedDone := add(t, run, "Rake the leaves", "--parent", parent)
	archivedOpen := add(t, run, "Paint the fence", "--parent", parent)
	listed := add(t, run, "Buy the seeds", "--parent", parent)
	run("edit", completed, "-s", "Done")
	run("edit", humanDone, "-s", "Done")
	run("edit", archivedDone, "-s", "Done")
	run("archive", archivedDone)
	run("archive", archivedOpen)
	done := filepath.Join(dir, "backlog", "completed")
	if err := os.MkdirAll(done, 0o755); err != nil {
		t.Fatal(err)
	}
	toCompleted := func(id string) {
		t.Helper()
		files, err := filepath.Glob(filepath.Join(dir, "backlog", "tasks", strings.ToLower(id)+" - *"))
		if err != nil || len(files) != 1 {
			t.Fatalf("task file of %s: %v, %v", id, files, err)
		}
		if err := os.Rename(files[0], filepath.Join(done, filepath.Base(files[0]))); err != nil {
			t.Fatal(err)
		}
	}
	toCompleted(completed)
	toCompleted(humanDone)

	rows, err := tr.EpicRows(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]api.EpicRow{}
	for _, r := range rows {
		got[r.ID] = r
	}
	if len(got) != len(rows) || len(rows) != 5 {
		t.Fatalf("EpicRows = %+v; want the parent, %s, %s, %s and %s", rows, completed, humanDone, archivedDone, listed)
	}
	for id, status := range map[string]string{parent: "open", completed: "closed", humanDone: "closed", archivedDone: "closed", listed: "open"} {
		r := got[id]
		if r.Status != status || (id != parent && r.Parent != parent) {
			t.Errorf("row %s = %+v; want status %s under %s", id, r, status, parent)
		}
	}
	if _, ok := got[archivedOpen]; ok {
		t.Errorf("EpicRows holds %s, archived before it was done", archivedOpen)
	}
	if !got[humanDone].Human {
		t.Errorf("row %s, labelled human, is not marked human", humanDone)
	}
	if got[completed].Human {
		t.Errorf("row %s, without the label, is marked human", completed)
	}
	if got[parent].Description != "Beds and paths." {
		t.Errorf("description %q; want the card's own", got[parent].Description)
	}
}

// wrapBacklog writes a backlog stand-in that notes each call in a log and
// forwards to the real one, except that `task view` answers msg on stderr
// and exits 1 when msg is not empty. It returns the wrapper's path and a
// reader of the calls so far, one line of arguments per call.
func wrapBacklog(t *testing.T, bin, msg string) (string, func() []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	wrap := filepath.Join(dir, "backlog")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\n"
	if msg != "" {
		script += "if [ \"$1\" = task ] && [ \"$2\" = view ]; then echo '" + msg + "' >&2; exit 1; fi\n"
	}
	script += "exec '" + bin + "' \"$@\"\n"
	if err := os.WriteFile(wrap, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return wrap, func() []string {
		data, err := os.ReadFile(log)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

// TestEpicRowsSkipsStrayFiles checks what EpicRows does with files in
// completed/ it cannot use: one whose id is not a task id is left out,
// even when its type would make it a card, and so is one that is not a
// task file at all; neither reaches the CLI nor fails the set.
func TestEpicRowsSkipsStrayFiles(t *testing.T) {
	tr, dir, run := newEpicRepo(t)
	parent := add(t, run, "Plan the roof", "--type", "epic", "-d", "Tiles first.")
	add(t, run, "Check the flashing", "--parent", parent)
	stray := filepath.Join(dir, "backlog", "completed")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	badID := "---\nid: roof 9\ntitle: Strange card\nstatus: To Do\ntype: epic\n---\n\nA card no id names.\n"
	if err := os.WriteFile(filepath.Join(stray, "roof 9 - Strange.md"), []byte(badID), 0o644); err != nil {
		t.Fatal(err)
	}
	notATask := "A note about the roof, with no front matter at all.\n"
	if err := os.WriteFile(filepath.Join(stray, "notes.md"), []byte(notATask), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := tr.EpicRows(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == "roof 9" {
			t.Errorf("EpicRows holds %q, which is not a task id", r.ID)
		}
	}
}

// TestEpicRowsViewFailure checks what a failing `task view` does to the
// set: a card the CLI cannot find keeps an empty description, and any
// other failure fails the whole set.
func TestEpicRowsViewFailure(t *testing.T) {
	_, dir, run := newEpicRepo(t)
	parent := add(t, run, "Plan the balcony", "--type", "epic", "-d", "Boxes first.")
	add(t, run, "Line the boxes", "--parent", parent)
	bin, _ := exec.LookPath("backlog")
	notFound, _ := wrapBacklog(t, bin, "Task not found")
	rows, err := (&backlog.Tracker{Bin: notFound}).EpicRows(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == parent && r.Description != "" {
			t.Errorf("card %s kept the description %q", parent, r.Description)
		}
	}
	locked, _ := wrapBacklog(t, bin, "Error: the store is locked")
	if _, err := (&backlog.Tracker{Bin: locked}).EpicRows(context.Background(), dir); err == nil {
		t.Error("a view failure other than not found did not fail the set")
	}
}

// TestEpicRowsCachesDescriptions checks that a second read of an unchanged
// repo runs no `backlog task view`.
func TestEpicRowsCachesDescriptions(t *testing.T) {
	_, dir, run := newEpicRepo(t)
	parent := add(t, run, "Plan the terrace", "--type", "epic", "-d", "Pots first.")
	add(t, run, "Scrub the pots", "--parent", parent)
	bin, _ := exec.LookPath("backlog")
	counting, calls := wrapBacklog(t, bin, "")
	tr := &backlog.Tracker{Bin: counting}
	views := func() int {
		n := 0
		for _, l := range calls() {
			if strings.HasPrefix(l, "task view") {
				n++
			}
		}
		return n
	}
	rows, err := tr.EpicRows(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == parent && r.Description != "Pots first." {
			t.Errorf("card %s read the description %q", parent, r.Description)
		}
	}
	first := views()
	if first == 0 {
		t.Fatal("the first read viewed no card")
	}
	if _, err := tr.EpicRows(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if got := views(); got != first {
		t.Errorf("a second read of an unchanged repo ran %d views; want %d", got, first)
	}
}
