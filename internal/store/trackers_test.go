package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// seedTicket gives bridge "laptop" a repo "shed" kept in a tracker that can
// mark done, dismiss and add, but not snooze, note, reopen, ask or add
// sub-tasks, with one open item; garden stays as an older bridge sends it.
func seedTicket(t *testing.T, s *Store) {
	t.Helper()
	seed(t, s)
	must(t, s.ReplaceRepos("laptop", []api.Repo{
		{Name: "garden", OptedIn: true},
		{Name: "chores"},
		{Name: "shed", OptedIn: true, AgentsAllowed: true, Tracker: "ticket",
			Actions: []string{api.KindDone, api.KindDismiss, api.KindAdd}},
	}))
	must(t, s.ReplaceItems("laptop", "shed", []api.Item{
		{ID: "sh-1", Title: "Oil the hinges", Status: "open", Priority: 2},
	}))
}

func repoNamed(t *testing.T, st api.State, bridge, name string) api.Repo {
	t.Helper()
	for _, r := range st.Repos {
		if r.Bridge == bridge && r.Name == name {
			return r
		}
	}
	t.Fatalf("no repo %s/%s in state", bridge, name)
	return api.Repo{}
}

func TestTrackerRoundTrip(t *testing.T) {
	s, _ := open(t)
	seedTicket(t, s)
	must(t, s.Hello("laptop", api.Hello{Version: "1.0.0", BdVersion: "0.40.0", Trackers: []api.TrackerVersion{
		{Name: "beads", Version: "0.40.0"}, {Name: "ticket"}, {Name: " "},
	}}))
	st, err := s.State()
	must(t, err)

	shed := repoNamed(t, st, "laptop", "shed")
	if shed.Tracker != "ticket" || !slices.Equal(shed.Actions, []string{"done", "dismiss", "add"}) {
		t.Errorf("shed = %+v", shed)
	}
	// An older bridge sends neither; the hub's own list has neither.
	for _, r := range []api.Repo{repoNamed(t, st, "laptop", "garden"), repoNamed(t, st, HubBridge, PersonalRepo)} {
		if r.Tracker != "" || r.Actions != nil {
			t.Errorf("%s/%s = %+v, want no tracker and every kind", r.Bridge, r.Name, r)
		}
	}
	want := []api.TrackerVersion{{Name: "beads", Version: "0.40.0"}, {Name: "ticket"}}
	if len(st.Bridges) != 1 || !slices.Equal(st.Bridges[0].Trackers, want) {
		t.Errorf("bridges = %+v, want trackers %+v", st.Bridges, want)
	}

	// An empty list reads as every kind, as it does on the wire.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "shed", OptedIn: true, Tracker: "ticket", Actions: []string{}}}))
	st, err = s.State()
	must(t, err)
	if got := repoNamed(t, st, "laptop", "shed"); got.Actions != nil {
		t.Errorf("empty actions stored as %q", got.Actions)
	}

	// A hello without trackers clears them; pairing the name again does too.
	must(t, s.Hello("laptop", api.Hello{Version: "1.0.0"}))
	bs, err := s.Bridges()
	must(t, err)
	if bs[0].Trackers != nil {
		t.Errorf("trackers after a plain hello = %+v", bs[0].Trackers)
	}
}

func TestTrackerRefusesWhatItCannotDo(t *testing.T) {
	s, c := open(t)
	seedTicket(t, s)
	shed := "laptop/shed/sh-1"
	ids := []string{
		"7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a01", "7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a02",
		"7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a03", "7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a04",
		"7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a05", "7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a06",
		"7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a07", "7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a08",
		"7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a09", "7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a10",
		"7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a11", "7a1e2b3c-4d5e-4f60-8a9b-0c1d2e3f4a12",
	}
	until := c.t.Add(3600e9)
	refused := []struct {
		na   api.NewAction
		want string
	}{
		{api.NewAction{Kind: api.KindSnooze, Key: shed, Payload: api.Payload{Until: &until}}, "ticket cannot snooze"},
		{api.NewAction{Kind: api.KindNote, Key: shed, Payload: api.Payload{Text: "Which oil?"}}, "ticket cannot take notes"},
		{api.NewAction{Kind: api.KindReopen, Key: shed}, "ticket cannot reopen tasks"},
		{api.NewAction{Kind: api.KindAsk, Key: shed}, "ticket cannot start agents"},
		{api.NewAction{Kind: api.KindAdd, Bridge: "laptop", Repo: "shed",
			Payload: api.Payload{Title: "Buy oil", Parent: "sh-1"}}, "ticket cannot add sub-tasks"},
		{api.NewAction{Kind: api.KindAdd, Bridge: "laptop", Repo: "shed",
			Payload: api.Payload{Title: "Buy oil", Ask: true}}, "ticket cannot start agents"},
	}
	for i, tc := range refused {
		tc.na.ID = ids[i]
		_, _, err := s.AppendAction(tc.na, "alex-app")
		if !errors.Is(err, ErrInvalid) || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want invalid %q", tc.na.Kind, err, tc.want)
		}
	}

	// What it lists goes through, and so do stop and the repo switches.
	on := false
	allowed := []api.NewAction{
		{ID: ids[6], Kind: api.KindAdd, Bridge: "laptop", Repo: "shed", Payload: api.Payload{Title: "Buy oil"}},
		{ID: ids[7], Kind: api.KindStop, Key: shed},
		{ID: ids[8], Kind: api.KindAgents, Bridge: "laptop", Repo: "shed", Payload: api.Payload{On: &on}},
		{ID: ids[9], Kind: api.KindDone, Key: shed},
		{ID: ids[10], Kind: api.KindSync, Bridge: "laptop", Repo: "shed", Payload: api.Payload{On: &on}},
	}
	for _, na := range allowed {
		if _, _, err := s.AppendAction(na, "alex-app"); err != nil {
			t.Errorf("%s: %v", na.Kind, err)
		}
	}

	// A repo without a tracker list takes every kind.
	if _, _, err := s.AppendAction(api.NewAction{ID: ids[11], Kind: api.KindSnooze, Key: "laptop/garden/gd-1",
		Payload: api.Payload{Until: &until}}, "alex-app"); err != nil {
		t.Errorf("snooze in garden: %v", err)
	}
}

func TestTrackerWithoutAddRefusesAdds(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, Actions: []string{api.KindDone}}}))
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Water the plants"}}, "alex-app")
	if !errors.Is(err, ErrInvalid) || err.Error() != "this list's tracker cannot add tasks" {
		t.Errorf("add = %v", err)
	}
}

func TestAllows(t *testing.T) {
	s, _ := open(t)
	seedTicket(t, s)
	ctx := context.Background()
	for _, tc := range []struct {
		key, kind string
		want      bool
	}{
		{"laptop/shed/sh-1", api.KindDone, true},
		{"laptop/shed/sh-1", api.KindSnooze, false},
		{"laptop/garden/gd-1", api.KindSnooze, true},
		{"hub/personal/p-1", api.KindSnooze, true},
		{"not a key", api.KindSnooze, true},
	} {
		got, err := s.Allows(ctx, tc.key, tc.kind)
		must(t, err)
		if got != tc.want {
			t.Errorf("Allows(%s, %s) = %v", tc.key, tc.kind, got)
		}
	}
}

func TestMigrationTenAddsTrackers(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	seed(t, s)
	_, err = s.DB().Exec(`ALTER TABLE repos DROP COLUMN tracker; ALTER TABLE repos DROP COLUMN actions;
		ALTER TABLE bridges DROP COLUMN trackers; PRAGMA user_version = 9`)
	must(t, err)
	must(t, s.Close())

	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	st, err := s.State()
	must(t, err)
	if g := repoNamed(t, st, "laptop", "garden"); g.Tracker != "" || g.Actions != nil {
		t.Errorf("garden after the migration = %+v, want every kind", g)
	}
	if st.Bridges[0].Trackers != nil {
		t.Errorf("trackers after the migration = %+v", st.Bridges[0].Trackers)
	}
}
