package antigravity

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/harness/harnesstest"
)

func TestConformance(t *testing.T) {
	harnesstest.Run(t, Name, []harnesstest.Fixture{
		{File: "testdata/antigravity-1.3.6-steer.jsonl", Source: "live, agy 1.3.6", Want: harnesstest.Want{
			Session: "3f2a9c1e-5b7d-4e8a-9c0b-1d2e3f4a5b6c",
			Tools:   []harness.Tool{{Kind: harness.ToolShell, Command: "sleep 10 && ls"}},
			End:     harness.End{Text: "blue"},
		}},
		{File: "testdata/antigravity-1.4.2-read.jsonl", Source: "live, agy 1.4.2", Want: harnesstest.Want{
			Session: "6b2d8f14-7c3a-4e95-8a1b-0c4d7e9f2a36",
			Tools: []harness.Tool{
				{Kind: harness.ToolRead, File: "/repo/notes.txt"},
				{Kind: harness.ToolShell, Command: "ls -la garden"},
			},
			End: harness.End{Text: "done"},
		}},
		{File: "testdata/antigravity-1.4.2-interrupted.jsonl", Source: "live, agy 1.4.2", Want: harnesstest.Want{
			Session: "9e7c5a31-2f4b-4d86-b0a9-3c1e5f7d8b42",
			Tools:   []harness.Tool{{Kind: harness.ToolShell, Command: "sleep 30"}},
			End:     harness.End{Error: true, Text: "interrupted"},
		}},
		{File: "testdata/antigravity-1.4.2-tool.jsonl", Source: "from source, agy 1.4.2", Want: harnesstest.Want{
			Session: "5f1c2b9e-3a47-4d8e-9b10-2c6f7a8d9e01",
			Tools: []harness.Tool{
				{Kind: harness.ToolRead, File: "/repo/notes.txt"},
				{Kind: harness.ToolShell, Command: "bd close task-1"},
				{Kind: harness.ToolWrite, File: "/repo/garden.md"},
			},
			End: harness.End{Text: "Used the blue watering can."},
		}},
		{File: "testdata/antigravity-1.4.2-limit.jsonl", Source: "from source, agy 1.4.2", Want: harnesstest.Want{
			Session: "8a3d4e5f-6b7c-4d8e-a9f0-1b2c3d4e5f60", Limit: true, End: harness.End{Error: true},
		}},
		{File: "testdata/antigravity-1.4.2-canceled.jsonl", Source: "from source, agy 1.4.2", Want: harnesstest.Want{
			Session: "2e4f6a8c-0b1d-4e3f-8a5c-7d9e1f2a3b4c",
			Tools:   []harness.Tool{{Kind: harness.ToolShell, Command: "go test ./..."}},
			End:     harness.End{Error: true, Text: "The turn ended with status CANCELED."},
		}},
	})
}

func TestLive(t *testing.T) { harnesstest.Live(t, Name) }

func TestLaunch(t *testing.T) {
	a := &Antigravity{command: command}
	l, err := a.Launch(harness.Spec{Dir: "/repo", Prompt: "Water the plants.", Session: "5f1c2b9e-3a47-4d8e-9b10-2c6f7a8d9e01"})
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]string{}, command...), "--conversation", "5f1c2b9e-3a47-4d8e-9b10-2c6f7a8d9e01")
	if !slices.Equal(l.Args, want) || !l.KeepStdin {
		t.Fatalf("Args = %q, keep %v", l.Args, l.KeepStdin)
	}
	var msg struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(l.Stdin, &msg); err != nil || msg.Event != "user" || msg.Message.Content != "Water the plants." {
		t.Fatalf("Stdin = %q", l.Stdin)
	}
}

// A prompt or note that starts with "/" goes in after a zero-width space, so
// agy sends it to the model instead of answering it as a command.
func TestSlashIsPlain(t *testing.T) {
	a := &Antigravity{command: command}
	l, _ := a.Launch(harness.Spec{Dir: "/repo", Prompt: "/tmp holds the seed list."})
	if want := "{\"event\":\"user\",\"message\":{\"content\":\"\u200b/tmp holds the seed list.\"}}\n"; string(l.Stdin) != want {
		t.Fatalf("Stdin = %q", l.Stdin)
	}
	if got := string(a.Note("/help")); got != "{\"event\":\"user\",\"message\":{\"content\":\"\u200b/help\"}}\n" {
		t.Fatalf("Note = %q", got)
	}
	if got := plain("Water the /garden plants."); got != "Water the /garden plants." {
		t.Fatalf("plain = %q", got)
	}
}

// Each user_input step is agy taking in one more line from stdin.
func TestReadCount(t *testing.T) {
	d := (&Antigravity{}).NewDecoder()
	var reads []int
	for _, raw := range []string{
		`{"event":"step_update","step_update":{"conversation_id":"c-1","step_index":0,"state":"DONE","step_type":"user_input"}}`,
		`{"event":"result","result":{"conversation_id":"c-1","status":"SUCCESS","response":"Done.\n"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c-1","step_index":3,"state":"DONE","step_type":"user_input"}}`,
	} {
		for _, e := range d.Line([]byte(raw)) {
			if e.Read > 0 {
				reads = append(reads, e.Read)
			}
		}
	}
	if !slices.Equal(reads, []int{1, 1}) {
		t.Fatalf("reads = %v, want [1 1]", reads)
	}
}

func TestToolKinds(t *testing.T) {
	for name, want := range map[string]string{
		"run_command": harness.ToolShell, "write_to_file": harness.ToolWrite, "replace_file_content": harness.ToolEdit,
		"notebook_edit": harness.ToolEdit, "view_file": harness.ToolRead, "list_dir": harness.ToolSearch,
		"search_web": harness.ToolWeb, "open_browser_url": harness.ToolWeb, "browser_click_element": harness.ToolWeb,
		"invoke_subagent": harness.ToolAgent, "browser_subagent": harness.ToolAgent,
		"generate_image": harness.ToolOther, "a_tool_agy_adds_later": harness.ToolOther,
	} {
		if got := toolCall(name, nil); got == nil || got.Kind != want {
			t.Errorf("%s: %+v, want kind %s", name, got, want)
		}
	}
	for _, name := range []string{"command_status", "send_command_input", "wait", "finish"} {
		if got := toolCall(name, nil); got != nil {
			t.Errorf("%s: %+v, want no activity", name, got)
		}
	}
}

func TestEmptySuccessFails(t *testing.T) {
	evs := (&Antigravity{}).NewDecoder().Line([]byte(`{"event":"result","result":{"conversation_id":"c-1","status":"SUCCESS","response":""}}`))
	if len(evs) != 1 || evs[0].End == nil || !evs[0].End.Error {
		t.Fatalf("events = %+v; want a failed turn end", evs)
	}
}

func TestUsageLimitResets(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	d := &decoder{seen: map[int]bool{}, now: func() time.Time { return now }}
	lim := d.usageLimit("RESOURCE_EXHAUSTED (code 429): Individual quota reached. Resets in 2h39m52s.")
	if lim == nil || lim.ResetsAt != now.Add(2*time.Hour+39*time.Minute+52*time.Second).Unix() {
		t.Fatalf("limit = %+v", lim)
	}
	if lim := d.usageLimit("Your AI credits balance is too low to continue."); lim == nil || lim.ResetsAt != 0 {
		t.Fatalf("credits limit = %+v", lim)
	}
	if d.usageLimit("invalid model selection") != nil {
		t.Fatal("an ordinary error read as a usage limit")
	}
}
