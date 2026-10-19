package store

import (
	"errors"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

const (
	id7 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a57"
	id8 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a58"
	id9 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a59"
)

// The same sub-task rules hold on the personal list, which the hub applies
// at once, and on a tracker's list, whose actions wait for the bridge.

func TestPersonalSubtasks(t *testing.T) {
	s, c := open(t)
	parent := addPersonal(t, s, id1, "Plan the party", nil)
	c.tick()
	child, _, err := s.AppendAction(api.NewAction{ID: id2, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: "Send invitations", Parent: parent.BeadID}}, "alex-app")
	must(t, err)
	st := mustState(t, s)
	if got := item(t, st, hubKey(child.BeadID)); got.Parent != parent.BeadID {
		t.Fatalf("child parent = %q, want %q", got.Parent, parent.BeadID)
	}

	// One level deep.
	if _, _, err := s.AppendAction(api.NewAction{ID: id3, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: "Buy stamps", Parent: child.BeadID}}, "alex-app"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sub-task of a sub-task: %v", err)
	}
	if _, _, err := s.AppendAction(api.NewAction{ID: id3, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: "Buy stamps", Parent: "mm-404"}}, "alex-app"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown parent: %v", err)
	}

	for i, kind := range []string{api.KindDone, api.KindDismiss} {
		_, _, err := s.AppendAction(api.NewAction{ID: []string{id4, id5}[i], Kind: kind, Key: hubKey(parent.BeadID)}, "alex-app")
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("%s on a parent with an open sub-task: %v", kind, err)
		}
	}
	c.tick()
	_, _, err = s.AppendAction(api.NewAction{ID: id6, Kind: api.KindDone, Key: hubKey(child.BeadID)}, "alex-app")
	must(t, err)
	c.tick()
	_, _, err = s.AppendAction(api.NewAction{ID: id7, Kind: api.KindDone, Key: hubKey(parent.BeadID)}, "alex-app")
	must(t, err)
	if got := item(t, mustState(t, s), hubKey(parent.BeadID)); got.Status != "closed" {
		t.Fatalf("parent status %q after its sub-task closed", got.Status)
	}
	// A closed parent takes no new sub-task.
	if _, _, err := s.AppendAction(api.NewAction{ID: id8, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: "Book a cake", Parent: parent.BeadID}}, "alex-app"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sub-task of a closed item: %v", err)
	}
}

func TestBridgeSubtasks(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open", Priority: 2},
		{ID: "gd-1.1", Title: "Fill the can", Status: "open", Priority: 2, Parent: "gd-1"},
		{ID: "gd-1.2", Title: "Find the hose", Status: "closed", Priority: 2, Parent: "gd-1"},
		// A sub-task whose parent the tracker does not send.
		{ID: "gd-9.1", Title: "Pick a fence", Status: "open", Priority: 2, Parent: "gd-9", ParentTitle: "Garden plan"},
	}))
	st := mustState(t, s)
	if got := item(t, st, "laptop/garden/gd-9.1"); got.Parent != "gd-9" || got.ParentTitle != "Garden plan" {
		t.Fatalf("orphan sub-task = %+v", got)
	}

	// The add carries its parent to the bridge and onto the provisional item.
	add, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Buy a watering can", Parent: "gd-1"}}, "alex-app")
	must(t, err)
	if add.Payload.Parent != "gd-1" {
		t.Fatalf("add payload parent = %q", add.Payload.Parent)
	}
	if got := item(t, mustState(t, s), "laptop/garden/"+provisionalPrefix+id1); got.Parent != "gd-1" {
		t.Fatalf("provisional parent = %q", got.Parent)
	}
	if _, _, err := s.AppendAction(api.NewAction{ID: id2, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "x", Parent: "gd-1.1"}}, "alex-app"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sub-task of a sub-task: %v", err)
	}

	// Open: gd-1.1 and the sub-task being added.
	if _, _, err := s.AppendAction(api.NewAction{ID: id2, Kind: api.KindDismiss, Key: "laptop/garden/gd-1"}, "alex-app"); !errors.Is(err, ErrConflict) {
		t.Fatalf("dismiss on a parent with open sub-tasks: %v", err)
	}
	c.tick()
	must(t, s.ReportResult("laptop", id1, api.ActionResult{OK: true, BeadID: "gd-1.3"}))
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open", Priority: 2},
		{ID: "gd-1.1", Title: "Fill the can", Status: "open", Priority: 2, Parent: "gd-1"},
		{ID: "gd-1.3", Title: "Buy a watering can", Status: "open", Priority: 2, Parent: "gd-1"},
	}))

	// A pending done or dismiss on a sub-task counts as closed, so the app
	// can send the sub-tasks' closes and then the parent's in one go; the
	// bridge applies them in that order. A later pending reopen undoes that.
	_, _, err = s.AppendAction(api.NewAction{ID: id3, Kind: api.KindDismiss, Key: "laptop/garden/gd-1.1"}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id4, Kind: api.KindDone, Key: "laptop/garden/gd-1.3"}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id5, Kind: api.KindReopen, Key: "laptop/garden/gd-1.3"}, "alex-app")
	must(t, err)
	if _, _, err := s.AppendAction(api.NewAction{ID: id6, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, "alex-app"); !errors.Is(err, ErrConflict) {
		t.Fatalf("done after a sub-task's reopen: %v", err)
	}
	_, _, err = s.AppendAction(api.NewAction{ID: id7, Kind: api.KindDone, Key: "laptop/garden/gd-1.3"}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id8, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, "alex-app")
	must(t, err)

	// Parents are checked like ids.
	bad := []api.Item{{ID: "gd-5", Title: "x", Status: "open", Parent: "gd-5"}}
	if err := s.ReplaceItems("laptop", "garden", bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an item that is its own parent: %v", err)
	}
	if _, _, err := s.AppendAction(api.NewAction{ID: id9, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "x", Parent: "a/b"}}, "alex-app"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("parent with a slash: %v", err)
	}
}

// TestAddedSubtaskCountsUntilSynced: between the bridge creating a sub-task
// and sending it, the hub holds no item for it, and a tracker may close the
// parent regardless. The applied add keeps the parent from closing until
// the repo's next set.
func TestAddedSubtaskCountsUntilSynced(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Fill the can", Parent: "gd-1"}}, "alex-app")
	must(t, err)
	c.tick()
	must(t, s.ReportResult("laptop", id1, api.ActionResult{OK: true, BeadID: "gd-1.1"}))
	c.tick()
	if _, _, err := s.AppendAction(api.NewAction{ID: id2, Kind: api.KindDismiss, Key: "laptop/garden/gd-1"}, "alex-app"); !errors.Is(err, ErrConflict) {
		t.Fatalf("dismiss before the sub-task synced: %v", err)
	}
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open", Priority: 2},
		{ID: "gd-1.1", Title: "Fill the can", Status: "closed", Priority: 2, Parent: "gd-1"},
	}))
	_, _, err = s.AppendAction(api.NewAction{ID: id3, Kind: api.KindDismiss, Key: "laptop/garden/gd-1"}, "alex-app")
	must(t, err)
}
