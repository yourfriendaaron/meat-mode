package tracker

import (
	"cmp"
	"fmt"
	"slices"
	"sync"
)

// Env is what a Factory gets to build its tracker.
type Env struct {
	// Bin is the path of the tracker's tool from the bridge config, or ""
	// to look it up on PATH.
	Bin      string
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

// Factory builds one tracker.
type Factory struct {
	Name string // Info().Name of what New returns
	New  func(Env) (Tracker, error)
}

var (
	mu        sync.Mutex
	factories = map[string]Factory{}
)

// Register adds a tracker's factory. A tracker package calls it from init.
// It panics on an empty or repeated name, as a programming error.
func Register(f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if f.Name == "" || f.New == nil {
		panic("tracker: Register needs a name and a New function")
	}
	if _, dup := factories[f.Name]; dup {
		panic(fmt.Sprintf("tracker: %q registered twice", f.Name))
	}
	factories[f.Name] = f
}

// Factories returns every registered factory, by name.
func Factories() []Factory {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Factory, 0, len(factories))
	for _, f := range factories {
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b Factory) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// Validate checks what a tracker declares: a name matching its factory,
// known capabilities, and Agent implemented exactly when it lists ask.
func Validate(name string, t Tracker) error {
	info := t.Info()
	if info.Name != name {
		return fmt.Errorf("tracker %q reports the name %q", name, info.Name)
	}
	for _, c := range info.Actions {
		if !slices.Contains(Capabilities, c) {
			return fmt.Errorf("tracker %q lists the unknown capability %q", name, c)
		}
	}
	_, isAgent := t.(Agent)
	if info.Can("ask") != isAgent {
		return fmt.Errorf("tracker %q must implement Agent exactly when it lists ask", name)
	}
	return nil
}

// Sort orders trackers by Rank, then name: the order Detect is tried in.
func Sort(ts []Tracker) {
	slices.SortFunc(ts, func(a, b Tracker) int {
		return cmp.Or(cmp.Compare(a.Info().Rank, b.Info().Rank), cmp.Compare(a.Info().Name, b.Info().Name))
	})
}
