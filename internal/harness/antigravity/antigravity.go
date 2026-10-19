// Package antigravity is the harness for Antigravity CLI
// (https://antigravity.google/docs/cli/overview/), the `agy` program, run
// headless with stream-json in and out. Prompts and notes are user lines on
// stdin, and each runs as its own turn in the same process.
// docs/harnesses/antigravity.md describes the command and its limits.
package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// Name is the harness's registry key.
const Name = "antigravity"

// command is the program and fixed flags. --print-timeout 0 waits for the
// turn however long it takes, so the bridge's time limit is the one that
// stops a run; agy's docs still name 5m as the default, and when its own
// timeout fires it returns a partial answer with exit 0.
var command = []string{
	"agy", "--input-format", "stream-json", "--output-format", "stream-json",
	"--dangerously-skip-permissions", "--print-timeout", "0",
}

func init() {
	harness.Register(harness.Factory{Name: Name, New: func(env harness.Env) (harness.Harness, error) {
		cmd := command
		if len(env.Command) > 0 {
			cmd = env.Command
		}
		return &Antigravity{command: append([]string{}, cmd...)}, nil
	}})
}

// Antigravity drives `agy`.
type Antigravity struct {
	command []string
}

var _ harness.Harness = (*Antigravity)(nil)

func (*Antigravity) Info() harness.Info {
	return harness.Info{
		Name:  Name,
		Title: "Antigravity CLI",
		// agy reads both AGENTS.md and GEMINI.md; its docs recommend AGENTS.md.
		InstructionFile: "AGENTS.md",
		Budget:          harness.BudgetNone,
		Steer:           true,
		// agy documents none; these are what the helper scripts built into
		// agy 1.3.6 read from the environment it gives them.
		EnvMarkers: []string{
			"ANTIGRAVITY_CONVERSATION_ID", "ANTIGRAVITY_LS_ADDRESS", "ANTIGRAVITY_CSRF_TOKEN",
			"ANTIGRAVITY_SIDECAR_WEB_PORT", "ANTIGRAVITY_AGENTAPI_EXE",
		},
	}
}

var versionNumber = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// Check runs `agy --version`.
func (a *Antigravity) Check(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, a.command[0], "--version").Output()
	if err != nil {
		return "", fmt.Errorf("agy --version: %w", err)
	}
	v := versionNumber.FindString(string(out))
	if v == "" {
		return "", fmt.Errorf("agy --version printed no version: %q", strings.TrimSpace(string(out)))
	}
	return v, nil
}

// Launch builds `agy ... [--conversation <id>]` with the prompt as the first
// user line on stdin; in stream-json input mode agy drops a prompt given as
// an argument.
func (a *Antigravity) Launch(s harness.Spec) (harness.Launch, error) {
	if s.Dir == "" {
		return harness.Launch{}, errors.New("antigravity: no repo folder")
	}
	args := append([]string{}, a.command...)
	if harness.ValidSession(s.Session) {
		args = append(args, "--conversation", s.Session)
	}
	return harness.Launch{Args: args, Stdin: userLine(plain(s.Prompt)), KeepStdin: true}, nil
}

// Note is one more user line. agy runs it as the next turn once the current
// one ends, as it does for prompts piped in together.
func (*Antigravity) Note(text string) []byte { return userLine(plain(text)) }

// plain keeps a message that starts with "/" from being run as an agy
// command. agy answers those itself and, with stream-json input, ends the
// turn with an error ("/help is answered by the CLI itself"). It trims
// spaces, so the text starts with a zero-width space instead, which agy
// passes to the model. --disable-slash-commands would also work but removes
// skills from the run.
func plain(text string) string {
	if strings.HasPrefix(text, "/") {
		return "\u200b" + text
	}
	return text
}

func userLine(text string) []byte {
	b, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": text}})
	return append(b, '\n')
}

func (*Antigravity) NewDecoder() harness.Decoder {
	return &decoder{seen: map[int]bool{}, now: time.Now}
}

// line is one line of `agy --output-format stream-json`.
type line struct {
	Event          string  `json:"event"`
	ConversationID string  `json:"conversation_id"` // init
	Step           *step   `json:"step_update"`
	Result         *result `json:"result"`
}

type step struct {
	ConversationID string `json:"conversation_id"`
	Index          int    `json:"step_index"`
	State          string `json:"state"` // ACTIVE or DONE
	Type           string `json:"step_type"`
	ToolName       string `json:"tool_name"`
	ToolInfo       *struct {
		Name       string         `json:"name"`
		Parameters map[string]any `json:"parameters"`
	} `json:"tool_info"`
}

type result struct {
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"` // SUCCESS, ERROR, CANCELED, INTERRUPTED, ...
	Response       string `json:"response"`
	Error          string `json:"error"`
}

type decoder struct {
	seen map[int]bool // tool steps already reported, by step index
	now  func() time.Time
}

func (d *decoder) Line(raw []byte) []harness.Event {
	var l line
	if json.Unmarshal(raw, &l) != nil {
		return nil
	}
	switch {
	case l.Event == "init":
		if l.ConversationID == "" {
			return nil
		}
		return []harness.Event{{Session: l.ConversationID}}
	case l.Event == "step_update" && l.Step != nil:
		return d.step(l.Step)
	case l.Event == "result" && l.Result != nil:
		return d.result(l.Result)
	}
	return nil
}

func (d *decoder) step(s *step) []harness.Event {
	switch s.Type {
	case "user_input":
		// agy's echo of each user line: the prompt, then each note.
		if s.State != "DONE" {
			return nil
		}
		return []harness.Event{{Session: s.ConversationID, Read: 1}}
	case "tool":
		if d.seen[s.Index] {
			return nil
		}
		name := s.ToolName
		var params map[string]any
		if s.ToolInfo != nil {
			if name == "" {
				name = s.ToolInfo.Name
			}
			params = s.ToolInfo.Parameters
		}
		// A running step may come before its parameters; wait for them.
		if name == "" || (params == nil && s.State != "DONE") {
			return nil
		}
		d.seen[s.Index] = true
		t := toolCall(name, params)
		if t == nil {
			return nil
		}
		return []harness.Event{{Session: s.ConversationID, Tool: t}}
	}
	return nil
}

func (d *decoder) result(r *result) []harness.Event {
	var evs []harness.Event
	end := &harness.End{Text: strings.TrimSpace(r.Response)}
	switch {
	case r.Status != "SUCCESS":
		end.Error = true
		end.Text = strings.TrimSpace(r.Error)
		if end.Text == "" {
			end.Text = "The turn ended with status " + r.Status + "."
		}
		if lim := d.usageLimit(r.Error); lim != nil {
			evs = append(evs, harness.Event{Limit: lim})
		}
	case end.Text == "":
		// agy can end a turn SUCCESS with no answer when it lost the turn
		// (google-antigravity/antigravity-cli#902, #1044).
		end.Error = true
		end.Text = "The agent ended the turn without an answer."
	}
	e := harness.Event{End: end}
	if r.ConversationID != "" {
		e.Session = r.ConversationID
	}
	return append(evs, e)
}

// toolCall maps one of agy's tools onto a tool kind. The names are those
// agy 1.4.2 lists in its init line; a live run confirmed view_file (with
// AbsolutePath) and run_command (with CommandLine), and listed a folder
// with run_command. Tools that only wait on, check or feed an earlier call
// are not activity of their own. Any other name is a generic activity.
func toolCall(name string, params map[string]any) *harness.Tool {
	file := firstString(params, "TargetFile", "AbsolutePath", "FilePath", "File", "Path")
	switch name {
	case "run_command":
		return &harness.Tool{Kind: harness.ToolShell, Command: firstString(params, "CommandLine", "Command")}
	case "write_to_file":
		return &harness.Tool{Kind: harness.ToolWrite, File: file}
	case "replace_file_content", "multi_replace_file_content", "sed_file", "notebook_edit":
		return &harness.Tool{Kind: harness.ToolEdit, File: file}
	case "view_file":
		return &harness.Tool{Kind: harness.ToolRead, File: file}
	case "grep_search", "find_by_name", "list_dir":
		return &harness.Tool{Kind: harness.ToolSearch}
	case "read_url_content", "search_web":
		return &harness.Tool{Kind: harness.ToolWeb}
	case "invoke_subagent", "browser_subagent":
		return &harness.Tool{Kind: harness.ToolAgent}
	case "ask_permission", "command_status", "send_command_input", "wait", "wait_5_seconds", "finish":
		return nil
	}
	if strings.Contains(name, "browser") {
		// open_browser_url, read_browser_page, browser_click_element and the
		// rest of agy's browser tools.
		return &harness.Tool{Kind: harness.ToolWeb}
	}
	return &harness.Tool{Kind: harness.ToolOther, File: file}
}

func firstString(params map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := params[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

var resetsIn = regexp.MustCompile(`(?i)resets in ((?:\d+h)?(?:\d+m)?(?:\d+s)?)`)

// usageLimit reads agy's quota errors, such as "RESOURCE_EXHAUSTED (code
// 429): Individual quota reached. ... Resets in 2h39m52s." or "Your AI
// credits balance is too low to continue."
func (d *decoder) usageLimit(msg string) *harness.Limit {
	low := strings.ToLower(msg)
	if !strings.Contains(low, "resource_exhausted") && !strings.Contains(low, "quota") &&
		!strings.Contains(low, "credits balance is too low") {
		return nil
	}
	lim := &harness.Limit{}
	if m := resetsIn.FindStringSubmatch(msg); m != nil && m[1] != "" {
		if dur, err := time.ParseDuration(m[1]); err == nil {
			lim.ResetsAt = d.now().Add(dur).Unix()
		}
	}
	return lim
}
