// Package claude runs Claude Code headless: `claude -p` speaking
// stream-json on stdin and stdout. It is the default harness
// (docs/harnesses/claude.md).
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// Name is the harness's registry key.
const Name = "claude"

// DefaultCommand is Claude Code headless, speaking stream-json on stdin and
// stdout and echoing each message it reads. Launch adds --max-budget-usd
// and, to continue a task's earlier conversation, --resume.
const DefaultCommand = "claude -p --input-format stream-json --output-format stream-json --verbose " +
	"--replay-user-messages --permission-mode auto --permission-prompts none"

func init() {
	harness.Register(harness.Factory{Name: Name, New: New})
}

// Claude is the Claude Code harness.
type Claude struct {
	command []string
	look    func(string) (string, error)
}

// New returns the harness, with e.Command replacing DefaultCommand when set.
func New(e harness.Env) (harness.Harness, error) {
	c := &Claude{command: strings.Fields(DefaultCommand), look: e.LookPath}
	if len(e.Command) > 0 {
		c.command = append([]string{}, e.Command...)
	}
	if c.look == nil {
		c.look = exec.LookPath
	}
	return c, nil
}

func (*Claude) Info() harness.Info {
	return harness.Info{
		Name: Name, Title: "Claude Code", InstructionFile: "CLAUDE.md",
		Budget: harness.BudgetNative, Steer: true,
		EnvMarkers: []string{"CLAUDECODE", "CLAUDE_CODE_*"},
	}
}

var versionNumber = regexp.MustCompile(`\d+(\.\d+)+`)

// Check runs `<program> --version`, which prints something like
// "2.1.303 (Claude Code)".
func (c *Claude) Check(ctx context.Context) (string, error) {
	path, err := c.look(c.command[0])
	if err != nil {
		return "", fmt.Errorf("%s not found: %w", c.command[0], err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", c.command[0], err)
	}
	v := versionNumber.FindString(string(out))
	if v == "" {
		return "", errors.New(c.command[0] + " --version printed no version")
	}
	return v, nil
}

func (c *Claude) Launch(s harness.Spec) (harness.Launch, error) {
	args := append([]string{}, c.command...)
	args = append(args, "--max-budget-usd", strconv.FormatFloat(s.BudgetUSD, 'f', -1, 64))
	if harness.ValidSession(s.Session) {
		args = append(args, "--resume", s.Session)
	}
	return harness.Launch{Args: args, Stdin: userLine(s.Prompt), KeepStdin: true}, nil
}

func (*Claude) Note(text string) []byte { return userLine(text) }

func (*Claude) NewDecoder() harness.Decoder { return decoder{} }

// userLine is one stream-json user message.
func userLine(text string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]string{"role": "user", "content": text},
	})
	return append(b, '\n')
}

// line is the part of a stream-json line the decoder reads.
type line struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	IsReplay  bool   `json:"isReplay"`
	Message   *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	RateLimit *struct {
		Status   string `json:"status"`
		ResetsAt int64  `json:"resetsAt"`
	} `json:"rate_limit_info"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

type decoder struct{}

func (decoder) Line(raw []byte) []harness.Event {
	var m line
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var out []harness.Event
	switch m.Type {
	case "user":
		if m.IsReplay {
			out = append(out, harness.Event{Read: 1})
		}
	case "assistant":
		if m.Message == nil {
			break
		}
		var content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(m.Message.Content, &content)
		for _, c := range content {
			if c.Type == "tool_use" {
				t := tool(c.Name, c.Input)
				out = append(out, harness.Event{Tool: &t})
			}
		}
	case "rate_limit_event":
		if m.RateLimit != nil && m.RateLimit.Status != "allowed" && m.RateLimit.Status != "allowed_warning" {
			out = append(out, harness.Event{Limit: &harness.Limit{ResetsAt: max(0, m.RateLimit.ResetsAt)}})
		}
	case "result":
		if m.IsError && looksLikeUsageLimit(m.Result) {
			out = append(out, harness.Event{Limit: &harness.Limit{}})
		}
		out = append(out, harness.Event{End: &harness.End{
			Error: m.IsError, Budget: strings.Contains(m.Subtype, "budget"), Text: m.Result,
		}})
	}
	if m.SessionID != "" {
		if len(out) == 0 {
			out = append(out, harness.Event{})
		}
		out[0].Session = m.SessionID
	}
	return out
}

// tool maps one of Claude Code's tool calls onto a harness tool kind.
func tool(name string, input json.RawMessage) harness.Tool {
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Notebook string `json:"notebook_path"`
	}
	_ = json.Unmarshal(input, &in)
	file := in.FilePath
	if file == "" {
		file = in.Notebook
	}
	switch name {
	case "Bash":
		return harness.Tool{Kind: harness.ToolShell, Command: in.Command}
	case "Edit", "MultiEdit", "NotebookEdit":
		return harness.Tool{Kind: harness.ToolEdit, File: file}
	case "Write":
		return harness.Tool{Kind: harness.ToolWrite, File: file}
	case "Read":
		return harness.Tool{Kind: harness.ToolRead, File: file}
	case "Glob", "Grep", "LS":
		return harness.Tool{Kind: harness.ToolSearch}
	case "WebFetch", "WebSearch":
		return harness.Tool{Kind: harness.ToolWeb}
	case "Task", "Agent":
		return harness.Tool{Kind: harness.ToolAgent}
	case "TodoWrite":
		return harness.Tool{Kind: harness.ToolPlan}
	}
	return harness.Tool{Kind: harness.ToolOther}
}

// looksLikeUsageLimit reports whether a failed turn's text reads as a usage
// limit, for the ends that come without a rejected rate_limit_event.
func looksLikeUsageLimit(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "usage limit") || strings.Contains(s, "limit reached") || strings.Contains(s, "hit your limit")
}
