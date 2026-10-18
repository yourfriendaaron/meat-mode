package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// agentsEnv is a paired-looking env with two repos under its home folder.
func agentsEnv(t *testing.T) (env Env, garden, chores string) {
	t.Helper()
	env, _, _ = testEnv(t, "")
	garden = filepath.Join(env.Paths.Home, "projects", "garden")
	chores = filepath.Join(env.Paths.Home, "projects", "chores")
	writeRepo(t, garden, "")
	writeRepo(t, chores, "")
	cfg := Config{HubURL: "https://hub.example.com", Token: "t", Sync: map[string]bool{"chores": true}}
	if err := cfg.Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	return env, garden, chores
}

func TestAgentsCommand(t *testing.T) {
	env, garden, _ := agentsEnv(t)
	ctx := context.Background()
	run := func(args ...string) (string, error) {
		out := &strings.Builder{}
		env.Stdout, env.Stderr = out, out
		err := Main(ctx, append([]string{"agents"}, args...), env)
		return out.String(), err
	}

	out, err := run()
	if err != nil || !strings.Contains(out, "projects/garden") || strings.Contains(out, "agents on") {
		t.Fatalf("list = %q, %v", out, err)
	}

	// By the reported name.
	if _, err := run("on", "projects/garden"); err != nil {
		t.Fatal(err)
	}
	c, _ := readConfigFile(env.Paths.Config)
	if !c.Agents["projects/garden"] || !c.Sync["chores"] || c.Token != "t" {
		t.Errorf("config after on = %+v", c)
	}
	if out, _ := run(); !strings.Contains(out, "projects/garden  ") || !strings.Contains(out, "agents on") {
		t.Errorf("list after on = %q", out)
	}

	// By a path, from inside the repo, and by an absolute one.
	t.Chdir(garden)
	if _, err := run("off", "."); err != nil {
		t.Fatal(err)
	}
	if c, _ := readConfigFile(env.Paths.Config); c.Agents["projects/garden"] {
		t.Errorf("config after off . = %+v", c.Agents)
	}
	if _, err := run("on", garden); err != nil {
		t.Fatal(err)
	}
	if c, _ := readConfigFile(env.Paths.Config); !c.Agents["projects/garden"] {
		t.Errorf("config after on <path> = %+v", c.Agents)
	}

	// A repo the bridge does not find, and a folder that is not a repo.
	for _, arg := range []string{"projects/shed", t.TempDir()} {
		if _, err := run("on", arg); err == nil || !strings.Contains(err.Error(), arg) {
			t.Errorf("on %q = %v, want an error naming it", arg, err)
		}
	}
	if c, _ := readConfigFile(env.Paths.Config); len(c.Agents) != 1 {
		t.Errorf("a refused repo was saved: %+v", c.Agents)
	}
	for _, args := range [][]string{{"on"}, {"maybe", "projects/garden"}, {"on", "a", "b"}} {
		if _, err := run(args...); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("agents %v = %v, want usage", args, err)
		}
	}
}

// TestConfigLockKeepsBothWrites: the CLI and the running bridge change
// bridge.json at the same time and neither loses the other's change.
func TestConfigLockKeepsBothWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	if err := (Config{Token: "t"}).Save(path); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(2)
		name := string(rune('a' + i))
		go func() { defer wg.Done(); must(t, SaveAgents(path, name, true)) }()
		go func() { defer wg.Done(); must(t, SaveSwitch(path, name, true)) }()
	}
	wg.Wait()
	c, err := readConfigFile(path)
	if err != nil || len(c.Agents) != 20 || len(c.Sync) != 20 {
		t.Fatalf("config = %d agents, %d sync, %v", len(c.Agents), len(c.Sync), err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Errorf("no lock file: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}

func TestBridgeRereadsAgentsBeforeReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	must(t, Config{Token: "t"}.Save(path))
	b := &Bridge{LoadAgents: func() (map[string]bool, error) { return LoadAgents(path) }}
	b.reloadAgents()
	if b.Agents["garden"] {
		t.Fatal("agents on before the switch")
	}
	must(t, SaveAgents(path, "garden", true))
	b.reloadAgents()
	if !b.Agents["garden"] {
		t.Error("the switch made by the command did not reach the bridge")
	}
	// A broken file keeps the map the bridge has.
	must(t, os.WriteFile(path, []byte("{"), 0o600))
	b.reloadAgents()
	if !b.Agents["garden"] {
		t.Error("a failed reread dropped the switches")
	}
}
