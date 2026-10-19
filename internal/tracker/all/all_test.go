package all

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Every shipped tracker registers and declares itself within the contract.
func TestRegistered(t *testing.T) {
	var names []string
	for _, f := range tracker.Factories() {
		names = append(names, f.Name)
		tr, err := f.New(tracker.Env{Getenv: func(string) string { return "" }, LookPath: exec.LookPath})
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if err := tracker.Validate(f.Name, tr); err != nil {
			t.Error(err)
		}
	}
	if want := []string{"backlog", "beads", "ticket"}; !slices.Equal(names, want) {
		t.Fatalf("registered %v, want %v", names, want)
	}
}

// The app keeps the raise command of each tracker older than raise in
// hello, for an older bridge that sends none; it must match Info().Raise.
func TestAppKnowsOlderRaise(t *testing.T) {
	src, err := os.ReadFile("../../../web/src/agents.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range tracker.Factories() {
		tr, err := f.New(tracker.Env{Getenv: func(string) string { return "" }, LookPath: exec.LookPath})
		if err != nil {
			t.Fatal(err)
		}
		if raise := tr.Info().Raise; !strings.Contains(string(src), "raise: '"+raise+"'") {
			t.Errorf("web/src/agents.ts KNOWN_RAISE has no %s entry with %q", f.Name, raise)
		}
	}
}
