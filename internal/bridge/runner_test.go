package bridge

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/harness"
	"github.com/yourfriendaaron/meat-mode/internal/harness/claude"
	fakeh "github.com/yourfriendaaron/meat-mode/internal/harness/fake"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// fakeAgentBin is the fake agent command, built once per test binary.
var fakeAgentBin struct {
	once sync.Once
	dir  string
	path string
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if fakeAgentBin.dir != "" {
		os.RemoveAll(fakeAgentBin.dir)
	}
	os.Exit(code)
}

// fakeAgent builds ./fakeagent and returns its path.
func fakeAgent(t *testing.T) string {
	t.Helper()
	fakeAgentBin.once.Do(func() {
		if _, err := exec.LookPath("go"); err != nil {
			fakeAgentBin.err = err
			return
		}
		dir, err := os.MkdirTemp("", "meat-mode-fakeagent-")
		if err != nil {
			fakeAgentBin.err = err
			return
		}
		fakeAgentBin.dir = dir
		path := filepath.Join(dir, "fakeagent")
		if out, err := exec.Command("go", "build", "-o", path, "./fakeagent").CombinedOutput(); err != nil {
			fakeAgentBin.err = err
			t.Logf("%s", out)
			return
		}
		fakeAgentBin.path = path
	})
	if fakeAgentBin.err != nil {
		t.Skipf("cannot build the fake agent: %v", fakeAgentBin.err)
	}
	return fakeAgentBin.path
}

// claudeHarness is the Claude Code harness running the fake agent, which
// speaks its stream-json.
func claudeHarness(t *testing.T) harness.Harness {
	t.Helper()
	h, err := claude.New(harness.Env{Command: []string{fakeAgent(t)}, Getenv: os.Getenv, LookPath: exec.LookPath})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// fakeHarness is internal/harness/fake running the fake agent in its
// protocol, set up by set.
func fakeHarness(t *testing.T, set func(*harness.Info)) *fakeh.Fake {
	t.Helper()
	f := fakeh.New()
	f.Command = []string{fakeAgent(t), "--protocol", "fake"}
	if set != nil {
		set(&f.Inf)
	}
	return f
}

// agentWorld is a world with real bd and the repo garden, synced, holding
// one bead created by planner.
func agentWorld(t *testing.T) (w *world, dir, id string) {
	t.Helper()
	w = newWorld(t, true)
	dir = w.repo("garden", true)
	id, err := w.tr.Create(w.ctx, dir, tracker.NewIssue{Title: "Water the plants", ActionID: "seed-water", Actor: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	return w, dir, id
}

// agentBridge starts a bridge with a runner over the world's state, as a
// fresh process would, with agents allowed in garden or not, running the
// fake agent through the Claude Code harness.
func (w *world) agentBridge(allowed bool, opts ...func(*AgentConfig)) *Bridge {
	w.t.Helper()
	return w.agentBridgeWith(allowed, claudeHarness(w.t), opts...)
}

// agentBridgeWith is agentBridge with the harness h.
func (w *world) agentBridgeWith(allowed bool, h harness.Harness, opts ...func(*AgentConfig)) *Bridge {
	w.t.Helper()
	b := w.start()
	cfg := DefaultAgentConfig()
	cfg.Harness = h.Info().Name
	cfg.Name = "helper"
	cfg.Timeout = time.Minute
	for _, o := range opts {
		o(&cfg)
	}
	r, err := OpenAgentRunner(filepath.Join(w.state, "agents"), cfg, h, quiet)
	if err != nil {
		w.t.Fatal(err)
	}
	r.Grace = 300 * time.Millisecond
	w.t.Cleanup(r.Shutdown)
	b.Runner = r
	b.Agents = map[string]bool{"garden": allowed}
	b.SaveAgents = func(string, bool) error { return nil }
	return b
}

func (w *world) ask(b *Bridge, name, bead, text string) {
	w.t.Helper()
	w.hub.Enqueue(us, action(name, api.KindAsk, "garden", bead, api.Payload{Text: text}))
	w.tick(b)
	w.oneOK(aid(name))
}

// runOf returns the runner's record of a bead, without marking the run list
// sent.
func runOf(b *Bridge, bead string) beadRecord {
	r := b.Runner
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.records[runKey("garden", bead)]
	if rec == nil {
		return beadRecord{}
	}
	cp := *rec
	if rec.Run != nil {
		run := *rec.Run
		cp.Run = &run
	}
	if rec.Process != nil {
		p := *rec.Process
		cp.Process = &p
	}
	return cp
}

func settled(r api.Run) bool { return r.State != api.RunWorking }

// waitRun waits for the bead's latest run to satisfy ok.
func (w *world) waitRun(b *Bridge, bead string, ok func(api.Run) bool) api.Run {
	w.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if rec := runOf(b, bead); rec.Run != nil && ok(*rec.Run) {
			return *rec.Run
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("run on %s never got there: %+v", bead, runOf(b, bead).Run)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// hubRun is the bead's run as the hub last heard it.
func (w *world) hubRun(bead string) api.RunReport {
	w.t.Helper()
	for _, r := range w.hub.Runs(us) {
		if r.Repo == "garden" && r.BeadID == bead {
			return r
		}
	}
	w.t.Fatalf("the hub has no run on %s: %+v", bead, w.hub.Runs(us))
	return api.RunReport{}
}

func (w *world) waitGroupGone(pid int) {
	w.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for groupAlive(pid) {
		if time.Now().After(deadline) {
			w.t.Fatalf("process group %d is still running", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func hasCommentBy(i tracker.Issue, author, text string) bool {
	for _, c := range i.Comments {
		if c.Author == author && c.Text == text {
			return true
		}
	}
	return false
}

func TestAgentRunWritesNoteThenCloses(t *testing.T) {
	t.Setenv("FAKEAGENT_STEP", "1s")
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-close", id, "Please go slow and close it")

	// The note is on the bead first, and the hub hears of the run at once.
	if !hasCommentBy(w.show(dir, id), author, "Please go slow and close it") {
		t.Errorf("the ask's note is missing: %+v", w.show(dir, id).Comments)
	}
	if r := w.hubRun(id); r.State != api.RunWorking || r.Count != 1 || r.Agent != "helper" || r.EndedAt != nil {
		t.Errorf("hub run at start = %+v", r)
	}
	// The run list reached the hub before the ask's result, so the app never
	// sees an applied ask with no run.
	if at := w.hub.RunsAtResult(aid("ask-close")); !slices.ContainsFunc(at, func(r api.RunReport) bool {
		return r.BeadID == id && r.State == api.RunWorking && r.Applying == aid("ask-close")
	}) {
		t.Errorf("runs when the ask's result arrived = %+v", at)
	}
	run := w.waitRun(b, id, settled)
	if run.State != api.RunDone || run.Activity != "Done: watered." || run.Reason != "" {
		t.Errorf("settled run = %+v", run)
	}
	if i := w.show(dir, id); i.Status != "closed" || !hasCommentBy(i, "helper", "Done: watered.") {
		t.Errorf("bead after the run = %s %+v", i.Status, i.Comments)
	}
	w.tick(b)
	if r := w.hubRun(id); r.ID != run.ID || r.State != api.RunDone || r.EndedAt == nil {
		t.Errorf("hub run after = %+v", r)
	}

	wt := filepath.Join(w.state, "agents", "worktrees", "garden", id)
	if got := strings.TrimSpace(beadsRun(t, wt, "git", "branch", "--show-current")); got != "meat-mode/"+id {
		t.Errorf("worktree branch = %q", got)
	}
	logf := filepath.Join(w.state, "agents", "logs", "garden", id, run.ID+".jsonl")
	if b, err := os.ReadFile(logf); err != nil || !strings.Contains(string(b), `"type":"result"`) {
		t.Errorf("run log %s: %v %q", logf, err, b)
	}
}

func beadsRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func TestAgentRunOutcomes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		words, state, reason, detail, said string
	}{
		// A settled run shows what the agent said, never its last step.
		{"reply", api.RunWaiting, "", "", "Which plants?"},
		{"have a look", api.RunStopped, api.ReasonNoReply, "without a note", ""},
		{"usage", api.RunStopped, api.ReasonUsageLimit, "resets at", ""},
		{"budget", api.RunStopped, api.ReasonBudget, "$2.00", ""},
		{"fail", api.RunStopped, api.ReasonFailed, "Something went wrong", ""},
		{"crash", api.RunStopped, api.ReasonCrashed, "exit status 1", ""},
	} {
		t.Run(c.words, func(t *testing.T) {
			t.Parallel()
			w, _, id := agentWorld(t)
			b := w.agentBridge(true)
			w.tick(b)
			w.ask(b, "ask-"+c.words, id, c.words)
			run := w.waitRun(b, id, settled)
			if run.State != c.state || run.Reason != c.reason || !strings.Contains(run.Detail, c.detail) || run.Activity != c.said || run.Count != 1 {
				t.Errorf("run = %+v, want %s/%s with %q", run, c.state, c.reason, c.detail)
			}
			w.tick(b)
			if r := w.hubRun(id); r.State != c.state || r.Reason != c.reason {
				t.Errorf("hub run = %+v", r)
			}
		})
	}
}

func TestAgentFollowUpJoinsLiveRun(t *testing.T) {
	t.Setenv("FAKEAGENT_STEP", "2s")
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-slow", id, "slow")
	first := w.waitRun(b, id, func(r api.Run) bool { return r.Activity == "Reading the bead" })
	w.ask(b, "ask-more", id, "Then close it")
	rec := runOf(b, id)
	if rec.Run.ID != first.ID || rec.Run.State != api.RunWorking || rec.Run.Queued != 1 || rec.Count != 1 {
		t.Errorf("after the follow-up: %+v", rec.Run)
	}
	if r := w.hubRun(id); r.Queued != 1 {
		t.Errorf("hub queued = %d", r.Queued)
	}
	// An ask with no note on a live run changes nothing.
	w.ask(b, "ask-empty", id, "")
	run := w.waitRun(b, id, settled)
	if run.ID != first.ID || run.State != api.RunDone || run.Count != 1 || run.Queued != 0 {
		t.Errorf("settled = %+v", run)
	}
	if !hasCommentBy(w.show(dir, id), author, "Then close it") {
		t.Error("the follow-up note is not on the bead")
	}
	logf := filepath.Join(w.state, "agents", "logs", "garden", id, run.ID+".jsonl")
	if b, _ := os.ReadFile(logf); !strings.Contains(string(b), `"isReplay":true,"message":{"content":"New note from the user`) {
		t.Errorf("the agent never read the follow-up:\n%s", b)
	}
}

// TestOnLineStoresOnlyAValidSessionID checks the guard around the session
// id the agent's stream reports: it becomes the argument of --resume on the
// next run, so a malformed one is ignored, warned of once, and never saved.
func TestOnLineStoresOnlyAValidSessionID(t *testing.T) {
	var log bytes.Buffer
	r := &AgentRunner{Harness: fakeh.New(), Dir: t.TempDir(), Log: slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelWarn}))}
	r.init()
	rec := &beadRecord{Repo: "garden", BeadID: "gd-1"}
	r.records[runKey("garden", "gd-1")] = rec
	lr := &liveRun{rec: rec, run: &api.Run{}, repo: Repo{Name: "garden"}}
	p := &agentProc{}
	line := func(id string) []harness.Event { return []harness.Event{{Session: id}} }
	for range 2 {
		r.onEvents(lr, p, line("--dangerously-skip-permissions"))
	}
	r.onEvents(lr, p, line("two words"))
	if rec.Session != "" {
		t.Fatalf("stored a malformed session id %q", rec.Session)
	}
	if n := strings.Count(log.String(), "level=WARN"); n != 2 {
		t.Fatalf("%d warnings for 2 malformed ids, want one each: %s", n, log.String())
	}
	r.onEvents(lr, p, line("abc-123"))
	if rec.Session != "abc-123" || rec.Harness != "fake" {
		t.Fatalf("a valid session id was not stored: %q", rec.Session)
	}
	b, err := os.ReadFile(filepath.Join(r.Dir, "runs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"session": "abc-123"`) || strings.Contains(string(b), "--dangerously") {
		t.Errorf("the journal holds the wrong session: %s", b)
	}
}

func TestAgentResumesAfterEnd(t *testing.T) {
	t.Parallel()
	w, _, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-1", id, "reply")
	one := w.waitRun(b, id, settled)
	session := runOf(b, id).Session
	if one.State != api.RunWaiting || session == "" {
		t.Fatalf("first run = %+v, session %q", one, session)
	}
	// A bridge restart in between keeps the session and the count.
	b = w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-2", id, "close")
	two := w.waitRun(b, id, settled)
	if two.ID == one.ID || two.State != api.RunDone || two.Count != 2 || runOf(b, id).Session != session {
		t.Errorf("second run = %+v, session %q", two, runOf(b, id).Session)
	}
	logf := filepath.Join(w.state, "agents", "logs", "garden", id, two.ID+".jsonl")
	if b, _ := os.ReadFile(logf); !strings.Contains(string(b), `"fake_resume":"`+session+`"`) {
		t.Errorf("the second run did not resume %s:\n%s", session, b)
	}
}

// TestOnlyAsksStartRuns is the author filter: the agent's own notes and
// closes, and the user's other actions, never start a run.
func TestOnlyAsksStartRuns(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-reply", id, "reply")
	run := w.waitRun(b, id, settled)
	if err := w.tr.AddComment(w.ctx, dir, id, "planner", "One more thing"); err != nil {
		t.Fatal(err)
	}
	for _, a := range []api.Action{
		action("f-note", api.KindNote, "garden", id, api.Payload{Text: "The ferns too"}),
		action("f-done", api.KindDone, "garden", id, api.Payload{}),
		action("f-reopen", api.KindReopen, "garden", id, api.Payload{}),
	} {
		w.hub.Enqueue(us, a)
	}
	w.tick(b)
	if err := w.tr.Close(w.ctx, dir, id, "Done", "planner"); err != nil {
		t.Fatal(err)
	}
	w.tick(b)
	time.Sleep(300 * time.Millisecond)
	w.tick(b)
	rec := runOf(b, id)
	if rec.Count != 1 || rec.Run.ID != run.ID || len(b.Runner.live) != 0 {
		t.Errorf("a run started without an ask: %+v count %d", rec.Run, rec.Count)
	}
}

func TestAgentRunLimit(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true, func(c *AgentConfig) { c.RunsPerTask = 1 })
	w.tick(b)
	w.ask(b, "ask-1", id, "have a look")
	w.waitRun(b, id, settled)
	// A restart does not reset the count.
	b = w.agentBridge(true, func(c *AgentConfig) { c.RunsPerTask = 1 })
	w.tick(b)
	w.ask(b, "ask-2", id, "close")
	run := runOf(b, id).Run
	if run.State != api.RunStopped || run.Reason != api.ReasonRunLimit || run.Count != 1 || run.EndedAt == nil {
		t.Errorf("run = %+v", run)
	}
	if i := w.show(dir, id); i.Status == "closed" || !hasCommentBy(i, author, "close") {
		t.Errorf("bead = %s %+v; the note must stand and nothing run", i.Status, i.Comments)
	}
}

func TestAgentBusy(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	other := w.seed(dir, "Feed the cat")
	b := w.agentBridge(true, func(c *AgentConfig) { c.Concurrent = 1 })
	w.tick(b)
	w.ask(b, "ask-hang", id, "hang")
	w.ask(b, "ask-other", other, "close")
	if run := runOf(b, other).Run; run.State != api.RunStopped || run.Reason != api.ReasonBusy || run.Count != 0 {
		t.Errorf("second run = %+v", run)
	}
	if run := runOf(b, id).Run; run.State != api.RunWorking {
		t.Errorf("first run = %+v", run)
	}
}

func TestAgentTimeout(t *testing.T) {
	t.Parallel()
	w, _, id := agentWorld(t)
	b := w.agentBridge(true, func(c *AgentConfig) { c.Timeout = 5 * time.Second })
	w.tick(b)
	w.ask(b, "ask-hang", id, "hang")
	w.waitRun(b, id, func(r api.Run) bool { return r.Activity == "Running sleep" })
	pid := runOf(b, id).Process.PID
	run := w.waitRun(b, id, settled)
	if run.State != api.RunStopped || run.Reason != api.ReasonTimeout || !strings.Contains(run.Detail, "5 seconds") {
		t.Errorf("run = %+v", run)
	}
	// The agent ignores SIGTERM, and so does the child it started.
	w.waitGroupGone(pid)
}

func TestAgentStop(t *testing.T) {
	t.Parallel()
	w, _, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-hang", id, "hang")
	w.waitRun(b, id, func(r api.Run) bool { return r.Activity == "Running sleep" })
	pid := runOf(b, id).Process.PID
	w.hub.Enqueue(us, action("stop", api.KindStop, "garden", id, api.Payload{}))
	w.tick(b)
	w.oneOK(aid("stop"))
	if at := w.hub.RunsAtResult(aid("stop")); !slices.ContainsFunc(at, func(r api.RunReport) bool {
		return r.BeadID == id && r.State == api.RunStopped && r.Applying == aid("stop")
	}) {
		t.Errorf("runs when the stop's result arrived = %+v", at)
	}
	// Applied once the run ended.
	rec := runOf(b, id)
	if rec.Run.State != api.RunStopped || rec.Run.Reason != api.ReasonStopped || rec.Process != nil || groupAlive(pid) {
		t.Errorf("after stop: %+v process %+v", rec.Run, rec.Process)
	}
	if r := w.hubRun(id); r.State != api.RunStopped || r.Reason != api.ReasonStopped {
		t.Errorf("hub run = %+v", r)
	}
	// A stop with no live run changes nothing.
	w.hub.Enqueue(us, action("stop-again", api.KindStop, "garden", id, api.Payload{}))
	w.tick(b)
	w.oneOK(aid("stop-again"))
}

func TestAgentRunEndsWhenBridgeRestarts(t *testing.T) {
	t.Parallel()
	w, _, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-hang", id, "hang")
	w.waitRun(b, id, func(r api.Run) bool { return r.Activity == "Running sleep" })
	pid := runOf(b, id).Process.PID
	// The bridge dies without a word: its runner writes nothing more.
	b.Runner.mu.Lock()
	b.Runner.gone = true
	b.Runner.mu.Unlock()

	b = w.agentBridge(true)
	w.waitGroupGone(pid)
	run := runOf(b, id).Run
	if run.State != api.RunStopped || run.Reason != api.ReasonRestarted || run.EndedAt == nil || runOf(b, id).Process != nil {
		t.Errorf("recovered run = %+v", run)
	}
	w.tick(b)
	if r := w.hubRun(id); r.State != api.RunStopped || r.Reason != api.ReasonRestarted {
		t.Errorf("hub run = %+v", r)
	}
}

func TestAgentShutdownEndsRuns(t *testing.T) {
	t.Parallel()
	w, _, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-hang", id, "hang")
	pid := runOf(b, id).Process.PID
	b.stopRuns()
	w.waitGroupGone(pid)
	if r := w.hubRun(id); r.State != api.RunStopped || r.Reason != api.ReasonRestarted {
		t.Errorf("hub run = %+v", r)
	}
}

// A run is named for the harness that runs it, never the bead's creator:
// a bead a person raised under their own name still runs as the agent.
func TestRunNamedForHarness(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true, func(c *AgentConfig) { c.Name = "" })
	w.tick(b)
	w.ask(b, "ask-name", id, "Please close it")
	run := w.waitRun(b, id, settled)
	if run.Agent != "Claude Code" {
		t.Errorf("run agent = %q, want the harness's title", run.Agent)
	}
	if i := w.show(dir, id); !hasCommentBy(i, "Claude Code", "Done: watered.") {
		t.Errorf("the agent's note is not by the harness: %+v", i.Comments)
	}
}

func TestAddWithAskStartsRun(t *testing.T) {
	t.Parallel()
	w, dir, _ := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.hub.Enqueue(us, action("add-ask", api.KindAdd, "garden", "", api.Payload{Title: "Repot the fern and close", Ask: true}))
	w.tick(b)
	id := w.oneOK(aid("add-ask")).BeadID
	run := w.waitRun(b, id, settled)
	if run.State != api.RunDone || run.Agent != "helper" || run.Count != 1 {
		t.Errorf("run = %+v", run)
	}
	if w.show(dir, id).Status != "closed" {
		t.Error("the new bead is still open")
	}
	// A plain add starts nothing.
	w.hub.Enqueue(us, action("add-plain", api.KindAdd, "garden", "", api.Payload{Title: "Buy compost and close"}))
	w.tick(b)
	if rec := runOf(b, w.oneOK(aid("add-plain")).BeadID); rec.Run != nil {
		t.Errorf("a plain add started %+v", rec.Run)
	}
}

func TestAgentsSwitchPerRepo(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	b := w.agentBridge(false)
	saved := map[string]bool{}
	b.SaveAgents = func(repo string, on bool) error { saved[repo] = on; return nil }
	w.tick(b)
	w.ask(b, "ask-off", id, "close")
	rec := runOf(b, id)
	if rec.Run.State != api.RunStopped || rec.Run.Reason != api.ReasonNotAllowed || rec.Count != 0 {
		t.Errorf("run with agents off = %+v", rec.Run)
	}
	if i := w.show(dir, id); i.Status == "closed" || !hasCommentBy(i, author, "close") {
		t.Errorf("bead = %s %+v", i.Status, i.Comments)
	}
	if _, err := os.Stat(filepath.Join(w.state, "agents", "worktrees", "garden", id)); !os.IsNotExist(err) {
		t.Errorf("a refused run made a worktree: %v", err)
	}

	// The app cannot switch agents on; the bridge refuses and saves nothing.
	on := true
	w.hub.Enqueue(us, action("agents-on", api.KindAgents, "garden", "", api.Payload{On: &on}))
	w.hub.Enqueue(us, action("agents-elsewhere", api.KindAgents, "nowhere", "", api.Payload{On: &on}))
	w.tick(b)
	want := "switch agents on at this machine: meat-mode bridge agents on garden"
	if rs := w.hub.Results(aid("agents-on")); len(rs) != 1 || rs[0].OK || rs[0].Error != want {
		t.Errorf("agents on from the app = %+v, want the error %q", rs, want)
	}
	if rs := w.hub.Results(aid("agents-elsewhere")); len(rs) != 1 || rs[0].Error != ErrUnknownRepo {
		t.Errorf("switch on an unknown repo = %+v", rs)
	}
	if _, ok := saved["garden"]; ok {
		t.Error("a refused switch was saved")
	}
	w.ask(b, "ask-refused", id, "close")
	if rec := runOf(b, id); rec.Run == nil || rec.Run.Reason != api.ReasonNotAllowed {
		t.Errorf("run after the refused switch = %+v", rec.Run)
	}

	// The machine's own switch reaches the next report and the next ask.
	file := map[string]bool{"garden": true}
	b.LoadAgents = func() (map[string]bool, error) { return file, nil }
	w.tick(b)
	for _, r := range w.hub.Repos(us) {
		if r.Name == "garden" && !r.AgentsAllowed {
			t.Errorf("hub repo after the machine switch = %+v", r)
		}
	}
	w.ask(b, "ask-on", id, "close")
	if run := w.waitRun(b, id, settled); run.State != api.RunDone || run.Count != 1 {
		t.Errorf("run with agents on = %+v", run)
	}

	// Switching off from the app still works and is saved.
	off := false
	w.hub.Enqueue(us, action("agents-off", api.KindAgents, "garden", "", api.Payload{On: &off}))
	w.tick(b)
	w.oneOK(aid("agents-off"))
	if on, ok := saved["garden"]; !ok || on {
		t.Errorf("saved = %v, want garden off", saved)
	}
}

func TestSaveAgentsKeepsOtherSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	if err := (Config{HubURL: "https://hub.example.com", Token: "t", Sync: map[string]bool{"garden": true}}).Save(path); err != nil {
		t.Fatal(err)
	}
	if err := SaveAgents(path, "garden", true); err != nil {
		t.Fatal(err)
	}
	c, err := readConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Agents["garden"] || !c.Sync["garden"] || c.Token != "t" {
		t.Errorf("config = %+v", c)
	}
}

func TestAgentWorktreeGoesWithTheBead(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	keep := w.seed(dir, "Feed the cat")
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-1", id, "close")
	w.ask(b, "ask-2", keep, "reply")
	w.waitRun(b, id, settled)
	w.waitRun(b, keep, settled)
	wt := filepath.Join(w.state, "agents", "worktrees", "garden", id)
	dirty := filepath.Join(w.state, "agents", "worktrees", "garden", keep)
	if err := os.WriteFile(filepath.Join(dirty, "draft.txt"), []byte("Water the plants\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Both beads leave the set: the clean worktree and its merged branch
	// go, the one with a new file stays.
	b.Runner.Listed("garden", map[string]string{})
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("clean worktree still there: %v", err)
	}
	if out := beadsRun(t, dir, "git", "branch", "--list", "meat-mode/*"); strings.Contains(out, id) || !strings.Contains(out, keep) {
		t.Errorf("branches = %q", out)
	}
	if _, err := os.Stat(filepath.Join(dirty, "draft.txt")); err != nil {
		t.Errorf("the worktree with changes was removed: %v", err)
	}
	p := &putStub{}
	if err := b.Runner.Flush(w.ctx, p.put, true); err != nil || len(p.sent) != 1 || len(p.sent[0]) != 0 {
		t.Errorf("runs of beads out of the set are still reported: %+v, %v", p.sent, err)
	}
	if rec := runOf(b, id); rec.Count != 1 || rec.Session != "" {
		t.Errorf("record after removal = %+v", rec)
	}
}

// later moves the runner's clock d ahead of the real one.
func later(b *Bridge, d time.Duration) {
	b.Runner.mu.Lock()
	b.Runner.Now = func() time.Time { return time.Now().Add(d) }
	b.Runner.mu.Unlock()
}

func TestIdleWorktreeGoesAndComesBack(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true, func(c *AgentConfig) { c.KeepWorktrees = time.Hour })
	w.tick(b)
	w.ask(b, "ask-1", id, "reply")
	if run := w.waitRun(b, id, settled); run.State != api.RunWaiting {
		t.Fatalf("first run = %+v", run)
	}
	wt := filepath.Join(w.state, "agents", "worktrees", "garden", id)
	session := runOf(b, id).Session

	// Inside the window the worktree stays.
	b.Runner.Collect()
	if !isDir(wt) {
		t.Fatal("a worktree went inside its keep window")
	}

	// Past it the folder goes; the task is still listed, so its branch,
	// session and path stay.
	later(b, 2*time.Hour)
	b.Runner.Collect()
	if isDir(wt) {
		t.Fatal("an idle clean worktree is still there")
	}
	if out := beadsRun(t, dir, "git", "branch", "--list", "meat-mode/"+id); !strings.Contains(out, id) {
		t.Errorf("the branch of a listed task went: %q", out)
	}
	if rec := runOf(b, id); rec.Session != session || rec.Worktree != wt {
		t.Errorf("record after an idle removal = %+v", rec)
	}

	// The next ask makes it again on the same branch and resumes.
	w.ask(b, "ask-2", id, "close")
	two := w.waitRun(b, id, func(r api.Run) bool { return r.Count == 2 && settled(r) })
	if two.State != api.RunDone {
		t.Errorf("second run = %+v", two)
	}
	if got := strings.TrimSpace(beadsRun(t, wt, "git", "branch", "--show-current")); got != "meat-mode/"+id {
		t.Errorf("remade worktree branch = %q", got)
	}
	logf := filepath.Join(w.state, "agents", "logs", "garden", id, two.ID+".jsonl")
	if b, _ := os.ReadFile(logf); !strings.Contains(string(b), `"fake_resume":"`+session+`"`) {
		t.Errorf("the run in the remade worktree did not resume %s:\n%s", session, b)
	}
}

func TestBranchGoesWhenTheBeadLeavesAfterAnIdleRemoval(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true, func(c *AgentConfig) { c.KeepWorktrees = time.Hour })
	w.tick(b)
	w.ask(b, "ask-1", id, "reply")
	w.waitRun(b, id, settled)
	later(b, 2*time.Hour)
	b.Runner.Collect()
	if out := beadsRun(t, dir, "git", "branch", "--list", "meat-mode/"+id); !strings.Contains(out, id) {
		t.Fatalf("the branch went with the idle worktree: %q", out)
	}
	b.Runner.Listed("garden", map[string]string{})
	if out := beadsRun(t, dir, "git", "branch", "--list", "meat-mode/*"); strings.Contains(out, id) {
		t.Errorf("the merged branch of a bead that left the set stays: %q", out)
	}
	if rec := runOf(b, id); rec.Worktree != "" || rec.Session != "" || rec.Main != "" {
		t.Errorf("record after the bead left = %+v", rec)
	}
}

func TestBranchStaysUntilTheDefaultBranchHoldsIt(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-1", id, "reply")
	w.waitRun(b, id, settled)
	wt := filepath.Join(w.state, "agents", "worktrees", "garden", id)
	if err := os.WriteFile(filepath.Join(wt, "hose.txt"), []byte("Water the plants\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beadsRun(t, wt, "git", "add", "hose.txt")
	beadsRun(t, wt, "git", "-c", "user.name=Sam", "-c", "user.email=sam@example.com", "commit", "-q", "-m", "Hose")
	// The main checkout sits on another branch that holds the commit; the
	// default branch does not.
	beadsRun(t, dir, "git", "switch", "-q", "-c", "greenhouse")
	beadsRun(t, dir, "git", "-c", "user.name=Sam", "-c", "user.email=sam@example.com", "merge", "-q", "--no-edit", "meat-mode/"+id)

	b.Runner.Listed("garden", map[string]string{})
	if isDir(wt) {
		t.Error("the clean worktree of a bead that left the set is still there")
	}
	if out := beadsRun(t, dir, "git", "branch", "--list", "meat-mode/*"); !strings.Contains(out, id) {
		t.Errorf("a branch the default branch lacks was deleted: %q", out)
	}
}

func TestIdleWorktreeKeepsFilesABuildCannotMake(t *testing.T) {
	t.Parallel()
	w, dir, id := agentWorld(t)
	b := w.agentBridge(true, func(c *AgentConfig) { c.KeepWorktrees = time.Hour })
	w.tick(b)
	w.ask(b, "ask-1", id, "reply")
	w.waitRun(b, id, settled)
	wt := filepath.Join(w.state, "agents", "worktrees", "garden", id)
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, []byte("node_modules/\nplants.secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"node_modules/hose/index.js", "plants.secret"} {
		p := filepath.Join(wt, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("water\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	later(b, 2*time.Hour)
	b.Runner.Collect()
	if _, err := os.Stat(filepath.Join(wt, "plants.secret")); err != nil {
		t.Fatalf("an ignored file no build makes was removed: %v", err)
	}
	if err := os.Remove(filepath.Join(wt, "plants.secret")); err != nil {
		t.Fatal(err)
	}
	b.Runner.Collect()
	if isDir(wt) {
		t.Error("a worktree holding only dependencies is still there")
	}
}

func TestAskWaitsForItsWorktreeRemoval(t *testing.T) {
	t.Parallel()
	w, _, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	k := runKey("garden", id)
	b.Runner.mu.Lock()
	b.Runner.removing[k] = true
	b.Runner.mu.Unlock()
	w.ask(b, "ask-1", id, "close")
	if rec := runOf(b, id); rec.Run != nil {
		t.Fatalf("a run started while its worktree was being removed: %+v", rec.Run)
	}
	b.Runner.collected(collectJob{key: k, bead: id}, true)
	if run := w.waitRun(b, id, settled); run.State != api.RunDone {
		t.Errorf("the waiting ask's run = %+v", run)
	}
}

func TestCollectSweepsWorktreesItHasNoRecordOf(t *testing.T) {
	t.Parallel()
	w, dir, _ := agentWorld(t)
	b := w.agentBridge(true, func(c *AgentConfig) { c.KeepWorktrees = time.Hour })
	root := filepath.Join(w.state, "agents", "worktrees")
	stray := filepath.Join(root, "garden", "stray")
	odd := filepath.Join(root, "garden", "odd")
	beadsRun(t, dir, "git", "worktree", "add", "-q", "-b", "meat-mode/stray", stray)
	beadsRun(t, dir, "git", "worktree", "add", "-q", "-b", "trellis", odd)
	empty := filepath.Join(root, "orchard", "pears")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatal(err)
	}

	b.Runner.Collect()
	if !isDir(stray) {
		t.Fatal("a worktree with no record went inside the keep window")
	}
	later(b, 2*time.Hour)
	b.Runner.Collect()
	if isDir(stray) {
		t.Error("a stray worktree on its bead's branch is still there")
	}
	if !isDir(odd) {
		t.Error("a worktree on another branch was removed")
	}
	if isDir(filepath.Join(root, "orchard")) {
		t.Error("empty folders are still there")
	}
}

// putStub records each run list put, and fails while fail is set.
type putStub struct {
	sent [][]api.RunReport
	fail bool
}

func (p *putStub) put(_ context.Context, list []api.RunReport) error {
	if p.fail {
		return errors.New("hub down")
	}
	p.sent = append(p.sent, list)
	return nil
}

// reportingRunner is a runner with a settled run on water and on feed in
// garden, on a clock the test moves.
func reportingRunner(t *testing.T) (*AgentRunner, *time.Time) {
	t.Helper()
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	r := &AgentRunner{Dir: t.TempDir(), Log: quiet, Now: func() time.Time { return now }}
	r.mu.Lock()
	r.init()
	r.record("garden", "water").Run = &api.Run{ID: "run-1", State: api.RunDone}
	r.record("garden", "feed").Run = &api.Run{ID: "run-2", State: api.RunStopped}
	r.notify()
	r.mu.Unlock()
	return r, &now
}

func (r *AgentRunner) testChange() {
	r.mu.Lock()
	r.notify()
	r.mu.Unlock()
}

func TestFlushThrottlesAndRetries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, now := reportingRunner(t)
	p := &putStub{}
	flush := func(force bool) {
		t.Helper()
		if err := r.Flush(ctx, p.put, force); err != nil {
			t.Fatal(err)
		}
	}

	flush(false)
	if len(p.sent) != 1 || len(p.sent[0]) != 2 {
		t.Fatalf("first flush sent %+v", p.sent)
	}
	// A change within RunsEvery of the last send waits; after it, it goes.
	r.testChange()
	*now = now.Add(RunsEvery - time.Millisecond)
	flush(false)
	if len(p.sent) != 1 {
		t.Fatalf("sent within RunsEvery: %+v", p.sent)
	}
	*now = now.Add(time.Millisecond)
	flush(false)
	if len(p.sent) != 2 {
		t.Fatalf("not sent after RunsEvery: %+v", p.sent)
	}

	// An unchanged list is not sent again.
	*now = now.Add(RunsEvery)
	flush(false)
	if len(p.sent) != 2 {
		t.Fatalf("an unchanged list was sent again: %+v", p.sent)
	}

	// force sends at once, changed or not.
	flush(true)
	if len(p.sent) != 3 {
		t.Fatalf("force did not send: %+v", p.sent)
	}

	// A failed send goes again with the next flush, with no new change.
	r.testChange()
	*now = now.Add(RunsEvery)
	p.fail = true
	if err := r.Flush(ctx, p.put, false); err == nil {
		t.Fatal("a failed put returned no error")
	}
	p.fail = false
	flush(false)
	if len(p.sent) != 4 {
		t.Fatalf("the failed list was not sent again: %+v", p.sent)
	}
}

func TestReportApplyingNamesTheAction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, now := reportingRunner(t)
	p := &putStub{}
	if err := r.ReportApplying(ctx, p.put, "act-1", "garden", "water"); err != nil {
		t.Fatal(err)
	}
	if len(p.sent) != 1 || len(p.sent[0]) != 2 {
		t.Fatalf("sent %+v", p.sent)
	}
	for _, rr := range p.sent[0] {
		if want := map[string]string{"water": "act-1"}[rr.BeadID]; rr.Applying != want {
			t.Errorf("%s applying = %q, want %q", rr.BeadID, rr.Applying, want)
		}
	}
	// It counts as the send: the list is clean afterwards.
	*now = now.Add(RunsEvery)
	if err := r.Flush(ctx, p.put, false); err != nil || len(p.sent) != 1 {
		t.Fatalf("flush after applying sent %+v, %v", p.sent, err)
	}
	// A failed one leaves the list to send again.
	p.fail = true
	if err := r.ReportApplying(ctx, p.put, "act-2", "garden", "feed"); err == nil {
		t.Fatal("a failed put returned no error")
	}
	p.fail = false
	if err := r.Flush(ctx, p.put, false); err != nil || len(p.sent) != 2 {
		t.Fatalf("flush after a failed applying send sent %+v, %v", p.sent, err)
	}
	if got := p.sent[1]; got[0].Applying != "" || got[1].Applying != "" {
		t.Errorf("a later flush still names an action: %+v", got)
	}
}

func TestWaitTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r, now := reportingRunner(t)
	if err := r.Flush(ctx, (&putStub{}).put, true); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(RunsEvery)
	start := time.Now()
	r.WaitTurn(ctx)
	if d := time.Since(start); d > RunsEvery/2 {
		t.Errorf("waited %v after RunsEvery had passed", d)
	}
	*now = now.Add(-RunsEvery)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	start = time.Now()
	r.WaitTurn(cctx)
	if d := time.Since(start); d > RunsEvery/2 {
		t.Errorf("waited %v with ctx ended", d)
	}
}

func TestFlushReportsOnlyListedBeads(t *testing.T) {
	t.Parallel()
	r, _ := reportingRunner(t)
	p := &putStub{}
	// Before garden is listed every run shows; after, only listed beads.
	r.Listed("garden", map[string]string{"water": "fp"})
	if err := r.Flush(context.Background(), p.put, true); err != nil {
		t.Fatal(err)
	}
	if got := p.sent[0]; len(got) != 1 || got[0].BeadID != "water" {
		t.Errorf("reported %+v", got)
	}
}
