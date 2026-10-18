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

func addRepeating(t *testing.T, s *Store, title string, due time.Time, r api.Repeat) string {
	t.Helper()
	a, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: title, Due: &due, Repeat: &r}}, "alex-app")
	must(t, err)
	return hubKey(a.BeadID)
}

func act(t *testing.T, s *Store, kind, key string, p api.Payload) api.Action {
	t.Helper()
	a, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: kind, Key: key, Payload: p}, "alex-app")
	must(t, err)
	return a
}

func TestDoneMovesARepeatingItemOn(t *testing.T) {
	s, c := open(t)
	// Every month on the 31st at 9:00 in Paris, sent with a start the hub
	// ignores.
	due := time.Date(2026, 1, 31, 8, 0, 0, 0, time.UTC)
	bogus := due.Add(-time.Hour)
	key := addRepeating(t, s, "Pay the rent", due, api.Repeat{Every: api.EveryMonth, TimeZone: "Europe/Paris", Start: &bogus})
	it := item(t, mustState(t, s), key)
	if it.Repeat == nil || it.Repeat.Start == nil || !it.Repeat.Start.Equal(due) || it.Repeat.Every != api.EveryMonth {
		t.Fatalf("added %+v", it.Repeat)
	}
	until := c.t.Add(time.Hour)
	act(t, s, api.KindSnooze, key, api.Payload{Until: &until})
	edit(t, s, key, api.Edit{Checklist: &[]api.Check{{ID: "c1", Text: "Transfer", Done: true}, {ID: "c2", Text: "File the receipt"}}})

	// Done a month late: February's date is skipped, not stacked.
	c.tick()
	a := act(t, s, api.KindDone, key, api.Payload{})
	it = item(t, mustState(t, s), key)
	march := time.Date(2026, 3, 31, 7, 0, 0, 0, time.UTC) // 9:00 in Paris, summer time
	if it.Status != "open" || it.ClosedAt != nil || it.CloseReason != "" || !it.DueAt.Equal(march) || it.DeferUntil != nil ||
		it.Checklist[0].Done || it.Checklist[1].Done || !it.UpdatedAt.Equal(c.t) || !it.Repeat.Start.Equal(due) {
		t.Fatalf("after done %+v", it)
	}
	w := a.Payload.Was
	if a.Status != api.StatusApplied || w == nil || !w.Due.Equal(due) || w.Checklist == nil || !(*w.Checklist)[0].Done ||
		w.Title != nil || w.Repeat != nil {
		t.Fatalf("done = %+v, was %+v", a, w)
	}
	stored, err := s.Action(a.ID)
	must(t, err)
	if b1, b2 := mustJSON(t, stored.Payload), mustJSON(t, a.Payload); b1 != b2 {
		t.Errorf("stored payload %s, returned %s", b1, b2)
	}

	// The next done lands on 30 April, and its Undo, an edit back to it,
	// keeps the start, so May returns to the 31st.
	c.t = time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	act(t, s, api.KindDone, key, api.Payload{})
	c.tick()
	a = act(t, s, api.KindDone, key, api.Payload{})
	april := time.Date(2026, 4, 30, 7, 0, 0, 0, time.UTC)
	if w := a.Payload.Was; !w.Due.Equal(april) {
		t.Fatalf("second done's was %+v", w)
	}
	undo := edit(t, s, key, *a.Payload.Was)
	it = item(t, mustState(t, s), key)
	if !it.DueAt.Equal(april) || !it.Repeat.Start.Equal(due) || undo.Payload.Was.Repeat != nil {
		t.Fatalf("after undo %+v, start %v", it, it.Repeat.Start)
	}
	act(t, s, api.KindDone, key, api.Payload{})
	if it := item(t, mustState(t, s), key); !it.DueAt.Equal(time.Date(2026, 5, 31, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("after April %v", it.DueAt)
	}

	// Moving the due date off the series moves the series.
	moved := time.Date(2026, 6, 10, 7, 0, 0, 0, time.UTC)
	edit(t, s, key, api.Edit{Due: &moved})
	if it := item(t, mustState(t, s), key); !it.Repeat.Start.Equal(moved) {
		t.Fatalf("start after a move %v", it.Repeat.Start)
	}

	// Dismiss ends the series; reopen picks it up at its last due time.
	a = act(t, s, api.KindDismiss, key, api.Payload{})
	it = item(t, mustState(t, s), key)
	if it.Status != "closed" || it.CloseReason != ReasonDismissed || !it.DueAt.Equal(moved) || a.Payload.Was != nil {
		t.Fatalf("after dismiss %+v", it)
	}
	act(t, s, api.KindReopen, key, api.Payload{})
	it = item(t, mustState(t, s), key)
	if it.Status != "open" || !it.DueAt.Equal(moved) || it.Repeat == nil {
		t.Fatalf("after reopen %+v", it)
	}

	// An item that does not repeat still closes.
	plain := hubKey(addPersonal(t, s, id1, "Water the plants", &due).BeadID)
	if a := act(t, s, api.KindDone, plain, api.Payload{}); a.Payload.Was != nil {
		t.Fatalf("plain done %+v", a)
	}
	if it := item(t, mustState(t, s), plain); it.Status != "closed" || it.CloseReason != ReasonDone {
		t.Fatalf("plain item after done %+v", it)
	}
}

func TestEditRepeatAndRemindBefore(t *testing.T) {
	s, c := open(t)
	due := c.t.Add(3 * time.Hour)
	key := hubKey(addPersonal(t, s, id1, "Water the plants", &due).BeadID)

	// A repeat starts at the due time; the client's start is ignored.
	bogus := due.Add(-48 * time.Hour)
	daily := api.Repeat{Every: api.EveryDay, TimeZone: "America/New_York", Start: &bogus}
	a := edit(t, s, key, api.Edit{Repeat: &daily, RemindBefore: ptr(15)})
	it := item(t, mustState(t, s), key)
	if it.Repeat == nil || !it.Repeat.Start.Equal(due) || it.RemindBefore == nil || *it.RemindBefore != 15 {
		t.Fatalf("after setting %+v, %+v", it.Repeat, it.RemindBefore)
	}
	if w := a.Payload.Was; !w.ClearRepeat || w.Repeat != nil || !w.ClearRemindBefore || w.RemindBefore != nil || w.Due != nil {
		t.Fatalf("was %+v", w)
	}
	if a.Payload.Set.Repeat.Start != nil {
		t.Errorf("set kept the client's start")
	}
	undo := edit(t, s, key, *a.Payload.Was)
	if it := item(t, mustState(t, s), key); it.Repeat != nil || it.RemindBefore != nil {
		t.Fatalf("after undo %+v", it)
	}
	if w := undo.Payload.Was; w.Repeat == nil || !w.Repeat.Start.Equal(due) || *w.RemindBefore != 15 {
		t.Fatalf("undo's was %+v", w)
	}

	// A repeat and a due time in one edit start at the new due time.
	later := due.Add(24 * time.Hour)
	edit(t, s, key, api.Edit{Due: &later, Repeat: &daily, RemindBefore: ptr(0)})
	if it := item(t, mustState(t, s), key); !it.Repeat.Start.Equal(later) || *it.RemindBefore != 0 {
		t.Fatalf("repeat with due %+v", it)
	}

	// Clearing the due date ends the repeat and records it for Undo.
	a = edit(t, s, key, api.Edit{ClearDue: true, ClearRemindBefore: true})
	if it := item(t, mustState(t, s), key); it.DueAt != nil || it.Repeat != nil || it.RemindBefore != nil {
		t.Fatalf("after clearing the due %+v", it)
	}
	if w := a.Payload.Was; !w.Due.Equal(later) || w.Repeat == nil || w.Repeat.Every != api.EveryDay || w.ClearRepeat || *w.RemindBefore != 0 {
		t.Fatalf("was after clearing the due %+v", w)
	}
	edit(t, s, key, *a.Payload.Was)
	if it := item(t, mustState(t, s), key); !it.DueAt.Equal(later) || it.Repeat == nil || !it.Repeat.Start.Equal(later) {
		t.Fatalf("after undoing the clear %+v", it)
	}

	// A repeat needs a due time, now or in the same edit.
	none := hubKey(addPersonal(t, s, id2, "Call Sam", nil).BeadID)
	_, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindEdit, Key: none,
		Payload: api.Payload{Set: &api.Edit{Repeat: &daily}}}, "alex-app")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("repeat with no due: %v", err)
	}
	edit(t, s, none, api.Edit{Due: &due, Repeat: &daily})

	now := time.Now()
	for name, set := range map[string]api.Edit{
		"repeat and clearRepeat":             {Repeat: &daily, ClearRepeat: true},
		"repeat and clearDue":                {Repeat: &daily, ClearDue: true},
		"bad repeat":                         {Repeat: &api.Repeat{Every: "fortnight", TimeZone: "UTC"}, Due: &now},
		"remindBefore and clearRemindBefore": {RemindBefore: ptr(5), ClearRemindBefore: true},
		"remindBefore negative":              {RemindBefore: ptr(-1)},
		"remindBefore over a week":           {RemindBefore: ptr(api.MaxLeadMinutes + 1)},
	} {
		_, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindEdit, Key: key,
			Payload: api.Payload{Set: &set}}, "alex-app")
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	edit(t, s, key, api.Edit{RemindBefore: ptr(api.MaxLeadMinutes)})
	edit(t, s, key, api.Edit{ClearRepeat: true})
}

func TestAddWithRepeat(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	due := c.t.Add(time.Hour)
	daily := api.Repeat{Every: api.EveryDay, TimeZone: "UTC"}
	for name, na := range map[string]api.NewAction{
		"no due":       {Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo, Payload: api.Payload{Title: "Water the plants", Repeat: &daily}},
		"bad rule":     {Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo, Payload: api.Payload{Title: "Water the plants", Due: &due, Repeat: &api.Repeat{Every: api.EveryDay}}},
		"agent's repo": {Kind: api.KindAdd, Bridge: "laptop", Repo: "garden", Payload: api.Payload{Title: "Water the plants", Due: &due, Repeat: &daily}},
	} {
		na.ID = uuid.NewString()
		if _, _, err := s.AppendAction(na, "alex-app"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}

	// An agent's to-do repeats too, and its done moves it on.
	_, err := s.CreateBridge("desk")
	must(t, err)
	key, _, err := s.AddPersonal(api.PersonalTodo{ID: id1, Title: "Take the bins out", Due: &due, Repeat: &daily}, "desk")
	must(t, err)
	_, _, id, _ := SplitKey(key)
	it, err := s.PersonalItem("desk", id)
	must(t, err)
	if it.Repeat == nil || !it.Repeat.Start.Equal(due) {
		t.Fatalf("agent's repeat %+v", it.Repeat)
	}
	if _, _, err := s.AddPersonal(api.PersonalTodo{Title: "Take the bins out", Repeat: &daily}, "desk"); !errors.Is(err, ErrInvalid) {
		t.Errorf("agent's repeat with no due: %v", err)
	}
	a, _, err := s.ChangePersonal("desk", id, api.PersonalChange{Kind: api.KindDone})
	must(t, err)
	it, err = s.PersonalItem("desk", id)
	must(t, err)
	if it.Status != "open" || !it.DueAt.Equal(due.Add(24*time.Hour)) || a.Payload.Was == nil || !a.Payload.Was.Due.Equal(due) {
		t.Fatalf("agent's done %+v, item %+v", a, it)
	}
}

func TestRepeatUntil(t *testing.T) {
	s, c := open(t)
	// Every Sunday at 20:30 in New York until 15 March, across the change to
	// summer time on 8 March. The last date is already the 16th in UTC.
	first := time.Date(2026, 3, 2, 1, 30, 0, 0, time.UTC)  // 1 March, 20:30 EST
	second := time.Date(2026, 3, 9, 0, 30, 0, 0, time.UTC) // 8 March, 20:30 EDT
	last := time.Date(2026, 3, 16, 0, 30, 0, 0, time.UTC)  // 15 March, 20:30 EDT
	weekly := api.Repeat{Every: api.EveryWeek, TimeZone: "America/New_York", Until: "2026-03-15"}
	key := addRepeating(t, s, "Water the plants", first, weekly)
	if it := item(t, mustState(t, s), key); it.Repeat == nil || it.Repeat.Until != "2026-03-15" {
		t.Fatalf("added %+v", it.Repeat)
	}

	a := act(t, s, api.KindDone, key, api.Payload{})
	if it := item(t, mustState(t, s), key); it.Status != "open" || !it.DueAt.Equal(second) || a.Payload.Was == nil {
		t.Fatalf("after the first done %+v", it)
	}
	// The Undo of a done keeps the rule and its until.
	edit(t, s, key, *a.Payload.Was)
	if it := item(t, mustState(t, s), key); !it.DueAt.Equal(first) || it.Repeat.Until != "2026-03-15" || !it.Repeat.Start.Equal(first) {
		t.Fatalf("after undoing the done %+v", it)
	}
	act(t, s, api.KindDone, key, api.Payload{})
	act(t, s, api.KindDone, key, api.Payload{})
	if it := item(t, mustState(t, s), key); it.Status != "open" || !it.DueAt.Equal(last) {
		t.Fatalf("the last occurrence %+v", it)
	}

	// The done after the last date closes it like any other, with no Was.
	c.tick()
	a = act(t, s, api.KindDone, key, api.Payload{})
	it := item(t, mustState(t, s), key)
	if a.Payload.Was != nil || it.Status != "closed" || it.CloseReason != ReasonDone || !it.ClosedAt.Equal(c.t) ||
		!it.DueAt.Equal(last) || it.Repeat == nil || it.Repeat.Until != "2026-03-15" {
		t.Fatalf("closing done %+v, item %+v", a, it)
	}
	// Its Undo is a reopen, at the last due time, and a done closes it again.
	act(t, s, api.KindReopen, key, api.Payload{})
	if it := item(t, mustState(t, s), key); it.Status != "open" || !it.DueAt.Equal(last) || it.Repeat == nil {
		t.Fatalf("after reopen %+v", it)
	}
	if a := act(t, s, api.KindDone, key, api.Payload{}); a.Payload.Was != nil {
		t.Fatalf("done after reopen %+v", a)
	}

	// Removing the until resends the rule without one; its Undo puts it back.
	act(t, s, api.KindReopen, key, api.Payload{})
	forGood := weekly
	forGood.Until = ""
	a = edit(t, s, key, api.Edit{Repeat: &forGood})
	if w := a.Payload.Was; w.Repeat == nil || w.Repeat.Until != "2026-03-15" {
		t.Fatalf("was %+v", w)
	}
	if a := act(t, s, api.KindDone, key, api.Payload{}); a.Payload.Was == nil {
		t.Fatal("a rule with no until closed the item")
	}
	edit(t, s, key, api.Edit{Due: &last})
	edit(t, s, key, *a.Payload.Was)
	if it := item(t, mustState(t, s), key); it.Repeat.Until != "2026-03-15" {
		t.Fatalf("after undoing the edit %+v", it.Repeat)
	}
}

func TestRepeatUntilOnTheDueDate(t *testing.T) {
	s, c := open(t)
	// Until the due date itself: one occurrence, and its done closes it.
	due := c.t.Add(time.Hour)
	key := addRepeating(t, s, "Water the plants", due, api.Repeat{Every: api.EveryDay, TimeZone: "UTC", Until: "2026-03-01"})
	if a := act(t, s, api.KindDone, key, api.Payload{}); a.Payload.Was != nil {
		t.Fatalf("done %+v", a)
	}
	if it := item(t, mustState(t, s), key); it.Status != "closed" || !it.DueAt.Equal(due) {
		t.Fatalf("after done %+v", it)
	}

	// Counted from the done day, the next date is the done day plus the
	// interval, so a late done can end the series early.
	due = time.Date(2026, 3, 1, 18, 0, 0, 0, time.UTC)
	key = addRepeating(t, s, "Call Sam", due, api.Repeat{Every: api.EveryDay, Interval: 3, FromDone: true, TimeZone: "Europe/Paris", Until: "2026-03-08"})
	c.t = time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	act(t, s, api.KindDone, key, api.Payload{})
	if it := item(t, mustState(t, s), key); it.Status != "open" || !it.DueAt.Equal(time.Date(2026, 3, 5, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("after the first done %+v", it)
	}
	// Done on the 6th: the 9th is past the 8th.
	c.t = time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	if a := act(t, s, api.KindDone, key, api.Payload{}); a.Payload.Was != nil {
		t.Fatalf("closing done %+v", a)
	}
	if it := item(t, mustState(t, s), key); it.Status != "closed" {
		t.Fatalf("after the closing done %+v", it)
	}
}

func TestRepeatUntilRefuses(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("desk")
	must(t, err)
	// 20:30 on 1 March in New York is the 2nd in UTC: an until of the 1st
	// is the due date, in the rule's zone.
	due := time.Date(2026, 3, 2, 1, 30, 0, 0, time.UTC)
	ny := func(until string) *api.Repeat {
		return &api.Repeat{Every: api.EveryDay, TimeZone: "America/New_York", Until: until}
	}
	addRepeating(t, s, "Water the plants", due, *ny("2026-03-01"))

	for name, na := range map[string]api.NewAction{
		"until before due": {Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo, Payload: api.Payload{Title: "Water the plants", Due: &due, Repeat: ny("2026-02-28")}},
		"until not a date": {Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo, Payload: api.Payload{Title: "Water the plants", Due: &due, Repeat: ny("2026-02-30")}},
	} {
		na.ID = uuid.NewString()
		if _, _, err := s.AppendAction(na, "alex-app"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	if _, _, err := s.AddPersonal(api.PersonalTodo{Title: "Take the bins out", Due: &due, Repeat: &api.Repeat{Every: api.EveryDay, TimeZone: "UTC", Until: "2026-03-01"}}, "desk"); !errors.Is(err, ErrInvalid) {
		t.Errorf("agent's until before due: %v", err)
	}

	plain := hubKey(addPersonal(t, s, id1, "Call Sam", &due).BeadID)
	key := addRepeating(t, s, "Sweep the porch", due, *ny("2026-03-10"))
	later := time.Date(2026, 3, 11, 13, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		key string
		set api.Edit
	}{
		"rule with until before due":  {plain, api.Edit{Repeat: ny("2026-02-28")}},
		"rule and due past until":     {plain, api.Edit{Repeat: ny("2026-03-10"), Due: &later}},
		"due moved past until":        {key, api.Edit{Due: &later}},
		"until changed to before due": {key, api.Edit{Repeat: ny("2026-02-28")}},
		"until not a date":            {key, api.Edit{Repeat: ny("10 March")}},
	} {
		_, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindEdit, Key: tc.key,
			Payload: api.Payload{Set: &tc.set}}, "alex-app")
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	if it := item(t, mustState(t, s), key); !it.DueAt.Equal(due) || it.Repeat.Until != "2026-03-10" {
		t.Fatalf("a refused edit changed the item %+v", it)
	}
	if it := item(t, mustState(t, s), plain); it.Repeat != nil {
		t.Fatalf("a refused edit set a repeat %+v", it.Repeat)
	}
	// A move up to the until, or a move with the rule cleared, is fine.
	onUntil := time.Date(2026, 3, 11, 3, 0, 0, 0, time.UTC) // 10 March, 23:00 EDT
	edit(t, s, key, api.Edit{Due: &onUntil})
	edit(t, s, key, api.Edit{Due: &later, ClearRepeat: true})
}

func TestChangingUntilKeepsTheSeries(t *testing.T) {
	s, c := open(t)
	// Every month on the 31st at 9:00 UTC, now due on 30 April.
	due := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	april := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	may := time.Date(2026, 5, 31, 9, 0, 0, 0, time.UTC)
	monthly := api.Repeat{Every: api.EveryMonth, TimeZone: "UTC"}
	key := addRepeating(t, s, "Pay the rent", due, monthly)
	edit(t, s, key, api.Edit{Due: &april})
	if it := item(t, mustState(t, s), key); !it.Repeat.Start.Equal(due) {
		t.Fatalf("start after a move onto the series %v", it.Repeat.Start)
	}
	c.t = april.Add(-time.Hour)

	// doneLandsOnThe31st checks that a done from 30 April lands on 31 May,
	// and puts it back with its Undo.
	doneLandsOnThe31st := func(step string) {
		t.Helper()
		it := item(t, mustState(t, s), key)
		if !it.DueAt.Equal(april) || !it.Repeat.Start.Equal(due) {
			t.Fatalf("%s: item %+v, start %v", step, it, it.Repeat.Start)
		}
		a := act(t, s, api.KindDone, key, api.Payload{})
		if it := item(t, mustState(t, s), key); !it.DueAt.Equal(may) {
			t.Fatalf("%s: done landed on %v", step, it.DueAt)
		}
		edit(t, s, key, *a.Payload.Was)
	}

	ended := monthly
	ended.Until = "2026-12-31"
	set := edit(t, s, key, api.Edit{Repeat: &ended})
	doneLandsOnThe31st("until set")
	if it := item(t, mustState(t, s), key); it.Repeat.Until != "2026-12-31" {
		t.Fatalf("until %+v", it.Repeat)
	}
	removed := edit(t, s, key, api.Edit{Repeat: &monthly})
	doneLandsOnThe31st("until removed")
	// The Undo of each edit keeps the series too.
	edit(t, s, key, *removed.Payload.Was)
	doneLandsOnThe31st("removal undone")
	if it := item(t, mustState(t, s), key); it.Repeat.Until != "2026-12-31" {
		t.Fatalf("until after the undo %+v", it.Repeat)
	}
	edit(t, s, key, *set.Payload.Was)
	doneLandsOnThe31st("until undone")

	// Any other change to the rule starts it at the due time.
	bimonthly := ended
	bimonthly.Interval = 2
	edit(t, s, key, api.Edit{Repeat: &bimonthly})
	if it := item(t, mustState(t, s), key); !it.Repeat.Start.Equal(april) {
		t.Fatalf("start after a new interval %v", it.Repeat.Start)
	}
	// So does a new until with the due time moved off the series.
	off := april.Add(time.Hour)
	bimonthly.Until = ""
	edit(t, s, key, api.Edit{Repeat: &bimonthly, Due: &off})
	if it := item(t, mustState(t, s), key); !it.Repeat.Start.Equal(off) {
		t.Fatalf("start after a move off the series %v", it.Repeat.Start)
	}
}
