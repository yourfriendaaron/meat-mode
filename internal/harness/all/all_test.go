package all

import (
	"os/exec"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// Every shipped harness registers and declares itself within the contract,
// the default among them.
func TestRegistered(t *testing.T) {
	if _, ok := harness.Lookup(harness.Default); !ok {
		t.Fatalf("the default harness %q is not registered", harness.Default)
	}
	for _, f := range harness.Factories() {
		h, err := f.New(harness.Env{Getenv: func(string) string { return "" }, LookPath: exec.LookPath})
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if err := harness.Validate(f.Name, h); err != nil {
			t.Error(err)
		}
	}
}
