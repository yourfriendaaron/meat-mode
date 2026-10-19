package fake

import (
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/harness/harnesstest"
)

func init() { harness.Register(New().Factory()) }

func TestConformance(t *testing.T) {
	harnesstest.Run(t, "fake", []harnesstest.Fixture{
		{File: "testdata/fake-tool.jsonl", Source: "from source, fake 1.0.0", Want: harnesstest.Want{
			Session: "fake-session-1",
			Tools:   []harness.Tool{{Kind: harness.ToolRead, File: "/repo/notes.txt"}, {Kind: harness.ToolShell, Command: "bd close task-1"}},
			End:     harness.End{Text: "Watered the plants."},
		}},
		{File: "testdata/fake-error.jsonl", Source: "from source, fake 1.0.0", Want: harnesstest.Want{
			Session: "fake-session-2", Limit: true, End: harness.End{Error: true},
		}},
	})
}
