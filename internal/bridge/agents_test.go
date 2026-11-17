package bridge

import (
	"context"
	"log/slog"
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

func TestBridgeRereadsSwitchesBeforeReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	must(t, Config{Token: "t"}.Save(path))
	b := &Bridge{LoadConfig: func() (Saved, error) { return LoadSaved(path) }, Log: quiet}
	b.reloadConfig()
	if b.Agents["garden"] || b.Epics["garden"] {
		t.Fatal("switches on before the commands")
	}
	must(t, SaveAgents(path, "garden", true))
	must(t, SaveEpics(path, "garden", true))
	b.reloadConfig()
	if !b.Agents["garden"] || !b.Epics["garden"] {
		t.Error("the switches made by the commands did not reach the bridge")
	}
	// A broken file keeps the maps the bridge has.
	must(t, os.WriteFile(path, []byte("{"), 0o600))
	b.reloadConfig()
	if !b.Agents["garden"] || !b.Epics["garden"] {
		t.Error("a failed reread dropped the switches")
	}
}

// TestConfigReadOncePerTick: each tick reads bridge.json once, and a file
// others can read is logged once per mode, not on every tick.
func TestConfigReadOncePerTick(t *testing.T) {
	t.Parallel()
	w := newWorld(t, false)
	w.repo("garden", true)
	path := filepath.Join(t.TempDir(), "bridge.json")
	must(t, Config{Token: "t"}.Save(path))
	must(t, os.Chmod(path, 0o644))
	var logs strings.Builder
	reads := 0
	b := w.start()
	b.Log = slog.New(slog.NewTextHandler(&logs, nil))
	b.LoadConfig = func() (Saved, error) { reads++; return LoadSaved(path) }
	warnings := func() int { return strings.Count(logs.String(), "chmod 600") }

	for i := range 3 {
		w.tick(b)
		if reads != i+1 {
			t.Fatalf("tick %d read the config %d times in all", i+1, reads)
		}
	}
	if n := warnings(); n != 1 {
		t.Errorf("three ticks over a 0644 file warned %d times, want 1", n)
	}
	must(t, os.Chmod(path, 0o640))
	w.tick(b)
	w.tick(b)
	if n := warnings(); n != 2 {
		t.Errorf("a new mode warned %d times in all, want 2", n)
	}
	must(t, os.Chmod(path, 0o600))
	w.tick(b)
	must(t, os.Chmod(path, 0o640))
	w.tick(b)
	if n := warnings(); n != 3 {
		t.Errorf("an open mode after a closed one warned %d times in all, want 3", n)
	}
}
