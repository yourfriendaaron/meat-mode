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

// Edit runs `meat-mode edit`: it changes the title, description, due time,
// repeat or priority of a personal to-do this machine added, and prints its
// id. --until and --no-until apply to the --every given with them, or else
// to the to-do's own repeat, which is sent back whole with the new end.
func Edit(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	title := fs.String("title", "", "the new title")
	desc := fs.String("description", "", `the new description; "" clears it`)
	due := fs.String("due", "", "the new due time, read as by `meat-mode add`")
	clearDue := fs.Bool("clear-due", false, "remove the due time")
	priority := fs.Int("priority", 2, "the new priority, 0 (highest) to 4")
	every, fromDone, until := repeatFlags(fs)
	noUntil := fs.Bool("no-until", false, "let its repeat go on for good")
	noRepeat := fs.Bool("no-repeat", false, "stop it repeating")
	author, id := changeFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, `usage: meat-mode edit <id> [--title TEXT] [--description TEXT] [--due TIME | --clear-due] [--every RULE [--from-done] | --no-repeat] [--until DATE | --no-until] [--priority N] [--author NAME] [--id UUID]`)
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	item, err := itemArg(fs, "edit", pos)
	if err != nil {
		return err
	}
	set := api.Edit{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title":
			set.Title = title
		case "description":
			set.Description = desc
		case "clear-due":
			set.ClearDue = *clearDue
		case "priority":
			set.Priority = priority
		case "no-repeat":
			set.ClearRepeat = *noRepeat
		}
	})
	if *due != "" && set.ClearDue {
		return errors.New("use --due or --clear-due, not both")
	}
	if set.Title != nil && strings.TrimSpace(*set.Title) == "" {
		return errors.New("--title cannot be empty")
	}
	if set.Priority != nil && (*set.Priority < 0 || *set.Priority > 4) {
		return errors.New("--priority must be 0 to 4")
	}
	if *due != "" {
		if _, err := ParseDue(*due, time.UTC); err != nil {
			return err
		}
	}
	repeat, err := repeatArg(*every, *fromDone)
	if err != nil {
		return err
	}
	if repeat != nil && set.ClearRepeat {
		return errors.New("use --every or --no-repeat, not both")
	}
	if repeat != nil && set.ClearDue {
		return errors.New("--every needs a due time; --clear-due also stops it repeating")
	}
	// newUntil is the end the repeat gets when endChanged; "" for none.
	var newUntil string
	endChanged := given(fs, "until") || *noUntil
	switch {
	case given(fs, "until") && *noUntil:
		return errors.New("use --until or --no-until, not both")
	case endChanged && set.ClearRepeat:
		return errors.New("use --until or --no-until with a repeat, not with --no-repeat")
	case endChanged && set.ClearDue:
		return errors.New("--until and --no-until need a repeat; --clear-due stops it repeating")
	case given(fs, "until"):
		if newUntil, err = ParseUntil(*until); err != nil {
			return err
		}
	}
	if repeat != nil {
		repeat.Until = newUntil
	}
	if set.Title == nil && set.Description == nil && *due == "" && !set.ClearDue && set.Priority == nil && repeat == nil && !set.ClearRepeat && !endChanged {
		fs.Usage()
		return errors.New("edit needs at least one of --title, --description, --due, --clear-due, --every, --no-repeat, --until, --no-until or --priority")
	}

	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	if *due != "" || repeat != nil {
		loc, err := hubZone(ctx, hub)
		if err != nil {
			return err
		}
		if *due != "" {
			d, err := ParseDue(*due, loc)
			if err != nil {
				return err
			}
			set.Due = &d
		}
		if repeat != nil {
			if repeat.TimeZone, err = zoneName(loc); err != nil {
				return err
			}
			set.Repeat = repeat
		}
	}
	if endChanged && repeat == nil {
		// The hub takes a repeat whole, so the to-do's own goes back with
		// the new end.
		it, err := hub.PersonalItem(ctx, item)
		if err != nil {
			return notOurs(item, err)
		}
		if it.Repeat == nil {
			return fmt.Errorf("%s does not repeat; give --every with --until", item)
		}
		r := *it.Repeat
		r.Start, r.Until = nil, newUntil
		set.Repeat = &r
	}
	return change(ctx, hub, env, item, api.PersonalChange{ID: *id, Kind: api.KindEdit,
		Payload: api.Payload{Set: &set}, Author: *author})
}

// Done runs `meat-mode done`: it closes a personal to-do this machine added.
func Done(ctx context.Context, args []string, env Env) error {
	return closeOrReopen(ctx, api.KindDone, args, env)
}

// Reopen runs `meat-mode reopen`: it opens a closed personal to-do this
// machine added again.
func Reopen(ctx context.Context, args []string, env Env) error {
	return closeOrReopen(ctx, api.KindReopen, args, env)
}

func closeOrReopen(ctx context.Context, kind string, args []string, env Env) error {
	fs := flag.NewFlagSet(kind, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	author, id := changeFlags(fs)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "usage: meat-mode %s <id> [--author NAME] [--id UUID]\n", kind)
		fmt.Fprintf(env.Stderr, "Marks a to-do this machine added as %s.\n", map[string]string{api.KindDone: "done", api.KindReopen: "open"}[kind])
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	item, err := itemArg(fs, kind, pos)
	if err != nil {
		return err
	}
	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	return change(ctx, hub, env, item, api.PersonalChange{ID: *id, Kind: kind, Author: *author})
}

// changeFlags adds the flags every change takes.
func changeFlags(fs *flag.FlagSet) (author, id *string) {
	author = fs.String("author", "", "the agent's name (default: this machine's bridge name)")
	id = fs.String("id", "", "a UUID for this change; sending the same id again changes nothing")
	return author, id
}

// itemArg reads the one item id a change takes. The item's key
// (hub/personal/<id>) works too.
func itemArg(fs *flag.FlagSet, cmd string, pos []string) (string, error) {
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		fs.Usage()
		return "", fmt.Errorf("%s needs the id of one to-do, as `meat-mode add` printed it", cmd)
	}
	return strings.TrimPrefix(strings.TrimSpace(pos[0]), "hub/personal/"), nil
}

// change posts c and prints the item's id.
func change(ctx context.Context, hub *Hub, env Env, item string, c api.PersonalChange) error {
	if _, err := hub.ChangePersonal(ctx, item, c); err != nil {
		return notOurs(item, err)
	}
	fmt.Fprintln(env.Stdout, item)
	return nil
}

// notOurs explains the hub's not found for a to-do: it is not one this
// machine added.
func notOurs(item string, err error) error {
	var herr *HTTPError
	if errors.As(err, &herr) && herr.Status == http.StatusNotFound {
		return fmt.Errorf("the hub has no to-do %q that this machine added; an agent can change only the to-dos its own machine added", item)
	}
	return err
}
