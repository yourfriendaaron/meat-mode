package harness_test

import (
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/harness/fake"
)

func init() {
	f := fake.New()
	f.Inf.Name = "fake-a"
	f.Inf.EnvMarkers = []string{"FAKE_A_SESSION", "FAKE_A_*"}
	harness.Register(f.Factory())
	g := fake.New()
	g.Inf.Name = "fake-b"
	g.Inf.EnvMarkers = []string{"FAKE_B"}
	harness.Register(g.Factory())
}

func TestValidSession(t *testing.T) {
	for s, want := range map[string]bool{
		"0b6a3c1e-2f": true, "ses_01ABC": true, "thread.1": true,
		"": false, "-x": false, "a b": false, "a\nb": false, "a=b": false,
	} {
		if harness.ValidSession(s) != want {
			t.Errorf("ValidSession(%q) = %v", s, !want)
		}
	}
}

func TestValidate(t *testing.T) {
	f := fake.New()
	if err := harness.Validate("fake", f); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*harness.Info){
		func(i *harness.Info) { i.Name = "other" },
		func(i *harness.Info) { i.Title = "" },
		func(i *harness.Info) { i.InstructionFile = "" },
		func(i *harness.Info) { i.InstructionFile = "docs/AGENTS.md" },
		func(i *harness.Info) { i.Budget = "unlimited" },
		func(i *harness.Info) { i.EnvMarkers = []string{"*"} },
		func(i *harness.Info) { i.EnvMarkers = []string{"A=B"} },
	} {
		g := fake.New()
		mutate(&g.Inf)
		if harness.Validate("fake", g) == nil {
			t.Errorf("Validate passed %+v", g.Inf)
		}
	}
}

// StripEnv drops the markers of every registered harness, not only the
// one in use, and the bridge's own settings.
func TestStripEnv(t *testing.T) {
	got := harness.StripEnv([]string{
		"PATH=/usr/bin", "FAKE_A_SESSION=1", "FAKE_A_DEPTH=2", "FAKE_B=1", "FAKE_BX=keep",
		"MEAT_MODE_BRIDGE_TOKEN=secret", "TERM=xterm",
	})
	if want := []string{"PATH=/usr/bin", "FAKE_BX=keep", "TERM=xterm"}; !slices.Equal(got, want) {
		t.Fatalf("StripEnv = %q, want %q", got, want)
	}
}

func TestNames(t *testing.T) {
	if got := harness.Names(); !slices.Equal(got, []string{"fake-a", "fake-b"}) {
		t.Fatalf("Names = %q", got)
	}
	if _, ok := harness.Lookup("fake-a"); !ok {
		t.Fatal("Lookup missed fake-a")
	}
}
