package view

import (
	"slices"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// nestedEpics is a repo where sorting alone would part a sub-epic from its
// parent: the parent is deferred and low, the sub-epic urgent.
func nestedEpics() api.Epics {
	return api.Epics{Repos: []api.RepoEpics{{Bridge: "desk", Repo: "garden", Rows: []api.EpicRow{
		{ID: "gd-1", Title: "Spring planting", Status: "deferred", Priority: 3, Epic: true},
		{ID: "gd-1.1", Title: "Buy seeds", Status: "open", Priority: 3, Parent: "gd-1"},
		{ID: "gd-2", Title: "Water the plants", Status: "open", Priority: 0, Parent: "gd-1", Epic: true},
		{ID: "gd-2.1", Title: "Fix the hose", Status: "open", Priority: 0, Parent: "gd-2"},
		{ID: "gd-3", Title: "Paint the fence", Status: "open", Priority: 1, Epic: true},
		{ID: "gd-3.1", Title: "Sand the boards", Status: "open", Priority: 1, Parent: "gd-3"},
	}}}}
}

func treeIDs(cs []cardAt) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.row.ID+":"+itoa(c.depth))
	}
	return out
}

func TestCardTreeNestsUnderParent(t *testing.T) {
	d := index(api.State{}, nestedEpics(), now)
	re := &d.epics.Repos[0]
	if got, want := treeIDs(d.cardTree("", re)), []string{"gd-3:0", "gd-1:0", "gd-2:1"}; !slices.Equal(got, want) {
		t.Errorf("tree %v, want %v", got, want)
	}
	// A match inside a sub-epic keeps its parent above it.
	if got, want := treeIDs(d.cardTree("hose", re)), []string{"gd-1:0", "gd-2:1"}; !slices.Equal(got, want) {
		t.Errorf("filtered tree %v, want %v", got, want)
	}
	if n := d.cardCount(); n != 3 {
		t.Errorf("card count %d, want 3", n)
	}
}

func TestTreeShowsASubEpicOnce(t *testing.T) {
	d := index(api.State{}, nestedEpics(), now)
	rows := d.epicTreeRows("", map[string]bool{"desk/garden/gd-1": true})
	var ids []string
	for _, r := range rows {
		if r.t != nil {
			ids = append(ids, r.id)
		}
	}
	want := []string{"desk/garden/gd-3", "desk/garden/gd-1", "desk/garden/gd-1>desk/garden/gd-1.1", "desk/garden/gd-2"}
	if !slices.Equal(ids, want) {
		t.Errorf("rows %v, want %v", ids, want)
	}
}

// chainEpics is a repo whose epics wait on each other in a chain, the way
// a real one did: each later epic waits on the first and on the one before.
func chainEpics() api.Epics {
	return api.Epics{Repos: []api.RepoEpics{{Bridge: "desk", Repo: "garden", Rows: []api.EpicRow{
		{ID: "gd-1", Title: "Clear the beds", Status: "open", Priority: 1, Epic: true},
		{ID: "gd-1.1", Title: "Pull the weeds", Status: "in_progress", Parent: "gd-1", Assignee: "sam"},
		{ID: "gd-1.2", Title: "Rake the soil", Status: "closed", Parent: "gd-1"},
		{ID: "gd-2", Title: "Plant the bulbs", Status: "open", Priority: 2, Epic: true, BlockedBy: []string{"gd-1"}},
		{ID: "gd-2.1", Title: "Buy the bulbs", Status: "open", Parent: "gd-2", BlockedBy: []string{"gd-1"}},
		{ID: "gd-3", Title: "Water the plants every morning before the sun is up, then mulch", Status: "open", Priority: 2, Epic: true, BlockedBy: []string{"gd-1", "gd-2"}},
		{ID: "gd-3.1", Title: "Fix the hose", Status: "deferred", Parent: "gd-3"},
		{ID: "gd-4", Title: "Paint the fence", Status: "open", Priority: 0, Epic: true, BlockedBy: []string{"gd-1", "gd-3", "gd-2"}},
		{ID: "gd-4.1", Title: "Sand the boards", Status: "open", Parent: "gd-4"},
	}}}}
}

func TestWaitingNamesTheDirectBlockerInOrderOfWork(t *testing.T) {
	d := index(api.State{}, chainEpics(), now)
	re := &d.epics.Repos[0]
	row := func(id string) *api.EpicRow { _, r := d.findRow("desk/garden/" + id); return r }
	if got := d.directBlockers(re, row("gd-4")); !slices.Equal(got, []string{"gd-3"}) {
		t.Errorf("direct blockers of gd-4 %v, want [gd-3]", got)
	}
	if got := d.unlocks(re, "gd-2"); !slices.Equal(got, []string{"gd-3"}) {
		t.Errorf("gd-2 unlocks %v, want [gd-3]", got)
	}
	for id, want := range map[string]int{"gd-1": 1, "gd-2": 2, "gd-3": 3, "gd-4": 4} {
		if got := d.tier(re, id); got != want {
			t.Errorf("tier of %s %d, want %d", id, got, want)
		}
	}
	if g := d.cardGroup(re, row("gd-1")); g != grpMoving {
		t.Errorf("gd-1 in group %d, want Moving", g)
	}
	// gd-4 is the most urgent, but the waiting group runs in order of work.
	var ids []string
	for _, r := range d.epicListRows("") {
		if r.t != nil && r.t.card {
			ids = append(ids, r.t.row.ID)
		}
	}
	if want := []string{"gd-1", "gd-2", "gd-3", "gd-4"}; !slices.Equal(ids, want) {
		t.Errorf("list %v, want %v", ids, want)
	}
}

func TestBlockerCycleEnds(t *testing.T) {
	e := chainEpics()
	rows := e.Repos[0].Rows
	rows[0].BlockedBy = []string{"gd-4"} // gd-1 now waits on the last of the chain
	d := index(api.State{}, e, now)
	re := &d.epics.Repos[0]
	_ = d.tier(re, "gd-4")
	if got := d.directBlockers(re, &rows[7]); len(got) == 0 {
		t.Error("a cycle leaves no blocker named")
	}
}

func TestStateBarFillsOnlyDone(t *testing.T) {
	d := index(api.State{}, chainEpics(), now)
	re := &d.epics.Repos[0]
	b := d.stateBar(re, "gd-1", 8)
	if b.width() != 8 {
		t.Fatalf("bar %d cells, want 8", b.width())
	}
	var full int
	for _, s := range b {
		full += strings.Count(s.text, gBarFull)
	}
	if full != 4 {
		t.Errorf("%d full cells for 1 of 2 done, want 4", full)
	}
}

func TestSelectedCardOpensInPlace(t *testing.T) {
	s, _ := load(t)
	m := New(hub).Resize(60, 30).SetConn(ConnLive).SetData(s, chainEpics(), now)
	m, _ = keys(t, m, "2")
	open := func(m Model) string { return strings.Join(plain(m), "\n") }
	if f := open(m); !strings.Contains(f, "▱ 1/2  ◒1 ✓1") || !strings.Contains(f, "unlocks gd-2") || !strings.Contains(f, "@sam") {
		t.Fatalf("the selected card does not show its facts:\n%s", f)
	}
	// j skips the facts lines and opens the next card, whose title wraps.
	m, _ = keys(t, m, "j")
	f := open(m)
	if strings.Contains(f, "unlocks gd-2") {
		t.Errorf("the first card stays open after the cursor left it:\n%s", f)
	}
	if !strings.Contains(f, "starts after gd-1") || !strings.Contains(f, "unlocks gd-3") {
		t.Errorf("gd-2 does not show what it starts after and unlocks:\n%s", f)
	}
	m, _ = keys(t, m, "j")
	if f := open(m); !strings.Contains(f, "then mulch") {
		t.Errorf("gd-3's whole title does not show:\n%s", f)
	}
}

func TestNestedCardInAnotherStateShowsItsOwn(t *testing.T) {
	d := index(api.State{}, nestedEpics(), now)
	re := &d.epics.Repos[0]
	_, sub := d.findRow("desk/garden/gd-2")
	_, top := d.findRow("desk/garden/gd-1")
	if got := d.nestedState(re, sub); len(got) == 0 || got[0].text != gOpen {
		t.Errorf("an open card under a deferred one shows %v, want its open mark", got)
	}
	if got := d.nestedState(re, top); got != nil {
		t.Errorf("a top-level card shows a state mark %v", got)
	}
}
