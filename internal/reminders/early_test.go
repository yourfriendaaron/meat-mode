package reminders

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

type fakeLead struct {
	minutes int
	err     error
}

func (f *fakeLead) ReminderLead(context.Context) (int, error) { return f.minutes, f.err }

func bodies(n *fakeNotifier) []string {
	var out []string
	for _, m := range n.got {
		out = append(out, m.Body)
	}
	return out
}

func expectBodies(t *testing.T, n *fakeNotifier, want ...string) {
	t.Helper()
	got := bodies(n)
	if len(got) != len(want) {
		t.Fatalf("reminders %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reminders %q, want %q", got, want)
		}
	}
}

func TestEarlyReminderThenDue(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	s.Lead = &fakeLead{minutes: 30}
	src.items = []api.Item{item(tp(t0.Add(time.Hour)))}
	tick := func(at time.Time) {
		t.Helper()
		c.now = at
		if err := s.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	tick(t0)
	tick(t0.Add(29 * time.Minute))
	expectBodies(t, n)
	tick(t0.Add(30 * time.Minute))
	tick(t0.Add(31 * time.Minute))
	expectBodies(t, n, "Due in 30 minutes")
	if m := n.got[0]; m.Title != "Water the plants" || m.Tag != "home/chores/ch-1" ||
		m.URL != "https://app.example.com/?item=home%2Fchores%2Fch-1" {
		t.Fatalf("message %+v", m)
	}
	tick(t0.Add(time.Hour))
	expectBodies(t, n, "Due in 30 minutes", "Due now")
}

func TestChangingTheLeadNeverResends(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	lead := &fakeLead{minutes: 60}
	s.Lead = lead
	src.items = []api.Item{item(tp(t0.Add(2 * time.Hour)))}
	c.now = t0.Add(time.Hour)
	s.Tick(context.Background())
	expectBodies(t, n, "Due in 1 hour")
	for _, m := range []int{90, 30, 10} {
		lead.minutes = m
		c.now = c.now.Add(10 * time.Minute)
		s.Tick(context.Background())
	}
	expectBodies(t, n, "Due in 1 hour")

	// A new due time has its own early reminder.
	src.items = []api.Item{item(tp(t0.Add(5 * time.Hour)))}
	c.now = t0.Add(5*time.Hour - 10*time.Minute)
	s.Tick(context.Background())
	expectBodies(t, n, "Due in 1 hour", "Due in 10 minutes")
}

func TestItemLeadOverridesTheHubs(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	s.Lead = &fakeLead{minutes: 30}
	none, own := 0, 120
	a := item(tp(t0.Add(3 * time.Hour)))
	a.RemindBefore = &none
	b := item(tp(t0.Add(3 * time.Hour)))
	b.Key, b.ID, b.Title, b.RemindBefore = "home/chores/ch-2", "ch-2", "Pay the rent", &own
	src.items = []api.Item{a, b}
	for _, at := range []time.Duration{time.Hour, 150 * time.Minute} {
		c.now = t0.Add(at)
		s.Tick(context.Background())
	}
	expectBodies(t, n, "Due in 2 hours")
	if n.got[0].Title != "Pay the rent" {
		t.Fatalf("got %+v", n.got)
	}
}

// Without a lead source there are no early reminders, and an item's own
// lead still applies.
func TestNoLeadSource(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	own := 15
	a := item(tp(t0.Add(time.Hour)))
	b := item(tp(t0.Add(time.Hour)))
	b.Key, b.ID, b.RemindBefore = "home/chores/ch-2", "ch-2", &own
	src.items = []api.Item{a, b}
	c.now = t0.Add(50 * time.Minute)
	s.Tick(context.Background())
	expectBodies(t, n, "Due in 10 minutes")
}

func TestNoEarlyReminderInsideASnooze(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	s.Lead = &fakeLead{minutes: 60}
	it := item(tp(t0.Add(2 * time.Hour)))
	it.Status, it.DeferUntil = "deferred", tp(t0.Add(90*time.Minute))
	src.items = []api.Item{it}
	for _, at := range []time.Duration{time.Hour, 90 * time.Minute, 2 * time.Hour} {
		c.now = t0.Add(at)
		s.Tick(context.Background())
	}
	expectBodies(t, n, "Snoozed until now", "Due now")

	// A snooze that ends before the early moment leaves it.
	it.DueAt, it.DeferUntil = tp(t0.Add(5*time.Hour)), tp(t0.Add(3*time.Hour))
	src.items = []api.Item{it}
	for _, at := range []time.Duration{3 * time.Hour, 4 * time.Hour, 5 * time.Hour} {
		c.now = t0.Add(at)
		s.Tick(context.Background())
	}
	expectBodies(t, n, "Snoozed until now", "Due now", "Snoozed until now", "Due in 1 hour", "Due now")
}

func TestClosedItemHasNoEarlyReminder(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	s.Lead = &fakeLead{minutes: 60}
	it := item(tp(t0.Add(30 * time.Minute)))
	it.Status = "closed"
	src.items = []api.Item{it}
	s.Tick(context.Background())
	expectBodies(t, n)
}

// An early reminder missed while the hub was down says the time left now;
// once the due time has passed only the due reminder comes.
func TestMissedEarlyReminder(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	s.Lead = &fakeLead{minutes: 24 * 60}
	a := item(tp(t0.Add(3 * time.Hour)))
	b := item(tp(t0.Add(-2 * time.Minute)))
	b.Key, b.ID = "home/chores/ch-2", "ch-2"
	src.items = []api.Item{a, b}
	s.Tick(context.Background())
	expectBodies(t, n, "Due now", "Due in 3 hours")
}

func TestEarlyReminderForAnItemThatArrivedInsideTheLead(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	s.Lead = &fakeLead{minutes: 60}
	it := item(tp(t0.Add(20 * time.Minute)))
	it.FirstSeen = t0
	src.items = []api.Item{it}
	s.Tick(context.Background())
	c.now = t0.Add(20 * time.Minute)
	s.Tick(context.Background())
	expectBodies(t, n, "Due now")
}

func TestFailingLeadSourceKeepsDueReminders(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	lead := &fakeLead{minutes: 60, err: errors.New("database is locked")}
	s.Lead = lead
	src.items = []api.Item{item(tp(t0.Add(-time.Minute))), item(tp(t0.Add(30 * time.Minute)))}
	src.items[1].Key, src.items[1].ID = "home/chores/ch-2", "ch-2"
	if err := s.Tick(context.Background()); err == nil {
		t.Fatal("Tick hid the lead time error")
	}
	expectBodies(t, n, "Due now")
	lead.err = nil
	s.Tick(context.Background())
	expectBodies(t, n, "Due now", "Due in 30 minutes")
}

func TestKeyOfFirstSeenWins(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	// The bridge reported the added bead after its due time; the task was
	// known from the add, before it.
	s.KeyOf = func(context.Context, api.Item) (string, time.Time, error) {
		return "home/chores/pending-1", t0.Add(-time.Hour), nil
	}
	real := item(tp(t0.Add(-2 * time.Minute)))
	real.FirstSeen = t0.Add(-time.Minute)
	src.items = []api.Item{real}
	s.Tick(context.Background())
	expectBodies(t, n, "Due now")
}

func TestSpan(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                              "1 minute",
		20 * time.Second:               "1 minute",
		time.Minute:                    "1 minute",
		30 * time.Minute:               "30 minutes",
		time.Hour:                      "1 hour",
		90 * time.Minute:               "1 hour 30 minutes",
		2*time.Hour + 29*time.Second:   "2 hours",
		24 * time.Hour:                 "1 day",
		26*time.Hour + 10*time.Minute:  "1 day 2 hours",
		7 * 24 * time.Hour:             "7 days",
		40*24*time.Hour + 59*time.Hour: "42 days 11 hours",
	} {
		if got := span(d); got != want {
			t.Errorf("span(%v) = %q, want %q", d, got, want)
		}
	}
}

// A personal item added with a due time already past never reminds for it.
func TestPersonalItemAddedOverdueIsSilent(t *testing.T) {
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
	now := time.Now()
	add := func(title string, due time.Time) {
		t.Helper()
		_, _, err := st.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindAdd, Bridge: store.HubBridge,
			Repo: store.PersonalRepo, Payload: api.Payload{Title: title, Due: &due}}, "alex-app")
		if err != nil {
			t.Fatal(err)
		}
	}
	add("Water the plants", now.Add(-time.Hour))
	add("Pay the rent", now.Add(time.Hour))
	c.now = now.Add(time.Hour + time.Second)
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 1 || n.got[0].Title != "Pay the rent" {
		t.Fatalf("got %+v", n.got)
	}
}
