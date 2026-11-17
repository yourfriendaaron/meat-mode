package bridge

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Epics timing (docs/api.md, Epics).
const (
	EpicsEvery   = time.Minute // how often the bridge reads a repo for epics, at most
	EpicsBackoff = time.Hour   // how long it sends no epics to a hub that has no epics route
	// EpicCommentReads is how many rows' comments the bridge reads in one
	// pass; the rest wait for the next.
	EpicCommentReads = 50
)

const epicsUsage = "usage: meat-mode bridge epics [on|off <repo>]"

// epicsOf is the tracker's epics reader, or nil for a tracker that cannot
// show epics.
func epicsOf(t tracker.Tracker) tracker.Epics {
	e, _ := t.(tracker.Epics)
	return e
}

// cannotShowEpics is the refusal for switching epics on in a repo whose
// tracker cannot show them.
func cannotShowEpics(t tracker.Tracker) error {
	return errors.New(t.Info().Title + " cannot show epics")
}

// epicsCommand runs `meat-mode bridge epics`: it lists the repos this bridge
// finds whose tracker can show epics, with their epics switch, or switches
// one on or off. The app's switch in Settings sets the same map.
func epicsCommand(args []string, env Env) error {
	s, err := LoadSettings(env)
	if err != nil {
		return err
	}
	repos, err := s.Repos()
	if err != nil {
		return err
	}
	return switchEpics(args, env, repos, s.Config.Epics)
}

func switchEpics(args []string, env Env, repos []Repo, switches map[string]bool) error {
	switch {
	case len(args) == 0:
		listEpics(env.Stdout, repos, switches)
		return nil
	case len(args) == 2 && (args[0] == "on" || args[0] == "off"):
		r, err := findRepo(repos, args[1], env.Paths.Home, "epics")
		if err != nil {
			return err
		}
		on := args[0] == "on"
		if on && epicsOf(r.Tracker) == nil {
			return cannotShowEpics(r.Tracker)
		}
		if err := SaveEpics(env.Paths.Config, r.Name, on); err != nil {
			return err
		}
		if on {
			fmt.Fprintf(env.Stdout, "The epics of %s show in the app.\n", r.Name)
		} else {
			fmt.Fprintf(env.Stdout, "The epics of %s no longer show in the app.\n", r.Name)
		}
		return nil
	}
	fmt.Fprintln(env.Stderr, epicsUsage)
	fmt.Fprintln(env.Stderr, "With no arguments it lists the repos this bridge finds whose tracker can show epics. <repo> is a repo's name as listed, or the path of its folder (. inside it).")
	return errors.New(epicsUsage)
}

func listEpics(out io.Writer, repos []Repo, switches map[string]bool) {
	repos = slices.DeleteFunc(slices.Clone(repos), func(r Repo) bool { return epicsOf(r.Tracker) == nil })
	if len(repos) == 0 {
		fmt.Fprintln(out, "This bridge finds no repos whose tracker can show epics.")
		return
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	for _, r := range repos {
		state := "off"
		if switches[r.Name] {
			state = "on"
		}
		fmt.Fprintf(out, "%-24s epics %-3s %s\n", r.Name, state, r.Dir)
	}
}

// epicsOn reports whether the bridge sends the repo's epics: its switch is
// on and its tracker can show them.
func (b *Bridge) epicsOn(r Repo) bool {
	return b.Epics[r.Name] && epicsOf(r.Tracker) != nil
}

// epicState is what the epics pass knows of one repo.
type epicState = passRepo[epicMore]

// epicMore is what the epics pass keeps of a repo beyond a hubPass.
type epicMore struct {
	sentLen int // rows in the last set the hub took
	// emptyOnce: the last read came back empty while the last set sent was
	// not, and was held back.
	emptyOnce bool
	// comments is what the bridge has read of each row's comments, by id,
	// and the count and time of the row it read them for.
	comments map[string]epicComments
}

// epicComments is one row's comments as read, with the CommentCount and
// UpdatedAt the row had then.
type epicComments struct {
	count     int
	updatedAt time.Time
	comments  []api.Comment
}

// syncEpics is the epics pass: for each repo with epics on it reads the
// rows at most once per EpicsEvery and sends the outline when it differs
// from the last set sent, or after RefreshEvery. It runs after discover and
// replay, so the hub knows each repo it sends and a switch-off applied this
// tick sends nothing. The hub's reply is handled by hubPass.settle.
func (b *Bridge) syncEpics(ctx context.Context) error {
	p := &b.epics
	p.what, p.backoff = "epics", EpicsBackoff
	names := make([]string, 0, len(b.repos))
	for name, st := range b.repos {
		if b.epicsOn(st.Repo) {
			names = append(names, name)
		}
	}
	if !p.begin(names, b.now()) {
		return nil
	}
	slices.Sort(names)
	for _, name := range names {
		r := b.repos[name].Repo
		es, due := p.due(name, r, b.now(), b.every(EpicsEvery))
		if !due {
			continue
		}
		rows, err := epicsOf(r.Tracker).EpicRows(ctx, r.Dir)
		if err != nil {
			b.log().Warn("cannot read epics", "repo", name, "err", err)
			continue
		}
		rows = tracker.Outline(rows)
		b.fillEpicComments(ctx, name, es, rows)
		if len(rows) == 0 && es.more.sentLen > 0 && !es.more.emptyOnce {
			// A tracker can answer an empty list by mistake; wait for the
			// next read to say so too.
			es.more.emptyOnce = true
			continue
		}
		es.more.emptyOnce = false
		sum := epicsHash(rows)
		if es.unchanged(sum, b.now(), RefreshEvery) {
			continue
		}
		err = b.Hub.PutEpics(ctx, name, rows)
		if err == nil {
			es.more.sentLen = len(rows)
		}
		if stop, err := p.settle(b, name, es, sum, err); stop {
			return err
		}
	}
	return nil
}

// fillEpicComments gives the rows with comments their text when the tracker
// can read it. A row whose count and UpdatedAt match what the cache holds
// takes the cached comments; the others are misses, and at most
// EpicCommentReads of them, in the order Budget spends in, are read in one
// call. The rest go without comments this pass. The cache drops ids that are
// no longer rows. A failed read is logged and leaves the cache as it was;
// misses then take whatever the cache holds. It ends by applying the
// budget again, now that rows carry comments.
func (b *Bridge) fillEpicComments(ctx context.Context, name string, es *epicState, rows []api.EpicRow) {
	ec, ok := es.tracker.(tracker.EpicComments)
	if !ok {
		return
	}
	if es.more.comments == nil {
		es.more.comments = map[string]epicComments{}
	}
	cache := es.more.comments
	inRows := make(map[string]bool, len(rows))
	var misses []int
	for i := range rows {
		r := &rows[i]
		inRows[r.ID] = true
		if r.CommentCount <= 0 {
			continue
		}
		if c, ok := cache[r.ID]; ok && c.count == r.CommentCount && c.updatedAt.Equal(r.UpdatedAt) {
			r.Comments = c.comments
			continue
		}
		misses = append(misses, i)
	}
	for id := range cache {
		if !inRows[id] {
			delete(cache, id)
		}
	}
	rank := func(r api.EpicRow) int {
		switch {
		case r.Epic:
			return 0
		case r.Status != tracker.StatusClosed:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(misses, func(i, j int) int {
		return cmp.Or(cmp.Compare(rank(rows[i]), rank(rows[j])), strings.Compare(rows[i].ID, rows[j].ID))
	})
	misses = misses[:min(len(misses), EpicCommentReads)]
	if len(misses) > 0 {
		ids := make([]string, len(misses))
		for k, i := range misses {
			ids[k] = rows[i].ID
		}
		got, err := ec.EpicComments(ctx, es.dir, ids)
		if err != nil {
			b.log().Warn("cannot read epic comments", "repo", name, "err", err)
		}
		for _, i := range misses {
			r := &rows[i]
			if err != nil {
				if c, ok := cache[r.ID]; ok {
					r.Comments = c.comments
				}
				continue
			}
			if cs, ok := got[r.ID]; ok {
				cache[r.ID] = epicComments{r.CommentCount, r.UpdatedAt, cs}
				r.Comments = cs
			}
		}
	}
	tracker.Budget(rows)
}

// epicsHash identifies a set of rows, so an unchanged set is not sent again.
func epicsHash(rows []api.EpicRow) string {
	b, _ := json.Marshal(rows)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
