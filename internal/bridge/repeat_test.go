package bridge

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
)

func TestParseEvery(t *testing.T) {
	for in, want := range map[string]api.Repeat{
		"day":                {Every: api.EveryDay},
		"Week":               {Every: api.EveryWeek},
		"month":              {Every: api.EveryMonth},
		"year":               {Every: api.EveryYear},
		"1 day":              {Every: api.EveryDay},
		"3 days":             {Every: api.EveryDay, Interval: 3},
		"2 weeks":            {Every: api.EveryWeek, Interval: 2},
		"6 months":           {Every: api.EveryMonth, Interval: 6},
		"100 years":          {Every: api.EveryYear, Interval: 100},
		"weekday":            {Every: api.EveryWeek, Weekdays: []int{1, 2, 3, 4, 5}},
		"mon,thu":            {Every: api.EveryWeek, Weekdays: []int{1, 4}},
		"Sunday":             {Every: api.EveryWeek, Weekdays: []int{0}},
		"sat, sun, sat":      {Every: api.EveryWeek, Weekdays: []int{6, 0}},
		"2 weeks on mon,thu": {Every: api.EveryWeek, Interval: 2, Weekdays: []int{1, 4}},
		" week on tuesday ":  {Every: api.EveryWeek, Weekdays: []int{2}},
	} {
		got, err := ParseEvery(in, false)
		if err != nil || got.Every != want.Every || got.Interval != want.Interval || !slices.Equal(got.Weekdays, want.Weekdays) || got.FromDone {
			t.Errorf("ParseEvery(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	if got, err := ParseEvery("3 days", true); err != nil || !got.FromDone || got.Interval != 3 {
		t.Errorf("from done: %+v, %v", got, err)
	}
	for _, in := range []string{"", "daily", "fortnight", "0 days", "101 days", "-2 weeks", "two weeks", "3 days on mon",
		"2 months on mon", "week on someday", "mon,someday", "2 mon", "3 days please"} {
		if _, err := ParseEvery(in, false); err == nil {
			t.Errorf("ParseEvery(%q) took it", in)
		}
	}
	for _, in := range []string{"mon,thu", "weekday", "week on fri"} {
		if _, err := ParseEvery(in, true); err == nil || !strings.Contains(err.Error(), "--from-done") {
			t.Errorf("ParseEvery(%q) from done: %v", in, err)
		}
	}
}

func TestZoneName(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := zoneName(paris); err != nil || got != "Europe/Paris" {
		t.Errorf("a named zone: %q, %v", got, err)
	}
	t.Setenv("TZ", "America/New_York")
	if got, err := zoneName(time.Local); err != nil || got != "America/New_York" {
		t.Errorf("TZ: %q, %v", got, err)
	}
}

func TestAddAndEditRepeat(t *testing.T) {
	saved := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = saved })
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	hub.SetTimeZone("America/New_York")
	env, out := pairedEnv(t, hub)
	ctx := context.Background()

	if err := Add(ctx, []string{"Water the plants", "--due", "2026-03-02 20:30", "--every", "2 days", "--from-done"}, env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ps := hub.Personals()
	p := ps[len(ps)-1]
	if r := p.Repeat; p.Due == nil || r == nil || r.Every != api.EveryDay || r.Interval != 2 || !r.FromDone || r.TimeZone != "America/New_York" || r.Start != nil {
		t.Fatalf("added %+v, repeat %+v", p, p.Repeat)
	}

	if err := Edit(ctx, []string{"mm-1", "--every", "mon,thu"}, env); err != nil {
		t.Fatal(err)
	}
	if err := Edit(ctx, []string{"mm-1", "--no-repeat"}, env); err != nil {
		t.Fatal(err)
	}
	cs := hub.Changes()
	if len(cs) != 2 {
		t.Fatalf("changes %+v", cs)
	}
	if set := cs[0].Payload.Set; set.Repeat == nil || !slices.Equal(set.Repeat.Weekdays, []int{1, 4}) ||
		set.Repeat.TimeZone != "America/New_York" || set.Due != nil || set.ClearRepeat {
		t.Fatalf("every set %+v", set)
	}
	if set := cs[1].Payload.Set; !set.ClearRepeat || set.Repeat != nil {
		t.Fatalf("no-repeat set %+v", set)
	}

	// Until the hub has a zone, this machine's is the rule's.
	hub.SetTimeZone("")
	if err := Add(ctx, []string{"Call Sam", "--due", "2026-03-03", "--every", "week"}, env); err != nil {
		t.Fatal(err)
	}
	ps = hub.Personals()
	if r := ps[len(ps)-1].Repeat; r == nil || r.TimeZone != "UTC" {
		t.Fatalf("repeat without a hub zone %+v", r)
	}
}

func TestRepeatFlagsRefuse(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	env, _ := pairedEnv(t, hub)
	ctx := context.Background()
	before := len(hub.Personals())
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no due":          {[]string{"Water the plants", "--every", "day"}, "--due"},
		"bad rule":        {[]string{"Water the plants", "--due", "2026-03-02", "--every", "fortnight"}, "--every"},
		"from-done alone": {[]string{"Water the plants", "--due", "2026-03-02", "--from-done"}, "--every"},
		"from-done, days": {[]string{"Water the plants", "--due", "2026-03-02", "--every", "mon", "--from-done"}, "--from-done"},
	} {
		if err := Add(ctx, tc.args, env); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("add %s: %v, want %q", name, err, tc.want)
		}
	}
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"every and no-repeat": {[]string{"mm-1", "--every", "day", "--no-repeat"}, "not both"},
		"every and clear-due": {[]string{"mm-1", "--every", "day", "--clear-due"}, "--clear-due"},
		"from-done alone":     {[]string{"mm-1", "--from-done"}, "--every"},
		"bad rule":            {[]string{"mm-1", "--every", "sometimes"}, "--every"},
	} {
		if err := Edit(ctx, tc.args, env); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("edit %s: %v, want %q", name, err, tc.want)
		}
	}
	if len(hub.Personals()) != before || len(hub.Changes()) != 0 {
		t.Fatal("a refused add or edit reached the hub")
	}
}
