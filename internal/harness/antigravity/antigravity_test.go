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
	if !slices.Equal(reads, []int{1, 2}) {
		t.Fatalf("reads = %v, want [1 2]", reads)
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
