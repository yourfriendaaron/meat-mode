package trackertest

import (
	"context"
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// EpicSeed makes the tasks of the epics fixture with the tracker's own tool.
// Add creates a task and returns its id: desc is its description, typ
// "epic" or "task", parent the id of its parent or empty, human whether it
// carries HumanLabel, assignee its assignee or empty (a tool may give an
// empty one a default). Close closes a task.
type EpicSeed struct {
	Add   func(title, desc, typ, parent string, human bool, assignee string) string
	Close func(id string)
}

// EpicSetup returns a tracker that implements tracker.Epics, a fresh repo
// directory it detects, and the tools to fill it. It skips the test when
// the tracker's tool is missing.
type EpicSetup func(t *testing.T) (tracker.Epics, string, EpicSeed)

// RunEpics checks tracker.Epics against the fixture below: the same tasks
// in every tracker, made with its own tool. The fixture holds:
//
//	Epic      an open epic, with children
//	  Done      a closed task
//	  Open      an open task
//	  Human     an open task labelled human, assigned to sam
//	  Nested    an open epic, with a child
//	    NestedKid
//	Plan      an open task that is no epic but has a child
//	  PlanKid
//	Solo      an open task with no parent
//	Retired   a closed epic, with a closed child
//	  RetiredKid
//
// Every task has its own description. EpicRows must return all eleven
// with their type, status, parent and human mark, and tracker.Outline of
// them must pick what docs/api.md (Epics) describes, every row it keeps
// with its description.
func RunEpics(t *testing.T, setup EpicSetup) {
	tr, dir, seed := setup(t)
	desc := func(title string) string { return "Notes on: " + title + "." }
	epic := seed.Add("Plan the garden", desc("Plan the garden"), "epic", "", false, "")
	done := seed.Add("Dig the beds", desc("Dig the beds"), "task", epic, false, "")
	seed.Close(done)
	open := seed.Add("Buy the seeds", desc("Buy the seeds"), "task", epic, false, "")
	human := seed.Add("Approve the layout", desc("Approve the layout"), "task", epic, true, "sam")
	nested := seed.Add("Build the shed", desc("Build the shed"), "epic", epic, false, "")
	nestedKid := seed.Add("Order the timber", desc("Order the timber"), "task", nested, false, "")
	plan := seed.Add("Water the plants", desc("Water the plants"), "task", "", false, "")
	planKid := seed.Add("Fill the can", desc("Fill the can"), "task", plan, false, "")
	solo := seed.Add("Sweep the path", desc("Sweep the path"), "task", "", false, "")
	retired := seed.Add("Plan the greenhouse", desc("Plan the greenhouse"), "epic", "", false, "")
	retiredKid := seed.Add("Measure the roof", desc("Measure the roof"), "task", retired, false, "")
	seed.Close(retiredKid)
	seed.Close(retired)

	rows, err := tr.EpicRows(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]api.EpicRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if len(rows) != len(byID) || len(rows) != 11 {
		t.Fatalf("EpicRows returned %d rows (%d distinct); want the 11 of the fixture: %+v", len(rows), len(byID), rows)
	}
	type want struct {
		id, title, typ, status, parent, assignee string
		human                                    bool
	}
	for _, w := range []want{
		{epic, "Plan the garden", "epic", "open", "", "", false},
		{done, "Dig the beds", "task", "closed", epic, "", false},
		{open, "Buy the seeds", "task", "open", epic, "", false},
		{human, "Approve the layout", "task", "open", epic, "sam", true},
		{nested, "Build the shed", "epic", "open", epic, "", false},
		{nestedKid, "Order the timber", "task", "open", nested, "", false},
		{plan, "Water the plants", "task", "open", "", "", false},
		{planKid, "Fill the can", "task", "open", plan, "", false},
		{solo, "Sweep the path", "task", "open", "", "", false},
		{retired, "Plan the greenhouse", "epic", "closed", "", "", false},
		{retiredKid, "Measure the roof", "task", "closed", retired, "", false},
	} {
		r := byID[w.id]
		if w.assignee == "" {
			// A tool may give a task with none a default, such as ticket's
			// git user.
			r.Assignee = ""
		}
		got := want{r.ID, r.Title, r.Type, r.Status, r.Parent, r.Assignee, r.Human}
		if got != w {
			t.Errorf("row %s = %+v; want %+v", w.id, got, w)
		}
		if r.Epic {
			t.Errorf("row %s has Epic set; EpicRows leaves it to Outline", w.id)
		}
		if r.UpdatedAt.IsZero() {
			t.Errorf("row %s has no UpdatedAt", w.id)
		}
		if (w.status == "closed") != (r.ClosedAt != nil) {
			t.Errorf("row %s status %s but ClosedAt %v", w.id, r.Status, r.ClosedAt)
		}
	}
	out := tracker.Outline(rows)
	var ids []string
	cards := map[string]bool{}
	parents := map[string]string{}
	for _, r := range out {
		if want := desc(byID[r.ID].Title); r.Description != want {
			t.Errorf("Outline: %s description = %q; want %q", r.ID, r.Description, want)
		}
		ids = append(ids, r.ID)
		if r.Epic {
			cards[r.ID] = true
		}
		parents[r.ID] = r.Parent
	}
	slices.Sort(ids)
	wantIDs := []string{epic, done, open, human, nested, nestedKid, plan, planKid, solo}
	slices.Sort(wantIDs)
	if !slices.Equal(ids, wantIDs) {
		t.Fatalf("Outline kept %v; want %v (the closed epic and its child are left out)", ids, wantIDs)
	}
	for id, w := range map[string]bool{epic: true, nested: true, plan: true, done: false, open: false, human: false, nestedKid: false, planKid: false, solo: false} {
		if cards[id] != w {
			t.Errorf("Outline: %s card = %v; want %v", id, cards[id], w)
		}
	}
	for id, w := range map[string]string{done: epic, open: epic, human: epic, nested: epic, nestedKid: nested, planKid: plan, epic: "", plan: "", solo: ""} {
		if parents[id] != w {
			t.Errorf("Outline: %s parent = %q; want %q", id, parents[id], w)
		}
	}
}
