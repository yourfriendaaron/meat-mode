package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/beads"
)

// Timing defaults.
const (
	ActionEvery  = 5 * time.Second  // how often the bridge asks the hub for actions
	SyncEvery    = 30 * time.Second // how often it lists every synced repo
	RefreshEvery = 10 * time.Minute // full push even when nothing changed
	MaxBackoff   = 60 * time.Second
)

// Close reasons the bridge writes.
const (
	DoneReason = "Done in Meat Mode"
)

// ErrNotSynced is the result for an action on a repo this bridge does not sync.
const ErrNotSynced = "repo not synced"

// Bridge syncs beads repos with the hub.
type Bridge struct {
	Hub      *Hub
	Beads    beads.Beads
	Journal  *Journal
	Roots    []string
	Personal string
	Version  string
	Log      *slog.Logger
	Now      func() time.Time
	// Poll, when set, replaces both ActionEvery and SyncEvery. New sets it
	// from the undocumented MEAT_MODE_BRIDGE_POLL variable (a Go duration),
	// which the end-to-end tests use to avoid waiting 30 seconds per round.
	Poll time.Duration

	// afterBD runs after a bd write and before the journal's done record.
	// Tests use it to simulate a crash.
	afterBD func(api.Action) error

	name       string // from hello; empty until the hub greeted us
	bdVersion  string
	repos      map[string]*repoState
	reposSent  []api.Repo
	reposAt    time.Time
	lastFullAt time.Time
}

type repoState struct {
	Repo
	fps    map[string]string
	items  map[string]api.Item
	pushed time.Time
	primed bool
}

// errCrash stops a replay without recording anything, as a killed process
// would.
var errCrash = errors.New("simulated crash")

func (b *Bridge) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Bridge) actionEvery() time.Duration {
	if b.Poll > 0 {
		return b.Poll
	}
	return ActionEvery
}

func (b *Bridge) syncEvery() time.Duration {
	if b.Poll > 0 {
		return b.Poll
	}
	return SyncEvery
}

func (b *Bridge) log() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

// Name is the bridge's name as the hub knows it, after the first hello.
func (b *Bridge) Name() string { return b.name }

// CheckBD reads the bd version and refuses one older than beads.MinVersion.
func (b *Bridge) CheckBD(ctx context.Context) error {
	v, err := b.Beads.Version(ctx)
	if err != nil {
		return fmt.Errorf("cannot run bd: %w", err)
	}
	if err := beads.CheckVersion(v); err != nil {
		return err
	}
	b.bdVersion = v
	return nil
}

// Run checks bd, then ticks until ctx ends. Hub errors are logged and backed
// off; they never stop the bridge.
func (b *Bridge) Run(ctx context.Context) error {
	if err := b.CheckBD(ctx); err != nil {
		return err
	}
	b.log().Info("bridge started", "version", b.Version, "bd", b.bdVersion, "hub", b.Hub.URL)
	backoff := time.Duration(0)
	for {
		err := b.Tick(ctx, false)
		wait := b.actionEvery()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			backoff = min(max(time.Second, backoff*2), MaxBackoff)
			wait = backoff
			b.log().Warn("round failed; retrying", "err", err, "retry", wait)
		} else {
			backoff = 0
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// Tick runs one round: hello if needed, report repos, replay actions, then
// sync the repos that had actions, or all of them when full is set or
// SyncEvery has passed since the last full round. It returns hub errors; bd errors are logged per repo.
func (b *Bridge) Tick(ctx context.Context, full bool) error {
	if b.name == "" {
		if b.bdVersion == "" {
			if err := b.CheckBD(ctx); err != nil {
				return err
			}
		}
		r, err := b.Hub.Hello(ctx, api.Hello{Version: b.Version, BdVersion: b.bdVersion})
		if err != nil {
			return err
		}
		b.name = r.Bridge
		b.log().Info("connected to hub", "bridge", b.name)
	}
	if err := b.hubErr(b.discover(ctx)); err != nil {
		return err
	}
	touched, err := b.replay(ctx)
	if err := b.hubErr(err); err != nil {
		return err
	}
	if b.now().Sub(b.lastFullAt) >= b.syncEvery() {
		full = true
	}
	for _, name := range b.syncedNames() {
		if !full && !touched[name] {
			continue
		}
		if err := b.syncRepo(ctx, b.repos[name]); err != nil {
			if isHubErr(err) {
				return b.hubErr(err)
			}
			b.log().Warn("sync failed", "repo", name, "err", err)
		}
	}
	if full {
		b.lastFullAt = b.now()
	}
	return nil
}

// hubErr makes the next tick say hello again after a hub failure, so the hub
// hears the versions again after it restarts.
func (b *Bridge) hubErr(err error) error {
	if err != nil {
		b.name = ""
	}
	return err
}

type hubError struct{ error }

func (e hubError) Unwrap() error { return e.error }

func isHubErr(err error) bool {
	var h hubError
	return errors.As(err, &h)
}

func (b *Bridge) syncedNames() []string {
	var out []string
	for name, r := range b.repos {
		if r.Synced() {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// discover rescans the roots and reports the repo list when it changed.
func (b *Bridge) discover(ctx context.Context) error {
	found := Discover(b.Roots, b.Personal, b.log())
	next := map[string]*repoState{}
	wire := make([]api.Repo, 0, len(found))
	for _, r := range found {
		st := b.repos[r.Name]
		if st == nil || st.Dir != r.Dir || st.Synced() != r.Synced() {
			st = &repoState{fps: map[string]string{}, items: map[string]api.Item{}}
		}
		st.Repo = r
		next[r.Name] = st
		wire = append(wire, api.Repo{Name: r.Name, Personal: r.Personal, OptedIn: r.Synced()})
	}
	b.repos = next
	if slices.Equal(wire, b.reposSent) && b.now().Sub(b.reposAt) < RefreshEvery {
		return nil
	}
	if err := b.Hub.PutRepos(ctx, wire); err != nil {
		return err
	}
	b.reposSent, b.reposAt = wire, b.now()
	return nil
}

// syncRepo lists a repo's human items and pushes the full set when any
// fingerprint changed, on the first run, or after RefreshEvery.
func (b *Bridge) syncRepo(ctx context.Context, st *repoState) error {
	list, err := b.Beads.ListHuman(ctx, st.Dir, st.Personal)
	if err != nil {
		return err
	}
	fps := make(map[string]string, len(list))
	var changed []string
	for _, i := range list {
		fp := i.Fingerprint()
		fps[i.ID] = fp
		if st.fps[i.ID] != fp {
			changed = append(changed, i.ID)
		}
	}
	removed := len(st.fps) != len(fps)-countNew(changed, st.fps)
	if st.primed && len(changed) == 0 && !removed && b.now().Sub(st.pushed) < RefreshEvery {
		return nil
	}
	items := make(map[string]api.Item, len(fps))
	for id := range fps {
		if it, ok := st.items[id]; ok {
			items[id] = it
		}
	}
	for _, id := range changed {
		full, err := b.Beads.Show(ctx, st.Dir, id)
		if errors.Is(err, beads.ErrNotFound) {
			delete(fps, id) // deleted between list and show
			delete(items, id)
			continue
		}
		if err != nil {
			return err
		}
		items[id] = full.Item()
	}
	set := make([]api.Item, 0, len(items))
	for _, it := range items {
		set = append(set, it)
	}
	slices.SortFunc(set, func(a, b api.Item) int { return strings.Compare(a.ID, b.ID) })
	if err := b.Hub.PutItems(ctx, st.Name, set); err != nil {
		return hubError{err}
	}
	st.fps, st.items, st.pushed, st.primed = fps, items, b.now(), true
	return nil
}

// countNew counts ids in changed that were not known before.
func countNew(changed []string, known map[string]string) int {
	n := 0
	for _, id := range changed {
		if _, ok := known[id]; !ok {
			n++
		}
	}
	return n
}
