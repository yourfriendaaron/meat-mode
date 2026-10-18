package reminders

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/notify"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time                       { return c.now }
func (c *fakeClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

type fakeSource struct{ items []api.Item }

func (f *fakeSource) Items(context.Context) ([]api.Item, error) { return f.items, nil }

type fakeNotifier struct {
	err error
	got []notify.Message
}

func (f *fakeNotifier) Notify(_ context.Context, m notify.Message) error {
	f.got = append(f.got, m)
	return f.err
}

var t0 = time.Date(2030, 1, 1, 9, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*sql.DB, *fakeClock, *fakeSource, *fakeNotifier) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, &fakeClock{now: t0}, &fakeSource{}, &fakeNotifier{}
}

func sched(t *testing.T, db *sql.DB, c *fakeClock, s *fakeSource, n *fakeNotifier) *Scheduler {
	t.Helper()
	sc, err := New(db, s, n, c, "https://app.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func tp(t time.Time) *time.Time { return &t }

func item(due *time.Time) api.Item {
	return api.Item{Key: "home/chores/ch-1", ID: "ch-1", Title: "Water the plants", Status: "open", DueAt: due}
}

func TestFiresOnceWhenDue(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	src.items = []api.Item{item(tp(t0.Add(time.Hour)))}
	s.Tick(context.Background())
	if len(n.got) != 0 {
		t.Fatal("fired early")
	}
	c.now = t0.Add(time.Hour + time.Minute)
	s.Tick(context.Background())
	s.Tick(context.Background())
	if len(n.got) != 1 {
		t.Fatalf("fired %d times", len(n.got))
	}
	m := n.got[0]
	if m.Title != "Water the plants" || m.Body != "Due now" || m.Tag != "home/chores/ch-1" ||
		m.URL != "https://app.example.com/?item=home%2Fchores%2Fch-1" {
		t.Fatalf("message %+v", m)
	}
}

func TestRefiresAfterSnooze(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	due := t0.Add(-time.Minute)
	src.items = []api.Item{item(&due)}
	s.Tick(context.Background())
	if len(n.got) != 1 {
		t.Fatal("first")
	}
	it := item(&due)
	it.Status = "deferred"
	it.DeferUntil = tp(t0.Add(2 * time.Hour))
	src.items = []api.Item{it}
	s.Tick(context.Background())
	if len(n.got) != 1 {
		t.Fatal("fired while snoozed")
	}
	c.now = t0.Add(2*time.Hour + time.Second)
	s.Tick(context.Background())
	s.Tick(context.Background())
	if len(n.got) != 2 || n.got[1].Body != "Snoozed until now" {
		t.Fatalf("got %+v", n.got)
	}
	// A later snooze fires again.
	it.DeferUntil = tp(t0.Add(3 * time.Hour))
	src.items = []api.Item{it}
	c.now = t0.Add(3 * time.Hour)
	s.Tick(context.Background())
	if len(n.got) != 3 {
		t.Fatal("second snooze did not fire")
	}
}

func TestNoRefireAfterRestart(t *testing.T) {
	db, c, src, n := setup(t)
	due := t0.Add(-time.Minute)
	src.items = []api.Item{item(&due)}
	sched(t, db, c, src, n).Tick(context.Background())
	sched(t, db, c, src, n).Tick(context.Background())
	if len(n.got) != 1 {
		t.Fatalf("fired %d times", len(n.got))
	}
}

func TestMissedWhileDownFiresOnceOldIsSilent(t *testing.T) {
	db, c, src, n := setup(t)
	recent := t0.Add(-3 * time.Hour)
	old := t0.Add(-8 * 24 * time.Hour)
	a, b := item(&recent), item(&old)
	b.Key, b.ID, b.Title = "home/chores/ch-2", "ch-2", "Old thing"
	src.items = []api.Item{a, b}
	s := sched(t, db, c, src, n)
	s.Tick(context.Background())
	s.Tick(context.Background())
	if len(n.got) != 1 || n.got[0].Title != "Water the plants" {
		t.Fatalf("got %+v", n.got)
	}
	var rows int
	db.QueryRow(`SELECT count(*) FROM reminder_fired`).Scan(&rows)
	if rows != 2 {
		t.Fatalf("recorded %d", rows)
	}
}

func TestClosedAndUndatedNeverFire(t *testing.T) {
	db, c, src, n := setup(t)
	due := t0.Add(-time.Minute)
	closed := item(&due)
	closed.Status = "closed"
	src.items = []api.Item{closed, item(nil)}
	sched(t, db, c, src, n).Tick(context.Background())
	if len(n.got) != 0 {
		t.Fatal("fired")
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestEveryOverridesInterval(t *testing.T) {
	if got := (&Scheduler{}).every(); got != Interval {
		t.Errorf("default: got %v, want %v", got, Interval)
	}
	if got := (&Scheduler{Every: time.Second}).every(); got != time.Second {
		t.Errorf("override: got %v, want 1s", got)
	}
}

func TestFailingNotifierRecordsAndReturnsError(t *testing.T) {
	db, c, src, n := setup(t)
	n.err = errors.New("push service down")
	due := t0.Add(-time.Minute)
	src.items = []api.Item{item(&due)}
	s := sched(t, db, c, src, n)
	if err := s.Tick(context.Background()); err == nil {
		t.Fatal("Tick did not return the delivery error")
	}
	var rows int
	db.QueryRow(`SELECT count(*) FROM reminder_fired`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("reminder row %d, want 1", rows)
	}
	if err := s.Tick(context.Background()); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if len(n.got) != 1 {
		t.Fatalf("sent %d times; the failure must not repeat", len(n.got))
	}
}

func TestLongTitleIsTruncated(t *testing.T) {
	db, c, src, n := setup(t)
	due := t0.Add(-time.Minute)
	it := item(&due)
	it.Title = strings.Repeat("é", 250)
	src.items = []api.Item{it}
	sched(t, db, c, src, n).Tick(context.Background())
	if len(n.got) != 1 {
		t.Fatal("did not fire")
	}
	title := []rune(n.got[0].Title)
	if len(title) != 201 || title[len(title)-1] != '…' {
		t.Fatalf("title is %d runes and ends %q", len(title), title[len(title)-1])
	}
}

func TestKeyOfSharesOneReminder(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	s.KeyOf = func(context.Context, api.Item) (string, error) { return "home/chores/pending-1", nil }
	// The app's copy carries milliseconds; the bead bd stored does not.
	early := item(tp(t0.Add(-time.Minute + 250*time.Millisecond)))
	early.Key, early.ID = "home/chores/pending-1", "pending-1"
	src.items = []api.Item{early}
	s.Tick(context.Background())
	src.items = []api.Item{item(tp(t0.Add(-time.Minute)))}
	s.Tick(context.Background())
	if len(n.got) != 1 {
		t.Fatalf("sent %d reminders for one added task", len(n.got))
	}
}

func TestLinkUsesTheItemsOwnKey(t *testing.T) {
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	s.KeyOf = func(context.Context, api.Item) (string, error) { return "home/chores/pending-1", nil }
	// The bridge has reported the added bead; the provisional item is gone.
	real := item(tp(t0.Add(-time.Minute)))
	real.Key, real.ID = "home/chores/ch-7", "ch-7"
	src.items = []api.Item{real}
	s.Tick(context.Background())
	if len(n.got) != 1 {
		t.Fatalf("sent %d reminders, want 1", len(n.got))
	}
	if got := n.got[0].URL; !strings.Contains(got, url.QueryEscape("home/chores/ch-7")) || strings.Contains(got, "pending") {
		t.Fatalf("link %q does not point at the item's own key", got)
	}
}
