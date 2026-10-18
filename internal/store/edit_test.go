package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func edit(t *testing.T, s *Store, key string, set api.Edit) api.Action {
	t.Helper()
	a, created, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindEdit, Key: key,
		Payload: api.Payload{Set: &set}}, "alex-app")
	must(t, err)
	if !created || a.Status != api.StatusApplied {
		t.Fatalf("edit = %+v, created %v", a, created)
	}
	return a
}

func TestEditPersonalItem(t *testing.T) {
	s, c := open(t)
	due := c.t.Add(3 * time.Hour)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", &due).BeadID)

	newDue := c.t.Add(5 * time.Hour)
	c.tick()
	a := edit(t, s, key, api.Edit{
		Title:       ptr("  Water the ferns  "),
		Description: ptr("The ones on the porch."),
		Due:         &newDue,
		Priority:    ptr(0),
		Checklist:   &[]api.Check{{Text: " Fill the can "}, {ID: "c2", Text: "Check the soil", Done: true}},
	})
	it := item(t, mustState(t, s), key)
	if it.Title != "Water the ferns" || it.Description != "The ones on the porch." || it.DueAt == nil || !it.DueAt.Equal(newDue) ||
		it.Priority != 0 || len(it.Checklist) != 2 || it.Checklist[0].Text != "Fill the can" || it.Checklist[0].ID == "" ||
		it.Checklist[1] != (api.Check{ID: "c2", Text: "Check the soil", Done: true}) || !it.UpdatedAt.Equal(c.t) {
		t.Fatalf("edited item %+v", it)
	}
	// The returned action and the stored one carry the old values of exactly
	// the fields set named, and the ids the hub gave.
	w := a.Payload.Was
	if w == nil || *w.Title != "Water the plants" || *w.Description != "" || w.Due == nil || !w.Due.Equal(due) || w.ClearDue ||
		*w.Priority != 2 || w.Checklist == nil || len(*w.Checklist) != 0 {
		t.Fatalf("was = %+v", w)
	}
	if got := (*a.Payload.Set.Checklist)[0].ID; got != it.Checklist[0].ID {
		t.Errorf("set's checklist id %q, item's %q", got, it.Checklist[0].ID)
	}
	stored, err := s.Action(a.ID)
	must(t, err)
	if b1, b2 := mustJSON(t, stored.Payload), mustJSON(t, a.Payload); b1 != b2 {
		t.Errorf("stored payload %s, returned %s", b1, b2)
	}
	if raw := mustJSON(t, a.Payload.Was); !strings.Contains(raw, `"checklist":[]`) || !strings.Contains(raw, `"description":""`) {
		t.Errorf("was on the wire %s", raw)
	}

	// Undo is an edit with set = was, and it records the edit's values.
	undo := edit(t, s, key, *a.Payload.Was)
	it = item(t, mustState(t, s), key)
	if it.Title != "Water the plants" || it.Description != "" || !it.DueAt.Equal(due) || it.Priority != 2 || it.Checklist != nil {
		t.Fatalf("after undo %+v", it)
	}
	if w := undo.Payload.Was; *w.Title != "Water the ferns" || !w.Due.Equal(newDue) || len(*w.Checklist) != 2 {
		t.Fatalf("undo's was %+v", w)
	}

	// Clearing the due date records clearDue for its undo when there was none.
	a = edit(t, s, key, api.Edit{ClearDue: true})
	if it := item(t, mustState(t, s), key); it.DueAt != nil {
		t.Fatalf("due not cleared: %+v", it)
	}
	if w := a.Payload.Was; w.Due == nil || !w.Due.Equal(due) || w.ClearDue || w.Title != nil || w.Priority != nil {
		t.Fatalf("was after clearing = %+v", w)
	}
	a = edit(t, s, key, api.Edit{Due: &due})
	if w := a.Payload.Was; w.Due != nil || !w.ClearDue {
		t.Fatalf("was for an unset due = %+v", w)
	}
}

func TestEditRefuses(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", nil).BeadID)
	now := time.Now()
	long := strings.Repeat("a", MaxTitleLen+1)
	many := make([]api.Check, MaxChecklist+1)
	for i := range many {
		many[i] = api.Check{Text: "Step"}
	}
	for name, set := range map[string]*api.Edit{
		"no set":            nil,
		"nothing set":       {},
		"empty title":       {Title: ptr("  ")},
		"long title":        {Title: &long},
		"long description":  {Description: ptr(strings.Repeat("a", MaxNoteLen+1))},
		"due and clearDue":  {Due: &now, ClearDue: true},
		"priority too low":  {Priority: ptr(-1)},
		"priority too high": {Priority: ptr(5)},
		"too many entries":  {Checklist: &many},
		"empty entry":       {Checklist: &[]api.Check{{Text: " "}}},
		"long entry":        {Checklist: &[]api.Check{{Text: long}}},
		"repeated entry id": {Checklist: &[]api.Check{{ID: "c1", Text: "One"}, {ID: "c1", Text: "Two"}}},
	} {
		_, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindEdit, Key: key,
			Payload: api.Payload{Set: set}}, "alex-app")
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	// A limit is fine.
	full := make([]api.Check, MaxChecklist)
	for i := range full {
		full[i] = api.Check{Text: "Step"}
	}
	edit(t, s, key, api.Edit{Checklist: &full, Priority: ptr(4), Description: ptr(strings.Repeat("a", MaxNoteLen))})

	// None of the personal-only kinds touches an agent's item, and none
	// reaches the bridge.
	agent := ItemKey("laptop", "garden", "gd-1")
	for _, na := range []api.NewAction{
		{Kind: api.KindEdit, Payload: api.Payload{Set: &api.Edit{Title: ptr("Water the ferns")}}},
		{Kind: api.KindNoteEdit, Payload: api.Payload{CommentID: "c1", Text: "Later"}},
		{Kind: api.KindNoteDelete, Payload: api.Payload{CommentID: "c1"}},
		{Kind: api.KindNoteRestore, Payload: api.Payload{Comment: &api.Comment{ID: "c1", Text: "Later"}}},
	} {
		na.ID, na.Key = uuid.NewString(), agent
		_, _, err := s.AppendAction(na, "alex-app")
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "note") {
			t.Errorf("%s on an agent's item: %v", na.Kind, err)
		}
	}
	pending, err := s.PendingActions("laptop")
	must(t, err)
	if len(pending) != 0 {
		t.Fatalf("the bridge would pull %+v", pending)
	}
}

func TestNoteEditDeleteRestore(t *testing.T) {
	s, c := open(t)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", nil).BeadID)
	for _, n := range []struct{ id, text string }{{id2, "Use the rain barrel"}, {id3, "Not the cactus"}, {id4, "Twice a week"}} {
		c.tick()
		_, _, err := s.AppendAction(api.NewAction{ID: n.id, Kind: api.KindNote, Key: key, Payload: api.Payload{Text: n.text}}, "alex-app")
		must(t, err)
	}
	post := func(na api.NewAction) (api.Action, error) {
		na.ID, na.Key = uuid.NewString(), key
		a, _, err := s.AppendAction(na, "alex-app")
		return a, err
	}

	a, err := post(api.NewAction{Kind: api.KindNoteEdit, Payload: api.Payload{CommentID: id3, Text: "Not the cactus, ever"}})
	must(t, err)
	if a.Payload.Comment == nil || a.Payload.Comment.Text != "Not the cactus" || a.Payload.Comment.ID != id3 {
		t.Fatalf("note-edit recorded %+v", a.Payload.Comment)
	}
	it := item(t, mustState(t, s), key)
	if it.Comments[1].Text != "Not the cactus, ever" || it.Comments[1].Author != "alex-app" {
		t.Fatalf("comments %+v", it.Comments)
	}

	a, err = post(api.NewAction{Kind: api.KindNoteDelete, Payload: api.Payload{CommentID: id3}})
	must(t, err)
	removed := *a.Payload.Comment
	if removed.Text != "Not the cactus, ever" {
		t.Fatalf("note-delete recorded %+v", removed)
	}
	if stored, _ := s.Action(a.ID); stored.Payload.Comment == nil || *stored.Payload.Comment != removed {
		t.Errorf("stored note-delete %+v", stored.Payload)
	}
	if it := item(t, mustState(t, s), key); len(it.Comments) != 2 || it.Comments[0].ID != id2 || it.Comments[1].ID != id4 {
		t.Fatalf("after delete %+v", it.Comments)
	}

	// Restore puts it back between the other two, by its time.
	_, err = post(api.NewAction{Kind: api.KindNoteRestore, Payload: api.Payload{Comment: &removed}})
	must(t, err)
	it = item(t, mustState(t, s), key)
	if len(it.Comments) != 3 || it.Comments[1] != removed {
		t.Fatalf("after restore %+v", it.Comments)
	}
	// Restoring it twice is refused.
	if _, err := post(api.NewAction{Kind: api.KindNoteRestore, Payload: api.Payload{Comment: &removed}}); !errors.Is(err, ErrConflict) {
		t.Errorf("restore of a present note: %v", err)
	}

	for name, na := range map[string]api.NewAction{
		"edit with no id":      {Kind: api.KindNoteEdit, Payload: api.Payload{Text: "Later"}},
		"edit with no text":    {Kind: api.KindNoteEdit, Payload: api.Payload{CommentID: id2, Text: " "}},
		"edit too long":        {Kind: api.KindNoteEdit, Payload: api.Payload{CommentID: id2, Text: strings.Repeat("a", MaxNoteLen+1)}},
		"delete with no id":    {Kind: api.KindNoteDelete},
		"restore with nothing": {Kind: api.KindNoteRestore},
		"restore with no id":   {Kind: api.KindNoteRestore, Payload: api.Payload{Comment: &api.Comment{Text: "Later"}}},
	} {
		if _, err := post(na); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	for _, na := range []api.NewAction{
		{Kind: api.KindNoteEdit, Payload: api.Payload{CommentID: "nope", Text: "Later"}},
		{Kind: api.KindNoteDelete, Payload: api.Payload{CommentID: "nope"}},
	} {
		if _, err := post(na); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s of an unknown note: %v", na.Kind, err)
		}
	}
	// A refused change leaves no action behind.
	as, err := s.RecentActions(100)
	must(t, err)
	if len(as) != 7 {
		t.Errorf("%d actions logged, want 7", len(as))
	}
}

func TestAgentChangesOnlyItsOwnPersonalTodos(t *testing.T) {
	s, c := open(t)
	for _, name := range []string{"laptop", "desk"} {
		_, err := s.CreateBridge(name)
		must(t, err)
	}
	key, created, err := s.AddPersonal(api.PersonalTodo{ID: id1, Title: "Renew the library card"}, "laptop")
	must(t, err)
	_, _, id, _ := SplitKey(key)
	// Repeating the add keeps the one owner.
	if again, created2, err := s.AddPersonal(api.PersonalTodo{ID: id1, Title: "Renew the library card"}, "desk"); err != nil || created2 || again != key || !created {
		t.Fatalf("repeat add = %q, %v, %v", again, created2, err)
	}
	app := addPersonal(t, s, id2, "Water the plants", nil).BeadID

	if it, err := s.PersonalItem("laptop", id); err != nil || it.Title != "Renew the library card" || it.Key != key {
		t.Fatalf("own item = %+v, %v", it, err)
	}
	var msg string
	for _, tc := range []struct{ bridge, id string }{{"desk", id}, {"laptop", app}, {"laptop", "mm-99"}, {"laptop", "../x"}} {
		_, err := s.PersonalItem(tc.bridge, tc.id)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s reading %s: %v", tc.bridge, tc.id, err)
		}
		_, _, err = s.ChangePersonal(tc.bridge, tc.id, api.PersonalChange{Kind: api.KindDone})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s changing %s: %v", tc.bridge, tc.id, err)
		}
		if got := strings.ReplaceAll(err.Error(), tc.id, "ID"); msg == "" {
			msg = got
		} else if got != msg {
			t.Errorf("message %q differs from %q", got, msg)
		}
	}

	due := time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)
	a, created, err := s.ChangePersonal("laptop", id, api.PersonalChange{ID: id3, Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{Due: &due}}, Author: "planner"})
	must(t, err)
	if !created || a.Author != "planner" || a.Status != api.StatusApplied || a.Payload.Was == nil || !a.Payload.Was.ClearDue {
		t.Fatalf("agent edit %+v", a)
	}
	if again, created, err := s.ChangePersonal("laptop", id, api.PersonalChange{ID: id3, Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{Title: ptr("Something else")}}}); err != nil || created || again.Seq != a.Seq {
		t.Fatalf("repeat = %+v, %v, %v", again, created, err)
	}
	if _, _, err := s.ChangePersonal("laptop", id, api.PersonalChange{ID: id3, Kind: api.KindDone}); !errors.Is(err, ErrConflict) {
		t.Errorf("id reused for another kind: %v", err)
	}
	a, _, err = s.ChangePersonal("laptop", id, api.PersonalChange{Kind: api.KindNote, Payload: api.Payload{Text: "Ask at the desk"}})
	must(t, err)
	if a.Author != "laptop" {
		t.Errorf("author %q, want the machine name", a.Author)
	}
	for _, k := range []string{api.KindNoteDelete, api.KindSnooze, api.KindAdd, ""} {
		if _, _, err := s.ChangePersonal("laptop", id, api.PersonalChange{Kind: k}); !errors.Is(err, ErrInvalid) {
			t.Errorf("kind %q: %v", k, err)
		}
	}
	if _, _, err := s.ChangePersonal("laptop", id, api.PersonalChange{Kind: api.KindDone, Author: "a\nb"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad author: %v", err)
	}
	it, err := s.PersonalItem("laptop", id)
	must(t, err)
	if !it.DueAt.Equal(due) || len(it.Comments) != 1 || it.Comments[0].Author != "laptop" {
		t.Fatalf("item %+v", it)
	}

	// A closed item stays the agent's for the week the app shows it.
	_, _, err = s.ChangePersonal("laptop", id, api.PersonalChange{Kind: api.KindDone})
	must(t, err)
	c.t = c.t.Add(6 * 24 * time.Hour)
	if _, err := s.PersonalItem("laptop", id); err != nil {
		t.Fatalf("closed item within the week: %v", err)
	}
	c.t = c.t.Add(2 * 24 * time.Hour)
	if _, err := s.PersonalItem("laptop", id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("closed item past the week: %v", err)
	}
	if _, _, err := s.ChangePersonal("laptop", id, api.PersonalChange{Kind: api.KindReopen}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reopen past the week: %v", err)
	}
}

// Items moved from a bridge's personal repo belong to no machine.
func TestImportedPersonalItemsHaveNoOwner(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "personal", Personal: true, OptedIn: true}}))
	must(t, s.ReplaceItems("laptop", "personal", []api.Item{{ID: "td-1", Title: "Call Sam", Status: "open"}}))
	if _, err := s.PersonalItem("laptop", "td-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("imported item: %v", err)
	}
}
