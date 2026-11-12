package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/notify"
	"github.com/yourfriendaaron/meat-mode/internal/reminders"
)

// pairEpics is pair with garden's epics shown and switched on, and chores
// a repo that can show epics but whose switch is off.
func (h *harness) pairEpics() {
	h.t.Helper()
	h.pair()
	h.expect(h.do("PUT", "/bridge/v1/repos", []api.Repo{
		{Name: "garden", OptedIn: true, Epics: true, EpicsOn: true},
		{Name: "chores", Epics: true},
	}, h.token), http.StatusNoContent)
}

// epicMarkers are the strings only the invented epic rows below hold: none
// may appear on any path that serves the person's own to-dos.
var epicMarkers = []string{"gd-77", "gd-78", "Plan the beds", "Dig the beds", "Fix the fence", "Spring beds", "epic copy", "Two coats"}

// epicSet is an invented epic set. gd-1 is a human row whose id is also the
// item gd-1 that pair adds, under another title.
func epicSet() []api.EpicRow {
	at := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	return []api.EpicRow{
		{ID: "gd-77", Title: "Plan the beds", Type: "epic", Status: "open", Priority: 1, Epic: true, Description: "Spring beds.", UpdatedAt: at},
		{ID: "gd-77.1", Title: "Dig the beds", Status: "open", Priority: 2, Parent: "gd-77", Assignee: "sam", UpdatedAt: at},
		{ID: "gd-1", Title: "Water the plants, the epic copy", Status: "open", Priority: 2, Parent: "gd-77", Human: true, UpdatedAt: at},
		{ID: "gd-78", Title: "Fix the fence", Status: "in_progress", Priority: 3, UpdatedAt: at, CommentCount: 1,
			Comments: []api.Comment{{ID: "c-1", Author: "sam", Text: "Two coats.", CreatedAt: at}}},
	}
}

func TestEpicsRoutes(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pairEpics()
	if e := decode[api.Epics](t, h.do("GET", "/api/epics", nil, "")); e.Repos == nil || len(e.Repos) != 0 {
		t.Fatalf("epics before a set: %+v", e)
	}

	rev := h.rev()
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", epicSet(), h.token), http.StatusNoContent)
	if h.rev() != rev {
		t.Error("an epics set bumped the change revision")
	}
	e := decode[api.Epics](t, h.do("GET", "/api/epics", nil, ""))
	if len(e.Repos) != 1 || e.Repos[0].Bridge != "laptop" || e.Repos[0].Repo != "garden" || len(e.Repos[0].Rows) != 4 ||
		e.Repos[0].Rows[0].ID != "gd-1" || e.Repos[0].SyncedAt.IsZero() {
		t.Fatalf("epics %+v", e)
	}
	for _, r := range e.Repos[0].Rows {
		if r.ID == "gd-78" && (len(r.Comments) != 1 || r.Comments[0].Text != "Two coats." || r.Comments[0].Author != "sam" || r.Comments[0].CreatedAt.IsZero()) {
			t.Errorf("gd-78 comments %+v", r.Comments)
		}
	}

	// 409, never 404, for a repo the report lacks or shows without epicsOn.
	for _, repo := range []string{"shed", "chores"} {
		h.expect(h.do("PUT", "/bridge/v1/epics?repo="+repo, epicSet(), h.token), http.StatusConflict)
	}
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=..", epicSet(), h.token), http.StatusBadRequest)
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", []api.EpicRow{{ID: "-x", Status: "open"}}, h.token), http.StatusBadRequest)
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", epicSet(), ""), http.StatusUnauthorized)
	big := `[{"id": "gd-1", "title": "` + strings.Repeat("a", maxItemsBody) + `"}]`
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", big, h.token), http.StatusRequestEntityTooLarge)

	// The switch, either way, shown pending until the next report.
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindEpics, Bridge: "laptop", Repo: "chores",
		Payload: api.Payload{On: ptrTo(true)}}, ""), http.StatusCreated)
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindEpics, Bridge: "laptop", Repo: "barn",
		Payload: api.Payload{On: ptrTo(false)}}, ""), http.StatusNotFound)
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindEpics, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{On: ptrTo(false)}}, ""), http.StatusCreated)
	st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	for _, r := range st.Repos {
		if r.Name == "garden" && (r.EpicsOn || !r.EpicsPending) {
			t.Errorf("garden while switching off: %+v", r)
		}
		if r.Name == "chores" && (!r.EpicsOn || !r.EpicsPending) {
			t.Errorf("chores while switching on: %+v", r)
		}
	}
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", epicSet(), h.token), http.StatusNoContent)
	if e := decode[api.Epics](t, h.do("GET", "/api/epics", nil, "")); len(e.Repos) != 0 {
		t.Errorf("epics while switching off: %+v", e)
	}

	// Behind the session, like /api/state.
	h.cookie = ""
	h.expect(h.do("GET", "/api/epics", nil, ""), http.StatusUnauthorized)
}

func ptrTo[T any](v T) *T { return &v }

// TestEpicsEvent checks that an epics write sends only `event: epics`, and
// that a repo report or a revoke that drops rows sends it too.
func TestEpicsEvent(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pairEpics()
	ts := httptest.NewServer(h.srv)
	// Closed after the stream's own cleanup cancels it.
	t.Cleanup(ts.Close)
	lines := openSessionStream(t, ts, h.cookie)
	// next returns the next event's name and data.
	next := func() (string, string) {
		t.Helper()
		var name string
		timeout := time.After(3 * time.Second)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatal("stream closed")
				}
				if n, ok := strings.CutPrefix(l, "event: "); ok {
					name = n
				}
				if d, ok := strings.CutPrefix(l, "data: "); ok {
					return name, d
				}
			case <-timeout:
				t.Fatal("no event")
			}
		}
	}
	if n, _ := next(); n != "change" {
		t.Fatalf("first event %q", n)
	}
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", epicSet(), h.token), http.StatusNoContent)
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", epicSet(), h.token), http.StatusNoContent)
	if n, d := next(); n != api.EpicsEvent || d != "{}" {
		t.Fatalf("event %q %q, want epics {}", n, d)
	}
	// A change after it is the next event: the epics writes sent no change,
	// and the two coalesced, or arrived both, as epics.
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindAdd,
		Bridge: "hub", Repo: "personal", Payload: api.Payload{Title: "Buy seeds"}}, ""), http.StatusCreated)
	n, _ := next()
	if n == api.EpicsEvent {
		n, _ = next()
	}
	if n != "change" {
		t.Fatalf("event %q, want change", n)
	}

	// A report without epicsOn drops the rows and sends both.
	h.expect(h.do("PUT", "/bridge/v1/repos", []api.Repo{{Name: "garden", OptedIn: true, Epics: true}}, h.token), http.StatusNoContent)
	got := map[string]bool{}
	for len(got) < 2 {
		n, _ := next()
		got[n] = true
	}
	if !got["change"] || !got[api.EpicsEvent] {
		t.Fatalf("events after the report %v", got)
	}

	// A revoke drops the bridge's rows and sends epics.
	h.expect(h.do("PUT", "/bridge/v1/repos", []api.Repo{{Name: "garden", OptedIn: true, Epics: true, EpicsOn: true}}, h.token), http.StatusNoContent)
	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", epicSet(), h.token), http.StatusNoContent)
	for len(got) > 0 {
		n, _ := next()
		delete(got, n)
	}
	h.expect(h.do("DELETE", "/api/bridges/laptop", nil, ""), http.StatusNoContent)
	got = map[string]bool{}
	for !got[api.EpicsEvent] {
		n, _ := next()
		got[n] = true
	}
	var rows int
	if err := h.st.DB().QueryRow(`SELECT COUNT(*) FROM epic_rows`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("epic rows after the revoke: %d %v", rows, err)
	}
}

// pushes records what the hub would push.
type pushes struct {
	mu  sync.Mutex
	got []notify.Message
}

func (p *pushes) Notify(_ context.Context, m notify.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, m)
	return nil
}

// stateSource feeds the scheduler State's items, as the hub does.
type stateSource struct{ h *harness }

func (s stateSource) Items(context.Context) ([]api.Item, error) {
	st, err := s.h.st.State()
	return st.Items, err
}

// TestEpicRowsStayOutOfTheTodos is the Epics view's main safeguard: epic
// rows reach no path that serves the person's own to-dos. State, the
// reminders and pushes, the calendar feed, the counts and the export look
// the same with them as without, and a restore brings none back.
func TestEpicRowsStayOutOfTheTodos(t *testing.T) {
	h := newHarness(t, nil, false)
	h.srv.cfg.PublicURL = "https://app.example.com/"
	h.pairEpics()
	due := time.Now().UTC().Add(-time.Minute)
	h.expect(h.do("PUT", "/bridge/v1/items?repo=garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open", Priority: 2, Labels: []string{api.HumanLabel}, DueAt: &due},
		{ID: "gd-2", Title: "Buy seeds", Status: "open", DueAt: &due},
	}, h.token), http.StatusNoContent)
	h.addTodo("Pay the rent")
	h.expect(h.do("POST", "/api/calendar", map[string]any{}, ""), http.StatusOK)
	link := decode[api.CalendarLink](t, h.do("GET", "/api/calendar", nil, "")).URL
	if link == "" {
		w := h.do("POST", "/api/calendar", map[string]any{}, "")
		link = decode[api.CalendarLink](t, w).URL
	}
	feedPath := "/calendar/" + strings.TrimPrefix(link, "https://app.example.com/calendar/")

	// Every view of the to-dos, as text with the clock taken out.
	views := func() map[string]string {
		t.Helper()
		st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
		// The bridge's last-seen moves with every request it sends.
		st.ServerTime = time.Time{}
		for i := range st.Bridges {
			st.Bridges[i].LastSeen = nil
		}
		state, _ := json.Marshal(st)
		w := feed(h.srv, feedPath, "localhost:8080")
		h.expect(w, http.StatusOK)
		w2 := h.do("GET", "/api/export", nil, "")
		h.expect(w2, http.StatusOK)
		e := exportOf(t, w2.Body.Bytes())
		if e.Version != 6 {
			t.Errorf("export version %d, want 6: Epics adds nothing to the export", e.Version)
		}
		e.ExportedAt = time.Time{}
		export, _ := json.Marshal(e)
		actions := h.do("GET", "/api/actions?limit=500", nil, "").Body.String()
		return map[string]string{"state": string(state), "calendar": w.Body.String(), "export": string(export), "actions": actions}
	}
	without := views()

	h.expect(h.do("PUT", "/bridge/v1/epics?repo=garden", epicSet(), h.token), http.StatusNoContent)
	// A run report for an epic row's bead is one the hub does not hold.
	end := time.Now().UTC()
	h.expect(h.do("PUT", "/bridge/v1/runs", []api.RunReport{{Repo: "garden", BeadID: "gd-77", Run: api.Run{
		ID: "r1", State: api.RunDone, Agent: "planner", StartedAt: end.Add(-time.Minute), EndedAt: &end, Count: 1,
	}}}, h.token), http.StatusNoContent)
	if e := decode[api.Epics](t, h.do("GET", "/api/epics", nil, "")); len(e.Repos) != 1 || len(e.Repos[0].Rows) != 4 {
		t.Fatalf("the epic set was not taken: %+v", e)
	}
	with := views()
	for name, body := range with {
		if body != without[name] {
			t.Errorf("%s differs with epic rows:\nwithout %s\nwith    %s", name, without[name], body)
		}
		for _, m := range epicMarkers {
			if strings.Contains(body, m) {
				t.Errorf("%s holds %q", name, m)
			}
		}
	}

	// No action takes an epic row.
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindDone, Key: "laptop/garden/gd-77"}, ""), http.StatusNotFound)
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindSnooze, Key: "laptop/garden/gd-78",
		Payload: api.Payload{Until: &end}}, ""), http.StatusNotFound)

	// Reminders and what they would push name only the real items.
	p := &pushes{}
	sc, err := reminders.New(h.st.DB(), stateSource{h}, p, nil, "https://app.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(p.got) == 0 {
		t.Fatal("the real items' reminders did not fire")
	}
	for _, m := range p.got {
		b, _ := json.Marshal(m)
		for _, mk := range epicMarkers {
			if strings.Contains(string(b), mk) {
				t.Errorf("push %s holds %q", b, mk)
			}
		}
	}

	// A restore of the export brings no epic rows.
	w := h.do("GET", "/api/export", nil, "")
	h.expect(w, http.StatusOK)
	fresh := newHarness(t, nil, false)
	fresh.expect(fresh.send("POST", "/api/restore", "application/octet-stream", w.Body.Bytes()), http.StatusOK)
	var rows int
	if err := fresh.st.DB().QueryRow(`SELECT COUNT(*) FROM epic_rows`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("epic rows after a restore: %d %v", rows, err)
	}
	if e := decode[api.Epics](t, fresh.do("GET", "/api/epics", nil, "")); len(e.Repos) != 0 {
		t.Errorf("epics after a restore: %+v", e)
	}
}
