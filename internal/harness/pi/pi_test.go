package pi

import (
	"slices"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/harness/harnesstest"
)

const session = "7c9e6679-7425-40de-944b-e07fc1f90ae7"

func TestConformance(t *testing.T) {
	harnesstest.Run(t, Name, []harnesstest.Fixture{
		{
			File: "testdata/pi-1.3.0-tool.jsonl", Source: "from source, pi 1.3.0",
			Want: harnesstest.Want{
				Session: session,
				Tools: []harness.Tool{
					{Kind: harness.ToolShell, Command: "tk show water-the-plants"},
					{Kind: harness.ToolEdit, File: "/repo/notes.txt"},
				},
				CostUSD: 0.0065,
				End:     harness.End{Text: "Noted that Sam watered the plants."},
			},
		},
		{
			File: "testdata/pi-1.3.0-error.jsonl", Source: "from source, pi 1.3.0",
			Want: harnesstest.Want{Session: session, End: harness.End{Error: true, Text: "401 invalid x-api-key"}},
		},
		{
			File: "testdata/pi-1.3.0-limit.jsonl", Source: "from source, pi 1.3.0",
			Want: harnesstest.Want{Session: session, Limit: true, End: harness.End{Error: true, Text: "429 rate limit exceeded"}},
		},
		{
			File: "testdata/pi-1.3.0-refused.jsonl", Source: "from source, pi 1.3.0",
			Want: harnesstest.Want{Session: session, End: harness.End{Error: true, Text: "No model selected"}},
		},
	})
}

func TestLive(t *testing.T) { harnesstest.Live(t, Name) }

func TestLaunch(t *testing.T) {
	h := &Pi{command: command}
	l, err := h.Launch(harness.Spec{Dir: "/repo", Prompt: "Water the plants.", Session: "abc-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pi", "--mode", "rpc", "--no-extensions", "--session-id", "abc-1"}
	if !slices.Equal(l.Args, want) {
		t.Fatalf("Args = %q, want %q", l.Args, want)
	}
	lines := strings.Split(strings.TrimSuffix(string(l.Stdin), "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"get_state"`) ||
		!strings.Contains(lines[1], `"type":"prompt"`) || !strings.Contains(lines[1], "Water the plants.") {
		t.Fatalf("Stdin = %q; want get_state then prompt", l.Stdin)
	}
	if !l.KeepStdin {
		t.Fatal("a steering harness keeps stdin")
	}
	if got := string(h.Note("Use the blue folder.")); got != `{"message":"Use the blue folder.","type":"steer"}`+"\n" {
		t.Fatalf("Note = %q", got)
	}
}

// Read counts the user messages Pi has taken in: the prompt, then each
// steering note once it is delivered.
func TestRead(t *testing.T) {
	d := (&Pi{}).NewDecoder()
	var got []int
	for _, in := range []string{
		`{"type":"message_end","message":{"role":"user","content":"Water the plants."}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"toolUse"}}`,
		`{"type":"message_end","message":{"role":"user","content":"Use the blue folder."}}`,
	} {
		for _, e := range d.Line([]byte(in)) {
			if e.Read > 0 {
				got = append(got, e.Read)
			}
		}
	}
	if !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("Read = %v, want [1 2]", got)
	}
}
