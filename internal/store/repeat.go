package store

import (
	"slices"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// checkRepeat validates a repeat rule and returns it cleaned up: weekdays
// sorted, and no start, because the hub sets that from the due time.
func checkRepeat(r api.Repeat) (api.Repeat, error) {
	switch r.Every {
	case api.EveryDay, api.EveryWeek, api.EveryMonth, api.EveryYear:
	default:
		return r, invalidf("repeat.every must be day, week, month or year")
	}
	if r.Interval < 0 || r.Interval > api.MaxRepeatInterval {
		return r, invalidf("repeat.interval must be 1 to %d", api.MaxRepeatInterval)
	}
	if len(r.Weekdays) > 0 {
		if r.Every != api.EveryWeek {
			return r, invalidf("only a weekly repeat takes weekdays")
		}
		if r.FromDone {
			return r, invalidf("a repeat counted from the done day takes no weekdays")
		}
		days := slices.Clone(r.Weekdays)
		slices.Sort(days)
		for i, d := range days {
			if d < 0 || d > 6 {
				return r, invalidf("repeat.weekdays must be 0 (Sunday) to 6")
			}
			if i > 0 && days[i-1] == d {
				return r, invalidf("weekday %d is listed twice", d)
			}
		}
		r.Weekdays = days
	} else {
		r.Weekdays = nil
	}
	if err := checkTimeZone(r.TimeZone); err != nil {
		return r, invalidf("repeat: %v", err)
	}
	r.Start = nil
	return r, nil
}

// nextDue is when a repeating item is due once it is done at now: the first
// occurrence after both its due time and now, so missed ones are skipped;
// with FromDone, the done day plus the interval, at the rule's time of day.
// The rule must have passed checkRepeat and have a start.
func nextDue(r api.Repeat, due, now time.Time) time.Time {
	loc, start := ruleStart(r)
	if r.FromDone {
		y, m, d := now.In(loc).Date()
		y, m, d = addUnits(r.Every, y, m, d, interval(r))
		return wallTime(y, m, d, start, loc)
	}
	after := due
	if now.After(after) {
		after = now
	}
	return nextAfter(r, after)
}

// isOccurrence reports whether t is one of the dates r's schedule counts
// from its start. A FromDone rule has no schedule.
func isOccurrence(r api.Repeat, t time.Time) bool {
	if r.FromDone || r.Start == nil || t.Before(*r.Start) {
		return false
	}
	return nextAfter(r, t.Add(-time.Nanosecond)).Equal(t)
}

// nextAfter is the first occurrence of r after t, and not before its start.
// It works out roughly which occurrence that is and steps from a little
// before it, so an ancient start with a short interval costs no more than a
// recent one.
func nextAfter(r api.Repeat, t time.Time) time.Time {
	loc, start := ruleStart(r)
	if t.Before(start) {
		t = start.Add(-time.Nanosecond)
	}
	n := interval(r)
	sy, sm, sd := start.Date()
	ty, tm, td := t.In(loc).Date()
	if r.Every == api.EveryWeek && len(r.Weekdays) > 0 {
		// Weeks run Monday to Sunday, and every nth one counts from the
		// week of the start.
		week0 := dayNumber(sy, sm, sd) - mondayOffset(start.Weekday())
		tWeek := dayNumber(ty, tm, td) - mondayOffset(t.In(loc).Weekday())
		k := max(0, floorDiv((tWeek-week0)/7, n)-1)
		days := slices.Clone(r.Weekdays)
		slices.SortFunc(days, func(a, b int) int { return mondayOffset(time.Weekday(a)) - mondayOffset(time.Weekday(b)) })
		for ; ; k++ {
			for _, wd := range days {
				y, m, d := fromDayNumber(week0 + 7*k*n + mondayOffset(time.Weekday(wd)))
				if occ := wallTime(y, m, d, start, loc); !occ.Before(start) && occ.After(t) {
					return occ
				}
			}
		}
	}
	var k int
	switch r.Every {
	case api.EveryDay:
		k = floorDiv(dayNumber(ty, tm, td)-dayNumber(sy, sm, sd), n)
	case api.EveryWeek:
		k = floorDiv(dayNumber(ty, tm, td)-dayNumber(sy, sm, sd), 7*n)
	case api.EveryMonth:
		k = floorDiv((ty-sy)*12+int(tm-sm), n)
	case api.EveryYear:
		k = floorDiv(ty-sy, n)
	}
	for k = max(0, k-1); ; k++ {
		// Each occurrence counts from the start, so a monthly rule from the
		// 31st returns to it after a shorter month.
		y, m, d := addUnits(r.Every, sy, sm, sd, k*n)
		if occ := wallTime(y, m, d, start, loc); occ.After(t) {
			return occ
		}
	}
}

// ruleStart is r's zone and its start in that zone.
func ruleStart(r api.Repeat) (*time.Location, time.Time) {
	loc, err := time.LoadLocation(r.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	return loc, r.Start.In(loc)
}

func interval(r api.Repeat) int {
	if r.Interval < 1 {
		return 1
	}
	return r.Interval
}

// addUnits adds n days, weeks, months or years to a date. A month or year
// that lacks the day lands on its last day.
func addUnits(every string, y int, m time.Month, d, n int) (int, time.Month, int) {
	switch every {
	case api.EveryDay:
		return fromDayNumber(dayNumber(y, m, d) + n)
	case api.EveryWeek:
		return fromDayNumber(dayNumber(y, m, d) + 7*n)
	case api.EveryMonth:
		months := int(m) - 1 + n
		y += floorDiv(months, 12)
		m = time.Month(months - 12*floorDiv(months, 12) + 1)
	case api.EveryYear:
		y += n
	}
	return y, m, min(d, daysIn(y, m))
}

// wallTime is the date at the wall-clock time of start in loc. A time a
// daylight saving change skips moves on by the change (2:30 becomes 3:30);
// a time it repeats is the first of the two.
func wallTime(y int, m time.Month, d int, start time.Time, loc *time.Location) time.Time {
	h, mi, s := start.Clock()
	naive := time.Date(y, m, d, h, mi, s, start.Nanosecond(), time.UTC)
	_, before := naive.Add(-36 * time.Hour).In(loc).Zone()
	_, after := naive.Add(36 * time.Hour).In(loc).Zone()
	early := naive.Add(-time.Duration(before) * time.Second)
	late := naive.Add(-time.Duration(after) * time.Second)
	fits := func(t time.Time) bool {
		l := t.In(loc)
		ly, lm, ld := l.Date()
		lh, lmi, ls := l.Clock()
		return time.Date(ly, lm, ld, lh, lmi, ls, l.Nanosecond(), time.UTC).Equal(naive)
	}
	switch {
	case fits(early) && fits(late) && late.Before(early):
		return late
	case !fits(early) && fits(late):
		return late
	}
	// Either it fits at the earlier offset, or it is skipped and the
	// earlier offset carries it past the gap.
	return early
}

// dayNumber counts days from 1970-01-01 to a date, and fromDayNumber turns
// it back, so date arithmetic never meets a time zone.
func dayNumber(y int, m time.Month, d int) int {
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

func fromDayNumber(n int) (int, time.Month, int) {
	return time.Unix(int64(n)*86400, 0).UTC().Date()
}

func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// mondayOffset is how many days d is after Monday.
func mondayOffset(d time.Weekday) int { return (int(d) + 6) % 7 }

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}
