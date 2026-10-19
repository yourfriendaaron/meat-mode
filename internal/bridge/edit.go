package bridge

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Edit runs `meat-mode edit`: it changes the title, description, due time,
// repeat or priority of a to-do this machine added to the personal list or
// a project, and prints its
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
	author, id, project := changeFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, `usage: meat-mode edit <id> [--project NAME|.] [--title TEXT] [--description TEXT] [--due TIME | --clear-due] [--every RULE [--from-done] | --no-repeat] [--until DATE | --no-until] [--priority N] [--author NAME] [--id UUID]`)
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	list, item, err := itemArg(fs, env, "edit", pos, *project)
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
	if set.Title != nil {
		if _, err := api.CheckTitle(*set.Title); err != nil {
			return fmt.Errorf("--title: %w", err)
		}
	}
	if set.Priority != nil {
		if err := api.CheckPriority(*set.Priority); err != nil {
			return fmt.Errorf("--priority: %w", err)
		}
	}
	w, err := parseWhen(fs, *due, *every, *fromDone, *until)
	if err != nil {
		return err
	}
	if w.repeat != nil && set.ClearRepeat {
		return errors.New("use --every or --no-repeat, not both")
	}
	if w.repeat != nil && set.ClearDue {
		return errors.New("--every needs a due time; --clear-due also stops it repeating")
	}
	endChanged := w.untilGiven || *noUntil
	switch {
	case w.untilGiven && *noUntil:
		return errors.New("use --until or --no-until, not both")
	case endChanged && set.ClearRepeat:
		return errors.New("use --until or --no-until with a repeat, not with --no-repeat")
	case endChanged && set.ClearDue:
		return errors.New("--until and --no-until need a repeat; --clear-due stops it repeating")
	}
	if set.Title == nil && set.Description == nil && w.due == "" && !set.ClearDue && set.Priority == nil && w.repeat == nil && !set.ClearRepeat && !endChanged {
		fs.Usage()
		return errors.New("edit needs at least one of --title, --description, --due, --clear-due, --every, --no-repeat, --until, --no-until or --priority")
	}

	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	if set.Due, set.Repeat, err = w.resolve(ctx, hub); err != nil {
		return err
	}
	if endChanged && w.repeat == nil {
		// The hub takes a repeat whole, so the to-do's own goes back with
		// the new end.
		it, err := hub.PersonalItem(ctx, list, item)
		if err != nil {
			return notOurs(item, err)
		}
		if it.Repeat == nil {
			return fmt.Errorf("%s does not repeat; give --every with --until", item)
		}
		r := *it.Repeat
		r.Start, r.Until = nil, w.until
		set.Repeat = &r
	}
	return change(ctx, hub, env, list, item, api.PersonalChange{ID: *id, Kind: api.KindEdit,
		Payload: api.Payload{Set: &set}, Author: *author})
}

// Done runs `meat-mode done`: it closes a to-do this machine added.
func Done(ctx context.Context, args []string, env Env) error {
	return closeOrReopen(ctx, api.KindDone, args, env)
}

// Reopen runs `meat-mode reopen`: it opens a closed to-do this machine
// added again.
func Reopen(ctx context.Context, args []string, env Env) error {
	return closeOrReopen(ctx, api.KindReopen, args, env)
}

func closeOrReopen(ctx context.Context, kind string, args []string, env Env) error {
	fs := flag.NewFlagSet(kind, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	author, id, project := changeFlags(fs)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "usage: meat-mode %s <id> [--project NAME|.] [--author NAME] [--id UUID]\n", kind)
		fmt.Fprintf(env.Stderr, "Marks a to-do this machine added as %s.\n", map[string]string{api.KindDone: "done", api.KindReopen: "open"}[kind])
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	list, item, err := itemArg(fs, env, kind, pos, *project)
	if err != nil {
		return err
	}
	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	return change(ctx, hub, env, list, item, api.PersonalChange{ID: *id, Kind: kind, Author: *author})
}

// changeFlags adds the flags every change takes.
func changeFlags(fs *flag.FlagSet) (author, id, project *string) {
	author = fs.String("author", "", "the agent's name (default: this machine's bridge name)")
	id = fs.String("id", "", "a UUID for this change; sending the same id again changes nothing")
	return author, id, projectFlag(fs)
}

// itemArg reads the one item id a change takes, and the list it is in:
// the personal list, or the project --project names. The item's key
// (hub/personal/<id>, hub/<project>/<id>) works too, and names its list.
func itemArg(fs *flag.FlagSet, env Env, cmd string, pos []string, project string) (list, item string, err error) {
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		fs.Usage()
		return "", "", fmt.Errorf("%s needs the id of one to-do, as `meat-mode add` printed it", cmd)
	}
	if list, err = resolveProject(env, project); err != nil {
		return "", "", err
	}
	item = strings.TrimSpace(pos[0])
	if rest, ok := strings.CutPrefix(item, "hub/"); ok && strings.Contains(rest, "/") {
		keyList := rest[:strings.LastIndex(rest, "/")]
		item = rest[len(keyList)+1:]
		if keyList == "personal" {
			keyList = ""
		}
		if given(fs, "project") && keyList != list {
			return "", "", fmt.Errorf("the key %s is not in the project --project names", pos[0])
		}
		list = keyList
	}
	return list, item, nil
}

// change posts c and prints the item's id.
func change(ctx context.Context, hub *Hub, env Env, list, item string, c api.PersonalChange) error {
	if _, err := hub.ChangePersonal(ctx, list, item, c); err != nil {
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
