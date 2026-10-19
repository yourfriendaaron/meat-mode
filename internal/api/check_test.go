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

// The fields no other limit covered are cut to size too: created-by and
// each comment's id to CommentAuthorLimit, the close reason to DetailLimit,
// and the labels to ItemLabelsLimit of at most LabelLimit runes each. A
// tracker's labels are only cut, never held to the app's tag rules.
func TestCheckItemsTruncatesTheRest(t *testing.T) {
	labels := make([]string, ItemLabelsLimit+50)
	for i := range labels {
		labels[i] = strings.Repeat("l", LabelLimit+50)
	}
	items := []Item{{
		ID: "gd-1", Title: "Water the plants", Status: "open",
		CreatedBy:   strings.Repeat("c", CommentAuthorLimit+50),
		CloseReason: strings.Repeat("r", DetailLimit+50),
		Labels:      labels,
		Comments:    []Comment{{ID: strings.Repeat("i", CommentAuthorLimit+50), Text: "Which pot?"}},
	}}
	if err := CheckItems(items); err != nil {
		t.Fatal(err)
	}
	it := items[0]
	runeLen := func(s string) int { return len([]rune(s)) }
	if got := runeLen(it.CreatedBy); got != CommentAuthorLimit+1 || !strings.HasSuffix(it.CreatedBy, "…") {
		t.Errorf("createdBy %d runes, want %d plus an ellipsis", got, CommentAuthorLimit)
	}
	if got := runeLen(it.CloseReason); got != DetailLimit+1 || !strings.HasSuffix(it.CloseReason, "…") {
		t.Errorf("closeReason %d runes, want %d plus an ellipsis", got, DetailLimit)
	}
	if got := runeLen(it.Comments[0].ID); got != CommentAuthorLimit+1 || !strings.HasSuffix(it.Comments[0].ID, "…") {
		t.Errorf("comment id %d runes, want %d plus an ellipsis", got, CommentAuthorLimit)
	}
	if len(it.Labels) != ItemLabelsLimit {
		t.Errorf("labels %d, want %d", len(it.Labels), ItemLabelsLimit)
	}
	for _, l := range it.Labels {
		if got := runeLen(l); got != LabelLimit+1 || !strings.HasSuffix(l, "…") {
			t.Errorf("label %d runes, want %d plus an ellipsis", got, LabelLimit)
			break
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
