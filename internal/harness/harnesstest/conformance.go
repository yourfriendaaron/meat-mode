// Package harnesstest holds the conformance suite every harness runs, so the
// bridge can treat them all the same.
package harnesstest

import (
	"bufio"
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// Fixture is one captured stdout of the harness, a file of lines in the
// package's testdata, with what its decoder must find in it.
type Fixture struct {
	File string // path of the capture, such as testdata/codex-0.163.0-tool.jsonl
	// Source says where the capture came from: "live, <program> <version>"
	// for a recorded run, or "from source, <program> <version>" for one
	// written from the harness's source or docs, to be re-recorded.
	Source string
	Want   Want
}

// Want is what a fixture's decoded events must hold.
type Want struct {
	Session string         // the session id it names; must pass harness.ValidSession
	Tools   []harness.Tool // tool calls found in this order, among others
	CostUSD float64        // the summed cost, checked for a metered harness
	End     harness.End    // the last turn end; Text is checked when set
	Limit   bool           // a usage limit is reported
}

// Run checks the harness registered under name against the contract in
// docs/harnesses.md. Fixtures must include a turn that ran a shell command
// and ended well, and a turn that ended in an error.
func Run(t *testing.T, name string, fixtures []Fixture) {
	f, ok := harness.Lookup(name)
	if !ok {
		t.Fatalf("no harness registered as %q", name)
	}
	newH := func(t *testing.T, command []string) harness.Harness {
		h, err := f.New(harness.Env{Command: command, Getenv: func(string) string { return "" }, LookPath: exec.LookPath})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	t.Run("info", func(t *testing.T) {
		h := newH(t, nil)
		if err := harness.Validate(name, h); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("launch", func(t *testing.T) {
		h := newH(t, nil)
		info := h.Info()
		const prompt, session = "Read the task and reply with a note.", "conformance-session-1"
		fresh := launch(t, h, harness.Spec{Dir: t.TempDir(), Prompt: prompt, BudgetUSD: 1.25})
		if fresh.KeepStdin != info.Steer {
			t.Fatalf("KeepStdin = %v; it must equal Steer (%v)", fresh.KeepStdin, info.Steer)
		}
		if !carries(fresh, prompt) {
			t.Fatal("the prompt is in neither the arguments nor stdin")
		}
		if carries(fresh, session) {
			t.Fatal("a new run names a session")
		}
		if info.Budget == harness.BudgetNative && !slices.ContainsFunc(fresh.Args, func(a string) bool { return strings.Contains(a, "1.25") }) {
			t.Fatal("a native-budget harness does not pass the cap of 1.25")
		}
		resumed := launch(t, h, harness.Spec{Dir: t.TempDir(), Prompt: prompt, Session: session, BudgetUSD: 1.25})
		if !slices.Contains(resumed.Args, session) && !slices.ContainsFunc(resumed.Args, func(a string) bool { return strings.HasSuffix(a, "="+session) }) {
			t.Fatalf("a resume does not pass the session as an argument: %q", resumed.Args)
		}
		bad := launch(t, h, harness.Spec{Dir: t.TempDir(), Prompt: prompt, Session: "-x --evil"})
		if carries(bad, "--evil") {
			t.Fatal("a malformed session id reached the command line")
		}
		if info.Steer {
			note := h.Note("Use the blue folder.")
			if len(note) == 0 || !bytes.Contains(note, []byte("Use the blue folder.")) || note[len(note)-1] != '\n' {
				t.Fatalf("Note = %q; want one line carrying the text", note)
			}
		}
	})

	t.Run("command override", func(t *testing.T) {
		l := launch(t, newH(t, []string{"/opt/agent/bin/agent-x", "--flag"}), harness.Spec{Dir: t.TempDir(), Prompt: "Water the plants."})
		if l.Args[0] != "/opt/agent/bin/agent-x" || !slices.Contains(l.Args, "--flag") {
			t.Fatalf("Env.Command did not replace the program: %q", l.Args)
		}
	})

	t.Run("env markers", func(t *testing.T) {
		env := []string{"TERM=xterm", "PATH=/usr/bin", "MEAT_MODE_BRIDGE_TOKEN=secret"}
		for _, m := range newH(t, nil).Info().EnvMarkers {
			env = append(env, strings.TrimSuffix(m, "*")+strings.Repeat("X", strings.Count(m, "*"))+"=1")
		}
		got := harness.StripEnv(env)
		if !slices.Equal(got, []string{"TERM=xterm", "PATH=/usr/bin"}) {
			t.Fatalf("StripEnv = %q; want only TERM and PATH", got)
		}
	})

	t.Run("decoder", func(t *testing.T) {
		h := newH(t, nil)
		d := h.NewDecoder()
		for _, junk := range []string{"", "not json", "{", `{"type":42}`, "[]", "null"} {
			if ev := d.Line([]byte(junk)); len(ev) != 0 {
				t.Errorf("Line(%q) = %+v; want nothing", junk, ev)
			}
		}
		var shell, failed bool
		for _, fx := range fixtures {
			if fx.Source == "" {
				t.Errorf("%s: no Source", fx.File)
			}
			evs := decodeFile(t, h, fx.File)
			check(t, h.Info(), fx, evs)
			if fx.Want.End.Error {
				failed = true
			} else if slices.ContainsFunc(fx.Want.Tools, func(tl harness.Tool) bool { return tl.Kind == harness.ToolShell && tl.Command != "" }) {
				shell = true
			}
		}
		if !shell || !failed {
			t.Errorf("fixtures need a turn that ran a shell command and ended well (%v) and a turn that failed (%v)", shell, failed)
		}
	})

	t.Run("check", func(t *testing.T) {
		h := newH(t, nil)
		l := launch(t, h, harness.Spec{Dir: t.TempDir(), Prompt: "x"})
		if _, err := exec.LookPath(l.Args[0]); err != nil {
			t.Skipf("%s is not installed", l.Args[0])
		}
		v, err := h.Check(context.Background())
		if err != nil || v == "" {
			t.Fatalf("Check = %q, %v; want a version", v, err)
		}
	})
}

func launch(t *testing.T, h harness.Harness, s harness.Spec) harness.Launch {
	t.Helper()
	l, err := h.Launch(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Args) == 0 || l.Args[0] == "" {
		t.Fatal("Launch returned no program")
	}
	return l
}

func carries(l harness.Launch, s string) bool {
	return bytes.Contains(l.Stdin, []byte(s)) || slices.ContainsFunc(l.Args, func(a string) bool { return strings.Contains(a, s) })
}

func decodeFile(t *testing.T, h harness.Harness, file string) []harness.Event {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	d := h.NewDecoder()
	var out []harness.Event
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		out = append(out, d.Line(sc.Bytes())...)
	}
	return out
}

func check(t *testing.T, info harness.Info, fx Fixture, evs []harness.Event) {
	t.Helper()
	name := filepath.Base(fx.File)
	var session string
	var cost float64
	var end *harness.End
	var limit bool
	tools := fx.Want.Tools
	for _, e := range evs {
		if e.Session != "" {
			if !harness.ValidSession(e.Session) {
				t.Errorf("%s: session %q fails ValidSession", name, e.Session)
			}
			session = e.Session
		}
		if e.Tool != nil {
			if !slices.Contains(harness.ToolKinds, e.Tool.Kind) {
				t.Errorf("%s: unknown tool kind %q", name, e.Tool.Kind)
			}
			if len(tools) > 0 && toolMatches(tools[0], *e.Tool) {
				tools = tools[1:]
			}
		}
		if e.CostUSD < 0 {
			t.Errorf("%s: negative cost %v", name, e.CostUSD)
		}
		cost += e.CostUSD
		if e.Limit != nil {
			limit = true
		}
		if e.End != nil {
			end = e.End
		}
	}
	if session != fx.Want.Session {
		t.Errorf("%s: session %q, want %q", name, session, fx.Want.Session)
	}
	if len(tools) > 0 {
		t.Errorf("%s: tool calls not found, in order: %+v", name, tools)
	}
	if info.Budget == harness.BudgetMetered && math.Abs(cost-fx.Want.CostUSD) > 1e-9 {
		t.Errorf("%s: cost %s, want %s", name, strconv.FormatFloat(cost, 'f', -1, 64), strconv.FormatFloat(fx.Want.CostUSD, 'f', -1, 64))
	}
	if info.Budget != harness.BudgetMetered && cost != 0 {
		t.Errorf("%s: a %s-budget harness reported cost %v", name, info.Budget, cost)
	}
	if limit != fx.Want.Limit {
		t.Errorf("%s: usage limit %v, want %v", name, limit, fx.Want.Limit)
	}
	switch {
	case end == nil:
		t.Errorf("%s: no turn end", name)
	case end.Error != fx.Want.End.Error || end.Budget != fx.Want.End.Budget:
		t.Errorf("%s: end %+v, want %+v", name, *end, fx.Want.End)
	case fx.Want.End.Text != "" && end.Text != fx.Want.End.Text:
		t.Errorf("%s: end text %q, want %q", name, end.Text, fx.Want.End.Text)
	}
}

func toolMatches(want, got harness.Tool) bool {
	return want.Kind == got.Kind && (want.Command == "" || want.Command == got.Command) && (want.File == "" || want.File == got.File)
}

// LiveEnv is the variable that switches on Live. A live run calls the
// harness's model and costs usage, so nothing sets it by default.
const LiveEnv = "MEAT_MODE_HARNESS_LIVE"

// Live runs the installed harness for real, twice: a new run in a fresh
// git repo, then a resume of its session after the repo was deleted and
// made again at the same path, as the bridge does with an idle task's
// worktree. The resumed run must still know what the first was told. It
// skips unless LiveEnv is 1 and the program is installed and signed in.
func Live(t *testing.T, name string) {
	if os.Getenv(LiveEnv) != "1" {
		t.Skipf("set %s=1 to run %s for real; it costs usage", LiveEnv, name)
	}
	f, ok := harness.Lookup(name)
	if !ok {
		t.Fatalf("no harness registered as %q", name)
	}
	h, err := f.New(harness.Env{Getenv: os.Getenv, LookPath: exec.LookPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Check(context.Background()); err != nil {
		t.Skipf("%s: %v", name, err)
	}
	dir := filepath.Join(t.TempDir(), "repo")
	gitInit := func() {
		if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
	}
	gitInit()
	first, _ := liveRun(t, h, harness.Spec{Dir: dir, Prompt: "Remember the word marigold. Reply with the single word ready. Do not use any tools.", BudgetUSD: 0.5})
	if first == "" {
		t.Fatal("the run named no session")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	gitInit()
	again, text := liveRun(t, h, harness.Spec{Dir: dir, Prompt: "Which word did I ask you to remember? Reply with that word only. Do not use any tools.", Session: first, BudgetUSD: 0.5})
	if again == "" {
		t.Fatal("the resumed run named no session")
	}
	if !strings.Contains(strings.ToLower(text), "marigold") {
		t.Errorf("the run resumed in a remade folder lost its conversation; it replied %q", text)
	}
}

// liveRun runs one turn and returns the session it named and its last
// reply.
func liveRun(t *testing.T, h harness.Harness, s harness.Spec) (session, text string) {
	t.Helper()
	l := launch(t, h, s)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, l.Args[0], l.Args[1:]...)
	cmd.Dir = s.Dir
	cmd.Env = harness.StripEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_, _ = stdin.Write(l.Stdin)
	if !l.KeepStdin {
		stdin.Close()
	}
	d := h.NewDecoder()
	var end *harness.End
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		for _, e := range d.Line(sc.Bytes()) {
			if e.Session != "" {
				session = e.Session
			}
			if e.End != nil {
				end = e.End
				if l.KeepStdin {
					stdin.Close()
				}
			}
		}
	}
	_ = cmd.Wait()
	if end == nil || end.Error {
		t.Fatalf("turn end %+v; stderr: %s", end, stderr.String())
	}
	return session, end.Text
}
