// Package trackertest holds the conformance suite every tracker runs, so
// the bridge can treat them all the same.
package trackertest

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Setup returns a tracker and a fresh repo directory its Detect matches,
// with no tasks yet. It skips the test when the tracker's tool is missing.
type Setup func(t *testing.T) (tracker.Tracker, string)

// Run checks a tracker against the contract in docs/trackers.md. Every
// tracker must support add; the other capabilities are checked when Info
// lists them, and must return tracker.ErrUnsupported when it does not.
func Run(t *testing.T, name string, setup Setup) {
	ctx := context.Background()
	t.Run("info", func(t *testing.T) {
		tr, dir := setup(t)
		if err := tracker.Validate(name, tr); err != nil {
			t.Fatal(err)
		}
		if !tr.Info().Can(api.KindAdd) {
			t.Fatal("every tracker must support add")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !tr.Detect(dir, entries) {
			t.Fatal("Detect does not match the setup's repo")
		}
		if _, err := tr.Check(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("create, list, show", func(t *testing.T) {
		tr, dir := setup(t)
		action := uuid.NewString()
		due := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
		id := create(t, tr, dir, tracker.NewIssue{Title: "Water the plants", Due: &due, ActionID: action, Actor: "alex"})
		got := listed(t, tr, dir, id)
		if got.Title != "Water the plants" || got.Status != tracker.StatusOpen || got.Version == "" {
			t.Fatalf("listed %+v; want the title, status open and a version", got)
		}
		if got.DueAt == nil {
			t.Fatal("the due date was not kept")
		}
		shown, err := tr.Show(ctx, dir, id)
		if err != nil || shown.ID != id || shown.Title != "Water the plants" {
			t.Fatalf("Show = %+v, %v", shown, err)
		}
		if _, err := tr.Show(ctx, dir, "no-such-task"); !errors.Is(err, tracker.ErrNotFound) {
			t.Fatalf("Show of a missing id = %v; want ErrNotFound", err)
		}
		if found, err := tr.FindByActionID(ctx, dir, action); err != nil || found != id {
			t.Fatalf("FindByActionID = %q, %v; want %q", found, err, id)
		}
		if found, err := tr.FindByActionID(ctx, dir, uuid.NewString()); err != nil || found != "" {
			t.Fatalf("FindByActionID of another action = %q, %v; want none", found, err)
		}
	})

	t.Run("a title starting with a dash", func(t *testing.T) {
		tr, dir := setup(t)
		id := create(t, tr, dir, tracker.NewIssue{Title: "-v Water the plants", Actor: "alex"})
		if got := listed(t, tr, dir, id); got.Title != "-v Water the plants" {
			t.Fatalf("title %q; want it as given", got.Title)
		}
	})

	t.Run("close and reopen", func(t *testing.T) {
		tr, dir := setup(t)
		id := create(t, tr, dir, tracker.NewIssue{Title: "Book the dentist", Actor: "alex"})
		before := listed(t, tr, dir, id).Version
		if !check(t, tr, api.KindDone, tr.Close(ctx, dir, id, "Done in Meat Mode", "alex")) {
			return
		}
		closed := listed(t, tr, dir, id)
		if closed.Status != tracker.StatusClosed || closed.Version == before {
			t.Fatalf("after Close: status %q, version changed %v", closed.Status, closed.Version != before)
		}
		if check(t, tr, api.KindReopen, tr.Reopen(ctx, dir, id, "alex")) {
			if got := listed(t, tr, dir, id); got.Status == tracker.StatusClosed {
				t.Fatal("still closed after Reopen")
			}
		}
	})

	t.Run("dismiss", func(t *testing.T) {
		tr, dir := setup(t)
		id := create(t, tr, dir, tracker.NewIssue{Title: "Renew the library card", Actor: "alex"})
		if check(t, tr, api.KindDismiss, tr.Dismiss(ctx, dir, id, "alex")) {
			if got := listed(t, tr, dir, id); got.Status != tracker.StatusClosed {
				t.Fatalf("status %q after Dismiss; want closed", got.Status)
			}
		}
	})

	t.Run("snooze", func(t *testing.T) {
		tr, dir := setup(t)
		id := create(t, tr, dir, tracker.NewIssue{Title: "Call the plumber", Actor: "alex"})
		until := time.Now().Add(72 * time.Hour).Truncate(time.Minute)
		if !check(t, tr, api.KindSnooze, tr.Defer(ctx, dir, id, &until, "alex")) {
			return
		}
		if got := listed(t, tr, dir, id); got.DeferUntil == nil {
			t.Fatal("no defer time after Defer")
		}
		if err := tr.Defer(ctx, dir, id, nil, "alex"); err != nil {
			t.Fatal(err)
		}
		if got := listed(t, tr, dir, id); got.DeferUntil != nil {
			t.Fatal("defer time kept after clearing it")
		}
	})

	t.Run("note", func(t *testing.T) {
		tr, dir := setup(t)
		id := create(t, tr, dir, tracker.NewIssue{Title: "Pick up the parcel", Actor: "alex"})
		before := listed(t, tr, dir, id).Version
		// The rule line is one Backlog.md reserves; it must still read back.
		text := "It is at the post office.\n\n---\n\nBring an ID."
		if !check(t, tr, api.KindNote, tr.AddComment(ctx, dir, id, "sam-app", text)) {
			return
		}
		if listed(t, tr, dir, id).Version == before {
			t.Fatal("a new note did not change the version")
		}
		shown, err := tr.Show(ctx, dir, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range shown.Comments {
			// The replay journal finds a note it may already have written by
			// its author and text, so both must come back as written.
			if c.Author == "sam-app" && c.Text == text {
				return
			}
		}
		t.Fatalf("note not found as written in %+v", shown.Comments)
	})

	t.Run("subtask", func(t *testing.T) {
		tr, dir := setup(t)
		parent := create(t, tr, dir, tracker.NewIssue{Title: "Plan the trip", Actor: "alex"})
		id, err := tr.Create(ctx, dir, tracker.NewIssue{Title: "Book the train", Parent: parent, Actor: "alex"})
		if !check(t, tr, tracker.CapSubtask, err) {
			return
		}
		if got := listed(t, tr, dir, id); got.Parent != parent {
			t.Fatalf("parent %q; want %q", got.Parent, parent)
		}
		if err := tr.Close(ctx, dir, parent, "Done in Meat Mode", "alex"); err != nil && !errors.Is(err, tracker.ErrOpenChildren) {
			t.Fatalf("closing a parent with an open sub-task: %v; want success or ErrOpenChildren", err)
		}
	})
}

// check reports whether the tracker supports capability, failing the test
// when err does not match what Info says.
func check(t *testing.T, tr tracker.Tracker, capability string, err error) bool {
	t.Helper()
	if !tr.Info().Can(capability) {
		if !errors.Is(err, tracker.ErrUnsupported) {
			t.Fatalf("%s is not in Info.Actions but returned %v; want ErrUnsupported", capability, err)
		}
		return false
	}
	if err != nil {
		t.Fatalf("%s: %v", capability, err)
	}
	return true
}

func create(t *testing.T, tr tracker.Tracker, dir string, in tracker.NewIssue) string {
	t.Helper()
	id, err := tr.Create(context.Background(), dir, in)
	if err != nil || id == "" {
		t.Fatalf("Create = %q, %v", id, err)
	}
	return id
}

// listed returns the task with id from List, failing when it is missing.
func listed(t *testing.T, tr tracker.Tracker, dir, id string) tracker.Issue {
	t.Helper()
	list, err := tr.List(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range list {
		if i.ID == id {
			return i
		}
	}
	t.Fatalf("%s not in List", id)
	return tracker.Issue{}
}
