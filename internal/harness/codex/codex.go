// Package codex is the harness for Codex (https://github.com/openai/codex),
// run as `codex exec --json`. Codex takes no input into a live run, so notes
// wait for the turn and go in a resumed session. docs/harnesses/codex.md
// describes the command and its limits.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// Name is the harness's registry key.
const Name = "codex"

// command is the program and fixed flags. --skip-git-repo-check because the
// bridge has already picked the repo; Codex's check guards a person running
// it in an unknown folder.
var command = []string{"codex", "exec", "--json", "--sandbox", "workspace-write", "--skip-git-repo-check"}

func init() {
	harness.Register(harness.Factory{Name: Name, New: func(env harness.Env) (harness.Harness, error) {
		cmd := command
		if len(env.Command) > 0 {
			cmd = env.Command
		}
		return &Codex{command: append([]string{}, cmd...)}, nil
	}})
}

// Codex drives `codex exec`.
type Codex struct {
	command []string
}

var _ harness.Harness = (*Codex)(nil)

func (*Codex) Info() harness.Info {
	return harness.Info{
		Name:            Name,
		Title:           "Codex",
		InstructionFile: "AGENTS.md",
		Budget:          harness.BudgetNone,
		Steer:           false,
		// What Codex sets in the shells it runs (codex-rs core exec_env.rs,
		// spawn.rs, unified_exec). CODEX_HOME and the API keys are the
		// user's own settings and stay.
		EnvMarkers: []string{
			"CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_TOOL_CALL_ID", "CODEX_VERSION",
			"CODEX_PERMISSION_PROFILE", "CODEX_CI", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED",
		},
	}
}

var versionNumber = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// Check runs `codex --version`, which prints "codex-cli 0.163.0".
func (c *Codex) Check(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, c.command[0], "--version").Output()
	if err != nil {
		return "", fmt.Errorf("codex --version: %w", err)
	}
	v := versionNumber.FindString(string(out))
	if v == "" {
		return "", fmt.Errorf("codex --version printed no version: %q", strings.TrimSpace(string(out)))
	}
	return v, nil
}

// Launch builds `codex exec ... -C <dir> [resume <id>] -- <prompt>`. The
// sandbox and -C go before `resume`, which accepts neither after it; a
// resume without -C runs in the session's saved folder (openai/codex#40702).
// The prompt follows `--` so one starting with a dash stays a prompt.
func (c *Codex) Launch(s harness.Spec) (harness.Launch, error) {
	if s.Dir == "" {
		return harness.Launch{}, errors.New("codex: no repo folder")
	}
	args := append(append([]string{}, c.command...), "-C", s.Dir)
	if harness.ValidSession(s.Session) {
		args = append(args, "resume", s.Session)
	}
	args = append(args, "--", s.Prompt)
	return harness.Launch{Args: args}, nil
}

// Note is never called: Codex does not steer.
func (*Codex) Note(string) []byte { return nil }

func (*Codex) NewDecoder() harness.Decoder { return &decoder{seen: map[string]bool{}, now: time.Now} }

// line is one line of `codex exec --json` (codex-rs exec/src/exec_events.rs).
type line struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"` // error
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"` // turn.failed
	Item *item `json:"item"`
}

type item struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Command string `json:"command"`
	Changes []struct {
		Path string `json:"path"`
		Kind string `json:"kind"` // add, delete or update
	} `json:"changes"`
	Tool string `json:"tool"` // collab_tool_call: spawn_agent, send_input, wait, close_agent
	Text string `json:"text"`
}

type decoder struct {
	seen  map[string]bool // items already reported as a tool call
	text  string          // the last agent message
	limit *harness.Limit  // a usage limit an error line named
	now   func() time.Time
}

func (d *decoder) Line(raw []byte) []harness.Event {
	var l line
	if json.Unmarshal(raw, &l) != nil {
		return nil
	}
	switch l.Type {
	case "thread.started":
		if l.ThreadID == "" {
			return nil
		}
		return []harness.Event{{Session: l.ThreadID}}
	case "item.started", "item.completed":
		if l.Item == nil {
			return nil
		}
		if l.Item.Type == "agent_message" && l.Type == "item.completed" {
			d.text = l.Item.Text
			return nil
		}
		// A file change comes only as completed; anything else is reported
		// when it starts, and once.
		if d.seen[l.Item.ID] && l.Item.ID != "" {
			return nil
		}
		evs := tools(l.Item)
		if len(evs) > 0 && l.Item.ID != "" {
			d.seen[l.Item.ID] = true
		}
		return evs
	case "error":
		// Not the end of a turn: Codex also reports a stream it is about to
		// retry this way. turn.failed follows a real failure.
		if lim := d.usageLimit(l.Message); lim != nil {
			d.limit = lim
			return []harness.Event{{Limit: lim}}
		}
		return nil
	case "turn.completed":
		return []harness.Event{{End: &harness.End{Text: d.text}}}
	case "turn.failed":
		msg := "The turn failed."
		if l.Error != nil && l.Error.Message != "" {
			msg = l.Error.Message
		}
		var evs []harness.Event
		if lim := d.usageLimit(msg); lim != nil && d.limit == nil {
			evs = append(evs, harness.Event{Limit: lim})
		}
		return append(evs, harness.Event{End: &harness.End{Error: true, Text: msg}})
	}
	return nil
}

// tools maps one item onto the tool calls it stands for.
func tools(it *item) []harness.Event {
	tool := func(t harness.Tool) []harness.Event { return []harness.Event{{Tool: &t}} }
	switch it.Type {
	case "command_execution":
		if it.Command == "" {
			return nil
		}
		return tool(harness.Tool{Kind: harness.ToolShell, Command: unwrapShell(it.Command)})
	case "file_change":
		var evs []harness.Event
		for _, c := range it.Changes {
			kind := harness.ToolEdit
			if c.Kind == "add" {
				kind = harness.ToolWrite
			}
			evs = append(evs, harness.Event{Tool: &harness.Tool{Kind: kind, File: c.Path}})
		}
		return evs
	case "mcp_tool_call":
		return tool(harness.Tool{Kind: harness.ToolOther})
	case "web_search":
		return tool(harness.Tool{Kind: harness.ToolWeb})
	case "collab_tool_call":
		if it.Tool == "spawn_agent" {
			return tool(harness.Tool{Kind: harness.ToolAgent})
		}
	case "todo_list":
		return tool(harness.Tool{Kind: harness.ToolPlan})
	}
	return nil
}

// unwrapShell drops the shell Codex wraps each command in ("/bin/bash -lc
// ls" or "/bin/zsh -lc 'go test ./...'"), so the activity line shows the
// command itself.
func unwrapShell(cmd string) string {
	prog, rest, ok := strings.Cut(cmd, " ")
	if !ok {
		return cmd
	}
	switch filepath.Base(prog) {
	case "bash", "zsh", "sh":
	default:
		return cmd
	}
	flag, body, ok := strings.Cut(rest, " ")
	if !ok || (flag != "-lc" && flag != "-c") {
		return cmd
	}
	if len(body) >= 2 && (body[0] == '\'' || body[0] == '"') && body[len(body)-1] == body[0] {
		body = body[1 : len(body)-1]
	}
	return body
}

var tryAgainAt = regexp.MustCompile(`(?i)try again at (.+?)\.?$`)

// usageLimit reads Codex's usage-limit message ("You've hit your usage
// limit. ... Try again at 3:15 PM." or "... at Oct 21st, 2026 3:15 PM."),
// whose time is in the machine's local zone.
func (d *decoder) usageLimit(msg string) *harness.Limit {
	if !strings.Contains(strings.ToLower(msg), "usage limit") {
		return nil
	}
	lim := &harness.Limit{}
	m := tryAgainAt.FindStringSubmatch(strings.TrimSpace(msg))
	if m == nil {
		return lim
	}
	at := ordinal.ReplaceAllString(m[1], "$1")
	now := d.now().In(time.Local)
	if t, err := time.ParseInLocation("3:04 PM", at, time.Local); err == nil {
		lim.ResetsAt = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, time.Local).Unix()
	} else if t, err := time.ParseInLocation("Jan 2, 2006 3:04 PM", at, time.Local); err == nil {
		lim.ResetsAt = t.Unix()
	}
	return lim
}

var ordinal = regexp.MustCompile(`(\d)(st|nd|rd|th)\b`)
