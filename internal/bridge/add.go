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
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, `usage: meat-mode add "<title>" [--due TIME] [--note TEXT] [--author NAME] [--id UUID]`)
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
	}
	reply, err := hub.AddPersonal(ctx, t)
	if err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, reply.ID)
	return nil
}

// pairedHub is the hub this machine's bridge config pairs it with.
func pairedHub(env Env) (*Hub, error) {
	cfg, err := LoadConfig(env.Paths.Config)
	if err != nil {
		return nil, err
	}
	if cfg.HubURL == "" || cfg.Token == "" {
		return nil, errors.New("this machine is not paired with a hub; run `meat-mode bridge pair <url> <token>` first")
	}
	if err := CheckURL(cfg.HubURL, cfg.AllowInsecureHTTP); err != nil {
		return nil, err
	}
	return env.hub(cfg), nil
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
