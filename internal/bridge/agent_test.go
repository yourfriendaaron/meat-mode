package bridge

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/beads"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/fake"
)

func TestAgentConfigFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := AgentConfigFromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Harness != "claude" || c.Command != nil || c.Name != "agent" || c.Timeout != 30*time.Minute || c.BudgetUSD != 2 ||
		c.RunsPerTask != 10 || c.Concurrent != 2 {
		t.Errorf("defaults = %+v", c)
	}
	c, err = AgentConfigFromEnv(env(map[string]string{
		"MEAT_MODE_AGENT_HARNESS": "claude", "MEAT_MODE_AGENT_COMMAND": "  my-agent  --flag value ", "MEAT_MODE_AGENT_NAME": "helper",
		"MEAT_MODE_AGENT_TIMEOUT": "90s", "MEAT_MODE_AGENT_BUDGET_USD": "0.5",
		"MEAT_MODE_AGENT_RUNS_PER_TASK": "3", "MEAT_MODE_AGENT_CONCURRENT": "1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Command, []string{"my-agent", "--flag", "value"}) || c.Name != "helper" || c.Timeout != 90*time.Second ||
		c.BudgetUSD != 0.5 || c.RunsPerTask != 3 || c.Concurrent != 1 {
		t.Errorf("set = %+v", c)
	}
	// The command reaches the harness unchanged, as a setting written for
	// Claude Code before harnesses did.
	h, err := c.NewHarness(env(nil), exec.LookPath)
	if err != nil {
		t.Fatal(err)
	}
	l, err := h.Launch(harness.Spec{Prompt: "Water the plants.", Session: "s-1", BudgetUSD: c.BudgetUSD})
	if err != nil || !slices.Equal(l.Args, []string{"my-agent", "--flag", "value", "--max-budget-usd", "0.5", "--resume", "s-1"}) {
		t.Errorf("args = %q, %v", l.Args, err)
	}
	_, err = AgentConfigFromEnv(env(map[string]string{"MEAT_MODE_AGENT_HARNESS": "nope"}))
	if err == nil || !strings.Contains(err.Error(), "MEAT_MODE_AGENT_HARNESS") || !strings.Contains(err.Error(), "claude") {
		t.Errorf("unknown harness: err = %v", err)
	}
	for k, v := range map[string]string{
		"MEAT_MODE_AGENT_TIMEOUT": "soon", "MEAT_MODE_AGENT_BUDGET_USD": "-1",
		"MEAT_MODE_AGENT_RUNS_PER_TASK": "0", "MEAT_MODE_AGENT_CONCURRENT": "two",
	} {
		if _, err := AgentConfigFromEnv(env(map[string]string{k: v})); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s: err = %v", k, v, err)
		}
	}
}

func TestAgentEnv(t *testing.T) {
	got := agentEnv([]string{
		"PATH=/usr/bin", "HOME=/srv/alex", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=x", "CLAUDE_CONFIG_DIR=/c",
		"MEAT_MODE_BRIDGE_TOKEN=secret", "BEADS_ACTOR=someone", "FAKE_ACTOR=someone",
	}, "planner", &beads.CLI{})
	want := []string{"PATH=/usr/bin", "HOME=/srv/alex", "CLAUDE_CONFIG_DIR=/c", "FAKE_ACTOR=someone", "BEADS_ACTOR=planner"}
	if !slices.Equal(got, want) {
		t.Errorf("env = %q, want %q", got, want)
	}
	// Another tracker's variables are its own; BEADS_ACTOR passes through.
	got = agentEnv([]string{"PATH=/usr/bin", "BEADS_ACTOR=someone", "FAKE_ACTOR=someone"}, "planner", fake.New().Tracker().(tracker.Agent))
	want = []string{"PATH=/usr/bin", "BEADS_ACTOR=someone", "FAKE_ACTOR=planner"}
	if !slices.Equal(got, want) {
		t.Errorf("env with the fake = %q, want %q", got, want)
	}
}

func TestAgentPrompt(t *testing.T) {
	p := (&beads.CLI{}).Prompt("tst-1")
	for _, want := range []string{"bd show tst-1", "bd close tst-1", "bd comment tst-1", "instruction files", "needs the user", "Never send, post, spend, delete or push"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}

func TestDescribeTool(t *testing.T) {
	sh := func(cmd string) harness.Tool { return harness.Tool{Kind: harness.ToolShell, Command: cmd} }
	for _, c := range []struct {
		tool harness.Tool
		want string
	}{
		{sh("bd show tst-1"), "Reading the bead"},
		{sh(`cd /work/garden && bd comment tst-1 "Which plants?"`), "Writing a note"},
		{sh("bd comments add tst-1 hi"), "Writing a note"},
		{sh("bd close tst-1"), "Closing the bead"},
		{sh("bd update tst-1 --claim"), "Updating the bead"},
		{sh("go test ./internal/... -run X"), "Running go test"},
		{sh("TOKEN=abc123 curl -H 'Authorization: Bearer abc123' https://example.com"), "Running curl"},
		{sh("/usr/bin/git push origin main"), "Running git push"},
		{sh("$(cat secret)"), "Running a command"},
		{sh("ls -la"), "Running ls"},
		{sh(""), "Running a command"},
		{harness.Tool{Kind: harness.ToolEdit, File: "/work/garden/notes.txt"}, "Editing notes.txt"},
		{harness.Tool{Kind: harness.ToolEdit}, "Editing a file"},
		{harness.Tool{Kind: harness.ToolWrite, File: "/work/garden/plan.md"}, "Writing plan.md"},
		{harness.Tool{Kind: harness.ToolRead, File: "/work/my notes.txt"}, "Reading a file"},
		{harness.Tool{Kind: harness.ToolRead, File: `C:\work\notes.txt`}, "Reading notes.txt"},
		{harness.Tool{Kind: harness.ToolSearch}, "Searching the files"},
		{harness.Tool{Kind: harness.ToolWeb}, "Searching the web"},
		{harness.Tool{Kind: harness.ToolAgent}, "Starting a helper"},
		{harness.Tool{Kind: harness.ToolPlan}, "Planning"},
		{harness.Tool{Kind: harness.ToolOther}, "Working"},
		{harness.Tool{Kind: "unknown"}, "Working"},
	} {
		if got := describeTool(c.tool, &beads.CLI{}); got != c.want {
			t.Errorf("%+v = %q, want %q", c.tool, got, c.want)
		}
	}
	long := strings.Repeat("a", 100) + ".txt"
	if got := describeTool(harness.Tool{Kind: harness.ToolEdit, File: long}, nil); len([]rune(got)) != maxActivity {
		t.Errorf("long name = %q", got)
	}
	// Each tracker names its own commands; another tracker's are commands.
	ag := fake.New().Tracker().(tracker.Agent)
	for cmd, want := range map[string]string{"fk show 1": "Using the fake tracker", "bd show tst-1": "Running bd"} {
		if got := describeTool(sh(cmd), ag); got != want {
			t.Errorf("%q with the fake = %q, want %q", cmd, got, want)
		}
	}
}
