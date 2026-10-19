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
	title, err := api.CheckTitle(strings.Join(pos, " "))
	if title == "" {
		fs.Usage()
		return errors.New("add needs a title")
	}
	if err != nil {
		return err
	}
	t := api.PersonalTodo{ID: *id, Title: title, Note: *note, Author: *author}
	w, err := parseWhen(fs, *due, *every, *fromDone, *until)
	if err != nil {
		return err
	}
	if w.untilGiven && w.repeat == nil {
		return errors.New("--until goes with --every")
	}
	if w.repeat != nil && w.due == "" {
		return errors.New("--every needs --due, the first time it is due")
	}

	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	if t.Due, t.Repeat, err = w.resolve(ctx, hub); err != nil {
		return err
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

// when is the due time and repeat rule a command was given, checked as far
// as this machine can before the hub's time zone reads them.
type when struct {
	due        string      // --due as given; "" for none
	repeat     *api.Repeat // --every and --from-done, ending on until; nil when it does not repeat
	until      string      // --until; "" for none
	untilGiven bool
}

// parseWhen reads --due, --every, --from-done and --until. --due is read in
// UTC here only to check it; resolve reads it in the hub's zone.
func parseWhen(fs *flag.FlagSet, due, every string, fromDone bool, until string) (when, error) {
	w := when{due: due, untilGiven: given(fs, "until")}
	if due != "" {
		if _, err := ParseDue(due, time.UTC); err != nil {
			return w, err
		}
	}
	if every == "" {
		if fromDone {
			return w, errors.New("--from-done goes with --every")
		}
	} else {
		r, err := ParseEvery(every, fromDone)
		if err != nil {
			return w, err
		}
		w.repeat = &r
	}
	if w.untilGiven {
		var err error
		if w.until, err = ParseUntil(until); err != nil {
			return w, err
		}
	}
	if w.repeat != nil {
		w.repeat.Until = w.until
	}
	return w, nil
}

// resolve reads w in the hub's time zone: the due time, and the repeat rule
// keeping that zone. It asks the hub only when w has either.
func (w when) resolve(ctx context.Context, hub *Hub) (*time.Time, *api.Repeat, error) {
	if w.due == "" && w.repeat == nil {
		return nil, nil, nil
	}
	loc, err := hubZone(ctx, hub)
	if err != nil {
		return nil, nil, err
	}
	var due *time.Time
	if w.due != "" {
		d, err := ParseDue(w.due, loc)
		if err != nil {
			return nil, nil, err
		}
		due = &d
	}
	var repeat *api.Repeat
	if w.repeat != nil {
		r := *w.repeat
		if r.TimeZone, err = zoneName(loc); err != nil {
			return nil, nil, err
		}
		repeat = &r
	}
	return due, repeat, nil
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
