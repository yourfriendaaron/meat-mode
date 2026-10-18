package bridge

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAgentConfigFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := AgentConfigFromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Command[0] != "claude" || c.Name != "agent" || c.Timeout != 30*time.Minute || c.BudgetUSD != 2 ||
		c.RunsPerTask != 10 || c.Concurrent != 2 {
		t.Errorf("defaults = %+v", c)
	}
	c, err = AgentConfigFromEnv(env(map[string]string{
		"MEAT_MODE_AGENT_COMMAND": "  my-agent  --flag value ", "MEAT_MODE_AGENT_NAME": "helper",
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
	if got := c.args("s-1"); !slices.Equal(got, []string{"--flag", "value", "--max-budget-usd", "0.5", "--resume", "s-1"}) {
		t.Errorf("args = %q", got)
	}
	if got := c.args(""); slices.Contains(got, "--resume") {
		t.Errorf("args without a session = %q", got)
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
		"MEAT_MODE_BRIDGE_TOKEN=secret", "BEADS_ACTOR=someone",
	}, "planner")
	want := []string{"PATH=/usr/bin", "HOME=/srv/alex", "CLAUDE_CONFIG_DIR=/c", "BEADS_ACTOR=planner"}
	if !slices.Equal(got, want) {
		t.Errorf("env = %q, want %q", got, want)
	}
}

func TestAgentPrompt(t *testing.T) {
	p := agentPrompt("tst-1")
	for _, want := range []string{"bd show tst-1", "bd close tst-1", "bd comment tst-1", "instruction files", "needs the user", "Never send, post, spend, delete or push"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	var line struct {
		Type    string `json:"type"`
		Message struct {
			Role, Content string
		} `json:"message"`
	}
	if err := json.Unmarshal(userLine(p), &line); err != nil || line.Type != "user" || line.Message.Role != "user" || line.Message.Content != p {
		t.Errorf("user line = %+v, %v", line, err)
	}
}

func TestDescribeTool(t *testing.T) {
	for _, c := range []struct {
		name, input, want string
	}{
		{"Bash", `{"command":"bd show tst-1"}`, "Reading the bead"},
		{"Bash", `{"command":"cd /work/garden && bd comment tst-1 \"Which plants?\""}`, "Writing a note"},
		{"Bash", `{"command":"bd comments add tst-1 hi"}`, "Writing a note"},
		{"Bash", `{"command":"bd close tst-1"}`, "Closing the bead"},
		{"Bash", `{"command":"bd update tst-1 --claim"}`, "Updating the bead"},
		{"Bash", `{"command":"go test ./internal/... -run X"}`, "Running go test"},
		{"Bash", `{"command":"TOKEN=abc123 curl -H 'Authorization: Bearer abc123' https://example.com"}`, "Running curl"},
		{"Bash", `{"command":"/usr/bin/git push origin main"}`, "Running git push"},
		{"Bash", `{"command":"$(cat secret)"}`, "Running a command"},
		{"Bash", `{"command":"ls -la"}`, "Running ls"},
		{"Edit", `{"file_path":"/work/garden/notes.txt"}`, "Editing notes.txt"},
		{"Write", `{"file_path":"/work/garden/plan.md"}`, "Writing plan.md"},
		{"Read", `{"file_path":"/work/my notes.txt"}`, "Reading a file"},
		{"Grep", `{"pattern":"password"}`, "Searching the files"},
		{"WebFetch", `{"url":"https://example.com/?key=abc"}`, "Searching the web"},
		{"mcp__thing__do", `{}`, "Working"},
	} {
		if got := describeTool(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.name, c.input, got, c.want)
		}
	}
	long := strings.Repeat("a", 100) + ".txt"
	if got := describeTool("Edit", json.RawMessage(`{"file_path":"`+long+`"}`)); len([]rune(got)) != maxActivity {
		t.Errorf("long name = %q", got)
	}
}
