package store

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// seedEpics is seed with epics shown and switched on in garden, and chores
// a repo whose tracker can show epics but whose switch is off.
func seedEpics(t *testing.T, s *Store) {
	t.Helper()
	seed(t, s)
	must(t, s.ReplaceRepos("laptop", []api.Repo{
		{Name: "garden", OptedIn: true, Epics: true, EpicsOn: true},
		{Name: "chores", Epics: true},
	}))
}

// gardenEpics is an invented epic set: a card with two children, one of
// them a human task that is also the item gd-1, and a top-level task.
func gardenEpics() []api.EpicRow {
	at := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	return []api.EpicRow{
		{ID: "gd-9", Title: "Plan the beds", Type: "epic", Status: "open", Priority: 1, Epic: true, Description: "Spring beds.", UpdatedAt: at},
		{ID: "gd-9.1", Title: "Dig the beds", Type: "task", Status: "closed", Priority: 2, Parent: "gd-9", UpdatedAt: at, ClosedAt: &at},
		{ID: "gd-1", Title: "Water the plants", Status: "open", Priority: 2, Parent: "gd-9", Human: true, UpdatedAt: at},
		{ID: "gd-8", Title: "Fix the fence", Status: "in_progress", Priority: 3, Parent: "gd-7", Description: "dropped", UpdatedAt: at},
	}
}

// changes records every Change the store emits.
type changes struct {
	mu  sync.Mutex
	got []Change
}

func watch(s *Store) *changes {
	c := &changes{}
	s.Watch(func(ch Change) {
		c.mu.Lock()
		c.got = append(c.got, ch)
		c.mu.Unlock()
	})
	return c
}

func (c *changes) kinds() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var ks []string
	for _, ch := range c.got {
		ks = append(ks, ch.Kind)
	}
	c.got = nil
	return strings.Join(ks, ",")
}

func mustEpics(t *testing.T, s *Store) api.Epics {
	t.Helper()
	e, err := s.Epics()
	must(t, err)
	return e
}

func TestReplaceEpics(t *testing.T) {
	s, c := open(t)
	seedEpics(t, s)
	ch := watch(s)
	// Switched on, but no set yet: left out.
	if e := mustEpics(t, s); e.Repos == nil || len(e.Repos) != 0 {
		t.Fatalf("epics before a set: %+v", e)
	}
	var itemsSynced sql.NullString
	must(t, s.db.QueryRow(`SELECT synced_at FROM repos WHERE bridge = 'laptop' AND name = 'garden'`).Scan(&itemsSynced))

	c.t = c.t.Add(time.Minute)
	must(t, s.ReplaceEpics("laptop", "garden", gardenEpics()))
	if k := ch.kinds(); k != "epics" {
		t.Errorf("changes %q, want only epics", k)
	}
	e := mustEpics(t, s)
	if len(e.Repos) != 1 {
		t.Fatalf("epics %+v", e)
	}
	r := e.Repos[0]
	var ids []string
	for _, row := range r.Rows {
		ids = append(ids, row.ID)
	}
	if r.Bridge != "laptop" || r.Repo != "garden" || !r.SyncedAt.Equal(c.t) || strings.Join(ids, ",") != "gd-1,gd-8,gd-9,gd-9.1" {
		t.Fatalf("repo epics %+v", r)
	}
	// Cleaned as api.CheckEpicRows does: a parent that is no card and a
	// description off a card are gone.
	if r.Rows[1].Parent != "" || r.Rows[1].Description != "" || r.Rows[2].Description != "Spring beds." || !r.Rows[0].Human {
		t.Errorf("rows %+v", r.Rows)
	}
	// The items' sync time is the items route's own.
	var after sql.NullString
	must(t, s.db.QueryRow(`SELECT synced_at FROM repos WHERE bridge = 'laptop' AND name = 'garden'`).Scan(&after))
	if after != itemsSynced {
		t.Errorf("repos.synced_at moved from %v to %v", itemsSynced, after)
	}

	// The whole set is replaced; an unchanged one still emits.
	must(t, s.ReplaceEpics("laptop", "garden", gardenEpics()[:1]))
	must(t, s.ReplaceEpics("laptop", "garden", gardenEpics()[:1]))
	if k := ch.kinds(); k != "epics,epics" {
		t.Errorf("changes %q", k)
	}
	if rows := mustEpics(t, s).Repos[0].Rows; len(rows) != 1 || rows[0].ID != "gd-9" {
		t.Errorf("rows after a smaller set %+v", rows)
	}
	// An empty set is a set: the repo shows with no rows.
	must(t, s.ReplaceEpics("laptop", "garden", nil))
	if e := mustEpics(t, s); len(e.Repos) != 1 || e.Repos[0].Rows == nil || len(e.Repos[0].Rows) != 0 {
		t.Errorf("epics after an empty set %+v", e)
	}
}

func TestReplaceEpicsRefusals(t *testing.T) {
	s, _ := open(t)
	seedEpics(t, s)
	for repo, want := range map[string]error{
		"shed":     ErrConflict, // not in the repo report
		"chores":   ErrConflict, // reported without epicsOn
		"personal": ErrConflict, // the bridge's own personal repo is no repo of epics
	} {
		if err := s.ReplaceEpics("laptop", repo, gardenEpics()); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", repo, err, want)
		}
	}
	twice := append(gardenEpics(), gardenEpics()[0])
	if err := s.ReplaceEpics("laptop", "garden", twice); !errors.Is(err, ErrInvalid) {
		t.Errorf("a row twice: %v", err)
	}
	if e := mustEpics(t, s); len(e.Repos) != 0 {
		t.Errorf("a refused set was taken: %+v", e)
	}
}

func epicRowCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	must(t, s.db.QueryRow(`SELECT COUNT(*) FROM epic_rows`).Scan(&n))
	return n
}

func TestEpicRowsGoWithTheirRepo(t *testing.T) {
	s, _ := open(t)
	seedEpics(t, s)
	ch := watch(s)
	must(t, s.ReplaceEpics("laptop", "garden", gardenEpics()))
	ch.kinds()

	// A report that keeps epicsOn keeps the rows, and sends no epics change.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, Epics: true, EpicsOn: true}}))
	if n := epicRowCount(t, s); n != 4 {
		t.Fatalf("rows after a report with epicsOn: %d", n)
	}
	if k := ch.kinds(); k != "repos" {
		t.Errorf("changes %q", k)
	}
	// Without epicsOn the rows go, and the repo has no set until the next.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, Epics: true}}))
	if n := epicRowCount(t, s); n != 0 {
		t.Fatalf("rows after a report without epicsOn: %d", n)
	}
	if k := ch.kinds(); k != "repos,epics" {
		t.Errorf("changes %q", k)
	}
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, Epics: true, EpicsOn: true}}))
	if e := mustEpics(t, s); len(e.Repos) != 0 {
		t.Errorf("switched on again shows the old set: %+v", e)
	}

	// Not reported at all.
	must(t, s.ReplaceEpics("laptop", "garden", gardenEpics()))
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "chores"}}))
	if n := epicRowCount(t, s); n != 0 {
		t.Fatalf("rows after the repo left the report: %d", n)
	}

	// A folder name moved to a path key leaves its rows behind; the bridge
	// sends its set again under the new name.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, Epics: true, EpicsOn: true}}))
	must(t, s.ReplaceEpics("laptop", "garden", gardenEpics()))
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "projects/garden", OptedIn: true, Epics: true, EpicsOn: true}}))
	if n := epicRowCount(t, s); n != 0 {
		t.Fatalf("rows after the folder-name move: %d", n)
	}
	must(t, s.ReplaceEpics("laptop", "projects/garden", gardenEpics()))

	// Revoking the bridge drops its rows.
	ch.kinds()
	must(t, s.RevokeBridge("laptop"))
	if n := epicRowCount(t, s); n != 0 {
		t.Fatalf("rows after the revoke: %d", n)
	}
	if k := ch.kinds(); k != "bridges,epics" {
		t.Errorf("changes %q", k)
	}
	// Paired again, the repo shows no set until the bridge sends one.
	_, err := s.CreateBridge("laptop")
	must(t, err)
	if e := mustEpics(t, s); len(e.Repos) != 0 {
		t.Errorf("epics after pairing again: %+v", e)
	}
}

func TestEpicsSwitch(t *testing.T) {
	s, _ := open(t)
	seedEpics(t, s)
	must(t, s.ReplaceRepos("laptop", []api.Repo{
		{Name: "garden", OptedIn: true, Epics: true, EpicsOn: true},
		{Name: "chores", Epics: true},
		{Name: "shed", OptedIn: true},
	}))
	project, _, err := s.AddPersonal(api.PersonalTodo{Title: "Pick a colour"}, "laptop", garden)
	must(t, err)
	_, projectRepo, _, _ := SplitKey(project)
	on, off := true, false
	refusals := map[string]struct {
		na   api.NewAction
		want error
	}{
		"on":                {api.NewAction{ID: id1, Kind: api.KindEpics, Bridge: "laptop", Repo: "garden", Payload: api.Payload{On: &on}}, ErrInvalid},
		"without on":        {api.NewAction{ID: id1, Kind: api.KindEpics, Bridge: "laptop", Repo: "garden"}, ErrInvalid},
		"an unknown repo":   {api.NewAction{ID: id1, Kind: api.KindEpics, Bridge: "laptop", Repo: "barn", Payload: api.Payload{On: &off}}, ErrNotFound},
		"no epics":          {api.NewAction{ID: id1, Kind: api.KindEpics, Bridge: "laptop", Repo: "shed", Payload: api.Payload{On: &off}}, ErrInvalid},
		"the personal list": {api.NewAction{ID: id1, Kind: api.KindEpics, Bridge: HubBridge, Repo: PersonalRepo, Payload: api.Payload{On: &off}}, ErrInvalid},
		"a project":         {api.NewAction{ID: id1, Kind: api.KindEpics, Bridge: HubBridge, Repo: projectRepo, Payload: api.Payload{On: &off}}, ErrInvalid},
		"an item key":       {api.NewAction{ID: id1, Kind: api.KindEpics, Key: ItemKey("laptop", "garden", "gd-1"), Payload: api.Payload{On: &off}}, ErrInvalid},
	}
	for name, r := range refusals {
		_, _, err := s.AppendAction(r.na, "alex-app")
		if !errors.Is(err, r.want) {
			t.Errorf("%s: %v, want %v", name, err, r.want)
		}
		if name == "no epics" && (err == nil || !strings.HasSuffix(err.Error(), "cannot show epics")) {
			t.Errorf("no epics: %v, want the tracker refusal", err)
		}
		if name == "on" && (err == nil || !strings.Contains(err.Error(), "`meat-mode bridge epics on garden`")) {
			t.Errorf("on: %v, want it to name the command", err)
		}
	}

	must(t, s.ReplaceEpics("laptop", "garden", gardenEpics()))
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindEpics, Bridge: "laptop", Repo: "garden", Payload: api.Payload{On: &off}}, "alex-app")
	must(t, err)
	if r := repoOf(t, mustState(t, s), "garden"); r.EpicsOn || !r.EpicsPending || !r.Epics {
		t.Errorf("garden while switching off: %+v", r)
	}
	// The hub still takes a set, but GET /api/epics leaves the repo out.
	must(t, s.ReplaceEpics("laptop", "garden", gardenEpics()))
	if e := mustEpics(t, s); len(e.Repos) != 0 {
		t.Errorf("epics while switching off: %+v", e)
	}
	// The bridge applies it and reports the repo without epicsOn.
	must(t, s.ReportResult("laptop", id2, api.ActionResult{OK: true}))
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, Epics: true}, {Name: "chores", Epics: true}}))
	if r := repoOf(t, mustState(t, s), "garden"); r.EpicsOn || r.EpicsPending {
		t.Errorf("garden after the report: %+v", r)
	}
	if n := epicRowCount(t, s); n != 0 {
		t.Errorf("rows after the switch-off: %d", n)
	}
	// A report's epicsPending is the hub's own and never stored.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, Epics: true, EpicsOn: true, EpicsPending: true}}))
	if r := repoOf(t, mustState(t, s), "garden"); !r.EpicsOn || r.EpicsPending {
		t.Errorf("garden after a report with epicsPending: %+v", r)
	}
}
