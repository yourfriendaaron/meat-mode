package harness

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Default is the harness the bridge runs when MEAT_MODE_AGENT_HARNESS is
// not set.
const Default = "claude"

// Env is what a Factory gets to build its harness.
type Env struct {
	// Command, from MEAT_MODE_AGENT_COMMAND split on whitespace, replaces
	// the harness's program and fixed flags; the harness still adds what
	// each run needs. Nil means the harness's own default.
	Command  []string
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

// Factory builds one harness.
type Factory struct {
	Name string // Info().Name of what New returns
	New  func(Env) (Harness, error)
}

var (
	mu        sync.Mutex
	factories = map[string]Factory{}
)

// Register adds a harness's factory. A harness package calls it from init.
// It panics on an empty or repeated name, as a programming error.
func Register(f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if f.Name == "" || f.New == nil {
		panic("harness: Register needs a name and a New function")
	}
	if _, dup := factories[f.Name]; dup {
		panic(fmt.Sprintf("harness: %q registered twice", f.Name))
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

// Lookup returns the factory registered under name.
func Lookup(name string) (Factory, bool) {
	mu.Lock()
	defer mu.Unlock()
	f, ok := factories[name]
	return f, ok
}

// Names returns the registered names, sorted.
func Names() []string {
	var out []string
	for _, f := range Factories() {
		out = append(out, f.Name)
	}
	return out
}

// Validate checks what a harness declares: a name matching its factory, a
// title, an instruction file, a known budget mode and well-formed env
// markers.
func Validate(name string, h Harness) error {
	info := h.Info()
	switch {
	case info.Name != name:
		return fmt.Errorf("harness %q reports the name %q", name, info.Name)
	case info.Title == "":
		return fmt.Errorf("harness %q has no title", name)
	case info.InstructionFile == "" || strings.ContainsAny(info.InstructionFile, `/\`):
		return fmt.Errorf("harness %q needs an instruction file name, not %q", name, info.InstructionFile)
	case !slices.Contains(Budgets, info.Budget):
		return fmt.Errorf("harness %q has the unknown budget mode %q", name, info.Budget)
	}
	for _, m := range info.EnvMarkers {
		k := strings.TrimSuffix(m, "*")
		if k == "" || strings.ContainsAny(k, "=* ") {
			return fmt.Errorf("harness %q has the malformed env marker %q", name, m)
		}
	}
	return nil
}

// StripEnv returns env without the bridge's own MEAT_MODE_* settings (the
// hub token among them) and without the session markers of every
// registered harness, so a run never looks nested in the session the
// bridge was started from, whichever harness that was.
func StripEnv(env []string) []string {
	var exact []string
	var prefixes []string
	for _, f := range Factories() {
		h, err := f.New(Env{Getenv: func(string) string { return "" }, LookPath: func(string) (string, error) { return "", fmt.Errorf("not looked up") }})
		if err != nil {
			continue
		}
		for _, m := range h.Info().EnvMarkers {
			if p, ok := strings.CutSuffix(m, "*"); ok {
				prefixes = append(prefixes, p)
			} else {
				exact = append(exact, m)
			}
		}
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "MEAT_MODE_") || slices.Contains(exact, k) ||
			slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(k, p) }) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
