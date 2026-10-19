// Package opencode is the harness for OpenCode (https://github.com/anomalyco/opencode),
// run as `opencode run --format json`. docs/harnesses/opencode.md describes
// what it runs, which limits it enforces and which it lacks.
package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// Name is the harness's registry key.
const Name = "opencode"

// command is the program and the fixed flags: a headless run with one JSON
// object per line, approving every permission the config does not deny.
var command = []string{"opencode", "run", "--format", "json", "--auto"}

func init() {
	harness.Register(harness.Factory{Name: Name, New: func(env harness.Env) (harness.Harness, error) {
		cmd := command
		if len(env.Command) > 0 {
			cmd = env.Command
		}
		look := env.LookPath
		if look == nil {
			look = exec.LookPath
		}
		return &OpenCode{command: append([]string{}, cmd...), look: look}, nil
	}})
}

// OpenCode drives `opencode run`.
type OpenCode struct {
	command []string
	look    func(string) (string, error)
}

// Info declares the harness. OpenCode has no spending cap of its own, so the
// bridge sums the cost it reports, and it reads a prompt only at start, so a
// note waits for the turn to end. OPENCODE and OPENCODE_PID are what its CLI
// sets in its own process and every shell it starts; OPENCODE_CONFIG and the
// other OPENCODE_* settings are the user's and stay.
func (o *OpenCode) Info() harness.Info {
	return harness.Info{
		Name:            Name,
		Title:           "OpenCode",
		InstructionFile: "AGENTS.md",
		Budget:          harness.BudgetMetered,
		Steer:           false,
		EnvMarkers:      []string{"OPENCODE", "OPENCODE_PID"},
	}
}

// Check returns the version `opencode --version` prints.
func (o *OpenCode) Check(ctx context.Context) (string, error) {
	bin, err := o.look(o.command[0])
	if err != nil {
		return "", fmt.Errorf("%s is not installed: %w", o.command[0], err)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", o.command[0], err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("%s --version printed nothing", o.command[0])
	}
	return v, nil
}

// Launch runs the prompt in Dir. --dir makes the working directory explicit,
// because opencode takes its root from $PWD, which a bridge started from
// another directory may carry stale. A resume is -s <id>; -c would pick the
// newest session in any project.
func (o *OpenCode) Launch(s harness.Spec) (harness.Launch, error) {
	args := append([]string{}, o.command...)
	if s.Dir != "" {
		args = append(args, "--dir", s.Dir)
	}
	if harness.ValidSession(s.Session) {
		args = append(args, "-s", s.Session)
	}
	args = append(args, s.Prompt)
	return harness.Launch{Args: args}, nil
}

// Note is never called: Info.Steer is false.
func (o *OpenCode) Note(string) []byte { return nil }

func (o *OpenCode) NewDecoder() harness.Decoder { return &decoder{} }

// line is the part of one stdout object the decoder reads. opencode writes
// {type, timestamp, sessionID, ...} with a part for the step, text and tool
// lines and an error for a failed session.
type line struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      struct {
		Type   string  `json:"type"`
		Tool   string  `json:"tool"`
		Text   string  `json:"text"`
		Reason string  `json:"reason"`
		Cost   float64 `json:"cost"`
		State  struct {
			Input map[string]any `json:"input"`
		} `json:"state"`
	} `json:"part"`
	Error struct {
		Name string `json:"name"`
		Data struct {
			Message         string            `json:"message"`
			StatusCode      int               `json:"statusCode"`
			ResponseHeaders map[string]string `json:"responseHeaders"`
		} `json:"data"`
	} `json:"error"`
}

type decoder struct {
	text string // the last text block of the current step
}

func (d *decoder) Line(raw []byte) []harness.Event {
	var l line
	if json.Unmarshal(bytes.TrimSpace(raw), &l) != nil {
		return nil
	}
	var evs []harness.Event
	switch l.Type {
	case "step_start":
		d.text = ""
		evs = append(evs, harness.Event{})
	case "text":
		d.text = l.Part.Text
		evs = append(evs, harness.Event{})
	case "tool_use":
		evs = append(evs, harness.Event{Tool: tool(l.Part.Tool, l.Part.State.Input)})
	case "step_finish":
		if l.Part.Cost > 0 {
			evs = append(evs, harness.Event{CostUSD: l.Part.Cost})
		}
		if l.Part.Reason == "stop" {
			evs = append(evs, harness.Event{End: &harness.End{Text: d.text}})
		}
		if len(evs) == 0 {
			evs = append(evs, harness.Event{})
		}
	case "error":
		msg := l.Error.Data.Message
		if msg == "" {
			msg = l.Error.Name
		}
		if limitRE.MatchString(msg) || l.Error.Data.StatusCode == 429 {
			evs = append(evs, harness.Event{Limit: &harness.Limit{ResetsAt: resetsAt(l.Error.Data.ResponseHeaders)}})
		}
		evs = append(evs, harness.Event{End: &harness.End{Error: true, Text: msg}})
	default:
		return nil
	}
	// The session id rides on the first event; a line that says nothing else
	// still reports it.
	if l.SessionID != "" {
		evs[0].Session = l.SessionID
	}
	if evs[0] == (harness.Event{}) {
		return nil
	}
	return evs
}

var limitRE = regexp.MustCompile(`(?i)usage limit|rate.?limit|too many requests|quota`)

// resetsAt reads a retry-after header in seconds as a Unix time.
func resetsAt(h map[string]string) int64 {
	for k, v := range h {
		if strings.EqualFold(k, "retry-after") {
			if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && n > 0 {
				return time.Now().Add(time.Duration(n * float64(time.Second))).Unix()
			}
		}
	}
	return 0
}

// tool maps one of opencode's tool ids and its input onto a tool kind.
// The ids are those of packages/opencode/src/tool: bash, edit, write, read,
// glob, grep, apply_patch, webfetch, websearch, task, todowrite, and
// skill, lsp and question for the rest.
func tool(name string, in map[string]any) *harness.Tool {
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch name {
	case "bash":
		return &harness.Tool{Kind: harness.ToolShell, Command: str("command")}
	case "edit":
		return &harness.Tool{Kind: harness.ToolEdit, File: str("filePath")}
	case "write":
		return &harness.Tool{Kind: harness.ToolWrite, File: str("filePath")}
	case "read":
		return &harness.Tool{Kind: harness.ToolRead, File: str("filePath")}
	case "apply_patch":
		return &harness.Tool{Kind: harness.ToolEdit, File: patchFile(str("patchText"))}
	case "glob", "grep", "list":
		return &harness.Tool{Kind: harness.ToolSearch}
	case "webfetch", "websearch":
		return &harness.Tool{Kind: harness.ToolWeb}
	case "task":
		return &harness.Tool{Kind: harness.ToolAgent}
	case "todowrite":
		return &harness.Tool{Kind: harness.ToolPlan}
	}
	return &harness.Tool{Kind: harness.ToolOther}
}

var patchHeader = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+?)\s*$`)

// patchFile is the first file an apply_patch text names.
func patchFile(patch string) string {
	if m := patchHeader.FindStringSubmatch(patch); m != nil {
		return m[1]
	}
	return ""
}
