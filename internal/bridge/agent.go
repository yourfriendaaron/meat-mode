package bridge

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/harness"
	_ "github.com/yourfriendaaron/meat-mode/internal/harness/all"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// AgentConfig is how the bridge runs agents, from the MEAT_MODE_AGENT_*
// variables.
type AgentConfig struct {
	// Harness is the name of the harness that runs agents
	// (docs/harnesses.md); harness.Default when unset.
	Harness string
	// Command, when set, replaces the harness's program and fixed flags,
	// split on whitespace with no shell. The harness still adds what each
	// run needs.
	Command []string
	// Name is shown for the agent, and its notes carry it; the harness's
	// title when unset.
	Name string
	// Timeout, BudgetUSD, RunsPerTask and Concurrent are the machine's
	// defaults for the four limits, used while the app has set none.
	Timeout     time.Duration // wall clock for one run
	BudgetUSD   float64       // spending cap for one run
	RunsPerTask int           // lifetime runs per task
	Concurrent  int           // runs live at once on this machine
	// Max holds the ceilings the app can set each limit up to, from
	// MEAT_MODE_AGENT_MAX_*. A zero ceiling lets the app set nothing.
	Max Limits
	// Saved are the limits the app set, as bridge.json keeps them.
	Saved SavedLimits
}

// Limits are the four limits on agent runs.
type Limits struct {
	BudgetUSD   float64
	Timeout     time.Duration
	RunsPerTask int
	Concurrent  int
}

// The ceilings when MEAT_MODE_AGENT_MAX_* is unset. A default limit above
// its ceiling raises the ceiling to it.
var defaultMax = Limits{BudgetUSD: 20, Timeout: 4 * time.Hour, RunsPerTask: 50, Concurrent: 5}

// DefaultAgentConfig returns the defaults every variable falls back to.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Harness:     harness.Default,
		Timeout:     30 * time.Minute,
		BudgetUSD:   2,
		RunsPerTask: 10,
		Concurrent:  2,
		Max:         defaultMax,
	}
}

// Defaults are the machine's own limits, from MEAT_MODE_AGENT_*.
func (c AgentConfig) Defaults() Limits {
	return Limits{BudgetUSD: c.BudgetUSD, Timeout: c.Timeout, RunsPerTask: c.RunsPerTask, Concurrent: c.Concurrent}
}

// Limits are the limits a run that starts now keeps: each one the app set,
// cut to its ceiling, else the machine's default. A saved value below its
// floor, which only a hand edit of bridge.json makes, reads as unset.
func (c AgentConfig) Limits() Limits {
	l := c.Defaults()
	s := c.Saved
	if v := s.BudgetUSD; v != nil && *v > 0 && c.Max.BudgetUSD > 0 {
		l.BudgetUSD = min(*v, c.Max.BudgetUSD)
	}
	if v := s.TimeoutSeconds; v != nil && *v >= api.MinTimeoutSeconds && c.Max.Timeout > 0 {
		l.Timeout = min(time.Duration(*v)*time.Second, c.Max.Timeout)
	}
	if v := s.RunsPerTask; v != nil && *v >= api.MinCount && c.Max.RunsPerTask > 0 {
		l.RunsPerTask = min(*v, c.Max.RunsPerTask)
	}
	if v := s.Concurrent; v != nil && *v >= api.MinCount && c.Max.Concurrent > 0 {
		l.Concurrent = min(*v, c.Max.Concurrent)
	}
	return l
}

// wire is l as the hub sees it, the time limit in whole seconds.
func (l Limits) wire() api.AgentLimits {
	return api.AgentLimits{BudgetUSD: l.BudgetUSD, TimeoutSeconds: int(l.Timeout / time.Second), RunsPerTask: l.RunsPerTask, Concurrent: l.Concurrent}
}

// RunLimits are the limits as the hello reports them.
func (c AgentConfig) RunLimits() *api.RunLimits {
	out := &api.RunLimits{AgentLimits: c.Limits().wire(), Defaults: c.Defaults().wire(), Max: c.Max.wire()}
	for _, n := range api.LimitNames {
		if c.Saved.has(n) {
			out.Set = append(out.Set, n)
		}
	}
	return out
}

// AgentConfigFromEnv reads MEAT_MODE_AGENT_HARNESS, _COMMAND, _NAME,
// _TIMEOUT, _BUDGET_USD, _RUNS_PER_TASK and _CONCURRENT over the defaults,
// and the ceilings _MAX_BUDGET_USD, _MAX_TIMEOUT, _MAX_RUNS_PER_TASK and
// _MAX_CONCURRENT.
func AgentConfigFromEnv(getenv func(string) string) (AgentConfig, error) {
	c := DefaultAgentConfig()
	if v := strings.TrimSpace(getenv("MEAT_MODE_AGENT_HARNESS")); v != "" {
		if _, ok := harness.Lookup(v); !ok {
			return c, fmt.Errorf("MEAT_MODE_AGENT_HARNESS must be one of %s, not %q", strings.Join(harness.Names(), ", "), v)
		}
		c.Harness = v
	}
	if v := strings.TrimSpace(getenv("MEAT_MODE_AGENT_COMMAND")); v != "" {
		c.Command = strings.Fields(v)
	}
	if v := strings.TrimSpace(getenv("MEAT_MODE_AGENT_NAME")); v != "" {
		c.Name = v
	}
	if v := getenv("MEAT_MODE_AGENT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return c, fmt.Errorf("MEAT_MODE_AGENT_TIMEOUT must be a positive duration such as 30m, not %q", v)
		}
		c.Timeout = d
	}
	if v := getenv("MEAT_MODE_AGENT_BUDGET_USD"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || !(f > 0) || f > 1e6 {
			return c, fmt.Errorf("MEAT_MODE_AGENT_BUDGET_USD must be a positive amount in US dollars such as 2, not %q", v)
		}
		c.BudgetUSD = f
	}
	for _, n := range []struct {
		name string
		to   *int
	}{{"MEAT_MODE_AGENT_RUNS_PER_TASK", &c.RunsPerTask}, {"MEAT_MODE_AGENT_CONCURRENT", &c.Concurrent}} {
		if v := getenv(n.name); v != "" {
			i, err := strconv.Atoi(v)
			if err != nil || i < 1 {
				return c, fmt.Errorf("%s must be a whole number of 1 or more, not %q", n.name, v)
			}
			*n.to = i
		}
	}
	if err := c.readMax(getenv); err != nil {
		return c, err
	}
	return c, nil
}

// readMax sets the ceilings from MEAT_MODE_AGENT_MAX_*. An unset ceiling is
// its default, or the machine's default limit when that is higher, so no
// setup is tighter for having no ceiling set. A ceiling set below the
// default limit is refused.
func (c *AgentConfig) readMax(getenv func(string) string) error {
	d := c.Defaults()
	c.Max = Limits{
		BudgetUSD:   max(defaultMax.BudgetUSD, d.BudgetUSD),
		Timeout:     max(defaultMax.Timeout, d.Timeout),
		RunsPerTask: max(defaultMax.RunsPerTask, d.RunsPerTask),
		Concurrent:  max(defaultMax.Concurrent, d.Concurrent),
	}
	if v := getenv(api.EnvMaxTimeout); v != "" {
		t, err := time.ParseDuration(v)
		if err != nil || t <= 0 {
			return fmt.Errorf("%s must be a positive duration such as 4h, not %q", api.EnvMaxTimeout, v)
		}
		if t < d.Timeout {
			return fmt.Errorf("%s is %s, below MEAT_MODE_AGENT_TIMEOUT of %s; raise it or lower the time limit", api.EnvMaxTimeout, v, d.Timeout)
		}
		c.Max.Timeout = t
	}
	if v := getenv(api.EnvMaxBudget); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || !(f > 0) || f > api.MaxLimitBudgetUSD {
			return fmt.Errorf("%s must be a positive amount in US dollars such as 20, not %q", api.EnvMaxBudget, v)
		}
		if f < d.BudgetUSD {
			return fmt.Errorf("%s is %s, below MEAT_MODE_AGENT_BUDGET_USD of %s; raise it or lower the budget", api.EnvMaxBudget, v, strconv.FormatFloat(d.BudgetUSD, 'f', -1, 64))
		}
		c.Max.BudgetUSD = f
	}
	for _, n := range []struct {
		name, limit string
		def         int
		to          *int
	}{
		{api.EnvMaxRunsPerTask, "MEAT_MODE_AGENT_RUNS_PER_TASK", d.RunsPerTask, &c.Max.RunsPerTask},
		{api.EnvMaxConcurrent, "MEAT_MODE_AGENT_CONCURRENT", d.Concurrent, &c.Max.Concurrent},
	} {
		if v := getenv(n.name); v != "" {
			i, err := strconv.Atoi(v)
			if err != nil || i < 1 {
				return fmt.Errorf("%s must be a whole number of 1 or more, not %q", n.name, v)
			}
			if i < n.def {
				return fmt.Errorf("%s is %d, below %s of %d; raise it or lower the limit", n.name, i, n.limit, n.def)
			}
			*n.to = i
		}
	}
	return nil
}

// NewHarness builds the configured harness and checks what it declares.
func (c AgentConfig) NewHarness(getenv func(string) string, lookPath func(string) (string, error)) (harness.Harness, error) {
	name := cmp.Or(c.Harness, harness.Default)
	f, ok := harness.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("no agent harness is called %q; the bridge has %s", name, strings.Join(harness.Names(), ", "))
	}
	h, err := f.New(harness.Env{Command: c.Command, Getenv: getenv, LookPath: lookPath})
	if err != nil {
		return nil, fmt.Errorf("harness %s: %w", name, err)
	}
	if err := harness.Validate(name, h); err != nil {
		return nil, err
	}
	return h, nil
}

// harnessProgram is the program h runs, Args[0] of a launch.
func harnessProgram(h harness.Harness) string {
	l, err := h.Launch(harness.Spec{Prompt: "x"})
	if err != nil || len(l.Args) == 0 {
		return ""
	}
	return l.Args[0]
}

// harnessInfo is h as the hello names it: version is its check's, "" when
// that failed. The limits are those a run that starts now keeps; the budget
// is 0 for a harness that enforces none.
func harnessInfo(h harness.Harness, c AgentConfig, version string) *api.HarnessInfo {
	info := h.Info()
	out := &api.HarnessInfo{
		Name: info.Name, Title: info.Title, Version: version, InstructionFile: info.InstructionFile,
		Budget: info.Budget, Steer: info.Steer,
	}
	setLimits(out, c)
	return out
}

// setLimits writes c's limits into a hello's harness.
func setLimits(out *api.HarnessInfo, c AgentConfig) {
	l := c.Limits()
	out.TimeoutSeconds = int(l.Timeout / time.Second)
	out.BudgetUSD = 0
	if out.Budget != harness.BudgetNone {
		out.BudgetUSD = l.BudgetUSD
	}
	out.Limits = c.RunLimits()
}

// checkHarness runs h's check for a hello outside the running bridge.
func checkHarness(ctx context.Context, h harness.Harness, c AgentConfig) (*api.HarnessInfo, error) {
	v, err := h.Check(ctx)
	if err != nil {
		v = ""
	}
	return harnessInfo(h, c, v), err
}

// agentEnv is the agent's environment: the bridge's own, without its
// MEAT_MODE_* settings (the hub token among them) and without the session
// markers of every harness the bridge may have been started from, plus the
// tracker's own, such as BEADS_ACTOR, so the agent's notes carry its name.
// An inherited variable the tracker sets is dropped.
func agentEnv(env []string, actor string, ag tracker.Agent) []string {
	extra := ag.Env(actor)
	set := make(map[string]bool, len(extra))
	for _, kv := range extra {
		k, _, _ := strings.Cut(kv, "=")
		set[k] = true
	}
	stripped := harness.StripEnv(env)
	out := make([]string, 0, len(stripped)+len(extra))
	for _, kv := range stripped {
		k, _, _ := strings.Cut(kv, "=")
		if set[k] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

// maxActivity caps an activity line.
const maxActivity = 60

// plainWord is a command or file name safe to show as is.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// subcommands are the tools whose second word says what they do.
var subcommands = map[string]bool{
	"go": true, "git": true, "npm": true, "pnpm": true, "yarn": true, "bun": true, "cargo": true,
	"make": true, "uv": true, "pip": true, "docker": true, "podman": true, "mise": true, "just": true,
}

// describeTool turns one tool call into a short line in plain words, such as
// "Editing notes.txt" or "Running go test". It never shows more of a command
// than its first word or two, nor any path but a file's base name. ag names
// the tracker's own commands.
func describeTool(t harness.Tool, ag tracker.Agent) string {
	file := fileName(t.File)
	switch t.Kind {
	case harness.ToolShell:
		return describeCommand(t.Command, ag)
	case harness.ToolEdit:
		return orWork("Editing ", file, "Editing a file")
	case harness.ToolWrite:
		return orWork("Writing ", file, "Writing a file")
	case harness.ToolRead:
		return orWork("Reading ", file, "Reading a file")
	case harness.ToolSearch:
		return "Searching the files"
	case harness.ToolWeb:
		return "Searching the web"
	case harness.ToolAgent:
		return "Starting a helper"
	case harness.ToolPlan:
		return "Planning"
	}
	return "Working"
}

func orWork(prefix, file, fallback string) string {
	if file == "" {
		return fallback
	}
	return clip(prefix + file)
}

// fileName is the base name of p when it is a plain name.
func fileName(p string) string {
	if p == "" {
		return ""
	}
	base := path.Base(strings.ReplaceAll(p, "\\", "/"))
	if !plainWord.MatchString(base) {
		return ""
	}
	return base
}

// describeCommand names a shell command by its first word or two. A leading
// `cd dir &&` and variable assignments are skipped; the tracker's own
// commands are named as its Describe says.
func describeCommand(cmd string, ag tracker.Agent) string {
	words := strings.Fields(cmd)
skip:
	for len(words) > 0 {
		w := words[0]
		switch {
		case w == "cd" && len(words) > 2 && words[2] == "&&":
			words = words[3:]
		case strings.Contains(w, "=") && !strings.HasPrefix(w, "-"):
			words = words[1:]
		case w == "sudo" || w == "env" || w == "time":
			words = words[1:]
		default:
			break skip
		}
	}
	if len(words) == 0 {
		return "Running a command"
	}
	first := path.Base(words[0])
	if !plainWord.MatchString(first) {
		return "Running a command"
	}
	if ag != nil {
		if d := ag.Describe(words); d != "" {
			return clip(d)
		}
	}
	second := ""
	if len(words) > 1 && plainWord.MatchString(words[1]) && !strings.HasPrefix(words[1], "-") {
		second = words[1]
	}
	if subcommands[first] && second != "" {
		return clip("Running " + first + " " + second)
	}
	return clip("Running " + first)
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxActivity {
		return s
	}
	return string([]rune(s)[:maxActivity-1]) + "…"
}
