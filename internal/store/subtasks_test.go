package store

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

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

// addSubtask adds a sub-task of parent to the personal list and returns its
// key.
func addSubtask(t *testing.T, s *Store, title, parent string) string {
	t.Helper()
	a, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: title, Parent: parent}}, "alex-app")
	must(t, err)
	return hubKey(a.BeadID)
}

// tryEdit appends an edit and returns its error.
func tryEdit(s *Store, key string, set api.Edit) error {
	_, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindEdit, Key: key,
		Payload: api.Payload{Set: &set}}, "alex-app")
	return err
}

func TestMoveSubtask(t *testing.T) {
	s, _ := open(t)
	party := addPersonal(t, s, id1, "Plan the party", nil).BeadID
	invites := hubKey(addPersonal(t, s, id2, "Send invitations", nil).BeadID)
	hall := hubKey(addPersonal(t, s, id3, "Book a hall", nil).BeadID)

	// A move records the old parent, "" for top-level, and its Undo moves
	// it back.
	a := edit(t, s, invites, api.Edit{Parent: ptr(party)})
	if got := item(t, mustState(t, s), invites); got.Parent != party {
		t.Fatalf("parent after the move = %q", got.Parent)
	}
	if w := a.Payload.Was; w == nil || w.Parent == nil || *w.Parent != "" || w.Title != nil {
		t.Fatalf("was %+v", w)
	}
	undo := edit(t, s, invites, *a.Payload.Was)
	if got := item(t, mustState(t, s), invites); got.Parent != "" {
		t.Fatalf("parent after the undo = %q", got.Parent)
	}
	if w := undo.Payload.Was; w.Parent == nil || *w.Parent != party {
		t.Fatalf("undo's was %+v", w)
	}
	edit(t, s, invites, api.Edit{Parent: ptr(" " + party + " ")})

	_, _, inviteID, _ := SplitKey(invites)
	_, _, hallID, _ := SplitKey(hall)
	for _, tc := range []struct {
		name   string
		key    string
		parent string
		want   error
	}{
		{"its own parent", hall, hallID, ErrInvalid},
		{"under a sub-task", hall, inviteID, ErrInvalid},
		{"a parent under another item", hubKey(party), hallID, ErrInvalid},
		{"under an unknown item", hall, "mm-404", ErrNotFound},
		{"a parent with a slash", hall, "a/b", ErrInvalid},
	} {
		if err := tryEdit(s, tc.key, api.Edit{Parent: ptr(tc.parent)}); (tc.want == nil && err != nil) || !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}

	// A closed sub-task still makes its parent one.
	act(t, s, api.KindDone, invites, api.Payload{})
	if err := tryEdit(s, hubKey(party), api.Edit{Parent: ptr(hallID)}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a parent of a closed sub-task moved: %v", err)
	}
	// A closed item takes no sub-task.
	act(t, s, api.KindDone, hall, api.Payload{})
	tea := hubKey(addPersonal(t, s, id4, "Buy tea", nil).BeadID)
	if err := tryEdit(s, tea, api.Edit{Parent: ptr(hallID)}); !errors.Is(err, ErrInvalid) {
		t.Errorf("moved under a closed item: %v", err)
	}
	// A closed sub-task moves to the top level.
	edit(t, s, invites, api.Edit{Parent: ptr("")})
	if got := item(t, mustState(t, s), invites); got.Parent != "" || got.Status != "closed" {
		t.Fatalf("after the move to the top %+v", got)
	}
	// Once its sub-task has gone, an item can become one.
	_, _, teaID, _ := SplitKey(tea)
	edit(t, s, invites, api.Edit{Parent: ptr(teaID)})
	edit(t, s, hubKey(party), api.Edit{Parent: ptr(teaID)})

	// An agent's item is moved in its tracker.
	_, err := s.CreateBridge("laptop")
	must(t, err)
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true}}))
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Title: "Plant the beans", Status: "open", Priority: 2},
		{ID: "gd-2", Title: "Water the beans", Status: "open", Priority: 2},
	}))
	if _, _, err := s.AppendAction(api.NewAction{ID: id5, Kind: api.KindEdit, Key: "laptop/garden/gd-2",
		Payload: api.Payload{Set: &api.Edit{Parent: ptr("gd-1")}}}, "alex-app"); !errors.Is(err, ErrInvalid) {
		t.Errorf("moved an agent's item: %v", err)
	}
}

func TestAgentMovesAnyItem(t *testing.T) {
	s, _ := open(t)
	for _, name := range []string{"laptop", "desk"} {
		_, err := s.CreateBridge(name)
		must(t, err)
	}
	add := func(id, title, bridge string) string {
		key, _, err := s.AddPersonal(api.PersonalTodo{ID: id, Title: title}, bridge, "")
		must(t, err)
		_, _, item, _ := SplitKey(key)
		return item
	}
	trip := add(id1, "Plan the trip", "laptop")
	tickets := add(id2, "Book the tickets", "laptop")
	other := add(id3, "Pack the bags", "desk")
	app := addPersonal(t, s, id4, "Water the plants", nil).BeadID

	// Under another machine's item and the app's, then back under its own.
	for _, parent := range []string{other, app, trip} {
		a, _, err := s.ChangePersonal("laptop", "", tickets, api.PersonalChange{Kind: api.KindEdit,
			Payload: api.Payload{Set: &api.Edit{Parent: &parent}}})
		if err != nil {
			t.Fatalf("move under %s: %v", parent, err)
		}
		if w := a.Payload.Was; w == nil || w.Parent == nil {
			t.Fatalf("agent move's was %+v", w)
		}
	}
	missing := "mm-404"
	if _, _, err := s.ChangePersonal("laptop", "", tickets, api.PersonalChange{Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{Parent: &missing}}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("move under an unknown item: %v", err)
	}
	if _, _, err := s.ChangePersonal("laptop", "", trip, api.PersonalChange{Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{SubtasksDone: []string{missing}}}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("closing an unknown sub-task: %v", err)
	}
	if _, _, err := s.ChangePersonal("laptop", "", trip, api.PersonalChange{Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{SubtasksDone: []string{app}}}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("closing an item that is not a sub-task: %v", err)
	}
	_, _, err := s.ChangePersonal("desk", "", trip, api.PersonalChange{Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{SubtasksDone: []string{tickets}}}})
	must(t, err)
	if it, err := s.PersonalItem("", tickets); err != nil || it.Status != "closed" || it.CloseReason != ReasonDone {
		t.Fatalf("sub-task after subtasksDone %+v, %v", it, err)
	}
}

func TestAgentAddsASubtask(t *testing.T) {
	s, _ := open(t)
	for _, name := range []string{"laptop", "desk"} {
		_, err := s.CreateBridge(name)
		must(t, err)
	}
	add := func(id, bridge, project string, todo api.PersonalTodo) (string, error) {
		todo.ID = id
		key, _, err := s.AddPersonal(todo, bridge, project)
		_, _, item, _ := SplitKey(key)
		return item, err
	}
	trip, err := add(id1, "laptop", "", api.PersonalTodo{Title: "Plan the trip"})
	must(t, err)
	tickets, err := add(id2, "laptop", "", api.PersonalTodo{Title: "Book the tickets", Parent: trip})
	must(t, err)
	if it, err := s.PersonalItem("", tickets); err != nil || it.Parent != trip {
		t.Fatalf("sub-task %+v, %v", it, err)
	}
	other, err := add(id3, "desk", "", api.PersonalTodo{Title: "Pack the bags"})
	must(t, err)
	app := addPersonal(t, s, id4, "Water the plants", nil).BeadID
	garden, err := add(id5, "laptop", "projects/garden", api.PersonalTodo{Title: "Plant the beans"})
	must(t, err)
	done, err := add(id6, "laptop", "", api.PersonalTodo{Title: "Buy a map"})
	must(t, err)
	_, _, err = s.ChangePersonal("laptop", "", done, api.PersonalChange{Kind: api.KindDone})
	must(t, err)

	for _, tc := range []struct {
		name, parent string
		want         error
	}{
		{"another machine's item", other, nil},
		{"the app's item", app, nil},
		{"an item in another list", garden, ErrNotFound},
		{"an unknown item", "mm-404", ErrNotFound},
		{"a closed item", done, ErrInvalid},
		{"a sub-task", tickets, ErrInvalid},
	} {
		if _, err := add(uuid.NewString(), "laptop", "", api.PersonalTodo{Title: "Find the passports", Parent: tc.parent}); (tc.want == nil && err != nil) || !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	// A retry returns the sub-task it added, though its parent has closed.
	_, _, err = s.ChangePersonal("laptop", "", tickets, api.PersonalChange{Kind: api.KindDone})
	must(t, err)
	_, _, err = s.ChangePersonal("laptop", "", trip, api.PersonalChange{Kind: api.KindDone})
	must(t, err)
	if again, err := add(id2, "laptop", "", api.PersonalTodo{Title: "Book the tickets", Parent: trip}); err != nil || again != tickets {
		t.Fatalf("retry = %q, %v", again, err)
	}
	// In a project too.
	if _, err := add(id7, "laptop", "projects/garden", api.PersonalTodo{Title: "Water the beans", Parent: garden}); err != nil {
		t.Fatalf("sub-task in a project: %v", err)
	}
}

func TestRepeatingParentReopensDoneSubtasks(t *testing.T) {
	s, c := open(t)
	due := c.t.Add(time.Hour)
	parent := addRepeating(t, s, "Clean the house", due, api.Repeat{Every: api.EveryWeek, TimeZone: "UTC"})
	_, _, parentID, _ := SplitKey(parent)
	floors := addSubtask(t, s, "Mop the floors", parentID)
	windows := addSubtask(t, s, "Wash the windows", parentID)
	oven := addSubtask(t, s, "Clean the oven", parentID)
	_, _, floorsID, _ := SplitKey(floors)
	_, _, ovenID, _ := SplitKey(oven)

	// The oven was done long enough ago to have left the app.
	act(t, s, api.KindDone, oven, api.Payload{})
	c.t = c.t.Add(8 * 24 * time.Hour)
	if hasItem(mustState(t, s), oven) {
		t.Fatal("a sub-task closed eight days ago is still shown")
	}
	act(t, s, api.KindDone, floors, api.Payload{})
	act(t, s, api.KindDismiss, windows, api.Payload{})

	c.tick()
	a := act(t, s, api.KindDone, parent, api.Payload{})
	w := a.Payload.Was
	want := []string{floorsID, ovenID}
	slices.Sort(want)
	if w == nil || !slices.Equal(w.SubtasksDone, want) {
		t.Fatalf("done's was %+v, want subtasksDone %v", w, want)
	}
	st := mustState(t, s)
	for _, key := range []string{floors, oven} {
		if it := item(t, st, key); it.Status != "open" || it.CloseReason != "" || it.ClosedAt != nil {
			t.Errorf("%s after the parent's done %+v", key, it)
		}
	}
	if it := item(t, st, windows); it.Status != "closed" || it.CloseReason != ReasonDismissed {
		t.Errorf("dismissed sub-task after the parent's done %+v", it)
	}
	stored, err := s.Action(a.ID)
	must(t, err)
	if !slices.Equal(stored.Payload.Was.SubtasksDone, want) {
		t.Errorf("stored was %+v", stored.Payload.Was)
	}

	// The Undo closes them again and puts the due time back.
	c.tick()
	edit(t, s, parent, *w)
	st = mustState(t, s)
	for _, key := range []string{floors, oven} {
		if it := item(t, st, key); it.Status != "closed" || it.CloseReason != ReasonDone || !it.ClosedAt.Equal(c.t) {
			t.Errorf("%s after the undo %+v", key, it)
		}
	}
	if it := item(t, st, parent); it.Status != "open" || !it.DueAt.Equal(due) {
		t.Fatalf("parent after the undo %+v", it)
	}

	// subtasksDone takes only the item's own sub-tasks.
	other := hubKey(addPersonal(t, s, id1, "Plan the party", nil).BeadID)
	_, _, otherID, _ := SplitKey(other)
	cake := addSubtask(t, s, "Order a cake", otherID)
	_, _, cakeID, _ := SplitKey(cake)
	for _, ids := range [][]string{{cakeID}, {otherID}, {parentID}, {"mm-404"}, {"a/b"}} {
		if err := tryEdit(s, parent, api.Edit{SubtasksDone: ids}); !errors.Is(err, ErrInvalid) {
			t.Errorf("subtasksDone %v: %v", ids, err)
		}
	}
	if it := item(t, mustState(t, s), cake); it.Status != "open" {
		t.Fatalf("another item's sub-task closed: %+v", it)
	}

	// A done that ends the series reopens nothing.
	last := c.t.Add(time.Hour)
	once := addRepeating(t, s, "Water the plants", last, api.Repeat{Every: api.EveryDay, TimeZone: "UTC", Until: last.Format(time.DateOnly)})
	_, _, onceID, _ := SplitKey(once)
	can := addSubtask(t, s, "Fill the can", onceID)
	act(t, s, api.KindDone, can, api.Payload{})
	if a := act(t, s, api.KindDone, once, api.Payload{}); a.Payload.Was != nil {
		t.Fatalf("series-over done %+v", a)
	}
	if it := item(t, mustState(t, s), can); it.Status != "closed" {
		t.Fatalf("sub-task after the series ended %+v", it)
	}
}
