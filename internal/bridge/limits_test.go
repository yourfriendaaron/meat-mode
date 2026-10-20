package bridge

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func ptr[T any](v T) *T { return &v }

func TestAgentCeilingsFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := AgentConfigFromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Max != (Limits{BudgetUSD: 20, Timeout: 4 * time.Hour, RunsPerTask: 50, Concurrent: 5}) {
		t.Errorf("default ceilings = %+v", c.Max)
	}
	// A limit above its default ceiling raises the ceiling, so an upgrade
	// never makes a setup tighter.
	c, err = AgentConfigFromEnv(env(map[string]string{
		"MEAT_MODE_AGENT_BUDGET_USD": "30", "MEAT_MODE_AGENT_TIMEOUT": "6h",
		"MEAT_MODE_AGENT_RUNS_PER_TASK": "80", "MEAT_MODE_AGENT_CONCURRENT": "8",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Max != (Limits{BudgetUSD: 30, Timeout: 6 * time.Hour, RunsPerTask: 80, Concurrent: 8}) {
		t.Errorf("raised ceilings = %+v", c.Max)
	}
	c, err = AgentConfigFromEnv(env(map[string]string{
		api.EnvMaxBudget: "5", api.EnvMaxTimeout: "1h", api.EnvMaxRunsPerTask: "12", api.EnvMaxConcurrent: "3",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Max != (Limits{BudgetUSD: 5, Timeout: time.Hour, RunsPerTask: 12, Concurrent: 3}) {
		t.Errorf("set ceilings = %+v", c.Max)
	}
	for k, v := range map[string]string{
		api.EnvMaxBudget: "1", api.EnvMaxTimeout: "10m", api.EnvMaxRunsPerTask: "9", api.EnvMaxConcurrent: "1",
	} {
		// Each is below its default limit.
		if _, err := AgentConfigFromEnv(env(map[string]string{k: v})); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s below the default: err = %v", k, v, err)
		}
	}
	for k, v := range map[string]string{
		api.EnvMaxBudget: "lots", api.EnvMaxTimeout: "-1h", api.EnvMaxRunsPerTask: "0", api.EnvMaxConcurrent: "many",
	} {
		if _, err := AgentConfigFromEnv(env(map[string]string{k: v})); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s: err = %v", k, v, err)
		}
	}
}

func TestAgentLimitsClampToCeilings(t *testing.T) {
	c := DefaultAgentConfig()
	if c.Limits() != c.Defaults() {
		t.Errorf("unset limits = %+v, want the defaults %+v", c.Limits(), c.Defaults())
	}
	c.Saved = SavedLimits{BudgetUSD: ptr(5.5), TimeoutSeconds: ptr(3600), RunsPerTask: ptr(20), Concurrent: ptr(3)}
	if got := c.Limits(); got != (Limits{BudgetUSD: 5.5, Timeout: time.Hour, RunsPerTask: 20, Concurrent: 3}) {
		t.Errorf("set limits = %+v", got)
	}
	// A hand-edited bridge.json, or a ceiling lowered since, never takes a
	// run past the ceiling.
	c.Saved = SavedLimits{BudgetUSD: ptr(500.0), TimeoutSeconds: ptr(86400), RunsPerTask: ptr(1000), Concurrent: ptr(99)}
	if got := c.Limits(); got != c.Max {
		t.Errorf("limits above the ceilings = %+v, want %+v", got, c.Max)
	}
	// Below the floors reads as unset.
	c.Saved = SavedLimits{BudgetUSD: ptr(-1.0), TimeoutSeconds: ptr(5), RunsPerTask: ptr(0), Concurrent: ptr(-2)}
	if got := c.Limits(); got != c.Defaults() {
		t.Errorf("limits below the floors = %+v, want the defaults", got)
	}
	// Without ceilings the app sets nothing.
	c.Saved = SavedLimits{BudgetUSD: ptr(3.0)}
	c.Max = Limits{}
	if got := c.Limits(); got.BudgetUSD != 2 {
		t.Errorf("budget without a ceiling = %v, want the default", got.BudgetUSD)
	}
	c = DefaultAgentConfig()
	c.Saved = SavedLimits{RunsPerTask: ptr(20)}
	if rl := c.RunLimits(); rl.RunsPerTask != 20 || rl.Defaults.RunsPerTask != 10 || rl.Max.RunsPerTask != 50 ||
		!slices.Equal(rl.Set, []string{api.LimitRunsPerTask}) || rl.TimeoutSeconds != 1800 {
		t.Errorf("run limits = %+v", rl)
	}
}

func TestSaveLimitsKeepsTheRestOfTheConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	if err := (Config{HubURL: "https://hub.example.com", Token: "t", Agents: map[string]bool{"garden": true}}).Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err := SaveLimits(path, api.LimitsChange{BudgetUSD: ptr(4.0), Concurrent: ptr(3)})
	if err != nil {
		t.Fatal(err)
	}
	if saved.BudgetUSD == nil || *saved.BudgetUSD != 4 || saved.Concurrent == nil || *saved.Concurrent != 3 || saved.TimeoutSeconds != nil {
		t.Errorf("saved = %+v", saved)
	}
	saved, err = SaveLimits(path, api.LimitsChange{TimeoutSeconds: ptr(600), Reset: []string{api.LimitBudget}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := readConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Limits.BudgetUSD != nil || c.Limits.Concurrent == nil || *c.Limits.Concurrent != 3 || c.Limits.TimeoutSeconds == nil ||
		*c.Limits.TimeoutSeconds != 600 || c.Token != "t" || !c.Agents["garden"] || !reflect.DeepEqual(saved, c.Limits) {
		t.Errorf("config = %+v, limits %+v", c, c.Limits)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v", fi.Mode(), err)
	}
	// LoadSettings hands the saved limits to the runner's config.
	s, err := LoadSettings(Env{Paths: Paths{Home: t.TempDir(), Config: path}, Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	if s.Agent.Limits().Timeout != 10*time.Minute || s.Agent.Limits().Concurrent != 3 {
		t.Errorf("settings limits = %+v", s.Agent.Limits())
	}
}

// The bridge refuses a limit above its ceiling whatever the hub sent, saves
// one within it, reports it in a hello before the result, and a live run
// keeps the limits it started with.
func TestBridgeAppliesLimits(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	other := w.seed(dir, "Feed the cat")
	path := filepath.Join(t.TempDir(), "bridge.json")
	b := w.agentBridge(true)
	b.SaveLimits = func(c api.LimitsChange) (SavedLimits, error) { return SaveLimits(path, c) }
	w.tick(b)
	w.ask(b, "ask-hang", id, "hang")

	w.hub.Enqueue(us, action("limits-over", api.KindLimits, "", "", api.Payload{Limits: &api.LimitsChange{BudgetUSD: ptr(25.0)}}))
	w.tick(b)
	rs := w.hub.Results(aid("limits-over"))
	if len(rs) != 1 || rs[0].OK || !strings.Contains(rs[0].Error, "$20") || !strings.Contains(rs[0].Error, api.EnvMaxBudget) {
		t.Fatalf("over the ceiling: %+v", rs)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a refused change was saved")
	}

	hellos := len(w.hub.Hellos())
	w.hub.Enqueue(us, action("limits-ok", api.KindLimits, "", "", api.Payload{Limits: &api.LimitsChange{TimeoutSeconds: ptr(7200), RunsPerTask: ptr(1)}}))
	w.tick(b)
	w.oneOK(aid("limits-ok"))
	hs := w.hub.Hellos()
	if len(hs) <= hellos {
		t.Fatal("no hello reported the new limits")
	}
	h := hs[len(hs)-1].Hello.Harness
	if h == nil || h.Limits == nil || h.TimeoutSeconds != 7200 || h.Limits.RunsPerTask != 1 || h.Limits.Defaults.TimeoutSeconds != 60 ||
		!slices.Equal(h.Limits.Set, []string{api.LimitTimeout, api.LimitRunsPerTask}) {
		t.Fatalf("hello harness = %+v limits %+v", h, h.Limits)
	}
	if c, _ := readConfigFile(path); c.Limits.TimeoutSeconds == nil || *c.Limits.TimeoutSeconds != 7200 {
		t.Errorf("saved = %+v", c.Limits)
	}
	b.Runner.mu.Lock()
	live := b.Runner.live[runKey("garden", id)]
	b.Runner.mu.Unlock()
	if live == nil || live.limits.Timeout != time.Minute {
		t.Errorf("the live run's limits changed: %+v", live)
	}
	// The new run limit reaches the next run: the other task has had none,
	// so it may start one; ask again and it has used its one.
	w.ask(b, "ask-other", other, "close")
	w.waitRun(b, other, settled)
	w.ask(b, "ask-other-2", other, "close")
	if run := runOf(b, other).Run; run.Reason != api.ReasonRunLimit {
		t.Errorf("after the run limit: %+v", run)
	}
}
