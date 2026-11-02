package view

import (
	"fmt"
	"time"
)

// Local-time date helpers, ported from web/src/dates.ts. Each takes the
// moment it works from; the Location is now's.

var (
	dayNames   = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	monthNames = [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// startOfDay is midnight of t's date.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// addDays moves t by n calendar days, keeping its wall-clock time.
func addDays(t time.Time, n int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day()+n, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// at is t's date at hour:minute.
func at(t time.Time, hour, minute int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, t.Location())
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

// nextMonday is the start of the next Monday, strictly after today's date.
func nextMonday(now time.Time) time.Time {
	ahead := (8 - int(now.Weekday())) % 7
	if ahead == 0 {
		ahead = 7
	}
	return startOfDay(addDays(now, ahead))
}

// fmtTime is "9:00", "17:30".
func fmtTime(t time.Time) string { return fmt.Sprintf("%d:%02d", t.Hour(), t.Minute()) }

// fmtDay is "Fri, Oct 23".
func fmtDay(t time.Time) string {
	return fmt.Sprintf("%s, %s %d", dayNames[t.Weekday()], monthNames[t.Month()-1], t.Day())
}

// fmtWhen is a short label for a moment near now: "14:30", "Tomorrow 9:00",
// "Yesterday 9:00", "Fri, Oct 23".
func fmtWhen(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return fmtTime(t)
	case sameDay(t, addDays(now, 1)):
		return "Tomorrow " + fmtTime(t)
	case sameDay(t, addDays(now, -1)):
		return "Yesterday " + fmtTime(t)
	}
	return fmtDay(t)
}

// fmtWhenAt is fmtWhen with the time on every day.
func fmtWhenAt(t, now time.Time) string {
	w := fmtWhen(t, now)
	for _, r := range w {
		if r == ':' {
			return w
		}
	}
	return w + " " + fmtTime(t)
}

// fmtFull is a moment with its date and time and no reference to now, for
// text that leaves the screen: "Mon, Nov 2 12:30".
func fmtFull(t time.Time) string { return fmtDay(t) + " " + fmtTime(t) }

// fmtOverdue is "35m overdue", "3h overdue", "2d overdue".
func fmtOverdue(due, now time.Time) string {
	mins := max(1, int(now.Sub(due)/time.Minute))
	if mins < 60 {
		return fmt.Sprintf("%dm overdue", mins)
	}
	if hours := mins / 60; hours < 24 {
		return fmt.Sprintf("%dh overdue", hours)
	}
	return fmt.Sprintf("%dd overdue", mins/60/24)
}

// fmtAgo is "just now", "5m ago", "3h ago", "2d ago".
func fmtAgo(t, now time.Time) string {
	mins := int(now.Sub(t) / time.Minute)
	switch {
	case mins < 1:
		return "just now"
	case mins < 60:
		return fmt.Sprintf("%dm ago", mins)
	case mins < 24*60:
		return fmt.Sprintf("%dh ago", mins/60)
	}
	return fmt.Sprintf("%dd ago", mins/60/24)
}

// snoozeOption is one preset of the snooze menu.
type snoozeOption struct {
	label, hint string
	until       time.Time
}

// snoozeOptions are the app's preset snooze choices for now, in display
// order (web/src/snooze.ts). This evening is left out from 18:00.
func snoozeOptions(now time.Time) []snoozeOption {
	var opts []snoozeOption
	inHour := now.Add(time.Hour).Truncate(time.Minute)
	opts = append(opts, snoozeOption{"In 1 hour", fmtTime(inHour), inHour})
	if evening := at(now, 18, 0); now.Before(evening) {
		opts = append(opts, snoozeOption{"This evening", "18:00", evening})
	}
	opts = append(opts, snoozeOption{"Tomorrow", "9:00", at(addDays(now, 1), 9, 0)})
	sat := startOfDay(addDays(now, (6-int(now.Weekday())+7)%7))
	if !at(sat, 10, 0).After(now) {
		sat = addDays(sat, 7)
	}
	opts = append(opts, snoozeOption{"This weekend", "Sat 10:00", at(sat, 10, 0)})
	opts = append(opts, snoozeOption{"Next week", "Mon 9:00", at(nextMonday(now), 9, 0)})
	month := time.Date(now.Year(), now.Month()+1, now.Day(), 9, 0, 0, 0, now.Location())
	opts = append(opts, snoozeOption{"In a month", fmtDay(month), month})
	return opts
}
