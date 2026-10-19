package store

import (
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// occurrences lists the dates in [from, to) as "key@time" strings in loc.
func occurrences(t *testing.T, s *Store, loc *time.Location, from, to string) []string {
	t.Helper()
	list, err := s.Occurrences(at(t, time.UTC, from), at(t, time.UTC, to))
	must(t, err)
	if list == nil {
		t.Fatal("Occurrences returned nil")
	}
	var out []string
	for _, o := range list {
		if o.At.Location() != time.UTC {
			t.Fatalf("%v is not in UTC", o.At)
		}
		out = append(out, o.Key+"@"+o.At.In(loc).Format("2006-01-02 15:04"))
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestOccurrencesDaily(t *testing.T) {
	s, _ := open(t)
	if got := occurrences(t, s, time.UTC, "2026-03-01 00:00", "2026-04-01 00:00"); len(got) != 0 {
		t.Fatalf("an empty hub lists %v", got)
	}
	key := addRepeating(t, s, "Water the plants", at(t, time.UTC, "2026-03-02 08:00"),
		api.Repeat{Every: api.EveryDay, TimeZone: "UTC"})
	// The current due time is not listed; from is inclusive, to exclusive.
	eq(t, occurrences(t, s, time.UTC, "2026-03-02 00:00", "2026-03-05 00:00"),
		[]string{key + "@2026-03-03 08:00", key + "@2026-03-04 08:00"})
	eq(t, occurrences(t, s, time.UTC, "2026-03-03 08:00", "2026-03-05 08:00"),
		[]string{key + "@2026-03-03 08:00", key + "@2026-03-04 08:00"})
	eq(t, occurrences(t, s, time.UTC, "2026-03-03 08:01", "2026-03-05 08:01"),
		[]string{key + "@2026-03-04 08:00", key + "@2026-03-05 08:00"})
	// A window before the due time lists nothing.
	eq(t, occurrences(t, s, time.UTC, "2026-02-01 00:00", "2026-03-02 09:00"), nil)
}

func TestOccurrencesWeeklyWeekdays(t *testing.T) {
	s, _ := open(t)
	// Monday 2 March; Mondays and Thursdays.
	key := addRepeating(t, s, "Take out the bins", at(t, time.UTC, "2026-03-02 08:00"),
		api.Repeat{Every: api.EveryWeek, Weekdays: []int{4, 1}, TimeZone: "UTC"})
	eq(t, occurrences(t, s, time.UTC, "2026-03-02 00:00", "2026-03-17 00:00"), []string{
		key + "@2026-03-05 08:00", key + "@2026-03-09 08:00", key + "@2026-03-12 08:00", key + "@2026-03-16 08:00"})
}

func TestOccurrencesMonthlyFromThe31st(t *testing.T) {
	s, _ := open(t)
	key := addRepeating(t, s, "Pay the rent", at(t, time.UTC, "2026-03-31 09:00"),
		api.Repeat{Every: api.EveryMonth, TimeZone: "UTC"})
	eq(t, occurrences(t, s, time.UTC, "2026-04-01 00:00", "2026-08-01 00:00"), []string{
		key + "@2026-04-30 09:00", key + "@2026-05-31 09:00", key + "@2026-06-30 09:00", key + "@2026-07-31 09:00"})
}

func TestOccurrencesStopAtUntil(t *testing.T) {
	s, _ := open(t)
	key := addRepeating(t, s, "Water the plants", at(t, time.UTC, "2026-03-02 08:00"),
		api.Repeat{Every: api.EveryDay, TimeZone: "UTC", Until: "2026-03-04"})
	eq(t, occurrences(t, s, time.UTC, "2026-03-01 00:00", "2026-03-10 00:00"),
		[]string{key + "@2026-03-03 08:00", key + "@2026-03-04 08:00"})
}

func TestOccurrencesSkipFromDoneClosedAndPlainItems(t *testing.T) {
	s, _ := open(t)
	due := at(t, time.UTC, "2026-03-02 08:00")
	addRepeating(t, s, "Call Sam", due, api.Repeat{Every: api.EveryDay, TimeZone: "UTC", FromDone: true})
	closed := addRepeating(t, s, "Sweep the porch", due, api.Repeat{Every: api.EveryDay, TimeZone: "UTC"})
	addPersonal(t, s, id1, "Buy seeds", &due)
	open := addRepeating(t, s, "Water the plants", due, api.Repeat{Every: api.EveryWeek, TimeZone: "UTC"})
	// Done moves a repeating item on; closing one that has no rule left
	// (dismiss) closes it.
	act(t, s, api.KindDismiss, closed, api.Payload{})
	eq(t, occurrences(t, s, time.UTC, "2026-03-01 00:00", "2026-03-20 00:00"),
		[]string{open + "@2026-03-09 08:00", open + "@2026-03-16 08:00"})
}

func TestOccurrencesOrderByTimeThenKey(t *testing.T) {
	s, _ := open(t)
	a := addRepeating(t, s, "Water the plants", at(t, time.UTC, "2026-03-02 08:00"), api.Repeat{Every: api.EveryDay, TimeZone: "UTC"})
	b := addRepeating(t, s, "Feed the cat", at(t, time.UTC, "2026-03-02 07:00"), api.Repeat{Every: api.EveryDay, TimeZone: "UTC"})
	c := addRepeating(t, s, "Walk the dog", at(t, time.UTC, "2026-03-02 08:00"), api.Repeat{Every: api.EveryDay, TimeZone: "UTC"})
	first, second := a, c
	if first > second {
		first, second = second, first
	}
	eq(t, occurrences(t, s, time.UTC, "2026-03-03 00:00", "2026-03-04 00:00"), []string{
		b + "@2026-03-03 07:00", first + "@2026-03-03 08:00", second + "@2026-03-03 08:00"})
}

func TestOccurrencesKeepWallClockAcrossDST(t *testing.T) {
	s, _ := open(t)
	ny := zone(t, "America/New_York")
	key := addRepeating(t, s, "Take the pills", at(t, ny, "2026-03-06 20:30"),
		api.Repeat{Every: api.EveryDay, TimeZone: "America/New_York"})
	got := occurrences(t, s, ny, "2026-03-07 00:00", "2026-03-10 04:00")
	eq(t, got, []string{key + "@2026-03-07 20:30", key + "@2026-03-08 20:30", key + "@2026-03-09 20:30"})
	// The UTC hour moves by the change.
	list, err := s.Occurrences(at(t, time.UTC, "2026-03-07 00:00"), at(t, time.UTC, "2026-03-10 04:00"))
	must(t, err)
	if h := list[0].At.Hour(); h != 1 {
		t.Fatalf("before the change 20:30 is %d UTC, want 1", h)
	}
	if h := list[1].At.Hour(); h != 0 {
		t.Fatalf("after the change 20:30 is %d UTC, want 0", h)
	}
}
