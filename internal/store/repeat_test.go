package store

import (
	"errors"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	must(t, err)
	return loc
}

// at reads "2006-01-02 15:04" in loc.
func at(t *testing.T, loc *time.Location, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	must(t, err)
	return v
}

func TestNextDue(t *testing.T) {
	ny, paris := zone(t, "America/New_York"), zone(t, "Europe/Paris")
	type rule struct {
		every    string
		interval int
		weekdays []int
		fromDone bool
		loc      *time.Location
		start    string
	}
	for _, tc := range []struct {
		name     string
		r        rule
		due, now string // in the rule's zone; due "" is the start
		want     string // in the rule's zone
		wantUTC  string // when the wall time alone is ambiguous
	}{
		// Daylight saving changes keep the wall-clock time.
		{"daily evening, New York spring forward", rule{every: "day", loc: ny, start: "2026-03-07 20:30"}, "", "2026-03-07 21:00", "2026-03-08 20:30", ""},
		{"daily evening, New York fall back", rule{every: "day", loc: ny, start: "2026-10-31 20:30"}, "", "2026-10-31 21:00", "2026-11-01 20:30", ""},
		{"daily evening, Paris spring forward", rule{every: "day", loc: paris, start: "2026-03-28 20:30"}, "", "2026-03-28 21:00", "2026-03-29 20:30", ""},
		{"daily evening, Paris fall back", rule{every: "day", loc: paris, start: "2026-10-24 20:30"}, "", "2026-10-24 21:00", "2026-10-25 20:30", ""},
		{"skipped hour, New York", rule{every: "day", loc: ny, start: "2026-03-07 02:30"}, "", "2026-03-07 03:00", "", "2026-03-08T07:30:00Z"},
		{"after the skipped hour, New York", rule{every: "day", loc: ny, start: "2026-03-07 02:30"}, "2026-03-08 03:30", "2026-03-08 04:00", "2026-03-09 02:30", ""},
		{"skipped hour, Paris", rule{every: "day", loc: paris, start: "2026-03-28 02:30"}, "", "2026-03-28 03:00", "", "2026-03-29T01:30:00Z"},
		{"repeated hour, New York, first of the two", rule{every: "day", loc: ny, start: "2026-10-31 01:30"}, "", "2026-10-31 02:00", "", "2026-11-01T05:30:00Z"},
		{"repeated hour, Paris, first of the two", rule{every: "day", loc: paris, start: "2026-10-24 02:30"}, "", "2026-10-24 03:00", "", "2026-10-25T00:30:00Z"},
		{"weekly across spring forward", rule{every: "week", loc: ny, start: "2026-03-02 20:30"}, "", "2026-03-02 21:00", "2026-03-09 20:30", ""},

		// Month ends clamp and return, counted from the start.
		{"31st to February", rule{every: "month", loc: paris, start: "2026-01-31 09:00"}, "", "2026-01-31 10:00", "2026-02-28 09:00", ""},
		{"back to the 31st", rule{every: "month", loc: paris, start: "2026-01-31 09:00"}, "2026-02-28 09:00", "2026-02-28 10:00", "2026-03-31 09:00", ""},
		{"31st to the 30th", rule{every: "month", loc: paris, start: "2026-01-31 09:00"}, "2026-03-31 09:00", "2026-03-31 10:00", "2026-04-30 09:00", ""},
		{"31st in a leap year", rule{every: "month", loc: paris, start: "2028-01-31 09:00"}, "", "2028-01-31 10:00", "2028-02-29 09:00", ""},
		{"every 2 months over the year end", rule{every: "month", interval: 2, loc: paris, start: "2025-12-31 09:00"}, "", "2025-12-31 10:00", "2026-02-28 09:00", ""},
		{"every 2 months back to the 31st", rule{every: "month", interval: 2, loc: paris, start: "2025-12-31 09:00"}, "2026-02-28 09:00", "2026-03-01 10:00", "2026-04-30 09:00", ""},
		{"every 12 months", rule{every: "month", interval: 12, loc: paris, start: "2026-05-15 09:00"}, "", "2026-05-15 10:00", "2027-05-15 09:00", ""},

		// 29 February.
		{"yearly from 29 February", rule{every: "year", loc: paris, start: "2024-02-29 09:00"}, "", "2024-02-29 10:00", "2025-02-28 09:00", ""},
		{"yearly back to 29 February", rule{every: "year", loc: paris, start: "2024-02-29 09:00"}, "2027-02-28 09:00", "2027-02-28 10:00", "2028-02-29 09:00", ""},
		{"every 4 years", rule{every: "year", interval: 4, loc: paris, start: "2024-02-29 09:00"}, "", "2024-03-01 10:00", "2028-02-29 09:00", ""},
		{"daily over 29 February", rule{every: "day", loc: paris, start: "2028-02-28 09:00"}, "", "2028-02-28 10:00", "2028-02-29 09:00", ""},

		// Weekdays, with weeks from Monday and every nth one counted from
		// the week of the start. 2 March 2026 is a Monday.
		{"Monday and Thursday", rule{every: "week", weekdays: []int{1, 4}, loc: paris, start: "2026-03-02 09:00"}, "", "2026-03-02 10:00", "2026-03-05 09:00", ""},
		{"Thursday to next Monday", rule{every: "week", weekdays: []int{4, 1}, loc: paris, start: "2026-03-02 09:00"}, "2026-03-05 09:00", "2026-03-05 10:00", "2026-03-09 09:00", ""},
		{"fortnightly from a Thursday", rule{every: "week", interval: 2, weekdays: []int{1, 4}, loc: paris, start: "2026-03-05 09:00"}, "", "2026-03-05 10:00", "2026-03-16 09:00", ""},
		{"fortnightly, second day", rule{every: "week", interval: 2, weekdays: []int{1, 4}, loc: paris, start: "2026-03-05 09:00"}, "2026-03-16 09:00", "2026-03-16 10:00", "2026-03-19 09:00", ""},
		{"fortnightly, after the gap", rule{every: "week", interval: 2, weekdays: []int{1, 4}, loc: paris, start: "2026-03-05 09:00"}, "2026-03-19 09:00", "2026-03-19 10:00", "2026-03-30 09:00", ""},
		{"start on a day not picked", rule{every: "week", interval: 2, weekdays: []int{1, 4}, loc: paris, start: "2026-03-04 09:00"}, "", "2026-03-04 10:00", "2026-03-05 09:00", ""},
		{"Sunday ends the week", rule{every: "week", interval: 2, weekdays: []int{0, 1}, loc: paris, start: "2026-03-02 09:00"}, "", "2026-03-02 10:00", "2026-03-08 09:00", ""},
		{"Sunday, then the next block", rule{every: "week", interval: 2, weekdays: []int{0, 1}, loc: paris, start: "2026-03-02 09:00"}, "2026-03-08 09:00", "2026-03-08 10:00", "2026-03-16 09:00", ""},
		{"weekdays across fall back", rule{every: "week", weekdays: []int{0}, loc: ny, start: "2026-10-25 01:30"}, "", "2026-10-25 02:00", "", "2026-11-01T05:30:00Z"},
		{"every 3 weeks", rule{every: "week", interval: 3, loc: paris, start: "2026-03-05 09:00"}, "", "2026-03-05 10:00", "2026-03-26 09:00", ""},

		// Next after both the due time and now: early, late, skipped.
		{"done early", rule{every: "day", loc: paris, start: "2026-03-01 09:00"}, "2026-03-06 09:00", "2026-03-05 10:00", "2026-03-07 09:00", ""},
		{"done a little early", rule{every: "day", loc: paris, start: "2026-03-01 09:00"}, "2026-03-05 09:00", "2026-03-05 08:00", "2026-03-06 09:00", ""},
		{"done days late, before today's", rule{every: "day", loc: paris, start: "2026-03-01 09:00"}, "", "2026-03-05 08:00", "2026-03-05 09:00", ""},
		{"done days late, after today's", rule{every: "day", loc: paris, start: "2026-03-01 09:00"}, "", "2026-03-05 10:00", "2026-03-06 09:00", ""},
		{"done right at the due time", rule{every: "day", loc: paris, start: "2026-03-01 09:00"}, "", "2026-03-01 09:00", "2026-03-02 09:00", ""},
		{"weekly, done weeks late", rule{every: "week", loc: paris, start: "2026-03-05 09:00"}, "", "2026-04-01 10:00", "2026-04-02 09:00", ""},
		{"monthly, done months late", rule{every: "month", loc: paris, start: "2026-01-31 09:00"}, "", "2026-05-31 10:00", "2026-06-30 09:00", ""},
		{"weekdays, done weeks late", rule{every: "week", interval: 2, weekdays: []int{1, 4}, loc: paris, start: "2026-03-05 09:00"}, "", "2026-04-10 10:00", "2026-04-13 09:00", ""},
		{"daily from long ago", rule{every: "day", loc: paris, start: "1900-01-01 09:00"}, "", "2026-03-05 10:00", "2026-03-06 09:00", ""},
		{"weekdays from long ago", rule{every: "week", weekdays: []int{3}, loc: paris, start: "1900-01-01 09:00"}, "", "2026-03-05 10:00", "2026-03-11 09:00", ""},
		{"monthly from long ago", rule{every: "month", loc: paris, start: "1900-01-31 09:00"}, "", "2026-02-10 10:00", "2026-02-28 09:00", ""},

		// From the done day, in the rule's zone.
		{"from done, every 3 days", rule{every: "day", interval: 3, fromDone: true, loc: ny, start: "2026-03-01 20:30"}, "", "2026-03-10 23:00", "2026-03-13 20:30", ""},
		{"from done, done early", rule{every: "week", fromDone: true, loc: ny, start: "2026-03-01 08:00"}, "2026-03-20 08:00", "2026-03-10 23:00", "2026-03-17 08:00", ""},
		{"from done, monthly from the 31st", rule{every: "month", fromDone: true, loc: paris, start: "2026-01-01 09:00"}, "", "2026-01-31 10:00", "2026-02-28 09:00", ""},
		{"from done, yearly", rule{every: "year", fromDone: true, loc: paris, start: "2026-01-01 09:00"}, "", "2026-07-04 10:00", "2027-07-04 09:00", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := at(t, tc.r.loc, tc.r.start)
			r := api.Repeat{Every: tc.r.every, Interval: tc.r.interval, Weekdays: tc.r.weekdays, FromDone: tc.r.fromDone,
				TimeZone: tc.r.loc.String(), Start: &start}
			due := start
			if tc.due != "" {
				due = at(t, tc.r.loc, tc.due)
			}
			got := nextDue(r, due, at(t, tc.r.loc, tc.now))
			var want time.Time
			if tc.wantUTC != "" {
				var err error
				want, err = time.Parse(time.RFC3339, tc.wantUTC)
				must(t, err)
			} else {
				want = at(t, tc.r.loc, tc.want)
			}
			if !got.Equal(want) {
				t.Errorf("next = %v, want %v", got.In(tc.r.loc), want.In(tc.r.loc))
			}
		})
	}
}

func TestNextDueIsQuickForAnyStart(t *testing.T) {
	start := time.Date(1, 1, 1, 9, 0, 0, 0, time.UTC)
	now := time.Date(9000, 6, 15, 10, 0, 0, 0, time.UTC)
	for _, r := range []api.Repeat{
		{Every: api.EveryDay},
		{Every: api.EveryWeek},
		{Every: api.EveryWeek, Weekdays: []int{0, 3}},
		{Every: api.EveryMonth},
		{Every: api.EveryYear},
	} {
		r.TimeZone, r.Start = "UTC", &start
		began := time.Now()
		got := nextDue(r, start, now)
		if !got.After(now) || got.After(now.AddDate(1, 0, 1)) || time.Since(began) > 50*time.Millisecond {
			t.Errorf("%+v: next %v in %v", r, got, time.Since(began))
		}
	}
}

func TestIsOccurrence(t *testing.T) {
	paris := zone(t, "Europe/Paris")
	start := at(t, paris, "2026-01-31 09:00")
	r := api.Repeat{Every: api.EveryMonth, TimeZone: "Europe/Paris", Start: &start}
	for s, want := range map[string]bool{
		"2026-01-31 09:00": true,
		"2026-02-28 09:00": true,
		"2026-03-31 09:00": true,
		"2026-03-30 09:00": false,
		"2026-02-28 10:00": false,
		"2025-12-31 09:00": false,
	} {
		if got := isOccurrence(r, at(t, paris, s)); got != want {
			t.Errorf("%s: %v, want %v", s, got, want)
		}
	}
	r.FromDone = true
	if isOccurrence(r, start) {
		t.Error("a rule from the done day has no schedule")
	}
}

func TestCheckRepeat(t *testing.T) {
	start := time.Now()
	r, err := checkRepeat(api.Repeat{Every: api.EveryWeek, Interval: 2, Weekdays: []int{4, 1}, TimeZone: "Europe/Paris", Start: &start})
	must(t, err)
	if r.Start != nil || r.Weekdays[0] != 1 || r.Weekdays[1] != 4 {
		t.Errorf("cleaned %+v", r)
	}
	for name, r := range map[string]api.Repeat{
		"no every":           {TimeZone: "UTC"},
		"unknown every":      {Every: "fortnight", TimeZone: "UTC"},
		"negative interval":  {Every: api.EveryDay, Interval: -1, TimeZone: "UTC"},
		"interval too large": {Every: api.EveryDay, Interval: api.MaxRepeatInterval + 1, TimeZone: "UTC"},
		"weekdays, daily":    {Every: api.EveryDay, Weekdays: []int{1}, TimeZone: "UTC"},
		"weekdays, fromDone": {Every: api.EveryWeek, Weekdays: []int{1}, FromDone: true, TimeZone: "UTC"},
		"weekday 7":          {Every: api.EveryWeek, Weekdays: []int{7}, TimeZone: "UTC"},
		"weekday -1":         {Every: api.EveryWeek, Weekdays: []int{-1}, TimeZone: "UTC"},
		"weekday twice":      {Every: api.EveryWeek, Weekdays: []int{2, 2}, TimeZone: "UTC"},
		"no zone":            {Every: api.EveryDay},
		"Local":              {Every: api.EveryDay, TimeZone: "Local"},
		"unknown zone":       {Every: api.EveryDay, TimeZone: "Mars/Olympus"},
		"until not a date":   {Every: api.EveryDay, TimeZone: "UTC", Until: "soon"},
		"until 30 February":  {Every: api.EveryDay, TimeZone: "UTC", Until: "2026-02-30"},
		"until unpadded":     {Every: api.EveryDay, TimeZone: "UTC", Until: "2026-3-1"},
		"until with a time":  {Every: api.EveryDay, TimeZone: "UTC", Until: "2026-03-01T00:00:00Z"},
		"until with a space": {Every: api.EveryDay, TimeZone: "UTC", Until: " 2026-03-01"},
	} {
		if _, err := checkRepeat(r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	for _, r := range []api.Repeat{
		{Every: api.EveryYear, TimeZone: "UTC"},
		{Every: api.EveryDay, Interval: api.MaxRepeatInterval, FromDone: true, TimeZone: "America/New_York"},
		{Every: api.EveryWeek, Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, TimeZone: "Europe/Paris"},
		{Every: api.EveryMonth, TimeZone: "UTC", Until: "2026-02-28"},
	} {
		if got, err := checkRepeat(r); err != nil || got.Until != r.Until {
			t.Errorf("%+v: %+v, %v", r, got, err)
		}
	}
}

func TestAfterUntil(t *testing.T) {
	r := api.Repeat{Every: api.EveryDay, TimeZone: "America/New_York", Until: "2026-03-15"}
	for at, want := range map[time.Time]bool{
		// 20:30 on 15 March in New York is already the 16th in UTC.
		time.Date(2026, 3, 16, 0, 30, 0, 0, time.UTC): false,
		time.Date(2026, 3, 16, 3, 59, 0, 0, time.UTC): false,
		time.Date(2026, 3, 16, 4, 0, 0, 0, time.UTC):  true,
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC):   false,
		time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC):  true,
	} {
		if got := afterUntil(r, at); got != want {
			t.Errorf("afterUntil(%v) = %v, want %v", at, got, want)
		}
	}
	r.Until = ""
	if afterUntil(r, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("a rule with no until never ends")
	}
}
