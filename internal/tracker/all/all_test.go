package all

import (
	"os/exec"
	"slices"
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
