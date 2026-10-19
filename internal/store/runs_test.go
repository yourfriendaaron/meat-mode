package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// seedAgents is seed with agents allowed in garden, and the bridge seen now.
func seedAgents(t *testing.T, s *Store) {
	t.Helper()
	seed(t, s)
	must(t, s.ReplaceRepos("laptop", []api.Repo{
		{Name: "garden", OptedIn: true, AgentsAllowed: true},
		{Name: "chores"},
	}))
	must(t, s.Touch("laptop"))
}

func run(bead, id, state string, start time.Time) api.RunReport {
	end := start.Add(time.Minute)
	r := api.RunReport{Repo: "garden", BeadID: bead, Run: api.Run{
		ID: id, State: state, Agent: "planner", Activity: "Reading the task", StartedAt: start, Count: 1,
	}}
	if state != api.RunWorking {
		r.Run.EndedAt = &end
	}
	return r
}

func repoOf(t *testing.T, st api.State, name string) api.Repo {
	t.Helper()
	for _, r := range st.Repos {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no repo %s", name)
	return api.Repo{}
}

func TestAskRefusals(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	hubKey, _, err := s.AddPersonal(api.PersonalTodo{ID: id6, Title: "Water the plants"}, "laptop", "")
	must(t, err)
	garden := ItemKey("laptop", "garden", "gd-1")
	on := true
	long := make([]rune, MaxNoteLen+1)
	for i := range long {
		long[i] = 'a'
	}
	for name, na := range map[string]api.NewAction{
		"ask on a personal item":            {ID: id1, Kind: api.KindAsk, Key: hubKey},
		"ask where agents are off":          {ID: id1, Kind: api.KindAsk, Key: garden},
		"ask with too long a note":          {ID: id1, Kind: api.KindAsk, Key: garden, Payload: api.Payload{Text: string(long)}},
		"add with ask on the personal list": {ID: id1, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo, Payload: api.Payload{Title: "Repot the fern", Ask: true}},
		"add with ask where agents are off": {ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden", Payload: api.Payload{Title: "Repot the fern", Ask: true}},
		"agents on the personal list":       {ID: id1, Kind: api.KindAgents, Bridge: HubBridge, Repo: PersonalRepo, Payload: api.Payload{On: &on}},
		"agents without on":                 {ID: id1, Kind: api.KindAgents, Bridge: "laptop", Repo: "garden"},
		"agents on an unknown repo":         {ID: id1, Kind: api.KindAgents, Bridge: "laptop", Repo: "shed", Payload: api.Payload{On: &on}},
		"agents on an item key":             {ID: id1, Kind: api.KindAgents, Key: garden, Payload: api.Payload{On: &on}},
		"stop without a key":                {ID: id1, Kind: api.KindStop},
	} {
		if _, _, err := s.AppendAction(na, "alex-app"); err == nil {
			t.Errorf("%s: accepted", name)
		} else if errors.Is(err, ErrNotFound) && name != "agents on an unknown repo" {
			t.Errorf("%s: %v, want a refusal", name, err)
		}
	}
	// A stop takes any item, the personal list's too, and changes nothing there.
	if _, _, err := s.AppendAction(api.NewAction{ID: id2, Kind: api.KindStop, Key: hubKey}, "alex-app"); err != nil {
		t.Errorf("stop on a personal item: %v", err)
	}
	if it := item(t, mustState(t, s), hubKey); it.Run != nil {
		t.Errorf("personal item has a run: %+v", it.Run)
	}
}

func TestAgentsSwitchGatesAsk(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	garden := ItemKey("laptop", "garden", "gd-1")
	on, off := true, false
	// The app cannot switch agents on; only the bridge's list does.
	if _, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAgents, Bridge: "laptop", Repo: "garden", Payload: api.Payload{On: &on}}, "alex-app"); !errors.Is(err, ErrInvalid) {
		t.Errorf("agents on from the app: %v, want a refusal", err)
	}
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, AgentsAllowed: true}, {Name: "chores"}}))
	if r := repoOf(t, mustState(t, s), "garden"); !r.AgentsAllowed || r.AgentsPending {
		t.Errorf("garden after the bridge switched it on: %+v", r)
	}
	_, _, err := s.AppendAction(api.NewAction{ID: id2, Kind: api.KindAsk, Key: garden}, "alex-app")
	must(t, err)
	// A pending switch off refuses at once, ask and add with ask alike.
	_, _, err = s.AppendAction(api.NewAction{ID: id3, Kind: api.KindAgents, Bridge: "laptop", Repo: "garden", Payload: api.Payload{On: &off}}, "alex-app")
	must(t, err)
	if r := repoOf(t, mustState(t, s), "garden"); r.AgentsAllowed || !r.AgentsPending {
		t.Errorf("garden while switching off: %+v", r)
	}
	if _, _, err := s.AppendAction(api.NewAction{ID: id4, Kind: api.KindAsk, Key: garden}, "alex-app"); !errors.Is(err, ErrInvalid) {
		t.Errorf("ask after a pending switch off: %v", err)
	}
	if _, _, err := s.AppendAction(api.NewAction{ID: id4, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Repot the fern", Ask: true}}, "alex-app"); !errors.Is(err, ErrInvalid) {
		t.Errorf("add with ask after a pending switch off: %v", err)
	}
	// An add without ask still goes.
	if _, _, err := s.AppendAction(api.NewAction{ID: id4, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Repot the fern"}}, "alex-app"); err != nil {
		t.Errorf("add without ask: %v", err)
	}
}

func TestAskFoldsNoteAndProvisionalRun(t *testing.T) {
	s, c := open(t)
	seedAgents(t, s)
	key := ItemKey("laptop", "garden", "gd-2")
	a, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAsk, Key: key, Payload: api.Payload{Text: "Use the blue pot"}}, "alex-app")
	must(t, err)
	if a.Payload.Text != "Use the blue pot" || a.BeadID != "gd-2" {
		t.Fatalf("action %+v", a)
	}
	it := item(t, mustState(t, s), key)
	if !it.Pending || len(it.Comments) != 2 {
		t.Fatalf("item %+v", it)
	}
	if cm := it.Comments[1]; cm.ID != "pending-"+id1 || cm.Author != "alex-app" || cm.Text != "Use the blue pot" {
		t.Errorf("note %+v", cm)
	}
	r := it.Run
	if r == nil || r.ID != "pending-"+id1 || r.State != api.RunWorking || r.Agent != "agent" ||
		r.Activity != "Starting" || !r.StartedAt.Equal(c.t) || r.Count != 1 || r.Queued != 0 {
		t.Fatalf("run %+v", r)
	}

	// Without text there is no note. The agent is the bead's creator, and a
	// settled run's count goes on.
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "closed", CreatedBy: "planner"},
	}))
	k1 := ItemKey("laptop", "garden", "gd-1")
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindAsk, Key: k1, Payload: api.Payload{Text: "  "}}, "alex-app")
	must(t, err)
	it = item(t, mustState(t, s), k1)
	if len(it.Comments) != 0 || it.Run == nil || it.Run.Agent != "planner" || it.Run.Count != 1 {
		t.Fatalf("ask on a closed item: %+v run %+v", it, it.Run)
	}
	settled := run("gd-1", "r1", api.RunWaiting, c.t.Add(-time.Hour))
	settled.Agent, settled.Count = "gardener", 3
	must(t, s.ReplaceRuns("laptop", []api.RunReport{settled}))
	it = item(t, mustState(t, s), k1)
	if r := it.Run; r.ID != "pending-"+id2 || r.State != api.RunWorking || r.Agent != "gardener" || r.Count != 4 {
		t.Fatalf("ask on a settled run: %+v", r)
	}
}

func TestAskOnWorkingRunIsAFollowUp(t *testing.T) {
	s, c := open(t)
	seedAgents(t, s)
	key := ItemKey("laptop", "garden", "gd-1")
	must(t, s.ReplaceRuns("laptop", []api.RunReport{run("gd-1", "r1", api.RunWorking, c.t)}))
	c.tick()
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAsk, Key: key, Payload: api.Payload{Text: "Only the front bed"}}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindAsk, Key: key}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id3, Kind: api.KindAsk, Key: key, Payload: api.Payload{Text: "And the pots"}}, "alex-app")
	must(t, err)
	it := item(t, mustState(t, s), key)
	if r := it.Run; r.ID != "r1" || r.State != api.RunWorking || r.Queued != 2 || r.Count != 1 {
		t.Fatalf("follow-ups started a second run or lost a note: %+v", r)
	}
	if len(it.Comments) != 2 {
		t.Errorf("comments %+v", it.Comments)
	}

	// Two asks on an idle item make one provisional run; the second note queues.
	k2 := ItemKey("laptop", "garden", "gd-2")
	_, _, err = s.AppendAction(api.NewAction{ID: id4, Kind: api.KindAsk, Key: k2, Payload: api.Payload{Text: "Which pot?"}}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a55", Kind: api.KindAsk, Key: k2, Payload: api.Payload{Text: "The blue one"}}, "alex-app")
	must(t, err)
	if r := item(t, mustState(t, s), k2).Run; r.ID != "pending-"+id4 || r.Queued != 1 {
		t.Fatalf("second ask on an idle item: %+v", r)
	}
}

func TestAppliedAskIsNotCountedTwice(t *testing.T) {
	s, c := open(t)
	seedAgents(t, s)
	key := ItemKey("laptop", "garden", "gd-1")
	must(t, s.ReplaceRuns("laptop", []api.RunReport{run("gd-1", "r1", api.RunWorking, c.t)}))
	c.tick()
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAsk, Key: key, Payload: api.Payload{Text: "Only the front bed"}}, "alex-app")
	must(t, err)
	var changes int
	s.Watch(func(ch Change) {
		if ch.Kind == "runs" {
			changes++
		}
	})

	// The bridge wrote the note to the run and reports it before the result.
	written := run("gd-1", "r1", api.RunWorking, c.t)
	written.Queued, written.Applying = 1, id1
	must(t, s.ReplaceRuns("laptop", []api.RunReport{written}))
	it := item(t, mustState(t, s), key)
	if r := it.Run; r.Queued != 1 {
		t.Fatalf("an applied ask counted again while pending: %+v", r)
	}
	if len(it.Comments) != 1 {
		t.Errorf("the applied ask's note is gone: %+v", it.Comments)
	}

	// The same list without applying shows the pending ask again, and says so.
	written.Applying = ""
	must(t, s.ReplaceRuns("laptop", []api.RunReport{written}))
	if r := item(t, mustState(t, s), key).Run; r.Queued != 2 {
		t.Fatalf("a pending ask the bridge has not applied: %+v", r)
	}
	if changes != 2 {
		t.Errorf("runs changes %d, want 2", changes)
	}

	written.Applying = "a/b"
	if err := s.ReplaceRuns("laptop", []api.RunReport{written}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad applying id: %v", err)
	}
}

func TestPendingStop(t *testing.T) {
	s, c := open(t)
	seedAgents(t, s)
	key := ItemKey("laptop", "garden", "gd-1")
	working := run("gd-1", "r1", api.RunWorking, c.t)
	working.Queued = 1
	must(t, s.ReplaceRuns("laptop", []api.RunReport{working, run("gd-2", "r2", api.RunDone, c.t)}))
	c.tick()
	stopAt := c.t
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindStop, Key: key, Payload: api.Payload{Text: "x"}}, "alex-app")
	must(t, err)
	st := mustState(t, s)
	r := item(t, st, key).Run
	if r.State != api.RunStopped || r.Reason != api.ReasonStopped || r.EndedAt == nil || !r.EndedAt.Equal(stopAt) || r.Queued != 0 {
		t.Fatalf("run while the stop is pending: %+v", r)
	}
	if n := len(item(t, st, key).Comments); n != 0 {
		t.Errorf("stop added a note")
	}
	// A stop on a settled run changes nothing.
	k2 := ItemKey("laptop", "garden", "gd-2")
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindStop, Key: k2}, "alex-app")
	must(t, err)
	if r := item(t, mustState(t, s), k2).Run; r.State != api.RunDone {
		t.Errorf("stop changed a settled run: %+v", r)
	}
}

func TestAddWithAskShowsAStartingRun(t *testing.T) {
	s, _ := open(t)
	seedAgents(t, s)
	a, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Repot the fern", Ask: true}}, "alex-app")
	must(t, err)
	if !a.Payload.Ask {
		t.Fatalf("add lost ask: %+v", a.Payload)
	}
	it := item(t, mustState(t, s), ItemKey("laptop", "garden", "pending-"+id1))
	if it.Run == nil || it.Run.State != api.RunWorking || it.Run.Activity != "Starting" {
		t.Fatalf("provisional item run %+v", it.Run)
	}
}

func TestReplaceRuns(t *testing.T) {
	s, c := open(t)
	seedAgents(t, s)
	var changes []Change
	s.Watch(func(ch Change) { changes = append(changes, ch) })
	started := c.t.Add(-time.Minute)
	ended := started.Add(time.Minute)
	must(t, s.ReplaceRuns("laptop", []api.RunReport{
		run("gd-1", "r1", api.RunWorking, started),
		run("gd-9", "r9", api.RunWorking, started), // a bead the hub does not hold
		{Repo: "attic", BeadID: "at-1", Run: api.Run{ID: "r8", State: api.RunDone, StartedAt: started, EndedAt: &ended}},
	}))
	if len(changes) != 1 || changes[0].Kind != "runs" || changes[0].Bridge != "laptop" {
		t.Fatalf("changes %+v", changes)
	}
	st := mustState(t, s)
	if r := item(t, st, ItemKey("laptop", "garden", "gd-1")).Run; r == nil || r.ID != "r1" || r.Agent != "planner" {
		t.Fatalf("run %+v", r)
	}
	if r := item(t, st, ItemKey("laptop", "garden", "gd-2")).Run; r != nil {
		t.Fatalf("gd-2 has a run: %+v", r)
	}
	// The same list again is no change.
	changes = nil
	must(t, s.ReplaceRuns("laptop", []api.RunReport{
		run("gd-1", "r1", api.RunWorking, started),
		run("gd-9", "r9", api.RunWorking, started),
		{Repo: "attic", BeadID: "at-1", Run: api.Run{ID: "r8", State: api.RunDone, StartedAt: started, EndedAt: &ended}},
	}))
	if len(changes) != 0 {
		t.Fatalf("an unchanged list emitted %+v", changes)
	}
	// A new list replaces the old one: gd-1's run is gone, gd-2 has one. A
	// stored run shows once its bead arrives.
	end := c.t
	done := run("gd-2", "r2", api.RunStopped, started)
	done.Reason, done.EndedAt, done.Detail = api.ReasonUsageLimit, &end, "The usage limit resets at 3 pm."
	must(t, s.ReplaceRuns("laptop", []api.RunReport{done, run("gd-9", "r9", api.RunWorking, started)}))
	if len(changes) != 1 {
		t.Fatalf("changes %+v", changes)
	}
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open"},
		{ID: "gd-2", Title: "Repot the fern", Status: "open"},
		{ID: "gd-9", Title: "Rake the leaves", Status: "open"},
	}))
	st = mustState(t, s)
	if r := item(t, st, ItemKey("laptop", "garden", "gd-1")).Run; r != nil {
		t.Errorf("gd-1 kept a dropped run: %+v", r)
	}
	if r := item(t, st, ItemKey("laptop", "garden", "gd-2")).Run; r == nil || r.Reason != api.ReasonUsageLimit || r.Detail == "" {
		t.Errorf("gd-2 run %+v", r)
	}
	if r := item(t, st, ItemKey("laptop", "garden", "gd-9")).Run; r == nil || r.ID != "r9" {
		t.Errorf("gd-9 run %+v", r)
	}
	// The personal list never shows a run, even one a bridge names.
	hubKey, _, err := s.AddPersonal(api.PersonalTodo{ID: id6, Title: "Call Sam"}, "laptop", "")
	must(t, err)
	if it := item(t, mustState(t, s), hubKey); it.Run != nil {
		t.Errorf("personal run %+v", it.Run)
	}
	// A bridge never sends a run inside an item.
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open", Run: &api.Run{ID: "x", State: api.RunDone}},
	}))
	if r := item(t, mustState(t, s), ItemKey("laptop", "garden", "gd-1")).Run; r != nil {
		t.Errorf("an item's own run was kept: %+v", r)
	}
}

func TestReplaceRunsValidation(t *testing.T) {
	s, c := open(t)
	seedAgents(t, s)
	good := run("gd-1", "r1", api.RunWorking, c.t)
	for name, bad := range map[string]func(*api.RunReport){
		"bad repo":            func(r *api.RunReport) { r.Repo = "a//b" },
		"bad bead":            func(r *api.RunReport) { r.BeadID = "a/b" },
		"provisional bead":    func(r *api.RunReport) { r.BeadID = "pending-x" },
		"no id":               func(r *api.RunReport) { r.ID = " " },
		"unknown state":       func(r *api.RunReport) { r.State = "idle" },
		"unknown reason":      func(r *api.RunReport) { r.State, r.Reason = api.RunStopped, "tired" },
		"offline reason":      func(r *api.RunReport) { r.State, r.Reason = api.RunStopped, api.ReasonOffline },
		"no start":            func(r *api.RunReport) { r.StartedAt = time.Time{} },
		"negative count":      func(r *api.RunReport) { r.Count = -1 },
		"huge count":          func(r *api.RunReport) { r.Count = 1000001 },
		"huge queued":         func(r *api.RunReport) { r.Queued = 1000001 },
		"working with an end": func(r *api.RunReport) { end := r.StartedAt.Add(time.Second); r.EndedAt = &end },
		"end before the start": func(r *api.RunReport) {
			r.State = api.RunWaiting
			end := r.StartedAt.Add(-time.Second)
			r.EndedAt = &end
		},
	} {
		r := good
		bad(&r)
		if err := s.ReplaceRuns("laptop", []api.RunReport{r}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := s.ReplaceRuns("laptop", []api.RunReport{good, good}); !errors.Is(err, ErrInvalid) {
		t.Errorf("two runs on one bead: %v", err)
	}
	// A settled run passes with an end after its start, and is refused without one.
	end := good.StartedAt.Add(time.Minute)
	settle := func() api.RunReport {
		r := good
		r.State, r.EndedAt = api.RunWaiting, &end
		return r
	}
	if err := s.ReplaceRuns("laptop", []api.RunReport{settle()}); err != nil {
		t.Errorf("a settled run with an end: %v", err)
	}
	noEnd := settle()
	noEnd.EndedAt = nil
	if err := s.ReplaceRuns("laptop", []api.RunReport{noEnd}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a settled run without an end: %v", err)
	}
}

func TestOfflineRun(t *testing.T) {
	s, c := open(t)
	seedAgents(t, s)
	key := ItemKey("laptop", "garden", "gd-1")
	seen := c.t
	must(t, s.ReplaceRuns("laptop", []api.RunReport{run("gd-1", "r1", api.RunWorking, c.t), run("gd-2", "r2", api.RunWaiting, c.t)}))
	var changes []Change
	s.Watch(func(ch Change) { changes = append(changes, ch) })

	c.t = seen.Add(OfflineAfter)
	if r := item(t, mustState(t, s), key).Run; r.State != api.RunWorking {
		t.Fatalf("run at the limit %+v", r)
	}
	c.t = seen.Add(OfflineAfter + time.Second)
	st := mustState(t, s)
	r := item(t, st, key).Run
	if r.State != api.RunStopped || r.Reason != api.ReasonOffline || r.Detail == "" || r.EndedAt == nil || !r.EndedAt.Equal(seen) {
		t.Fatalf("run of a silent bridge %+v", r)
	}
	if r := item(t, st, ItemKey("laptop", "garden", "gd-2")).Run; r.State != api.RunWaiting {
		t.Errorf("a settled run changed: %+v", r)
	}
	// The first State that shows it emits once.
	mustState(t, s)
	if len(changes) != 1 || changes[0].Kind != "runs" {
		t.Fatalf("changes %+v", changes)
	}
	// A pending ask on it starts a new run.
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAsk, Key: key}, "alex-app")
	must(t, err)
	if r := item(t, mustState(t, s), key).Run; r.State != api.RunWorking || r.ID != "pending-"+id1 || r.Count != 2 {
		t.Errorf("ask on an offline run %+v", r)
	}
	// The bridge reporting again shows the run as it reports it, and the
	// first State after that emits once more: apps told of the offline stop
	// are told of the recovery too.
	must(t, s.Touch("laptop"))
	if r := item(t, mustState(t, s), key).Run; r.State != api.RunWorking || r.ID != "r1" {
		t.Errorf("run after the bridge came back %+v", r)
	}
	var runChanges int
	for _, ch := range changes {
		if ch.Kind == "runs" {
			runChanges++
		}
	}
	if runChanges != 2 {
		t.Fatalf("%d runs changes, want 2 (offline, then recovery): %+v", runChanges, changes)
	}
	// Nothing new: no further emit.
	n := len(changes)
	mustState(t, s)
	for _, ch := range changes[n:] {
		if ch.Kind == "runs" {
			t.Errorf("an unchanged state emitted %+v", ch)
		}
	}
}

func TestReposCarryAgentsAllowedThroughRename(t *testing.T) {
	s, c := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "garden", OptedIn: true, AgentsAllowed: true}}))
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{{ID: "gd-1", Title: "Water the plants", Status: "open"}}))
	must(t, s.Touch("laptop"))
	must(t, s.ReplaceRuns("laptop", []api.RunReport{run("gd-1", "r1", api.RunWaiting, c.t)}))
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "projects/garden", OptedIn: true, AgentsAllowed: true}}))
	st := mustState(t, s)
	if r := repoOf(t, st, "projects/garden"); !r.AgentsAllowed {
		t.Errorf("repo %+v", r)
	}
	if r := item(t, st, ItemKey("laptop", "projects/garden", "gd-1")).Run; r == nil || r.ID != "r1" {
		t.Errorf("run did not move with the repo: %+v", r)
	}
}

func TestClosedInApp(t *testing.T) {
	s, c := open(t)
	seedAgents(t, s)
	key := ItemKey("laptop", "garden", "gd-1")
	ctx := context.Background()
	before := c.t
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindDone, Key: key}, "alex-app")
	must(t, err)
	c.tick()
	start := c.t
	closed := func(since time.Time) bool {
		t.Helper()
		ok, err := s.ClosedInApp(ctx, key, since)
		must(t, err)
		return ok
	}
	if !closed(before) {
		t.Error("a done after since does not count")
	}
	if closed(start) {
		t.Error("a done before the run started counts")
	}
	c.tick()
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindDismiss, Key: key}, "alex-app")
	must(t, err)
	if !closed(start) {
		t.Error("a dismiss after the start does not count")
	}
	// Undo cancels it.
	_, err = s.CancelAction(id2)
	must(t, err)
	if closed(start) {
		t.Error("a cancelled dismiss counts")
	}
	c.tick()
	_, _, err = s.AppendAction(api.NewAction{ID: id3, Kind: api.KindDone, Key: key}, "alex-app")
	must(t, err)
	c.tick()
	_, _, err = s.AppendAction(api.NewAction{ID: id4, Kind: api.KindReopen, Key: key}, "alex-app")
	must(t, err)
	if closed(start) {
		t.Error("a reopened item counts as closed")
	}
}
