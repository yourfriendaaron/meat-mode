package claude

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/harness/harnesstest"
)

func TestConformance(t *testing.T) {
	harnesstest.Run(t, Name, []harnesstest.Fixture{
		{File: "testdata/claude-2.1.303-tool.jsonl", Source: "live, claude 2.1.303", Want: harnesstest.Want{
			Session: "7c3e9a52-4b1d-4f6e-8a20-1d5c9e7b3f40",
			Tools:   []harness.Tool{{Kind: harness.ToolShell, Command: "ls"}},
			End:     harness.End{Text: "done"},
		}},
		{File: "testdata/claude-2.1.303-error.jsonl", Source: "live, claude 2.1.303", Want: harnesstest.Want{
			Session: "4e1b7c93-0a2d-4f58-9c36-8d2e5a7f1b04",
			End: harness.End{Error: true, Text: "There's an issue with the selected model (claude-garden-9). " +
				"It may not exist or you may not have access to it. Run --model to pick a different model."},
		}},
		{File: "testdata/claude-2.1.303-limit.jsonl", Source: "from source, claude 2.1.303", Want: harnesstest.Want{
			Session: "2a6f0d8e-91c3-4b57-a4e2-6b8d0f1c9e35",
			Tools:   []harness.Tool{{Kind: harness.ToolRead, File: "/repo/notes.txt"}, {Kind: harness.ToolSearch}},
			Limit:   true,
			End:     harness.End{Error: true, Text: "You've hit your usage limit · resets 3pm"},
		}},
		{File: "testdata/claude-2.1.303-budget.jsonl", Source: "live, claude 2.1.303", Want: harnesstest.Want{
			Session: "8c3f1a27-6d4e-4b90-a5c2-1f7e9b3d0a58",
			Tools:   []harness.Tool{{Kind: harness.ToolRead, File: "/repo/notes.txt"}},
			End:     harness.End{Error: true, Budget: true},
		}},
	})
}

func TestLive(t *testing.T) { harnesstest.Live(t, Name) }

func TestLaunch(t *testing.T) {
	h, _ := New(harness.Env{Command: []string{"my-agent", "--flag", "value"}})
	l, _ := h.Launch(harness.Spec{Prompt: "Water the plants.", Session: "s-1", BudgetUSD: 0.5})
	if !slices.Equal(l.Args, []string{"my-agent", "--flag", "value", "--max-budget-usd", "0.5", "--resume", "s-1"}) {
		t.Errorf("args = %q", l.Args)
	}
	var m struct {
		Type    string `json:"type"`
		Message struct{ Role, Content string }
	}
	if err := json.Unmarshal(l.Stdin, &m); err != nil || m.Type != "user" || m.Message.Role != "user" || m.Message.Content != "Water the plants." {
		t.Errorf("stdin = %s, %v", l.Stdin, err)
	}
	h, _ = New(harness.Env{})
	l, _ = h.Launch(harness.Spec{Prompt: "x", BudgetUSD: 2})
	if l.Args[0] != "claude" || !slices.Contains(l.Args, "--replay-user-messages") || slices.Contains(l.Args, "--resume") {
		t.Errorf("default args = %q", l.Args)
	}
}

func TestTools(t *testing.T) {
	for _, c := range []struct {
		name, input string
		want        harness.Tool
	}{
		{"Bash", `{"command":"go test ./..."}`, harness.Tool{Kind: harness.ToolShell, Command: "go test ./..."}},
		{"Edit", `{"file_path":"/repo/notes.txt"}`, harness.Tool{Kind: harness.ToolEdit, File: "/repo/notes.txt"}},
		{"MultiEdit", `{"file_path":"a.go"}`, harness.Tool{Kind: harness.ToolEdit, File: "a.go"}},
		{"NotebookEdit", `{"notebook_path":"plants.ipynb"}`, harness.Tool{Kind: harness.ToolEdit, File: "plants.ipynb"}},
		{"Write", `{"file_path":"plan.md"}`, harness.Tool{Kind: harness.ToolWrite, File: "plan.md"}},
		{"Read", `{"file_path":"notes.txt"}`, harness.Tool{Kind: harness.ToolRead, File: "notes.txt"}},
		{"Glob", `{}`, harness.Tool{Kind: harness.ToolSearch}},
		{"LS", `{}`, harness.Tool{Kind: harness.ToolSearch}},
		{"WebSearch", `{}`, harness.Tool{Kind: harness.ToolWeb}},
		{"Agent", `{}`, harness.Tool{Kind: harness.ToolAgent}},
		{"Task", `{}`, harness.Tool{Kind: harness.ToolAgent}},
		{"TodoWrite", `{}`, harness.Tool{Kind: harness.ToolPlan}},
		{"mcp__thing__do", `{}`, harness.Tool{Kind: harness.ToolOther}},
		{"Bash", `"not an object"`, harness.Tool{Kind: harness.ToolShell}},
	} {
		if got := tool(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("%s %s = %+v, want %+v", c.name, c.input, got, c.want)
		}
	}
}

func TestDecoderEvents(t *testing.T) {
	d := decoder{}
	// A session id on a line with nothing else still comes through.
	if ev := d.Line([]byte(`{"type":"system","subtype":"init","session_id":"s-1"}`)); len(ev) != 1 || ev[0] != (harness.Event{Session: "s-1"}) {
		t.Errorf("init = %+v", ev)
	}
	if ev := d.Line([]byte(`{"type":"user","isReplay":true,"session_id":"s-1"}`)); len(ev) != 1 || ev[0].Read != 1 || ev[0].Session != "s-1" {
		t.Errorf("replay = %+v", ev)
	}
	for _, status := range []string{"allowed", "allowed_warning"} {
		if ev := d.Line([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"` + status + `"}}`)); len(ev) != 0 {
			t.Errorf("%s = %+v", status, ev)
		}
	}
	ev := d.Line([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1790000000}}`))
	if len(ev) != 1 || ev[0].Limit == nil || ev[0].Limit.ResetsAt != 1790000000 {
		t.Errorf("rejected = %+v", ev)
	}
	// A usage limit read from the end's text comes before the end.
	ev = d.Line([]byte(`{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached"}`))
	if len(ev) != 2 || ev[0].Limit == nil || ev[1].End == nil || !ev[1].End.Error {
		t.Errorf("limit result = %+v", ev)
	}
	ev = d.Line([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"The usage limit is fine."}`))
	if len(ev) != 1 || ev[0].End == nil || ev[0].End.Error {
		t.Errorf("good result = %+v", ev)
	}
}
