package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	// comments is what EpicComments answers, by id; commentCalls are the
	// ids it was asked for, one slice per call; commentErr fails it.
	comments     map[string][]api.Comment
	commentCalls [][]string
	commentErr   error
}

func (e *epicTracker) EpicComments(_ context.Context, _ string, ids []string) (map[string][]api.Comment, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.commentCalls = append(e.commentCalls, slices.Clone(ids))
	if e.commentErr != nil {
		return nil, e.commentErr
	}
	got := map[string][]api.Comment{}
	for _, id := range ids {
		if cs, ok := e.comments[id]; ok {
			got[id] = cs
		}
	}
	return got, nil
}

func (e *epicTracker) calls() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.commentCalls)
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

// commented is a row with n comments, updated at the given time.
func commented(r api.EpicRow, n int, at time.Time) api.EpicRow {
	r.CommentCount, r.UpdatedAt = n, at
	return r
}

func gardenComments(id string, n int) []api.Comment {
	var cs []api.Comment
	for i := range n {
		cs = append(cs, api.Comment{ID: fmt.Sprintf("%s-c%d", id, i), Author: "sam", Text: "Water the plants " + id, CreatedAt: time.Date(2030, 1, 1+i, 9, 0, 0, 0, time.UTC)})
	}
	return cs
}

func hasComments(rows []api.EpicRow, id string) bool {
	for _, r := range rows {
		if r.ID == id {
			return len(r.Comments) > 0
		}
	}
	return false
}

// TestEpicsComments: a row with comments gets their text, a row without
// none and no call; an unchanged row is served from the cache, a row whose
// count or updatedAt moved is read again, and a row that left the set leaves
// the cache.
func TestEpicsComments(t *testing.T) {
	ew := newEpicsWorld(t)
	t0 := time.Date(2030, 2, 1, 9, 0, 0, 0, time.UTC)
	seed, hose := commented(gardenSeed, 2, t0), commented(gardenHose, 1, t0)
	ew.et.comments = map[string][]api.Comment{"gd-1.1": gardenComments("gd-1.1", 2), "gd-2": gardenComments("gd-2", 1)}
	ew.et.set(gardenEpic, seed, hose)
	ew.tick(ew.b)
	rows := ew.rows()
	if got := rows[1].Comments; !reflect.DeepEqual(got, ew.et.comments["gd-1.1"]) {
		t.Fatalf("seed comments = %+v", got)
	}
	if len(rows[0].Comments) != 0 {
		t.Errorf("a row with no comments got %+v", rows[0].Comments)
	}
	if got := ew.et.calls(); !reflect.DeepEqual(got, [][]string{{"gd-1.1", "gd-2"}}) {
		t.Fatalf("first pass asked for %v", got)
	}

	// Nothing changed: the cache answers and no call is made.
	ew.after(time.Minute)
	if got := len(ew.et.calls()); got != 1 {
		t.Errorf("unchanged rows made %d calls", got)
	}
	// A new comment moves the count: only that row is read again.
	ew.et.comments["gd-2"] = gardenComments("gd-2", 2)
	ew.et.set(gardenEpic, seed, commented(gardenHose, 2, t0))
	ew.after(time.Minute)
	if got := ew.et.calls(); len(got) != 2 || !reflect.DeepEqual(got[1], []string{"gd-2"}) {
		t.Errorf("changed count: calls %v", got)
	}
	if got := ew.rows()[2].Comments; len(got) != 2 {
		t.Errorf("hose comments = %+v", got)
	}
	// An edited comment moves updatedAt at the same count.
	ew.et.set(gardenEpic, seed, commented(gardenHose, 2, t0.Add(time.Hour)))
	ew.after(time.Minute)
	if got := ew.et.calls(); len(got) != 3 {
		t.Errorf("changed updatedAt: calls %v", got)
	}
	// A row that leaves the set leaves the cache: back again, it is read.
	ew.et.set(gardenEpic, seed)
	ew.after(time.Minute)
	ew.et.set(gardenEpic, seed, commented(gardenHose, 2, t0.Add(time.Hour)))
	ew.after(time.Minute)
	if got := ew.et.calls(); len(got) != 4 || !reflect.DeepEqual(got[3], []string{"gd-2"}) {
		t.Errorf("row back after leaving: calls %v", got)
	}
}

// TestEpicsCommentsCapPerPass: at most 50 rows are read a pass, in Budget
// order (cards, open, closed, then id), and the rest go without comments
// until a later pass.
func TestEpicsCommentsCapPerPass(t *testing.T) {
	ew := newEpicsWorld(t)
	t0 := time.Date(2030, 2, 1, 9, 0, 0, 0, time.UTC)
	ew.et.comments = map[string][]api.Comment{}
	rows := []api.EpicRow{commented(api.EpicRow{ID: "gd-0", Title: "Plan", Type: "epic", Status: "open"}, 1, t0)}
	ew.et.comments["gd-0"] = gardenComments("gd-0", 1)
	for i := range 59 {
		id := fmt.Sprintf("gd-c%02d", i)
		status := "closed"
		if i >= 30 {
			status = "open"
		}
		rows = append(rows, commented(api.EpicRow{ID: id, Title: "Task", Status: status, Parent: "gd-0"}, 1, t0))
		ew.et.comments[id] = gardenComments(id, 1)
	}
	ew.et.set(rows...)
	ew.tick(ew.b)
	calls := ew.et.calls()
	if len(calls) != 1 || len(calls[0]) != EpicCommentReads {
		t.Fatalf("calls = %v", calls)
	}
	// The card, then the 29 open rows by id (gd-c30..gd-c58 are open
	// children), then the closed ones by id.
	if calls[0][0] != "gd-0" || calls[0][1] != "gd-c30" || calls[0][29] != "gd-c58" || calls[0][30] != "gd-c00" || calls[0][49] != "gd-c19" {
		t.Errorf("read order = %v", calls[0])
	}
	got := ew.rows()
	if !hasComments(got, "gd-c19") || hasComments(got, "gd-c20") {
		t.Errorf("comments past the cap: c19 %v, c20 %v", hasComments(got, "gd-c19"), hasComments(got, "gd-c20"))
	}
	// The next pass reads the rest and only the rest.
	ew.after(time.Minute)
	calls = ew.et.calls()
	if len(calls) != 2 || len(calls[1]) != 10 || calls[1][0] != "gd-c20" {
		t.Fatalf("second pass calls = %v", calls)
	}
	if !hasComments(ew.rows(), "gd-c20") || !hasComments(ew.rows(), "gd-c58") {
		t.Error("rows still without comments after the second pass")
	}
}

// TestEpicsCommentsFailed: a failed read is logged once for the pass, the
// set is still sent, and comments the bridge already holds stay.
func TestEpicsCommentsFailed(t *testing.T) {
	ew := newEpicsWorld(t)
	t0 := time.Date(2030, 2, 1, 9, 0, 0, 0, time.UTC)
	ew.et.comments = map[string][]api.Comment{"gd-1.1": gardenComments("gd-1.1", 1), "gd-2": gardenComments("gd-2", 1)}
	ew.et.set(gardenEpic, commented(gardenSeed, 1, t0), gardenHose)
	ew.tick(ew.b)

	ew.et.commentErr = errors.New("bd is busy")
	ew.et.set(gardenEpic, commented(gardenSeed, 1, t0), commented(gardenHose, 1, t0))
	ew.after(time.Minute)
	rows := ew.rows()
	if len(rows) != 3 || !hasComments(rows, "gd-1.1") || hasComments(rows, "gd-2") {
		t.Errorf("rows after a failed read: %+v", rows)
	}
	if n := strings.Count(ew.log.String(), "cannot read epic comments"); n != 1 {
		t.Errorf("logged %d times:\n%s", n, ew.log)
	}
}

// TestEpicsCommentsBudget: the bridge applies the budget after filling
// comments, so a set whose comment text is past 4 MiB drops whole rows'
// comments rather than being refused by the hub.
func TestEpicsCommentsBudget(t *testing.T) {
	ew := newEpicsWorld(t)
	t0 := time.Date(2030, 2, 1, 9, 0, 0, 0, time.UTC)
	big := []api.Comment{{ID: "c1", Author: "sam", Text: strings.Repeat("a", api.CommentLimit)}}
	ew.et.comments = map[string][]api.Comment{}
	var rows []api.EpicRow
	for i := range api.MaxEpicDescriptions/api.CommentLimit + 5 {
		id := fmt.Sprintf("gd-%03d", i)
		rows = append(rows, commented(api.EpicRow{ID: id, Status: "open"}, 1, t0))
		ew.et.comments[id] = big
	}
	ew.et.set(rows...)
	ew.tick(ew.b)
	for range 4 { // 214 rows, EpicCommentReads a pass
		ew.after(time.Minute)
	}
	got := ew.rows()
	if len(got) != len(rows) {
		t.Fatalf("the hub took %d rows of %d", len(got), len(rows))
	}
	total := 0
	for _, r := range got {
		for _, c := range r.Comments {
			total += len(c.Text)
		}
	}
	if total > api.MaxEpicDescriptions || total < api.MaxEpicDescriptions-api.CommentLimit {
		t.Errorf("comment text totals %d bytes", total)
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
