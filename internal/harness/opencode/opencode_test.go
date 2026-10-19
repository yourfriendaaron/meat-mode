package opencode

import (
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/harness/harnesstest"
)

const session = "ses_4a1b2c3d4e5fAbCdEfGhIjKlMn"

func TestConformance(t *testing.T) {
	harnesstest.Run(t, Name, []harnesstest.Fixture{
		{
			File: "testdata/opencode-1.19.3-tool.jsonl", Source: "from source, opencode 1.19.3",
			Want: harnesstest.Want{
				Session: session,
				Tools: []harness.Tool{
					{Kind: harness.ToolShell, Command: "tk show water-the-plants"},
					{Kind: harness.ToolEdit, File: "/repo/notes.txt"},
				},
				CostUSD: 0.02,
				End:     harness.End{Text: "Noted that Alex watered the plants."},
			},
		},
		{
			File: "testdata/opencode-1.19.3-error.jsonl", Source: "from source, opencode 1.19.3",
			Want: harnesstest.Want{
				Session: session,
				End:     harness.End{Error: true, Text: "Authentication failed. Run opencode auth login."},
			},
		},
		{
			File: "testdata/opencode-1.19.3-limit.jsonl", Source: "from source, opencode 1.19.3",
			Want: harnesstest.Want{Session: session, Limit: true, End: harness.End{Error: true}},
		},
	})
}

func TestLive(t *testing.T) { harnesstest.Live(t, Name) }

func TestLaunch(t *testing.T) {
	h := &OpenCode{command: command}
	l, err := h.Launch(harness.Spec{Dir: "/repo", Prompt: "Water the plants.", Session: "ses_abc"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"opencode", "run", "--format", "json", "--auto", "--dir", "/repo", "-s", "ses_abc", "Water the plants."}
	if !slices.Equal(l.Args, want) {
		t.Fatalf("Args = %q, want %q", l.Args, want)
	}
	if l.KeepStdin || len(l.Stdin) != 0 {
		t.Fatalf("a run that reads its prompt from the arguments keeps no stdin: %+v", l)
	}
}

func TestToolKinds(t *testing.T) {
	for name, want := range map[string]string{
		"bash": harness.ToolShell, "edit": harness.ToolEdit, "write": harness.ToolWrite, "read": harness.ToolRead,
		"apply_patch": harness.ToolEdit, "glob": harness.ToolSearch, "grep": harness.ToolSearch,
		"webfetch": harness.ToolWeb, "websearch": harness.ToolWeb, "task": harness.ToolAgent,
		"todowrite": harness.ToolPlan, "skill": harness.ToolOther,
	} {
		if got := tool(name, nil).Kind; got != want {
			t.Errorf("%s: kind %s, want %s", name, got, want)
		}
	}
	if got := tool("apply_patch", map[string]any{"patchText": "*** Begin Patch\n*** Update File: src/a.go\n*** End Patch"}).File; got != "src/a.go" {
		t.Errorf("apply_patch file = %q", got)
	}
}
