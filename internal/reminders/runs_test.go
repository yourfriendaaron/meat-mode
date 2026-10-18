package reminders

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/notify"
)

const runKey = "laptop/garden/gd-1"

func runItem(state, reason string) api.Item {
	it := api.Item{Key: runKey, ID: "gd-1", Title: "Water the plants", Status: "open",
		Run: &api.Run{ID: "r1", State: state, Agent: "planner", StartedAt: t0.Add(-10 * time.Minute), Count: 1}}
	if state != api.RunWorking {
		end := t0.Add(-time.Minute)
		it.Run.EndedAt, it.Run.Reason = &end, reason
	}
	return it
}

func actionNames(m notify.Message) []string {
	var out []string
	for _, a := range m.Actions {
		out = append(out, a.Action)
	}
	return out
}

func TestRunEndsPushOnce(t *testing.T) {
	ctx := context.Background()
	link := "https://app.example.com/?item=laptop%2Fgarden%2Fgd-1"
	for _, tc := range []struct {
		state, reason, detail string
		title, body           string
		actions               []string
	}{
		{api.RunWaiting, "", "", "planner replied", "Water the plants", []string{"reply", "open"}},
		{api.RunDone, "", "", "planner finished", "Water the plants", []string{"reply", "open"}},
		{api.RunStopped, api.ReasonUsageLimit, "The limit resets at 3 pm.", "planner stopped: usage limit reached",
			"Water the plants\nThe limit resets at 3 pm.", []string{"reply", "open", "retry"}},
	} {
		db, c, src, n := setup(t)
		s := sched(t, db, c, src, n)
		it := runItem(tc.state, tc.reason)
		it.Run.Detail = tc.detail
		src.items = []api.Item{it}
		if err := s.CheckRuns(ctx); err != nil {
			t.Fatal(err)
		}
		// The same report again, and the periodic check, send nothing more.
		if err := s.CheckRuns(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		if len(n.got) != 1 {
			t.Fatalf("%s: %d pushes", tc.state, len(n.got))
		}
		m := n.got[0]
		if m.Title != tc.title || m.Body != tc.body || m.URL != link || m.Key != runKey || m.Tag != "agent:"+runKey {
			t.Errorf("%s: message %+v", tc.state, m)
		}
		if got := actionNames(m); len(got) != len(tc.actions) || got[0] != tc.actions[0] || got[len(got)-1] != tc.actions[len(tc.actions)-1] {
			t.Errorf("%s: actions %v", tc.state, got)
		}
		if m.Actions[0].URL != link+"&reply=1" || m.Actions[1].URL != link {
			t.Errorf("%s: action links %+v", tc.state, m.Actions)
		}
	}
}

func TestRunPushesFollowTheRun(t *testing.T) {
	ctx := context.Background()
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	// Mid-run reports, a provisional run among them, push nothing.
	it := runItem(api.RunWorking, "")
	it.Run.Activity = "Filling the can"
	src.items = []api.Item{it}
	must := func() {
		t.Helper()
		if err := s.CheckRuns(ctx); err != nil {
			t.Fatal(err)
		}
	}
	must()
	it.Run.Activity, it.Run.Queued = "Watering", 1
	must()
	if len(n.got) != 0 {
		t.Fatalf("mid-run pushes %+v", n.got)
	}
	// The run ends waiting, then a follow-up starts a new run that ends done.
	src.items = []api.Item{runItem(api.RunWaiting, "")}
	must()
	next := runItem(api.RunDone, "")
	next.Run.ID = "r2"
	src.items = []api.Item{next}
	must()
	if len(n.got) != 2 || n.got[0].Title != "planner replied" || n.got[1].Title != "planner finished" {
		t.Fatalf("pushes %+v", n.got)
	}
}

func TestNoPushWhenTheUserStoppedOrClosed(t *testing.T) {
	ctx := context.Background()
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	closedSince := map[string]time.Time{}
	s.ClosedInApp = func(_ context.Context, key string, since time.Time) (bool, error) {
		at, ok := closedSince[key]
		return ok && !at.Before(since), nil
	}
	// The user stopped it.
	src.items = []api.Item{runItem(api.RunStopped, api.ReasonStopped)}
	if err := s.CheckRuns(ctx); err != nil {
		t.Fatal(err)
	}
	// The user closed the item while the run worked.
	closedSince[runKey] = t0.Add(-5 * time.Minute)
	done := runItem(api.RunDone, "")
	done.Run.ID, done.Status = "r2", "closed"
	src.items = []api.Item{done}
	if err := s.CheckRuns(ctx); err != nil {
		t.Fatal(err)
	}
	// Reopened later, the run that ended stays silent.
	delete(closedSince, runKey)
	done.Status = "open"
	if err := s.CheckRuns(ctx); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 0 {
		t.Fatalf("pushes %+v", n.got)
	}
	// A close before the run started does not hush it: the user can ask an
	// agent on a closed item.
	closedSince[runKey] = t0.Add(-time.Hour)
	later := runItem(api.RunWaiting, "")
	later.Run.ID, later.Status = "r3", "closed"
	src.items = []api.Item{later}
	if err := s.CheckRuns(ctx); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 1 {
		t.Fatalf("pushes %+v", n.got)
	}
}

func TestOfflineRunPushesOnce(t *testing.T) {
	ctx := context.Background()
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	// The store shows a silent bridge's working run as stopped, offline.
	it := runItem(api.RunStopped, api.ReasonOffline)
	src.items = []api.Item{it}
	for range 3 {
		if err := s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Back, the bridge reports the run stopped as restarted: same run, same
	// state, no second push.
	src.items = []api.Item{runItem(api.RunStopped, api.ReasonRestarted)}
	if err := s.CheckRuns(ctx); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 1 || n.got[0].Title != "planner stopped: the machine stopped reporting" {
		t.Fatalf("pushes %+v", n.got)
	}
}

func TestOldRunEndIsSilent(t *testing.T) {
	ctx := context.Background()
	db, c, src, n := setup(t)
	s := sched(t, db, c, src, n)
	it := runItem(api.RunWaiting, "")
	old := t0.Add(-RunLate - time.Minute)
	it.Run.EndedAt = &old
	src.items = []api.Item{it}
	if err := s.CheckRuns(ctx); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 0 {
		t.Fatalf("pushes %+v", n.got)
	}
}

func TestWakeChecksRuns(t *testing.T) {
	db, _, _, _ := setup(t)
	src := &lockedSource{calls: make(chan struct{}, 4)}
	ch := &chanNotifier{got: make(chan notify.Message, 4)}
	s, err := New(db, src, ch, &fakeClock{now: t0}, "https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	// The fake clock's interval never passes, so after the first check only
	// a wake checks again.
	<-src.calls
	src.set([]api.Item{runItem(api.RunDone, "")})
	s.Wake()
	select {
	case m := <-ch.got:
		if m.Title != "planner finished" {
			t.Fatalf("push %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a wake sent no push")
	}
}

type lockedSource struct {
	mu    sync.Mutex
	items []api.Item
	calls chan struct{}
}

func (l *lockedSource) set(items []api.Item) {
	l.mu.Lock()
	l.items = items
	l.mu.Unlock()
}

func (l *lockedSource) Items(context.Context) ([]api.Item, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case l.calls <- struct{}{}:
	default:
	}
	return l.items, nil
}

type chanNotifier struct{ got chan notify.Message }

func (c *chanNotifier) Notify(_ context.Context, m notify.Message) error {
	c.got <- m
	return nil
}
