package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// epicTracker is a tracker with tracker.Epics: its rows are whatever the
// test sets.
type epicTracker struct {
	tracker.Tracker
	mu    sync.Mutex
	rows  []api.EpicRow
	reads int
}

func (e *epicTracker) EpicRows(context.Context, string) ([]api.EpicRow, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reads++
	return append([]api.EpicRow(nil), e.rows...), nil
}

func (e *epicTracker) set(rows ...api.EpicRow) {
	e.mu.Lock()
	e.rows = rows
	e.mu.Unlock()
}

func (e *epicTracker) readCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reads
}

// plainTracker hides every optional interface of the tracker it wraps, so
// it cannot show epics whatever the fake learns.
type plainTracker struct{ tracker.Tracker }

var (
	gardenEpic = api.EpicRow{ID: "gd-1", Title: "Plant the beds", Type: "epic", Status: "open", Priority: 1}
	gardenSeed = api.EpicRow{ID: "gd-1.1", Title: "Buy the seeds", Type: "task", Status: "open", Priority: 2, Parent: "gd-1"}
	gardenHose = api.EpicRow{ID: "gd-2", Title: "Mend the hose", Type: "task", Status: "open", Priority: 2}
)

// epicsWorld is a bridge over one repo whose tracker shows epics, switched
// on, with sync off, and a clock the test moves.
type epicsWorld struct {
	*world
	et  *epicTracker
	b   *Bridge
	now time.Time
	log *bytes.Buffer
}

func newEpicsWorld(t *testing.T) *epicsWorld {
	t.Helper()
	w := newWorld(t, false)
	et := &epicTracker{Tracker: w.fake.Tracker()}
	et.set(gardenEpic, gardenSeed, gardenHose)
	w.tr = et
	w.repo("projects/garden", false)
	ew := &epicsWorld{world: w, et: et, now: time.Date(2030, 3, 1, 9, 0, 0, 0, time.UTC), log: &bytes.Buffer{}}
	ew.b = w.start()
	ew.b.Now = func() time.Time { return ew.now }
	ew.b.Log = slog.New(slog.NewTextHandler(ew.log, nil))
	ew.b.Epics = map[string]bool{"projects/garden": true}
	ew.b.SaveEpics = func(string, bool) error { return nil }
	return ew
}

// after moves the clock on by d and ticks.
func (ew *epicsWorld) after(d time.Duration) {
	ew.t.Helper()
	ew.now = ew.now.Add(d)
	ew.tick(ew.b)
}

func (ew *epicsWorld) puts() int { return ew.hub.EpicPuts(us, "projects/garden") }

func (ew *epicsWorld) rows() []api.EpicRow {
	rows, _ := ew.hub.Epics(us, "projects/garden")
	return rows
}

func ids(rows []api.EpicRow) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// TestEpicsSent: a repo with epics on and sync off sends its outline,
// reports epics and epicsOn, reads at most once a minute, and sends an
// unchanged set again only after RefreshEvery.
func TestEpicsSent(t *testing.T) {
	ew := newEpicsWorld(t)
	ew.tick(ew.b)
	want := ew.wire(api.Repo{Name: "projects/garden", Epics: true, EpicsOn: true})
	if got := ew.hub.Repos(us); !reflect.DeepEqual(got, want) {
		t.Errorf("repos = %+v, want %+v", got, want)
	}
	if _, ok := ew.hub.Items(us, "projects/garden"); ok {
		t.Error("a repo with sync off sent items")
	}
	rows := ew.rows()
	if got := ids(rows); !reflect.DeepEqual(got, []string{"gd-1", "gd-1.1", "gd-2"}) || !rows[0].Epic || rows[1].Parent != "gd-1" {
		t.Fatalf("rows = %+v", rows)
	}

	ew.after(30 * time.Second)
	if ew.et.readCount() != 1 {
		t.Errorf("read %d times within a minute", ew.et.readCount())
	}
	ew.after(time.Minute)
	if ew.et.readCount() != 2 || ew.puts() != 1 {
		t.Errorf("unchanged set: %d reads, %d puts", ew.et.readCount(), ew.puts())
	}
	ew.after(RefreshEvery)
	if ew.puts() != 2 {
		t.Errorf("after RefreshEvery: %d puts, want 2", ew.puts())
	}

	ew.et.set(gardenEpic, gardenSeed)
	ew.after(time.Minute)
	if got := ids(ew.rows()); ew.puts() != 3 || !reflect.DeepEqual(got, []string{"gd-1", "gd-1.1"}) {
		t.Errorf("changed set: %d puts, rows %v", ew.puts(), got)
	}
}

// TestEpicsEmptyReadHeld: one empty read never replaces a full set; two in
// a row do.
func TestEpicsEmptyReadHeld(t *testing.T) {
	ew := newEpicsWorld(t)
	ew.tick(ew.b)
	ew.et.set()
	ew.after(time.Minute)
	if len(ew.rows()) != 3 || ew.puts() != 1 {
		t.Fatalf("one empty read: %d puts, rows %v", ew.puts(), ids(ew.rows()))
	}
	// A full read in between starts the count again.
	ew.et.set(gardenEpic, gardenSeed, gardenHose)
	ew.after(time.Minute)
	ew.et.set()
	ew.after(time.Minute)
	if len(ew.rows()) != 3 || ew.puts() != 1 {
		t.Fatalf("empty, full, empty: %d puts, rows %v", ew.puts(), ids(ew.rows()))
	}
	ew.after(time.Minute)
	if rows, ok := ew.hub.Epics(us, "projects/garden"); !ok || len(rows) != 0 || ew.puts() != 2 {
		t.Errorf("two empty reads: %d puts, rows %v", ew.puts(), rows)
	}
}

// TestEpicsOldHub: a hub without the epics route is logged once and sent
// no epics for an hour, and the round carries on without a new hello.
func TestEpicsOldHub(t *testing.T) {
	ew := newEpicsWorld(t)
	ew.hub.SetNoEpics(true)
	ew.tick(ew.b)
	ew.after(2 * time.Minute)
	ew.after(30 * time.Minute)
	if ew.puts() != 1 {
		t.Errorf("within the hour: %d puts, want 1", ew.puts())
	}
	ew.after(30 * time.Minute)
	if ew.puts() != 2 {
		t.Errorf("after the hour: %d puts, want 2", ew.puts())
	}
	if n := strings.Count(ew.log.String(), "does not take epics"); n != 1 {
		t.Errorf("logged %d times:\n%s", n, ew.log)
	}
	if n := len(ew.hub.Hellos()); n != 1 {
		t.Errorf("%d hellos, want 1", n)
	}
	ew.hub.SetNoEpics(false)
	ew.after(EpicsBackoff)
	if len(ew.rows()) != 3 {
		t.Errorf("an upgraded hub got %v", ids(ew.rows()))
	}
}

// TestEpicsConflictReportsRepos: a 409 makes the next round report the
// repos again, and the set goes once the hub holds the repo with epics on.
func TestEpicsConflictReportsRepos(t *testing.T) {
	ew := newEpicsWorld(t)
	ew.tick(ew.b)
	// The hub loses the repo's epics switch behind the bridge's back.
	if err := ew.b.Hub.PutRepos(ew.ctx, ew.wire(api.Repo{Name: "projects/garden", Epics: true})); err != nil {
		t.Fatal(err)
	}
	ew.et.set(gardenEpic, gardenSeed)
	ew.after(RefreshEvery / 2)
	if ew.puts() != 2 || ew.rows() != nil {
		t.Fatalf("after the conflict: %d puts, rows %v", ew.puts(), ids(ew.rows()))
	}
	ew.after(time.Second)
	if got := ew.hub.Repos(us); len(got) != 1 || !got[0].EpicsOn {
		t.Errorf("repos not reported again: %+v", got)
	}
	ew.after(time.Minute)
	if len(ew.rows()) != 2 {
		t.Errorf("unchanged set not sent after the conflict: %v", ids(ew.rows()))
	}
	if n := len(ew.hub.Hellos()); n != 1 {
		t.Errorf("%d hellos, want 1", n)
	}
}

// TestEpicsRefusedSet: a set the hub refuses is logged and sent again only
// once it changes.
func TestEpicsRefusedSet(t *testing.T) {
	ew := newEpicsWorld(t)
	ew.hub.RefuseEpics(func(rows []api.EpicRow) bool { return len(rows) == 3 })
	extra := gardenHose
	extra.ID = "gd-3"
	ew.et.set(gardenEpic, gardenHose, extra)
	ew.tick(ew.b)
	ew.after(time.Minute)
	ew.after(RefreshEvery)
	if ew.puts() != 1 || ew.rows() != nil {
		t.Errorf("refused set: %d puts, rows %v", ew.puts(), ids(ew.rows()))
	}
	if !strings.Contains(ew.log.String(), "refused this repo's epics") {
		t.Errorf("not logged:\n%s", ew.log)
	}
	ew.et.set(gardenEpic, gardenHose)
	ew.after(time.Minute)
	if got := ids(ew.rows()); !reflect.DeepEqual(got, []string{"gd-1", "gd-2"}) {
		t.Errorf("changed set: rows %v", got)
	}
}

// TestEpicsSwitch mirrors the sync switch: the app switches epics either
// way; a switch-off is reported at once, drops the rows and stops the
// sends, and a switch-on sends the set again.
func TestEpicsSwitch(t *testing.T) {
	ew := newEpicsWorld(t)
	saved := map[string]bool{}
	ew.b.SaveEpics = func(repo string, on bool) error { saved[repo] = on; return nil }
	ew.tick(ew.b)
	on, off := true, false
	ew.hub.Enqueue(us, action("ep-unknown", api.KindEpics, "projects/shed", "", api.Payload{On: &off}))
	ew.hub.Enqueue(us, action("ep-off", api.KindEpics, "projects/garden", "", api.Payload{On: &off}))
	ew.after(RefreshEvery)
	if rs := ew.hub.Results(aid("ep-unknown")); len(rs) != 1 || rs[0].Error != ErrUnknownRepo {
		t.Errorf("unknown repo: %+v", rs)
	}
	ew.oneOK(aid("ep-off"))
	if !maps.Equal(saved, map[string]bool{"projects/garden": false}) {
		t.Errorf("saved %v", saved)
	}
	want := ew.wire(api.Repo{Name: "projects/garden", Epics: true})
	if got := ew.hub.Repos(us); !reflect.DeepEqual(got, want) {
		t.Errorf("repos = %+v, want %+v", got, want)
	}
	if _, ok := ew.hub.Epics(us, "projects/garden"); ok || ew.puts() != 1 {
		t.Errorf("after the switch-off: %d puts, rows kept %v", ew.puts(), ok)
	}
	if len(ew.b.epicStates) != 0 {
		t.Errorf("pass state kept: %+v", ew.b.epicStates)
	}
	ew.after(RefreshEvery)
	if ew.puts() != 1 {
		t.Errorf("sent after the switch-off: %d puts", ew.puts())
	}

	ew.hub.Enqueue(us, action("ep-on", api.KindEpics, "projects/garden", "", api.Payload{On: &on}))
	ew.after(RefreshEvery)
	ew.oneOK(aid("ep-on"))
	if !maps.Equal(saved, map[string]bool{"projects/garden": true}) {
		t.Errorf("saved after the switch-on %v", saved)
	}
	if got := ew.hub.Repos(us); len(got) != 1 || !got[0].EpicsOn {
		t.Errorf("repos after the switch-on = %+v", got)
	}
	if _, ok := ew.hub.Epics(us, "projects/garden"); !ok {
		t.Errorf("no set after the switch-on: %d puts", ew.puts())
	}

	ew.b.SaveEpics = nil
	ew.hub.Enqueue(us, action("ep-nosave", api.KindEpics, "projects/garden", "", api.Payload{On: &off}))
	ew.after(time.Second)
	if rs := ew.hub.Results(aid("ep-nosave")); len(rs) != 1 || rs[0].Error != "this bridge cannot save epics switches" {
		t.Errorf("without SaveEpics: %+v", rs)
	}
}

// TestEpicsNeedTheTracker: a repo whose tracker cannot show epics is
// reported epics false and sends none, even with its switch on.
func TestEpicsNeedTheTracker(t *testing.T) {
	w := newWorld(t, false)
	w.tr = plainTracker{w.fake.Tracker()}
	w.repo("projects/garden", false)
	b := w.start()
	b.Epics = map[string]bool{"projects/garden": true}
	w.tick(b)
	if got := w.hub.Repos(us); len(got) != 1 || got[0].Epics || got[0].EpicsOn {
		t.Errorf("repos = %+v", got)
	}
	if n := w.hub.EpicPuts(us, "projects/garden"); n != 0 {
		t.Errorf("%d epics puts", n)
	}
}

func TestEpicsCommand(t *testing.T) {
	env, garden, chores := agentsEnv(t)
	t.Chdir(t.TempDir())
	s, err := LoadSettings(env)
	if err != nil {
		t.Fatal(err)
	}
	repos, err := s.Repos()
	if err != nil {
		t.Fatal(err)
	}
	for i := range repos {
		// garden's tracker shows epics; chores' cannot.
		if repos[i].Dir == garden {
			repos[i].Tracker = &epicTracker{Tracker: repos[i].Tracker}
		} else {
			repos[i].Tracker = plainTracker{repos[i].Tracker}
		}
	}
	run := func(args ...string) (string, error) {
		out := &strings.Builder{}
		env.Stdout, env.Stderr = out, out
		c, _ := readConfigFile(env.Paths.Config)
		err := switchEpics(args, env, repos, c.Epics)
		return out.String(), err
	}

	out, err := run()
	if err != nil || !strings.Contains(out, "projects/garden") || strings.Contains(out, "chores") || strings.Contains(out, "epics on") {
		t.Fatalf("list = %q, %v", out, err)
	}
	if _, err := run("on", "projects/garden"); err != nil {
		t.Fatal(err)
	}
	c, _ := readConfigFile(env.Paths.Config)
	if !c.Epics["projects/garden"] || !c.Sync["chores"] || c.Token != "t" || len(c.Agents) != 0 {
		t.Errorf("config after on = %+v", c)
	}
	if out, _ := run(); !strings.Contains(out, "epics on") {
		t.Errorf("list after on = %q", out)
	}
	if _, err := run("off", garden); err != nil {
		t.Fatal(err)
	}
	if c, _ := readConfigFile(env.Paths.Config); c.Epics["projects/garden"] {
		t.Errorf("config after off <path> = %+v", c.Epics)
	}

	// A tracker that cannot show epics, and a repo the bridge does not find.
	if _, err := run("on", chores); err == nil || !strings.Contains(err.Error(), "cannot show epics") {
		t.Errorf("on chores = %v", err)
	}
	if _, err := run("on", "projects/shed"); err == nil || !strings.Contains(err.Error(), "meat-mode bridge epics") {
		t.Errorf("on an unknown repo = %v", err)
	}
	if c, _ := readConfigFile(env.Paths.Config); len(c.Epics) != 1 {
		t.Errorf("a refused repo was saved: %+v", c.Epics)
	}

	// Through Main, the usage.
	for _, args := range [][]string{{"on"}, {"maybe", "projects/garden"}, {"on", "a", "b"}} {
		out := &strings.Builder{}
		env.Stdout, env.Stderr = out, out
		if err := Main(context.Background(), append([]string{"epics"}, args...), env); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("epics %v = %v, want usage", args, err)
		}
	}
}

func TestBridgeRereadsEpicsBeforeReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	must(t, Config{Token: "t"}.Save(path))
	b := &Bridge{LoadEpics: func() (map[string]bool, error) { return LoadEpics(path) }}
	b.reloadEpics()
	if b.Epics["garden"] {
		t.Fatal("epics on before the switch")
	}
	must(t, SaveEpics(path, "garden", true))
	b.reloadEpics()
	if !b.Epics["garden"] {
		t.Error("the switch made by the command did not reach the bridge")
	}
	must(t, os.WriteFile(path, []byte("{"), 0o600))
	b.reloadEpics()
	if !b.Epics["garden"] {
		t.Error("a failed reread dropped the switches")
	}
}
