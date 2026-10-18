package bridge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

var weekdayNames = map[string]int{
	"sun": 0, "sunday": 0,
	"mon": 1, "monday": 1,
	"tue": 2, "tues": 2, "tuesday": 2,
	"wed": 3, "wednesday": 3,
	"thu": 4, "thur": 4, "thurs": 4, "thursday": 4,
	"fri": 5, "friday": 5,
	"sat": 6, "saturday": 6,
}

// ParseEvery reads --every into a repeat rule without its time zone.
func ParseEvery(s string, fromDone bool) (api.Repeat, error) {
	bad := fmt.Errorf("--every %q is not a repeat; use %s", s, everyForms)
	spec := strings.ToLower(strings.TrimSpace(s))
	r := api.Repeat{Every: api.EveryWeek, FromDone: fromDone}
	unit, list, on := strings.Cut(spec, " on ")
	switch {
	case !on && (spec == "weekday" || spec == "weekdays"):
		r.Weekdays = []int{1, 2, 3, 4, 5}
	case !on && isDayList(spec):
		r.Weekdays = dayList(spec)
	default:
		fields := strings.Fields(unit)
		if len(fields) == 2 {
			n, err := strconv.Atoi(fields[0])
			if err != nil {
				return r, bad
			}
			if n < 1 || n > api.MaxRepeatInterval {
				return r, fmt.Errorf("--every repeats every 1 to %d days, weeks, months or years", api.MaxRepeatInterval)
			}
			if n > 1 {
				r.Interval = n
			}
			fields = fields[1:]
		}
		if len(fields) != 1 {
			return r, bad
		}
		r.Every = strings.TrimSuffix(fields[0], "s")
		switch r.Every {
		case api.EveryDay, api.EveryWeek, api.EveryMonth, api.EveryYear:
		default:
			return r, bad
		}
		if on {
			if r.Every != api.EveryWeek || !isDayList(list) {
				return r, bad
			}
			r.Weekdays = dayList(list)
		}
	}
	if fromDone && len(r.Weekdays) > 0 {
		return r, errors.New("--from-done counts from the day it is done, so it takes no days of the week")
	}
	return r, nil
}

// everyForms lists what --every takes.
const everyForms = `day, week, month, year, "3 days", "2 weeks", weekday (Monday to Friday), mon,thu or "2 weeks on mon,thu"`

// isDayList reports whether s is a comma list of days of the week.
func isDayList(s string) bool {
	for _, name := range strings.Split(s, ",") {
		if _, ok := weekdayNames[strings.TrimSpace(name)]; !ok {
			return false
		}
	}
	return true
}

// dayList reads a list isDayList accepted, each day once.
func dayList(s string) []int {
	var days []int
	for _, name := range strings.Split(s, ",") {
		if d := weekdayNames[strings.TrimSpace(name)]; !slices.Contains(days, d) {
			days = append(days, d)
		}
	}
	return days
}

// zoneName is the IANA name of a zone hubZone returned. This machine's
// zone has none in Go, so it comes from TZ or /etc/localtime.
func zoneName(loc *time.Location) (string, error) {
	if name := loc.String(); name != "Local" {
		return name, nil
	}
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil && !filepath.IsAbs(tz) {
			return tz, nil
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(link, "zoneinfo/"); ok {
			if _, err := time.LoadLocation(name); err == nil {
				return name, nil
			}
		}
	}
	return "", errors.New("--every needs a time zone: set one in Meat Mode's settings, or TZ on this machine")
}
