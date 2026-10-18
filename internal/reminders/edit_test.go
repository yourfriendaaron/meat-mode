package reminders

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// A due time set after a snooze has ended still fires.
func TestDueAfterAnEndedSnoozeFires(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	it := item(nil)
	it.DeferUntil = tp(t0.Add(-time.Hour))
	src.items = []api.Item{it}
	s.Tick(context.Background())
	if len(n.got) != 1 || n.got[0].Body != "Snoozed until now" {
		t.Fatalf("snooze end: %+v", n.got)
	}
	it.DueAt = tp(t0.Add(time.Hour))
	src.items = []api.Item{it}
	s.Tick(context.Background())
	if len(n.got) != 1 {
		t.Fatal("fired before the due time")
	}
	c.now = t0.Add(time.Hour)
	s.Tick(context.Background())
	s.Tick(context.Background())
	if len(n.got) != 2 || n.got[1].Body != "Due now" {
		t.Fatalf("due after the snooze: %+v", n.got)
	}
	// A due time inside a snooze waits for the snooze to end.
	it.DueAt, it.DeferUntil = tp(t0.Add(2*time.Hour)), tp(t0.Add(3*time.Hour))
	src.items = []api.Item{it}
	c.now = t0.Add(2 * time.Hour)
	s.Tick(context.Background())
	if len(n.got) != 2 {
		t.Fatal("fired during the snooze")
	}
	c.now = t0.Add(3 * time.Hour)
	s.Tick(context.Background())
	if len(n.got) != 3 || n.got[2].Body != "Snoozed until now" {
		t.Fatalf("snooze end: %+v", n.got)
	}
}

type storeItems struct{ st *store.Store }

func (s storeItems) Items(context.Context) ([]api.Item, error) {
	state, err := s.st.State()
	return state.Items, err
}

// Editing a personal item's due time moves its reminder; clearing it
// leaves none.
func TestEditedDueTimeReminds(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, c, _, n := setup(t)
	s, err := New(st.DB(), storeItems{st}, n, c, "https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	s.KeyOf = st.ReminderKey
	post := func(na api.NewAction) api.Action {
		t.Helper()
		na.ID = uuid.NewString()
		a, _, err := st.AppendAction(na, "alex-app")
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	tick := func(at time.Time, want int) {
		t.Helper()
		c.now = at
		if err := s.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(n.got) != want {
			t.Fatalf("at %v: %d reminders, want %d", at, len(n.got), want)
		}
	}
	add := post(api.NewAction{Kind: api.KindAdd, Bridge: store.HubBridge, Repo: store.PersonalRepo,
		Payload: api.Payload{Title: "Water the plants", Due: tp(t0.Add(time.Hour))}})
	key := store.ItemKey(store.HubBridge, store.PersonalRepo, add.BeadID)
	edit := func(set api.Edit) { post(api.NewAction{Kind: api.KindEdit, Key: key, Payload: api.Payload{Set: &set}}) }

	tick(t0.Add(time.Hour), 1)
	edit(api.Edit{Due: tp(t0.Add(3 * time.Hour))})
	tick(t0.Add(2*time.Hour), 1)
	tick(t0.Add(3*time.Hour), 2)

	// Snoozed and then moved past the snooze.
	post(api.NewAction{Kind: api.KindSnooze, Key: key, Payload: api.Payload{Until: tp(t0.Add(4 * time.Hour))}})
	tick(t0.Add(4*time.Hour), 3)
	edit(api.Edit{Due: tp(t0.Add(5 * time.Hour))})
	tick(t0.Add(5*time.Hour), 4)

	edit(api.Edit{Due: tp(t0.Add(6 * time.Hour))})
	edit(api.Edit{ClearDue: true})
	tick(t0.Add(7*time.Hour), 4)
}
