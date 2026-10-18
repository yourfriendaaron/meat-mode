package bridge

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultAgentCommand is Claude Code headless, speaking stream-json on stdin
// and stdout. The bridge adds --max-budget-usd and, to continue a bead's
// earlier conversation, --resume.
const DefaultAgentCommand = "claude -p --input-format stream-json --output-format stream-json --verbose " +
	"--replay-user-messages --permission-mode auto --permission-prompts none"

// AgentConfig is how the bridge runs agents, from the MEAT_MODE_AGENT_*
// variables.
type AgentConfig struct {
	// Command is the agent program and its arguments, split on whitespace
	// with no shell. It must speak Claude Code's stream-json protocol and
	// accept --max-budget-usd and --resume.
	Command []string
	// Name is shown for the agent when the bead's creator is not one.
	Name        string
	Timeout     time.Duration // wall clock for one run
	BudgetUSD   float64       // spending cap for one run
	RunsPerTask int           // lifetime runs per bead
	Concurrent  int           // runs live at once on this machine
}

// DefaultAgentConfig returns the defaults every variable falls back to.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Command:     strings.Fields(DefaultAgentCommand),
		Name:        "agent",
		Timeout:     30 * time.Minute,
		BudgetUSD:   2,
		RunsPerTask: 10,
		Concurrent:  2,
	}
}

// AgentConfigFromEnv reads MEAT_MODE_AGENT_COMMAND, _NAME, _TIMEOUT,
// _BUDGET_USD, _RUNS_PER_TASK and _CONCURRENT over the defaults.
func AgentConfigFromEnv(getenv func(string) string) (AgentConfig, error) {
	c := DefaultAgentConfig()
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
	return c, nil
}

// args returns the command line for one run.
func (c AgentConfig) args(session string) []string {
	out := append([]string{}, c.Command[1:]...)
	out = append(out, "--max-budget-usd", strconv.FormatFloat(c.BudgetUSD, 'f', -1, 64))
	if session != "" {
		out = append(out, "--resume", session)
	}
	return out
}

// agentPrompt is the first message of every run. The bead's own notes carry
// the task and the user's latest words, so the prompt only says where to
// look and the rules.
func agentPrompt(id string) string {
	return "You are working on the task in bead " + id + ". Run `bd show " + id + "` and read its comments for the latest state. " +
		"Then do the work and close the bead with `bd close " + id + "` when it is done, or reply with `bd comment " + id + " \"...\"`. " +
		"Follow this repository's own instruction files. " +
		"When something is denied or needs the user, stop and say what you need in a comment. " +
		"Never send, post, spend, delete or push anything for the user."
}

// followUp is the stdin message for a note the user added to a live run.
func followUp(id, text string) string {
	return "New note from the user on bead " + id + ":\n\n" + text
}

// userLine is one stream-json user message.
func userLine(text string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]string{"role": "user", "content": text},
	})
	return append(b, '\n')
}

// agentEnv is the agent's environment: the bridge's own, without its
// MEAT_MODE_* settings (the hub token among them) and without the variables
// of a Claude Code session the bridge may have been started from, plus
// BEADS_ACTOR so the agent's notes carry its name.
func agentEnv(env []string, actor string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "MEAT_MODE_") || k == "CLAUDECODE" || strings.HasPrefix(k, "CLAUDE_CODE_") || k == "BEADS_ACTOR" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "BEADS_ACTOR="+actor)
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
// than its first word or two, nor any path but a file's base name.
func describeTool(name string, input json.RawMessage) string {
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Path     string `json:"notebook_path"`
	}
	_ = json.Unmarshal(input, &in)
	file := fileName(in.FilePath)
	if file == "" {
		file = fileName(in.Path)
	}
	switch name {
	case "Bash":
		return describeCommand(in.Command)
	case "Edit", "MultiEdit", "NotebookEdit":
		return orWork("Editing ", file, "Editing a file")
	case "Write":
		return orWork("Writing ", file, "Writing a file")
	case "Read":
		return orWork("Reading ", file, "Reading a file")
	case "Glob", "Grep", "LS":
		return "Searching the files"
	case "WebFetch", "WebSearch":
		return "Searching the web"
	case "Task", "Agent":
		return "Starting a helper"
	case "TodoWrite":
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
// `cd dir &&` and variable assignments are skipped; bd commands are named
// for what they do to the bead.
func describeCommand(cmd string) string {
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
	second := ""
	if len(words) > 1 && plainWord.MatchString(words[1]) && !strings.HasPrefix(words[1], "-") {
		second = words[1]
	}
	if first == "bd" {
		switch second {
		case "comments":
			if len(words) > 2 && words[2] == "add" {
				return "Writing a note"
			}
			return "Reading the bead"
		case "show", "list", "ready", "search":
			return "Reading the bead"
		case "comment":
			return "Writing a note"
		case "close":
			return "Closing the bead"
		}
		return "Updating the bead"
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
