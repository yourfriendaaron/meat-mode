// Package pi is the harness for Pi (https://github.com/earendil-works/pi),
// run as `pi --mode rpc`: one long-lived process that takes JSON commands on
// stdin. docs/harnesses/pi.md describes what it runs, which limits it
// enforces and which it lacks.
package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// Name is the harness's registry key.
const Name = "pi"

// command is the program and the fixed flags. --no-extensions keeps a
// stray extension that never tears down from holding the process open
// after stdin closes.
var command = []string{"pi", "--mode", "rpc", "--no-extensions"}

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
		return &Pi{command: append([]string{}, cmd...), look: look}, nil
	}})
}

// Pi drives `pi --mode rpc`.
type Pi struct {
	command []string
	look    func(string) (string, error)
}

// Info declares the harness. Pi has no spending cap, so the bridge sums the
// cost it reports; it takes a steering command while a turn runs. AI_AGENT
// and PI_CODING_AGENT are what its entry points set for the process and
// every shell it starts.
func (p *Pi) Info() harness.Info {
	return harness.Info{
		Name:            Name,
		Title:           "Pi",
		InstructionFile: "AGENTS.md",
		Budget:          harness.BudgetMetered,
		Steer:           true,
		EnvMarkers:      []string{"PI_CODING_AGENT", "AI_AGENT"},
	}
}

// Check returns the version `pi --version` prints.
func (p *Pi) Check(ctx context.Context) (string, error) {
	bin, err := p.look(p.command[0])
	if err != nil {
		return "", fmt.Errorf("%s is not installed: %w", p.command[0], err)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", p.command[0], err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("%s --version printed nothing", p.command[0])
	}
	return v, nil
}

// Launch starts the process in Dir (Pi has no directory flag). RPC mode
// prints no session header, so Stdin asks for the state first, whose
// response names the session id, then sends the prompt. A resume passes
// --session-id, which opens exactly that id; --session also matches partial
// ids and may offer a fork, which a headless run cannot answer.
func (p *Pi) Launch(s harness.Spec) (harness.Launch, error) {
	args := append([]string{}, p.command...)
	if harness.ValidSession(s.Session) {
		args = append(args, "--session-id", s.Session)
	}
	stdin := cmd(map[string]any{"id": "state", "type": "get_state"})
	stdin = append(stdin, cmd(map[string]any{"id": "prompt", "type": "prompt", "message": s.Prompt})...)
	return harness.Launch{Args: args, Stdin: stdin, KeepStdin: true}, nil
}

// Note queues a steering message: Pi delivers it after the current turn's
// tool calls and before the next model call.
func (p *Pi) Note(text string) []byte {
	return cmd(map[string]any{"type": "steer", "message": text})
}

func (p *Pi) NewDecoder() harness.Decoder { return &decoder{} }

func cmd(m map[string]any) []byte {
	b, _ := json.Marshal(m)
	return append(b, '\n')
}

// usage is Pi's token and cost record; cost.total is dollars.
type usage struct {
	Cost struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

// message is the part of an assistant or user message the decoder reads.
type message struct {
	Role         string          `json:"role"`
	StopReason   string          `json:"stopReason"`
	ErrorMessage string          `json:"errorMessage"`
	Usage        usage           `json:"usage"`
	Content      json.RawMessage `json:"content"`
}

// text joins the text blocks of an assistant message.
func (m message) text() string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// line is the part of one stdout object the decoder reads: a command
// response or a session event.
type line struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    struct {
		SessionID string `json:"sessionId"`
	} `json:"data"`
	Message    message        `json:"message"`
	ToolName   string         `json:"toolName"`
	Args       map[string]any `json:"args"`
	Aborted    bool           `json:"aborted"`
	FinalError string         `json:"finalError"`
	Result     struct {
		Usage usage `json:"usage"`
	} `json:"result"`
}

type decoder struct {
	read     int    // user messages Pi has taken in
	last     string // the last assistant message's text
	stop     string // its stop reason
	errText  string // its error text, or the final error of a failed retry
	retryErr string
}

func (d *decoder) Line(raw []byte) []harness.Event {
	var l line
	if json.Unmarshal(bytes.TrimSpace(raw), &l) != nil {
		return nil
	}
	switch l.Type {
	case "response":
		switch {
		case l.Command == "get_state" && l.Success && l.Data.SessionID != "":
			return []harness.Event{{Session: l.Data.SessionID}}
		case l.Command == "prompt" && !l.Success:
			// The prompt was refused, so no run starts and nothing settles.
			return failed(l.Error)
		}
	case "message_end":
		switch l.Message.Role {
		case "user":
			d.read++
			return []harness.Event{{Read: d.read}}
		case "assistant":
			d.last, d.stop, d.errText, d.retryErr = l.Message.text(), l.Message.StopReason, l.Message.ErrorMessage, ""
			if c := l.Message.Usage.Cost.Total; c > 0 {
				return []harness.Event{{CostUSD: c}}
			}
		}
	case "compaction_end":
		// Compaction asks the model for a summary, which costs too.
		if c := l.Result.Usage.Cost.Total; c > 0 {
			return []harness.Event{{CostUSD: c}}
		}
	case "auto_retry_end":
		if l.FinalError != "" {
			d.retryErr = l.FinalError
		}
	case "tool_execution_start":
		return []harness.Event{{Tool: tool(l.ToolName, l.Args)}}
	case "agent_settled":
		return d.settled(l.Aborted)
	}
	return nil
}

// settled ends the turn. Pi's JSON streams show a failed or aborted reply
// only in its stop reason, so the last assistant message decides.
func (d *decoder) settled(aborted bool) []harness.Event {
	switch {
	case aborted || d.stop == "aborted":
		return failed(first(d.errText, "The run was aborted."))
	case d.stop == "error":
		return failed(first(d.errText, d.retryErr, "The model request failed."))
	case d.stop == "length":
		return failed(first(d.errText, "The reply hit the model's output limit."))
	case d.retryErr != "":
		return failed(d.retryErr)
	}
	return []harness.Event{{End: &harness.End{Text: d.last}}}
}

var limitRE = regexp.MustCompile(`(?i)usage limit|rate.?limit|too many requests|quota|\b429\b`)

// failed ends the turn with an error, after a usage limit event when the
// text reads as one.
func failed(text string) []harness.Event {
	var evs []harness.Event
	if limitRE.MatchString(text) {
		evs = append(evs, harness.Event{Limit: &harness.Limit{}})
	}
	return append(evs, harness.Event{End: &harness.End{Error: true, Text: text}})
}

func first(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// tool maps one of Pi's tool names and its arguments onto a tool kind.
// The built-ins are read, bash, edit, write, and the opt-in grep, find, ls.
func tool(name string, args map[string]any) *harness.Tool {
	str := func(k string) string { s, _ := args[k].(string); return s }
	switch name {
	case "bash":
		return &harness.Tool{Kind: harness.ToolShell, Command: str("command")}
	case "edit":
		return &harness.Tool{Kind: harness.ToolEdit, File: str("path")}
	case "write":
		return &harness.Tool{Kind: harness.ToolWrite, File: str("path")}
	case "read":
		return &harness.Tool{Kind: harness.ToolRead, File: str("path")}
	case "grep", "find", "ls":
		return &harness.Tool{Kind: harness.ToolSearch}
	}
	return &harness.Tool{Kind: harness.ToolOther}
}
