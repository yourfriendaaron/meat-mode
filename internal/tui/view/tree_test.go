package view

import (
	"slices"
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
