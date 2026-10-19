package bridge

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// defaultDueHour is the time of day a due date without one gets, as in the
// app's quick add.
const defaultDueHour = 9

// Add runs `meat-mode add`: it puts a to-do on the hub's personal list with
// the hub URL and token of this machine's bridge config, and prints its id
// for `meat-mode edit`, `done` and `reopen`.
func Add(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	due := fs.String("due", "", "when it is due: an RFC 3339 time, YYYY-MM-DD HH:MM in the hub's time zone, or YYYY-MM-DD (9:00 there)")
	note := fs.String("note", "", "a note, added as the first comment")
	author := fs.String("author", "", "the agent's name (default: this machine's bridge name)")
	id := fs.String("id", "", "a UUID for this to-do; adding the same id again changes nothing")
	every, fromDone, until := repeatFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, `usage: meat-mode add "<title>" [--due TIME [--every RULE [--from-done] [--until DATE]]] [--note TEXT] [--author NAME] [--id UUID]`)
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(pos, " "))
	if title == "" {
		fs.Usage()
		return errors.New("add needs a title")
	}
	t := api.PersonalTodo{ID: *id, Title: title, Note: *note, Author: *author}
	if *due != "" {
		// Checked before the hub is asked for its time zone.
		if _, err := ParseDue(*due, time.UTC); err != nil {
			return err
		}
	}
	repeat, err := repeatArg(*every, *fromDone)
	if err != nil {
		return err
	}
	if given(fs, "until") {
		if repeat == nil {
			return errors.New("--until goes with --every")
		}
		if repeat.Until, err = ParseUntil(*until); err != nil {
			return err
		}
	}
	if repeat != nil && *due == "" {
		return errors.New("--every needs --due, the first time it is due")
	}

	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	if *due != "" {
		loc, err := hubZone(ctx, hub)
		if err != nil {
			return err
		}
		d, err := ParseDue(*due, loc)
		if err != nil {
			return err
		}
		t.Due = &d
		if repeat != nil {
			if repeat.TimeZone, err = zoneName(loc); err != nil {
				return err
			}
			t.Repeat = repeat
		}
	}
	reply, err := hub.AddPersonal(ctx, t)
	if err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, reply.ID)
	return nil
}

// repeatFlags adds the flags that make a to-do repeat.
func repeatFlags(fs *flag.FlagSet) (every *string, fromDone *bool, until *string) {
	every = fs.String("every", "", "how often it repeats once done: "+everyForms+"; it keeps the due time's time of day in the hub's time zone")
	fromDone = fs.Bool("from-done", false, "with --every, count the next date from the day it is done instead of from the schedule")
	until = fs.String("until", "", "the last date it repeats on, YYYY-MM-DD; the done after it closes the to-do")
	return every, fromDone, until
}

// given reports whether the flag name was set on the command line.
func given(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// repeatArg reads --every and --from-done: nil when it does not repeat.
// The time zone is left for the caller.
func repeatArg(every string, fromDone bool) (*api.Repeat, error) {
	if every == "" {
		if fromDone {
			return nil, errors.New("--from-done goes with --every")
		}
		return nil, nil
	}
	r, err := ParseEvery(every, fromDone)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// hubZone is the hub's time zone, which reads a date or time without one:
// the person who wrote it means their own, which the hub keeps rather than
// this machine, often a server in UTC. It is this machine's zone while the
// hub has none yet, or is too old to say.
func hubZone(ctx context.Context, hub *Hub) (*time.Location, error) {
	settings, err := hub.Settings(ctx)
	var herr *HTTPError
	if errors.As(err, &herr) && herr.Status == http.StatusNotFound {
		return time.Local, nil
	}
	if err != nil {
		return nil, err
	}
	if settings.TimeZone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(settings.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("the hub's time zone %q is not known on this machine: %w", settings.TimeZone, err)
	}
	return loc, nil
}

// ParseDue reads a due time: RFC 3339, or a date and time or a date alone in
// loc. A date alone is due at 9:00.
func ParseDue(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.UTC(), nil
		}
	}
	if d, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return time.Date(d.Year(), d.Month(), d.Day(), defaultDueHour, 0, 0, 0, loc).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("--due %q is not a time; use 2026-03-02T09:00:00Z, 2026-03-02 09:00 or 2026-03-02", s)
}
