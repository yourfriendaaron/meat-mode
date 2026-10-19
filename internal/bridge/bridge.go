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
	ScanEvery    = 5 * time.Minute  // how often it walks the roots for repos
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
	Hub     *Hub
	Beads   beads.Beads
	Journal *Journal
	Roots   []string
	Home    string // repo keys are relative to it; empty in tests that need no keys
	// Switches are the app's sync switches by repo name, and SaveSwitch
	// records one (in bridge.json). A nil SaveSwitch refuses switches.
	Switches   map[string]bool
	SaveSwitch func(repo string, on bool) error
	// Agents are the app's agents switches by repo name, and SaveAgents
	// records one (in bridge.json). A nil SaveAgents refuses them.
	Agents     map[string]bool
	SaveAgents func(repo string, on bool) error
	// LoadAgents, when set, rereads the agents map (from bridge.json) before
	// each repo report and before each ask, so a change made with `meat-mode
	// bridge agents` while the bridge runs takes effect at once.
	LoadAgents func() (map[string]bool, error)
	// Runner runs agents for asks; nil on a bridge that runs none.
	Runner  *AgentRunner
	Version string
	Log     *slog.Logger
	Now     func() time.Time
	// Poll, when set, replaces ActionEvery, SyncEvery and ScanEvery. New sets it
	// from the undocumented MEAT_MODE_BRIDGE_POLL variable (a Go duration),
	// which the end-to-end tests use to avoid waiting 30 seconds per round.
	Poll time.Duration

	// afterBD runs after a bd write and before the journal's done record.
	// Tests use it to simulate a crash.
	afterBD func(api.Action) error

	name       string // from hello; empty until the hub greeted us
	bdVersion  string
	repos      map[string]*repoState
	found      []Repo // the last scan
	scannedAt  time.Time
	reposSent  []api.Repo
	reposAt    time.Time
	lastFullAt time.Time
	runsAt     time.Time // when the run list was last sent
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

func (b *Bridge) scanEvery() time.Duration {
	if b.Poll > 0 {
		return b.Poll
	}
	return ScanEvery
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
// off; they never stop the bridge. Between ticks it sends the run list soon
// after a run changes. When ctx ends it ends every live agent run first.
func (b *Bridge) Run(ctx context.Context) error {
	if err := b.CheckBD(ctx); err != nil {
		return err
	}
	b.log().Info("bridge started", "version", b.Version, "bd", b.bdVersion, "hub", b.Hub.URL)
	defer b.stopRuns()
	var changed <-chan struct{}
	if b.Runner != nil {
		changed = b.Runner.Changed()
	}
	backoff := time.Duration(0)
	next := time.Now()
	for {
		if !time.Now().Before(next) {
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
			next = time.Now().Add(wait)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Until(next)):
		case <-changed:
			b.flushRuns(ctx)
		}
	}
}

// flushRuns syncs the repos a run end changed and sends the run list, at
// most once per RunsEvery. Errors wait for the next tick.
func (b *Bridge) flushRuns(ctx context.Context) {
	if wait := RunsEvery - b.now().Sub(b.runsAt); wait > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
	if b.name == "" {
		return // not connected; the next tick sends everything
	}
	for _, name := range b.Runner.TakeTouched() {
		if st := b.repos[name]; st != nil && st.Synced() {
			if err := b.syncRepo(ctx, st); err != nil {
				b.log().Warn("sync failed", "repo", name, "err", err)
			}
		}
	}
	if err := b.pushRuns(ctx, false); err != nil {
		b.log().Warn("cannot send the run list; retrying with the next round", "err", err)
	}
}

// pushRuns sends the run list when it changed and RunsEvery has passed, or
// always when force is set.
func (b *Bridge) pushRuns(ctx context.Context, force bool) error {
	if b.Runner == nil {
		return nil
	}
	if !force && (!b.Runner.Dirty() || b.now().Sub(b.runsAt) < RunsEvery) {
		return nil
	}
	return b.putRuns(ctx, b.Runner.Reports())
}

// pushApplied sends the run list before a's result, naming a on the run of
// beadID, so the hub does not count a both in that run and as pending.
func (b *Bridge) pushApplied(ctx context.Context, a api.Action, beadID string) error {
	if b.Runner == nil {
		return nil
	}
	reports := b.Runner.Reports()
	for i := range reports {
		if reports[i].Repo == a.Repo && reports[i].BeadID == beadID {
			reports[i].Applying = a.ID
		}
	}
	return b.putRuns(ctx, reports)
}

func (b *Bridge) putRuns(ctx context.Context, reports []api.RunReport) error {
	if err := b.Hub.PutRuns(ctx, reports); err != nil {
		b.Runner.MarkDirty()
		return hubError{err}
	}
	b.runsAt = b.now()
	return nil
}

// stopRuns ends every live run for a stopping bridge and reports them.
func (b *Bridge) stopRuns() {
	if b.Runner == nil {
		return
	}
	b.Runner.Shutdown()
	if b.name == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.pushRuns(ctx, true); err != nil {
		b.log().Warn("cannot report the runs ended by the stop", "err", err)
	}
}

// Tick runs one round: hello if needed, report repos, replay actions, then
// sync the repos that had actions, or all of them when full is set or
// SyncEvery has passed since the last full round. full also rescans the
// roots, which otherwise happens every ScanEvery. It returns hub errors; bd errors are logged per repo.
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
	if err := b.hubErr(b.discover(ctx, full)); err != nil {
		return err
	}
	touched, err := b.replay(ctx)
	if err := b.hubErr(err); err != nil {
		return err
	}
	if b.now().Sub(b.lastFullAt) >= b.syncEvery() {
		full = true
	}
	if b.Runner != nil {
		for _, name := range b.Runner.TakeTouched() {
			touched[name] = true
		}
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
	return b.hubErr(b.pushRuns(ctx, full))
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

// discover rescans the roots when rescan is set or ScanEvery has passed,
// rereads each found repo's opt-in line, and reports the repo list when it
// changed.
func (b *Bridge) discover(ctx context.Context, rescan bool) error {
	b.reloadAgents()
	if rescan || b.scannedAt.IsZero() || b.now().Sub(b.scannedAt) >= b.scanEvery() {
		b.found = Discover(b.Roots, b.Home, b.log())
		b.scannedAt = b.now()
	}
	next := map[string]*repoState{}
	wire := make([]api.Repo, 0, len(b.found))
	for i := range b.found {
		r := &b.found[i]
		r.OptedIn = OptedIn(r.Dir)
		r.Switch = syncSwitch(b.Switches, r.Name)
		st := b.repos[r.Name]
		if st == nil || st.Dir != r.Dir || st.Synced() != r.Synced() {
			st = &repoState{fps: map[string]string{}, items: map[string]api.Item{}}
		}
		st.Repo = *r
		next[r.Name] = st
		wire = append(wire, api.Repo{Name: r.Name, OptedIn: r.Synced(), AgentsAllowed: b.Agents[r.Name]})
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

// reloadAgents rereads the agents map from the config file. A failed read
// keeps the map the bridge has.
func (b *Bridge) reloadAgents() {
	if b.LoadAgents == nil {
		return
	}
	m, err := b.LoadAgents()
	if err != nil {
		b.log().Warn("cannot reread the agents switches; keeping the last ones", "err", err)
		return
	}
	b.Agents = m
}

// syncRepo lists a repo's human items and pushes the full set when any
// fingerprint changed, on the first run, or after RefreshEvery.
func (b *Bridge) syncRepo(ctx context.Context, st *repoState) error {
	list, err := b.Beads.ListHuman(ctx, st.Dir)
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
	if b.Runner != nil {
		ids := make(map[string]bool, len(fps))
		for id := range fps {
			ids[id] = true
		}
		b.Runner.Listed(st.Name, ids)
	}
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
