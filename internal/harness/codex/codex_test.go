package codex

import (
	"slices"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/harness/harnesstest"
)

func TestConformance(t *testing.T) {
	harnesstest.Run(t, Name, []harnesstest.Fixture{
		{File: "testdata/codex-0.163.0-tool.jsonl", Source: "live, codex 0.163.0", Want: harnesstest.Want{
			Session: "0199a213-81c0-7800-8aa1-bbab2a035a53",
			Tools:   []harness.Tool{{Kind: harness.ToolShell, Command: "ls"}},
			End:     harness.End{Text: "done"},
		}},
		{File: "testdata/codex-0.163.0-edit.jsonl", Source: "from source, codex 0.163.0", Want: harnesstest.Want{
			Session: "0199b7e4-2c1a-7d30-9f4e-5a6b7c8d9e0f",
			Tools: []harness.Tool{
				{Kind: harness.ToolPlan},
				{Kind: harness.ToolShell, Command: "cat notes.txt"},
				{Kind: harness.ToolEdit, File: "/repo/notes.txt"},
				{Kind: harness.ToolWrite, File: "/repo/garden.md"},
				{Kind: harness.ToolWeb},
				{Kind: harness.ToolOther},
			},
			End: harness.End{Text: "Added the watering note."},
		}},
		{File: "testdata/codex-0.163.0-limit.jsonl", Source: "from source, codex 0.163.0", Want: harnesstest.Want{
			Session: "0199c3d5-6e7f-7a80-b1c2-d3e4f5a6b7c8", Limit: true, End: harness.End{Error: true},
		}},
	})
}

func TestLive(t *testing.T) { harnesstest.Live(t, Name) }

func TestLaunchOrder(t *testing.T) {
	c := &Codex{command: command}
	l, err := c.Launch(harness.Spec{Dir: "/repo", Prompt: "-n Water the plants.", Session: "0199a213-81c0-7800-8aa1-bbab2a035a53"})
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]string{}, command...), "-C", "/repo", "resume", "0199a213-81c0-7800-8aa1-bbab2a035a53", "--", "-n Water the plants.")
	if !slices.Equal(l.Args, want) {
		t.Fatalf("Args = %q\nwant %q", l.Args, want)
	}
	if l.KeepStdin || len(l.Stdin) != 0 {
		t.Fatalf("stdin = %q, keep %v; want none, closed", l.Stdin, l.KeepStdin)
	}
}

func TestUnwrapShell(t *testing.T) {
	for in, want := range map[string]string{
		"/bin/bash -lc ls":               "ls",
		"/bin/zsh -lc 'go test ./...'":   "go test ./...",
		`bash -c "bd close task-1"`:      "bd close task-1",
		"git status":                     "git status",
		"/usr/bin/python3 -c 'print(1)'": "/usr/bin/python3 -c 'print(1)'",
		"ls":                             "ls",
	} {
		if got := unwrapShell(in); got != want {
			t.Errorf("unwrapShell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUsageLimitResets(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)
	d := &decoder{seen: map[string]bool{}, now: func() time.Time { return now }}
	for msg, want := range map[string]time.Time{
		"You’ve hit your usage limit. Try again at 3:15 PM.":                                   time.Date(2026, 10, 21, 15, 15, 0, 0, time.Local),
		"You’ve hit your usage limit. Upgrade to Plus, or try again at Nov 2nd, 2026 8:05 AM.": time.Date(2026, 11, 2, 8, 5, 0, 0, time.Local),
		"You’ve hit your usage limit. Try again later.":                                        {},
	} {
		lim := d.usageLimit(msg)
		if lim == nil {
			t.Fatalf("%q: no limit", msg)
		}
		if w := want.Unix(); want.IsZero() && lim.ResetsAt != 0 || !want.IsZero() && lim.ResetsAt != w {
			t.Errorf("%q: resets %d, want %v", msg, lim.ResetsAt, want)
		}
	}
	if d.usageLimit("stream disconnected before completion") != nil {
		t.Error("an ordinary error read as a usage limit")
	}
}
