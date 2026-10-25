package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// List runs `meat-mode list`: it prints the to-dos of the personal list, or
// of the project --project names, one a line, sub-tasks under their
// parent. Any paired machine reads every to-do, whoever added it.
func List(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	closed := fs.Bool("closed", false, "list the closed to-dos the app still shows (done or deleted in the last 7 days) instead of the open ones")
	all := fs.Bool("all", false, "list open and closed to-dos")
	search := fs.String("search", "", "only to-dos whose title holds this text, whatever its case")
	var tags tagFlag
	fs.Var(&tags, "tag", "only to-dos with this tag, whatever its case; repeat the flag for to-dos with every tag given")
	asJSON := fs.Bool("json", false, "print the hub's JSON instead: an array of items, as docs/api.md describes")
	project := projectFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, `usage: meat-mode list [--project NAME|.] [--closed | --all] [--search TEXT] [--tag TAG]... [--json]`)
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		fs.Usage()
		return fmt.Errorf("list takes no arguments; use --search %q to find a title", strings.Join(pos, " "))
	}
	f := api.PersonalFilter{Query: strings.TrimSpace(*search), Tags: tags}
	switch {
	case *closed && *all:
		return errors.New("use --closed or --all, not both")
	case *closed:
		f.Status = "closed"
	case *all:
		f.Status = "all"
	}
	list, err := resolveProject(env, *project)
	if err != nil {
		return err
	}
	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	items, err := hub.ListPersonal(ctx, list, f)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(env.Stdout, items)
	}
	if len(items) == 0 {
		fmt.Fprintln(env.Stderr, "No to-dos match.")
		return nil
	}
	loc, err := hubZone(ctx, hub)
	if err != nil {
		return err
	}
	// Sub-tasks go under their parent when it is listed too.
	listed := map[string]bool{}
	children := map[string][]api.Item{}
	for _, it := range items {
		listed[it.ID] = true
	}
	for _, it := range items {
		if it.Parent != "" && listed[it.Parent] {
			children[it.Parent] = append(children[it.Parent], it)
		}
	}
	for _, it := range items {
		if it.Parent != "" && listed[it.Parent] {
			continue
		}
		fmt.Fprintln(env.Stdout, itemLine(it, "", loc))
		for _, sub := range children[it.ID] {
			fmt.Fprintln(env.Stdout, itemLine(sub, "  ", loc))
		}
	}
	return nil
}

// itemLine is one to-do in `meat-mode list`: its id, title, and what else
// a glance needs.
func itemLine(it api.Item, indent string, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%-8s %s", indent, it.ID, it.Title)
	if it.DueAt != nil {
		fmt.Fprintf(&b, "  due %s", it.DueAt.In(loc).Format("2006-01-02 15:04"))
	}
	if it.Repeat != nil {
		b.WriteString("  repeats")
	}
	for _, t := range it.Labels {
		fmt.Fprintf(&b, "  #%s", t)
	}
	if it.Parent != "" && indent == "" {
		fmt.Fprintf(&b, "  (sub-task of %s)", it.Parent)
	}
	if it.Status == "closed" {
		fmt.Fprintf(&b, "  [%s]", closedWord(it))
	}
	return b.String()
}

// closedWord says how a closed to-do closed.
func closedWord(it api.Item) string {
	if it.CloseReason == "Dismissed" {
		return "deleted"
	}
	return "done"
}

// Show runs `meat-mode show`: it prints one to-do of the personal list or
// a project, with its description and notes.
func Show(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	asJSON := fs.Bool("json", false, "print the hub's JSON instead: the item, as docs/api.md describes")
	project := projectFlag(fs)
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, `usage: meat-mode show <id> [--project NAME|.] [--json]`)
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	list, item, err := itemArg(fs, env, "show", pos, *project)
	if err != nil {
		return err
	}
	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	it, err := hub.PersonalItem(ctx, list, item)
	if err != nil {
		return notOnList(item, err)
	}
	if *asJSON {
		return printJSON(env.Stdout, it)
	}
	loc, err := hubZone(ctx, hub)
	if err != nil {
		return err
	}
	out := env.Stdout
	fmt.Fprintf(out, "%s  %s\n", it.ID, it.Title)
	field := func(name, v string) {
		if v != "" {
			fmt.Fprintf(out, "%-10s %s\n", name+":", v)
		}
	}
	status := "open"
	if it.Status == "closed" {
		status = closedWord(it)
	}
	field("Status", status)
	if it.DueAt != nil {
		field("Due", it.DueAt.In(loc).Format("2006-01-02 15:04 MST"))
	}
	if it.Repeat != nil {
		field("Repeats", "every "+it.Repeat.Every)
	}
	field("Priority", fmt.Sprint(it.Priority))
	field("Tags", strings.Join(it.Labels, ", "))
	field("Parent", it.Parent)
	field("Added by", it.CreatedBy)
	field("Key", it.Key)
	if d := strings.TrimSpace(it.Description); d != "" {
		fmt.Fprintf(out, "\n%s\n", d)
	}
	for _, c := range it.Comments {
		fmt.Fprintf(out, "\n%s, %s:\n%s\n", c.Author, c.CreatedAt.In(loc).Format("2006-01-02 15:04"), strings.TrimSpace(c.Text))
	}
	return nil
}

// Projects runs `meat-mode projects`: it prints the hub's projects, each
// with how many of its to-dos are open.
func Projects(ctx context.Context, args []string, env Env) error {
	fs := flag.NewFlagSet("projects", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	asJSON := fs.Bool("json", false, "print the hub's JSON instead")
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, `usage: meat-mode projects [--json]`)
		fs.PrintDefaults()
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		fs.Usage()
		return errors.New("projects takes no arguments")
	}
	hub, err := pairedHub(env)
	if err != nil {
		return err
	}
	ps, err := hub.Projects(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(env.Stdout, ps)
	}
	if len(ps) == 0 {
		fmt.Fprintln(env.Stderr, "No projects yet; `meat-mode add --project NAME` makes one.")
		return nil
	}
	for _, p := range ps {
		fmt.Fprintf(env.Stdout, "%-32s %d open\n", p.Name, p.Open)
	}
	return nil
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// notOnList explains the hub's not found for a to-do: the list holds no
// such id, or the app no longer shows it.
func notOnList(item string, err error, other ...string) error {
	var herr *HTTPError
	if !errors.As(err, &herr) || herr.Status != http.StatusNotFound {
		return err
	}
	what := fmt.Sprintf("%q", item)
	for _, o := range other {
		what += fmt.Sprintf(" or %q", o)
	}
	return fmt.Errorf("the hub has no to-do %s on this list; `meat-mode list` shows the ids (with --project for a project, --all for closed ones)", what)
}
