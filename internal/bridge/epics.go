package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Epics timing (docs/api.md, Epics).
const (
	EpicsEvery   = time.Minute // how often the bridge reads a repo for epics, at most
	EpicsBackoff = time.Hour   // how long it sends no epics to a hub that has no epics route
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

// reloadEpics rereads the epics map from the config file. A failed read
// keeps the map the bridge has.
func (b *Bridge) reloadEpics() {
	if b.LoadEpics == nil {
		return
	}
	m, err := b.LoadEpics()
	if err != nil {
		b.log().Warn("cannot reread the epics switches; keeping the last ones", "err", err)
		return
	}
	b.Epics = m
}

// epicState is what the epics pass knows of one repo.
type epicState struct {
	dir     string
	tracker tracker.Tracker
	readAt  time.Time // the last read
	sent    string    // hash of the last set the hub took; "" before one
	sentAt  time.Time
	sentLen int
	// emptyOnce: the last read came back empty while the last set sent was
	// not, and was held back.
	emptyOnce bool
	// refused is the hash of the last set the hub refused (400 or 413); it
	// is not sent again until it changes.
	refused string
}

func (b *Bridge) epicsEvery() time.Duration {
	if b.Poll > 0 {
		return b.Poll
	}
	return EpicsEvery
}

// syncEpics is the epics pass: for each repo with epics on it reads the
// rows at most once per EpicsEvery and sends the outline when it differs
// from the last set sent, or after RefreshEvery. It runs after discover and
// replay, so the hub knows each repo it sends and a switch-off applied this
// tick sends nothing. A 404, 409, 400 or 413 from the hub stops nothing
// else; it returns other hub errors as items do.
func (b *Bridge) syncEpics(ctx context.Context) error {
	if b.epicStates == nil {
		b.epicStates = map[string]*epicState{}
	}
	on := map[string]bool{}
	names := make([]string, 0, len(b.repos))
	for name, st := range b.repos {
		if b.epicsOn(st.Repo) {
			on[name] = true
			names = append(names, name)
		}
	}
	for name := range b.epicStates {
		if !on[name] {
			delete(b.epicStates, name) // switched off or gone
		}
	}
	if b.now().Before(b.epicsOffUntil) {
		return nil
	}
	slices.Sort(names)
	for _, name := range names {
		r := b.repos[name].Repo
		es := b.epicStates[name]
		if es == nil || es.dir != r.Dir || es.tracker != r.Tracker {
			es = &epicState{dir: r.Dir, tracker: r.Tracker}
			b.epicStates[name] = es
		}
		if !es.readAt.IsZero() && b.now().Sub(es.readAt) < b.epicsEvery() {
			continue
		}
		es.readAt = b.now()
		rows, err := epicsOf(r.Tracker).EpicRows(ctx, r.Dir)
		if err != nil {
			b.log().Warn("cannot read epics", "repo", name, "err", err)
			continue
		}
		rows = tracker.Outline(rows)
		if len(rows) == 0 && es.sentLen > 0 && !es.emptyOnce {
			// A tracker can answer an empty list by mistake; wait for the
			// next read to say so too.
			es.emptyOnce = true
			continue
		}
		es.emptyOnce = false
		sum := epicsHash(rows)
		if sum == es.refused || (sum == es.sent && b.now().Sub(es.sentAt) < RefreshEvery) {
			continue
		}
		err = b.Hub.PutEpics(ctx, name, rows)
		var h *HTTPError
		switch {
		case err == nil:
			es.sent, es.sentAt, es.sentLen, es.refused = sum, b.now(), len(rows), ""
		case errors.As(err, &h) && h.Status == http.StatusNotFound:
			if !b.epicsGoneLogged {
				b.log().Warn("the hub does not take epics; sending none to it for an hour", "err", err)
				b.epicsGoneLogged = true
			}
			b.epicsOffUntil = b.now().Add(EpicsBackoff)
			return nil
		case errors.As(err, &h) && h.Status == http.StatusConflict:
			// The hub's repo list is not ours; report it again next tick.
			b.log().Warn("the hub does not hold this repo with epics on; reporting the repos again", "repo", name, "err", err)
			b.reposSent, es.sent = nil, ""
		case refusedSet(err):
			b.log().Warn("the hub refused this repo's epics; sending them again once they change", "repo", name, "err", err)
			es.sent, es.refused = "", sum
		default:
			return hubError{err}
		}
	}
	return nil
}

// epicsHash identifies a set of rows, so an unchanged set is not sent again.
func epicsHash(rows []api.EpicRow) string {
	b, _ := json.Marshal(rows)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
