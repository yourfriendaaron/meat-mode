package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// gardenID is the tracker id both machines report for their clone of the
// garden tracker.
var gardenID = strings.Repeat("ab", api.TrackerIDLen/2)

// seedLinked makes two machines, desk (added first) and laptop, each with
// a clone of one tracker under its own folder name, switched on, holding
// the same two tasks, and both online.
func seedLinked(t *testing.T, s *Store, c *clock) {
	t.Helper()
	for _, b := range []struct{ bridge, repo string }{{"desk", "projects/garden"}, {"laptop", "Developer/garden"}} {
		_, err := s.CreateBridge(b.bridge)
		must(t, err)
		c.tick()
		must(t, s.ReplaceRepos(b.bridge, []api.Repo{{Name: b.repo, OptedIn: true, TrackerID: gardenID}, {Name: "chores", OptedIn: true}}))
		must(t, s.ReplaceItems(b.bridge, b.repo, []api.Item{
			{ID: "gd-1", Title: "Water the plants", Status: "open", Priority: 2},
			{ID: "gd-2", Title: "Repot the fern", Status: "open", Priority: 1},
		}))
		must(t, s.Touch(b.bridge))
	}
}

func linkedRepo(t *testing.T, st api.State, bridge, name string) api.Repo {
	t.Helper()
	for _, r := range st.Repos {
		if r.Bridge == bridge && r.Name == name {
			return r
		}
	}
	t.Fatalf("no repo %s/%s in state", bridge, name)
	return api.Repo{}
}

// TestLinkedListShowsEachTaskOnce checks that two clones of one tracker
// show their tasks once, from the machine added first while it is online,
// move to the other when it goes quiet, and move back when it returns.
func TestLinkedListShowsEachTaskOnce(t *testing.T) {
	s, c := open(t)
	seedLinked(t, s, c)
	st, err := s.State()
	must(t, err)
	if !hasItem(st, "desk/projects/garden/gd-1") || hasItem(st, "laptop/Developer/garden/gd-1") {
		t.Fatalf("items = %v, want desk's copy alone", keys(st))
	}
	desk, laptop := linkedRepo(t, st, "desk", "projects/garden"), linkedRepo(t, st, "laptop", "Developer/garden")
	if desk.Group != "desk/projects/garden" || laptop.Group != desk.Group {
		t.Fatalf("groups = %q, %q, want both desk/projects/garden", desk.Group, laptop.Group)
	}
	if desk.Standby || !laptop.Standby {
		t.Fatalf("standby = %v, %v, want laptop's alone", desk.Standby, laptop.Standby)
	}
	if laptop.Colour == nil || desk.Colour == nil || *laptop.Colour != *desk.Colour {
		t.Fatalf("colours = %v, %v, want the primary's on both", desk.Colour, laptop.Colour)
	}
	// Repos with no tracker id are never linked, whatever their names.
	if r := linkedRepo(t, st, "laptop", "chores"); r.Group != "" || r.Standby {
		t.Fatalf("chores = %+v, want unlinked", r)
	}
	before, err := s.LinkedSources()
	must(t, err)

	// desk goes quiet past the window; laptop keeps reporting.
	c.t = c.t.Add(OnlineWindow + time.Second)
	must(t, s.Touch("laptop"))
	st, err = s.State()
	must(t, err)
	if hasItem(st, "desk/projects/garden/gd-1") || !hasItem(st, "laptop/Developer/garden/gd-1") {
		t.Fatalf("after desk went quiet, items = %v, want laptop's copy alone", keys(st))
	}
	if !linkedRepo(t, st, "desk", "projects/garden").Standby || linkedRepo(t, st, "laptop", "Developer/garden").Standby {
		t.Fatal("after desk went quiet, desk should be the standby")
	}
	after, err := s.LinkedSources()
	must(t, err)
	if after == before {
		t.Fatal("LinkedSources did not change when the source moved")
	}

	// desk comes back and is the source again.
	must(t, s.Touch("desk"))
	st, err = s.State()
	must(t, err)
	if !hasItem(st, "desk/projects/garden/gd-1") || hasItem(st, "laptop/Developer/garden/gd-1") {
		t.Fatalf("after desk returned, items = %v, want desk's copy alone", keys(st))
	}

	// Both quiet: the first opted-in copy keeps showing.
	c.t = c.t.Add(OnlineWindow + time.Second)
	st, err = s.State()
	must(t, err)
	if !hasItem(st, "desk/projects/garden/gd-2") || hasItem(st, "laptop/Developer/garden/gd-2") {
		t.Fatalf("with both quiet, items = %v, want desk's copy alone", keys(st))
	}
}

// TestLinkedListFollowsTheSwitch checks that a copy switched off is never
// the source, and that a list with one copy switched on shows that one.
func TestLinkedListFollowsTheSwitch(t *testing.T) {
	s, c := open(t)
	seedLinked(t, s, c)
	must(t, s.ReplaceRepos("desk", []api.Repo{{Name: "projects/garden", TrackerID: gardenID}, {Name: "chores", OptedIn: true}}))
	st, err := s.State()
	must(t, err)
	if !hasItem(st, "laptop/Developer/garden/gd-1") || hasItem(st, "desk/projects/garden/gd-1") {
		t.Fatalf("items = %v, want laptop's copy alone", keys(st))
	}
	if r := linkedRepo(t, st, "desk", "projects/garden"); !r.Standby || r.Group != "desk/projects/garden" {
		t.Fatalf("desk = %+v, want the standby of the desk group", r)
	}
	// A revoked bridge leaves the list; one copy is no list.
	must(t, s.RevokeBridge("desk"))
	st, err = s.State()
	must(t, err)
	if r := linkedRepo(t, st, "laptop", "Developer/garden"); r.Group != "" || r.Standby {
		t.Fatalf("after revoking desk, laptop = %+v, want unlinked", r)
	}
}

// TestLinkedEpicsShowOnce checks that a linked list's epics come from one
// repo, an online one first.
func TestLinkedEpicsShowOnce(t *testing.T) {
	s, c := open(t)
	seedLinked(t, s, c)
	for _, b := range []struct{ bridge, repo string }{{"desk", "projects/garden"}, {"laptop", "Developer/garden"}} {
		must(t, s.ReplaceRepos(b.bridge, []api.Repo{{Name: b.repo, OptedIn: true, TrackerID: gardenID, Epics: true, EpicsOn: true}}))
		must(t, s.ReplaceEpics(b.bridge, b.repo, gardenEpics()))
	}
	repos := func() []string {
		e, err := s.Epics()
		must(t, err)
		var out []string
		for _, re := range e.Repos {
			out = append(out, re.Bridge+"/"+re.Repo)
		}
		return out
	}
	if got := repos(); len(got) != 1 || got[0] != "desk/projects/garden" {
		t.Fatalf("epic sets = %v, want desk's alone", got)
	}
	c.t = c.t.Add(OnlineWindow + time.Second)
	must(t, s.Touch("laptop"))
	if got := repos(); len(got) != 1 || got[0] != "laptop/Developer/garden" {
		t.Fatalf("with desk quiet, epic sets = %v, want laptop's alone", got)
	}
}

// TestLinkedReminderKey checks that a task answers to one reminder key and
// one first-seen time from either copy.
func TestLinkedReminderKey(t *testing.T) {
	s, c := open(t)
	seedLinked(t, s, c)
	ctx := context.Background()
	st, err := s.State()
	must(t, err)
	fromDesk := item(t, st, "desk/projects/garden/gd-1")
	c.t = c.t.Add(OnlineWindow + time.Second)
	must(t, s.Touch("laptop"))
	st, err = s.State()
	must(t, err)
	fromLaptop := item(t, st, "laptop/Developer/garden/gd-1")
	k1, f1, err := s.ReminderKey(ctx, fromDesk)
	must(t, err)
	k2, f2, err := s.ReminderKey(ctx, fromLaptop)
	must(t, err)
	if k1 != "desk/projects/garden/gd-1" || k2 != k1 {
		t.Fatalf("keys = %q, %q, want desk's key from both", k1, k2)
	}
	if !f1.Equal(f2) || !f2.Equal(fromDesk.FirstSeen) {
		t.Fatalf("first seen = %v, %v, want desk's %v from both", f1, f2, fromDesk.FirstSeen)
	}
}

func keys(st api.State) []string {
	out := make([]string, 0, len(st.Items))
	for _, it := range st.Items {
		out = append(out, it.Key)
	}
	return out
}
