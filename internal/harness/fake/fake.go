// Package fake is a harness for tests. Its Info is the test's to set, so the
// bridge can be tested against a harness that steers or does not, and any
// budget mode. Its protocol is plain: each stdout line is one harness.Event
// as JSON, a prompt or note on stdin is {"prompt": text} or {"note": text},
// and the command line carries --prompt, --resume and --budget.
package fake

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// Fake is a harness whose declarations and command the test sets.
type Fake struct {
	// Inf is what Info returns: by default the name "fake", native budget,
	// steering, and FAKE_HARNESS_* as its env marker.
	Inf harness.Info
	// Command is the program and fixed flags; fakeagent by default.
	Command  []string
	Ver      string // what Check returns
	CheckErr error  // when set, Check fails with it
}

// New returns a fake that steers and caps its own budget.
func New() *Fake {
	return &Fake{
		Inf: harness.Info{
			Name: "fake", Title: "Fake", InstructionFile: "AGENTS.md",
			Budget: harness.BudgetNative, Steer: true, EnvMarkers: []string{"FAKE_HARNESS_*"},
		},
		Command: []string{"fakeagent"},
		Ver:     "1.0.0",
	}
}

// Factory returns a factory that builds f, with Env.Command replacing its
// command when set.
func (f *Fake) Factory() harness.Factory {
	return harness.Factory{Name: f.Inf.Name, New: func(e harness.Env) (harness.Harness, error) {
		g := *f
		if len(e.Command) > 0 {
			g.Command = e.Command
		}
		return &g, nil
	}}
}

func (f *Fake) Info() harness.Info { return f.Inf }

func (f *Fake) Check(context.Context) (string, error) { return f.Ver, f.CheckErr }

func (f *Fake) Launch(s harness.Spec) (harness.Launch, error) {
	l := harness.Launch{Args: append([]string{}, f.Command...), KeepStdin: f.Inf.Steer}
	if f.Inf.Budget == harness.BudgetNative {
		l.Args = append(l.Args, "--budget", strconv.FormatFloat(s.BudgetUSD, 'f', -1, 64))
	}
	if harness.ValidSession(s.Session) {
		l.Args = append(l.Args, "--resume", s.Session)
	}
	if f.Inf.Steer {
		l.Stdin = line("prompt", s.Prompt)
	} else {
		l.Args = append(l.Args, "--prompt", s.Prompt)
	}
	return l, nil
}

func (f *Fake) Note(text string) []byte { return line("note", text) }

func (f *Fake) NewDecoder() harness.Decoder { return decoder{} }

type decoder struct{}

func (decoder) Line(raw []byte) []harness.Event {
	var e harness.Event
	if json.Unmarshal(raw, &e) != nil || e == (harness.Event{}) {
		return nil
	}
	return []harness.Event{e}
}

func line(key, text string) []byte {
	b, _ := json.Marshal(map[string]string{key: text})
	return append(b, '\n')
}
