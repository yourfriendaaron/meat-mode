package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

// runLog is the raw output of a bead's run.
func (w *world) runLog(bead, run string) string {
	w.t.Helper()
	b, _ := os.ReadFile(filepath.Join(w.state, "agents", "logs", "garden", bead, run+".jsonl"))
	return string(b)
}

// A steering harness takes a note into the live run at once.
func TestFakeHarnessSteers(t *testing.T) {
	t.Setenv("FAKEAGENT_STEP", "2s")
	w, dir, id := agentWorld(t)
	b := w.agentBridgeWith(true, fakeHarness(t, nil))
	w.tick(b)
	w.ask(b, "ask-slow", id, "slow")
	first := w.waitRun(b, id, func(r api.Run) bool { return r.Activity == "Reading the bead" })
	w.ask(b, "ask-more", id, "Then close it")
	if rec := runOf(b, id); rec.Run.Queued != 1 || rec.Run.State != api.RunWorking {
		t.Errorf("after the note: %+v", rec.Run)
	}
	run := w.waitRun(b, id, settled)
	if run.ID != first.ID || run.State != api.RunDone || run.Count != 1 {
		t.Errorf("settled = %+v", run)
	}
	if !hasCommentBy(w.show(dir, id), author, "Then close it") {
		t.Error("the note is not on the bead")
	}
	if n := strings.Count(w.runLog(id, run.ID), `{"read":1}`); n != 2 {
		t.Errorf("the agent read %d messages, want the prompt and the note", n)
	}
}

// Without Steer, notes wait for the turn to end and then resume the
// session in a new process, joined into one prompt, within the same run.
func TestFakeHarnessHoldsNotesUntilTheTurnEnds(t *testing.T) {
	t.Setenv("FAKEAGENT_STEP", "2s")
	w, dir, id := agentWorld(t)
	b := w.agentBridgeWith(true, fakeHarness(t, func(i *harness.Info) { i.Steer = false }))
	w.tick(b)
	w.ask(b, "ask-slow", id, "slow")
	first := w.waitRun(b, id, func(r api.Run) bool { return r.Activity == "Reading the bead" })
	w.ask(b, "ask-water", id, "Water the ferns")
	w.ask(b, "ask-close", id, "Then close it")
	if rec := runOf(b, id); rec.Run.Queued != 2 || rec.Run.State != api.RunWorking {
		t.Errorf("after two notes: %+v", rec.Run)
	}
	run := w.waitRun(b, id, settled)
	rec := runOf(b, id)
	if run.ID != first.ID || run.State != api.RunDone || run.Count != 1 || rec.Count != 1 || run.Queued != 0 {
		t.Errorf("settled = %+v, count %d", run, rec.Count)
	}
	if i := w.show(dir, id); i.Status != "closed" {
		t.Errorf("the resumed turn did not close the bead: %s", i.Status)
	}
	log := w.runLog(id, run.ID)
	if n := strings.Count(log, `"fakeResume":"`+rec.Session+`"`); n != 1 || rec.Session == "" {
		t.Errorf("want one resume of %q:\n%s", rec.Session, log)
	}
	if !strings.Contains(log, `New note from the user on bead `+id+`:\n\nWater the ferns\n\nNew note from the user on bead `+id+`:\n\nThen close it"`) {
		t.Errorf("the notes were not joined into one prompt:\n%s", log)
	}
	if rec.Harness != "fake" {
		t.Errorf("runs.json records the harness %q", rec.Harness)
	}
}

// After a turn that failed, held notes stay on the bead for the next ask.
func TestFakeHarnessKeepsNotesAfterAFailedTurn(t *testing.T) {
	t.Setenv("FAKEAGENT_STEP", "2s")
	w, dir, id := agentWorld(t)
	b := w.agentBridgeWith(true, fakeHarness(t, func(i *harness.Info) { i.Steer = false }))
	w.tick(b)
	w.ask(b, "ask-slow", id, "slow fail")
	first := w.waitRun(b, id, func(r api.Run) bool { return r.Activity == "Reading the bead" })
	w.ask(b, "ask-close", id, "Then close it")
	run := w.waitRun(b, id, settled)
	if run.ID != first.ID || run.Reason != api.ReasonFailed || run.Queued != 0 {
		t.Errorf("settled = %+v", run)
	}
	if strings.Contains(w.runLog(id, run.ID), `"fakeResume":"`+runOf(b, id).Session+`"`) {
		t.Error("a failed turn was resumed")
	}
	if i := w.show(dir, id); i.Status == "closed" || !hasCommentBy(i, author, "Then close it") {
		t.Errorf("the note should wait on the open bead: %+v", i)
	}
}

// A metered harness's run ends once its reported spending passes the cap,
// and one that stays under it runs as any other.
func TestFakeHarnessMetered(t *testing.T) {
	t.Parallel()
	metered := func(i *harness.Info) { i.Budget = harness.BudgetMetered }
	cap := func(c *AgentConfig) { c.BudgetUSD = 1 }

	w, _, id := agentWorld(t)
	b := w.agentBridgeWith(true, fakeHarness(t, metered), cap)
	w.tick(b)
	w.ask(b, "ask-spend", id, "spend")
	run := w.waitRun(b, id, settled)
	if run.Reason != api.ReasonBudget || !strings.Contains(run.Detail, "$1.00") {
		t.Errorf("over the cap: %+v", run)
	}

	w2, _, id2 := agentWorld(t)
	b2 := w2.agentBridgeWith(true, fakeHarness(t, metered), cap)
	w2.tick(b2)
	w2.ask(b2, "ask-reply", id2, "reply")
	if run := w2.waitRun(b2, id2, settled); run.State != api.RunWaiting {
		t.Errorf("under the cap: %+v", run)
	}
}

// A session recorded under another harness is never resumed; a record
// that names none is Claude Code's.
func TestSessionOfAnotherHarnessIsNotResumed(t *testing.T) {
	t.Parallel()
	w, _, id := agentWorld(t)
	b := w.agentBridge(true)
	w.tick(b)
	w.ask(b, "ask-1", id, "reply")
	w.waitRun(b, id, settled)
	session := runOf(b, id).Session
	if rec := runOf(b, id); rec.Harness != "claude" || session == "" {
		t.Fatalf("record = %+v", rec)
	}
	// An older bridge recorded no harness: still resumed by Claude Code.
	b.Runner.mu.Lock()
	b.Runner.records[runKey("garden", id)].Harness = ""
	b.Runner.mu.Unlock()
	w.ask(b, "ask-2", id, "reply")
	two := w.waitRun(b, id, func(r api.Run) bool { return r.Count == 2 && settled(r) })
	if !strings.Contains(w.runLog(id, two.ID), `"fake_resume":"`+session+`"`) {
		t.Error("a record without a harness was not resumed by claude")
	}
	// Another harness starts fresh.
	b = w.agentBridgeWith(true, fakeHarness(t, nil))
	w.tick(b)
	w.ask(b, "ask-3", id, "reply")
	three := w.waitRun(b, id, func(r api.Run) bool { return r.Count == 3 && settled(r) })
	if !strings.Contains(w.runLog(id, three.ID), `"fakeResume":""`) {
		t.Errorf("the fake resumed claude's session:\n%s", w.runLog(id, three.ID))
	}
	if rec := runOf(b, id); rec.Harness != "fake" || rec.Session == session {
		t.Errorf("record after the fake = %+v", rec)
	}
}

// The hello names the harness and its limits; a failed check sends no
// version and refuses asks while syncing carries on.
func TestHelloNamesTheHarness(t *testing.T) {
	t.Parallel()
	f := fakeHarness(t, func(i *harness.Info) { i.Budget = harness.BudgetNone; i.Steer = false })
	f.CheckErr = errors.New("fakeagent is not signed in")
	w, _, id := agentWorld(t)
	b := w.agentBridgeWith(true, f)
	w.tick(b)
	hellos := w.hub.Hellos()
	got := hellos[len(hellos)-1].Hello.Harness
	want := api.HarnessInfo{Name: "fake", Title: "Fake", InstructionFile: "AGENTS.md", Budget: harness.BudgetNone, TimeoutSeconds: 60}
	if got == nil || *got != want {
		t.Fatalf("hello harness = %+v, want %+v", got, want)
	}
	w.ask(b, "ask-1", id, "reply")
	run := w.waitRun(b, id, settled)
	if run.Reason != api.ReasonFailed || !strings.Contains(run.Detail, "Fake is not available on this machine: fakeagent is not signed in") || run.Count != 0 {
		t.Errorf("run = %+v", run)
	}

	w2, _, _ := agentWorld(t)
	b2 := w2.agentBridge(true)
	w2.tick(b2)
	hellos = w2.hub.Hellos()
	got = hellos[len(hellos)-1].Hello.Harness
	want = api.HarnessInfo{Name: "claude", Title: "Claude Code", Version: "1.0.0", InstructionFile: "CLAUDE.md",
		Budget: harness.BudgetNative, BudgetUSD: 2, TimeoutSeconds: 60, Steer: true}
	if got == nil || *got != want {
		t.Errorf("hello harness = %+v, want %+v", got, want)
	}
}
