package api

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckRuns(t *testing.T) {
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, time.FixedZone("x", 3600))
	end := start.Add(time.Minute)
	before := start.Add(-time.Minute)
	ok := RunReport{Repo: "projects/garden", BeadID: "gd-1", Run: Run{ID: "r1", State: RunWorking, StartedAt: start, Count: 1}}
	with := func(f func(*RunReport)) RunReport { r := ok; f(&r); return r }

	got, err := CheckRuns([]RunReport{with(func(r *RunReport) {
		r.State, r.EndedAt, r.Reason, r.Agent = RunWaiting, &end, ReasonBusy, ""
	})})
	if err != nil {
		t.Fatalf("a run with no agent: %v", err)
	}
	if got[0].Reason != "" || got[0].StartedAt.Location() != time.UTC || got[0].EndedAt.Location() != time.UTC {
		t.Fatalf("not cleaned: %+v", got[0])
	}

	for name, reports := range map[string][]RunReport{
		"unknown reason":    {with(func(r *RunReport) { r.State, r.EndedAt, r.Reason = RunStopped, &end, "bored" })},
		"offline reason":    {with(func(r *RunReport) { r.State, r.EndedAt, r.Reason = RunStopped, &end, ReasonOffline })},
		"ended early":       {with(func(r *RunReport) { r.State, r.EndedAt = RunDone, &before })},
		"count too large":   {with(func(r *RunReport) { r.Count = MaxRunCount + 1 })},
		"queued too large":  {with(func(r *RunReport) { r.Queued = MaxRunCount + 1 })},
		"two runs one bead": {ok, with(func(r *RunReport) { r.ID = "r2" })},
		"bad applying":      {with(func(r *RunReport) { r.Applying = "a/b" })},
		"provisional bead":  {with(func(r *RunReport) { r.BeadID = ProvisionalPrefix + "x" })},
		"bad bead":          {with(func(r *RunReport) { r.BeadID = "-x" })},
		"bad repo":          {with(func(r *RunReport) { r.Repo = "a//b" })},
	} {
		if _, err := CheckRuns(reports); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestCheckItems(t *testing.T) {
	items := []Item{{ID: "gd-1", Key: "a/b/gd-1", Bridge: "a", Repo: "b", Pending: true, Title: strings.Repeat("t", ItemTitleLimit+1)}}
	if err := CheckItems(items); err != nil {
		t.Fatal(err)
	}
	it := items[0]
	if it.Key != "" || it.Bridge != "" || it.Repo != "" || it.Pending || it.Comments == nil || !strings.HasSuffix(it.Title, "…") {
		t.Fatalf("not cleaned: %+v", it)
	}
	for name, items := range map[string][]Item{
		"duplicate":   {{ID: "gd-1"}, {ID: "gd-1"}},
		"provisional": {{ID: ProvisionalPrefix + "x"}},
		"bad id":      {{ID: ""}},
	} {
		if err := CheckItems(items); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestCheckRepos(t *testing.T) {
	got, err := CheckRepos([]Repo{{Name: "projects/garden", Bridge: "laptop", Pending: true, AgentsPending: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Bridge != "" || got[0].Pending || got[0].AgentsPending {
		t.Fatalf("not cleaned: %+v", got[0])
	}
	for name, repos := range map[string][]Repo{
		"duplicate": {{Name: "garden"}, {Name: "garden"}},
		"bad name":  {{Name: "../garden"}},
	} {
		if _, err := CheckRepos(repos); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}
