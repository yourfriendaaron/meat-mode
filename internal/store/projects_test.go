package store

import (
	"errors"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

const garden = "projects/garden"

func TestAgentAddMakesAProject(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	key, created, err := s.AddPersonal(api.PersonalTodo{ID: id1, Title: "Approve the footer text", Note: "See the pull request.", Author: "planner"}, "laptop", garden)
	must(t, err)
	b, r, id, _ := SplitKey(key)
	if !created || b != HubBridge || r != garden || id == "" {
		t.Fatalf("add = %q, %v", key, created)
	}
	st := mustState(t, s)
	var found bool
	for _, repo := range st.Repos {
		if repo.Bridge == HubBridge && repo.Name == garden {
			found = true
			if repo.Personal || !repo.OptedIn || repo.Actions != nil || repo.Tracker != "" {
				t.Errorf("project repo %+v", repo)
			}
		}
	}
	if !found {
		t.Fatalf("repos %+v have no project", st.Repos)
	}
	it := item(t, st, key)
	if it.Title != "Approve the footer text" || it.CreatedBy != "planner" || len(it.Comments) != 1 || it.Pending {
		t.Fatalf("project item %+v", it)
	}
	// A second add goes to the same project.
	key2, _, err := s.AddPersonal(api.PersonalTodo{Title: "Pick a colour"}, "laptop", garden)
	must(t, err)
	if _, r2, _, _ := SplitKey(key2); r2 != garden {
		t.Errorf("second add went to %q", key2)
	}
}

func TestProjectItemsBehaveLikePersonalOnes(t *testing.T) {
	s, c := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	key, _, err := s.AddPersonal(api.PersonalTodo{Title: "Water the plants"}, "laptop", garden)
	must(t, err)

	due := c.t.Add(24 * time.Hour)
	for i, na := range []api.NewAction{
		{ID: id2, Kind: api.KindEdit, Key: key, Payload: api.Payload{Set: &api.Edit{Due: &due, Checklist: &[]api.Check{{Text: "Fill the can"}}}}},
		{ID: id3, Kind: api.KindNote, Key: key, Payload: api.Payload{Text: "The ferns first"}},
		{ID: id4, Kind: api.KindAdd, Bridge: HubBridge, Repo: garden, Payload: api.Payload{Title: "Mow the lawn", Due: &due, Repeat: &api.Repeat{Every: api.EveryWeek, TimeZone: "UTC"}}},
	} {
		a, _, err := s.AppendAction(na, "alex-app")
		must(t, err)
		if a.Status != api.StatusApplied {
			t.Fatalf("action %d = %+v, want applied by the hub", i, a)
		}
	}
	st := mustState(t, s)
	if it := item(t, st, key); it.DueAt == nil || !it.DueAt.Equal(due) || len(it.Checklist) != 1 || len(it.Comments) != 1 {
		t.Fatalf("edited item %+v", it)
	}

	// Asking an agent is refused, as on the personal list, and so is the
	// sync switch.
	for _, na := range []api.NewAction{
		{ID: id5, Kind: api.KindAsk, Key: key},
		{ID: id5, Kind: api.KindAdd, Bridge: HubBridge, Repo: garden, Payload: api.Payload{Title: "Plan the beds", Ask: true}},
		{ID: id5, Kind: api.KindSync, Bridge: HubBridge, Repo: garden, Payload: api.Payload{On: new(bool)}},
		{ID: id5, Kind: api.KindAgents, Bridge: HubBridge, Repo: garden, Payload: api.Payload{On: new(bool)}},
	} {
		if _, _, err := s.AppendAction(na, "alex-app"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s on a project: %v", na.Kind, err)
		}
	}
}

func TestProjectsKeepEachMachinesItemsApart(t *testing.T) {
	s, _ := open(t)
	for _, name := range []string{"laptop", "desk"} {
		_, err := s.CreateBridge(name)
		must(t, err)
	}
	key, _, err := s.AddPersonal(api.PersonalTodo{Title: "Approve the footer text"}, "laptop", garden)
	must(t, err)
	_, _, id, _ := SplitKey(key)

	if it, err := s.PersonalItem("laptop", garden, id); err != nil || it.Key != key {
		t.Fatalf("own project item = %+v, %v", it, err)
	}
	for _, tc := range []struct{ bridge, project string }{{"desk", garden}, {"laptop", ""}, {"laptop", "projects/other"}} {
		if _, err := s.PersonalItem(tc.bridge, tc.project, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s reading in %q: %v", tc.bridge, tc.project, err)
		}
		if _, _, err := s.ChangePersonal(tc.bridge, tc.project, id, api.PersonalChange{Kind: api.KindDone}); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s changing in %q: %v", tc.bridge, tc.project, err)
		}
	}
	a, _, err := s.ChangePersonal("laptop", garden, id, api.PersonalChange{Kind: api.KindDone})
	must(t, err)
	if a.Repo != garden || a.Status != api.StatusApplied {
		t.Fatalf("done = %+v", a)
	}
	// An add's id answers only in the list it was posted to.
	if _, _, err := s.AddPersonal(api.PersonalTodo{ID: a.ID, Title: "Water the plants"}, "laptop", ""); !errors.Is(err, ErrConflict) {
		t.Errorf("reused id in another list: %v", err)
	}
}

func TestProjectNames(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	for _, name := range []string{"personal", "Personal", "../x", "a//b", "-flag"} {
		if _, _, err := s.AddPersonal(api.PersonalTodo{Title: "Water the plants"}, "laptop", name); !errors.Is(err, ErrInvalid) {
			t.Errorf("project %q: %v", name, err)
		}
		if _, err := s.PersonalItem("laptop", name, "mm-1"); !errors.Is(err, ErrInvalid) {
			t.Errorf("reading in project %q: %v", name, err)
		}
	}
	if len(mustState(t, s).Repos) != 1 {
		t.Errorf("a refused name made a list")
	}
}

func TestEmptyProjectLeavesState(t *testing.T) {
	s, c := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	hasProject := func() bool {
		for _, r := range mustState(t, s).Repos {
			if r.Bridge == HubBridge && r.Name == garden {
				return true
			}
		}
		return false
	}
	// An add the hub refuses makes no list the app shows.
	if _, _, err := s.AddPersonal(api.PersonalTodo{}, "laptop", garden); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty title: %v", err)
	}
	if hasProject() {
		t.Fatal("a refused add shows its project")
	}
	key, _, err := s.AddPersonal(api.PersonalTodo{Title: "Water the plants"}, "laptop", garden)
	must(t, err)
	_, _, id, _ := SplitKey(key)
	if !hasProject() {
		t.Fatal("project with an open item is missing")
	}
	_, _, err = s.ChangePersonal("laptop", garden, id, api.PersonalChange{Kind: api.KindDone})
	must(t, err)
	c.t = c.t.Add(6 * 24 * time.Hour)
	if !hasProject() {
		t.Fatal("project with a recently closed item is missing")
	}
	c.t = c.t.Add(2 * 24 * time.Hour)
	if hasProject() {
		t.Fatal("project with nothing to show is still in State")
	}
	// The next add brings it back.
	_, _, err = s.AddPersonal(api.PersonalTodo{Title: "Pick a colour"}, "laptop", garden)
	must(t, err)
	if !hasProject() {
		t.Fatal("project is missing after another add")
	}
}

func TestMigrationElevenKeepsOwners(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	_, err = s.CreateBridge("laptop")
	must(t, err)
	key, _, err := s.AddPersonal(api.PersonalTodo{Title: "Renew the library card"}, "laptop", "")
	must(t, err)
	_, _, id, _ := SplitKey(key)
	// Back to the owners table before projects.
	_, err = s.DB().Exec(`DROP TABLE personal_owners;
		CREATE TABLE personal_owners (item_id TEXT PRIMARY KEY, bridge TEXT NOT NULL);
		INSERT INTO personal_owners (item_id, bridge) VALUES (?, 'laptop');
		PRAGMA user_version = 10`, id)
	must(t, err)
	must(t, s.Close())

	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	if it, err := s.PersonalItem("laptop", "", id); err != nil || it.Key != key {
		t.Fatalf("owned item after the migration = %+v, %v", it, err)
	}
}
