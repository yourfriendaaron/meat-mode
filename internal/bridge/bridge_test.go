package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/beads"
	"github.com/yourfriendaaron/meat-mode/internal/beads/beadstest"
	"github.com/yourfriendaaron/meat-mode/internal/beads/fakebeads"
	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
)

const (
	token  = "token-alex"
	us     = "desk"
	author = "alex-app"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// world is one bridge test setup: a fake hub, a roots directory and a beads
// implementation (real bd or the fake).
type world struct {
	t      *testing.T
	ctx    context.Context
	hub    *fakehub.Hub
	bd     beads.Beads
	real   bool
	root   string
	state  string
	bridge *Bridge
}

func newWorld(t *testing.T, real bool) *world {
	t.Helper()
	if real {
		beadstest.NeedBD(t)
	}
	hub := fakehub.New(map[string]string{token: us, "token-other": "other"})
	t.Cleanup(hub.Close)
	w := &world{t: t, ctx: context.Background(), hub: hub, real: real, root: t.TempDir(), state: t.TempDir()}
	if real {
		w.bd = &beads.CLI{}
	} else {
		w.bd = fakebeads.New()
	}
	return w
}

// both runs a test against the fake beads and, when bd is installed, real bd.
func both(t *testing.T, f func(t *testing.T, w *world)) {
	t.Run("fake", func(t *testing.T) { f(t, newWorld(t, false)) })
	t.Run("bd", func(t *testing.T) { f(t, newWorld(t, true)) })
}

// repo makes a beads repo under the root, opted in or not.
func (w *world) repo(name string, optIn bool) string {
	w.t.Helper()
	if w.real {
		dir := beadstest.NewRepo(w.t, w.root, name)
		if optIn {
			beadstest.OptIn(w.t, dir)
		}
		return dir
	}
	dir := filepath.Join(w.root, name)
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		w.t.Fatal(err)
	}
	cfg := "# Beads configuration\n"
	if optIn {
		cfg += "meat-mode.sync: true\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte(cfg), 0o644); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".beads", "metadata.json"), []byte("{}\n"), 0o644); err != nil {
		w.t.Fatal(err)
	}
	return dir
}

// seed creates a human bead and returns its id.
func (w *world) seed(dir, title string) string {
	w.t.Helper()
	id, err := w.bd.Create(w.ctx, dir, beads.NewIssue{Title: title, ActionID: "seed-" + title, Actor: "agent"})
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

// start builds a bridge over the world's state directory, as a fresh process
// would. The bridge before it is gone, so its journal lock is released.
func (w *world) start(personal string) *Bridge {
	w.t.Helper()
	if w.bridge != nil {
		w.bridge.Journal.Close()
	}
	j, err := OpenJournal(w.state)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { j.Close() })
	w.bridge = &Bridge{
		Hub: &Hub{URL: w.hub.URL, Token: token}, Beads: w.bd, Journal: j,
		Roots: []string{w.root}, Home: w.root, Personal: personal, Version: "test", Log: quiet,
	}
	return w.bridge
}

func (w *world) tick(b *Bridge) {
	w.t.Helper()
	if err := b.Tick(w.ctx, true); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) show(dir, id string) beads.Issue {
	w.t.Helper()
	i, err := w.bd.Show(w.ctx, dir, id)
	if err != nil {
		w.t.Fatal(err)
	}
	return i
}

func (w *world) oneOK(id string) api.ActionResult {
	w.t.Helper()
	rs := w.hub.Results(id)
	if len(rs) != 1 || !rs[0].OK {
		w.t.Fatalf("results for %s = %+v, want one ok", id, rs)
	}
	return rs[0]
}

// aid turns a readable test name into the UUID-shaped action id the hub
// would send.
func aid(name string) string {
	h := sha256.Sum256([]byte(name))
	x := hex.EncodeToString(h[:16])
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

func action(name, kind, repo, bead string, p api.Payload) api.Action {
	return api.Action{ID: aid(name), Kind: kind, Bridge: us, Repo: repo, BeadID: bead, Payload: p, Author: author}
}

func TestReplaysEveryKindOnce(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		dir := w.repo("garden", true)
		water := w.seed(dir, "Water the plants")
		feed := w.seed(dir, "Feed the cat")
		until := time.Date(2030, 3, 1, 9, 0, 0, 0, time.UTC)
		due := time.Date(2030, 3, 2, 17, 0, 0, 0, time.UTC)
		acts := []api.Action{
			action("a-note", api.KindNote, "garden", water, api.Payload{Text: "Bought soil"}),
			action("a-snooze", api.KindSnooze, "garden", water, api.Payload{Until: &until}),
			action("a-done", api.KindDone, "garden", feed, api.Payload{}),
			action("a-reopen", api.KindReopen, "garden", feed, api.Payload{}),
			action("a-dismiss", api.KindDismiss, "garden", feed, api.Payload{}),
			action("a-add", api.KindAdd, "garden", "", api.Payload{Title: "Repot the fern", Due: &due}),
		}
		for _, a := range acts {
			w.hub.Enqueue(us, a)
		}
		b := w.start("")
		w.tick(b)
		for _, a := range acts {
			w.oneOK(a.ID)
		}

		wi := w.show(dir, water)
		if len(wi.Comments) != 1 || wi.Comments[0].Author != author || wi.Comments[0].Text != "Bought soil" {
			t.Errorf("comments = %+v", wi.Comments)
		}
		if wi.DeferUntil == nil || !wi.DeferUntil.Equal(until) || wi.Status != "deferred" {
			t.Errorf("snooze: status %s defer %v", wi.Status, wi.DeferUntil)
		}
		fi := w.show(dir, feed)
		if fi.Status != "closed" || fi.CloseReason == nil || *fi.CloseReason != "Dismissed" {
			t.Errorf("feed after done, reopen, dismiss: %s %v", fi.Status, fi.CloseReason)
		}
		added := w.oneOK(aid("a-add")).BeadID
		ai := w.show(dir, added)
		if ai.Title != "Repot the fern" || ai.ActionID() != aid("a-add") || ai.DueAt == nil || !ai.DueAt.Equal(due) ||
			!slices.Contains(ai.Labels, "human") || ai.CreatedBy != author {
			t.Errorf("added bead = %+v", ai)
		}

		items, ok := w.hub.Items(us, "garden")
		if !ok || len(items) != 3 {
			t.Fatalf("hub items = %+v", items)
		}
		for _, it := range items {
			if it.ID == water && len(it.Comments) != 1 {
				t.Errorf("hub copy of %s has comments %+v", water, it.Comments)
			}
		}

		// A second round applies nothing again.
		w.tick(b)
		for _, a := range acts {
			w.oneOK(a.ID)
		}
		if n := len(w.show(dir, water).Comments); n != 1 {
			t.Errorf("comments after second round = %d", n)
		}
	})
}

// TestSurvivesCrashMidReplay kills the bridge after bd ran but before the
// journal recorded done or the hub heard the result. The restarted bridge
// must find the change and not make it twice.
func TestSurvivesCrashMidReplay(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		dir := w.repo("garden", true)
		water := w.seed(dir, "Water the plants")
		acts := []api.Action{
			action("c-note", api.KindNote, "garden", water, api.Payload{Text: "Watered twice"}),
			action("c-add", api.KindAdd, "garden", "", api.Payload{Title: "Buy compost"}),
			action("c-done", api.KindDone, "garden", water, api.Payload{}),
		}
		for _, a := range acts {
			// One action per run, so each crash happens at that action.
			w.hub.Enqueue(us, a)
			b := w.start("")
			b.afterBD = func(api.Action) error { return errCrash }
			if err := b.Tick(w.ctx, true); !errors.Is(err, errCrash) {
				t.Fatalf("%s: tick = %v, want the simulated crash", a.ID, err)
			}
			if rs := w.hub.Results(a.ID); len(rs) != 0 {
				t.Fatalf("%s: result reached the hub before done: %+v", a.ID, rs)
			}
			b.Journal.Close()

			w.tick(w.start(""))
			w.oneOK(a.ID)
		}

		wi := w.show(dir, water)
		if len(wi.Comments) != 1 {
			t.Errorf("note applied %d times", len(wi.Comments))
		}
		if wi.Status != "closed" {
			t.Errorf("done not applied: %s", wi.Status)
		}
		list, err := w.bd.ListHuman(w.ctx, dir, false)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, i := range list {
			if i.Title == "Buy compost" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("add applied %d times", n)
		}
		if got := w.oneOK(aid("c-add")).BeadID; got == "" {
			t.Error("add result has no bead id after the restart")
		}
	})
}

// TestResendsResultAfterLostReport: bd ran and done was journaled, but the
// hub never heard the result. The next round re-sends it without bd.
func TestResendsResultAfterLostReport(t *testing.T) {
	w := newWorld(t, false)
	dir := w.repo("garden", true)
	water := w.seed(dir, "Water the plants")
	b := w.start("")
	w.tick(b)
	w.hub.Enqueue(us, action("r-note", api.KindNote, "garden", water, api.Payload{Text: "Done already"}))
	b.afterBD = nil
	// Apply directly, as if the result POST then failed.
	st := b.repos["garden"]
	if _, err := b.apply(w.ctx, w.hub.Pending(us)[0], st.Repo); err != nil {
		t.Fatal(err)
	}
	w.tick(w.start(""))
	w.oneOK(aid("r-note"))
	if n := len(w.show(dir, water).Comments); n != 1 {
		t.Errorf("comments = %d", n)
	}
}

func TestSkipsReposNotOptedIn(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		in := w.repo("garden", true)
		out := w.repo("kitchen", false)
		w.seed(in, "Water the plants")
		cake := w.seed(out, "Bake a cake")
		personal := filepath.Join(t.TempDir(), "me")
		if w.real {
			personal = beadstest.NewRepo(t, filepath.Dir(personal), "me")
			beadstest.Run(t, personal, "bd", "create", "Call Sam", "--json")
		} else {
			writeFile(t, filepath.Join(personal, ".beads", "metadata.json"), "{}\n")
			w.bd.(*fakebeads.Fake).Put(personal, beads.Issue{ID: "me-1", Title: "Call Sam", Status: "open"})
		}
		w.hub.Enqueue(us, action("n-1", api.KindDone, "kitchen", cake, api.Payload{}))

		b := w.start(personal)
		w.tick(b)

		want := []api.Repo{
			{Name: "garden", OptedIn: true},
			{Name: "kitchen", OptedIn: false},
			{Name: "personal", Personal: true, OptedIn: true},
		}
		if got := w.hub.Repos(us); !slices.Equal(got, want) {
			t.Errorf("repos = %+v, want %+v", got, want)
		}
		if _, ok := w.hub.Items(us, "kitchen"); ok {
			t.Error("items sent for a repo that has not opted in")
		}
		if items, _ := w.hub.Items(us, "garden"); len(items) != 1 {
			t.Errorf("garden items = %+v", items)
		}
		if items, _ := w.hub.Items(us, "personal"); len(items) != 1 || items[0].Title != "Call Sam" {
			t.Errorf("personal items (no human label needed) = %+v", items)
		}
		rs := w.hub.Results(aid("n-1"))
		if len(rs) != 1 || rs[0].OK || rs[0].Error != ErrNotSynced {
			t.Errorf("action on an unsynced repo: %+v", rs)
		}
		if w.show(out, cake).Status == "closed" {
			t.Error("action applied to a repo that has not opted in")
		}
	})
}

// TestRefusesUnknownBeads: the hub can only name beads this bridge listed.
// Anything else, such as a bead without the human label or a flag-shaped id,
// is refused without running bd.
func TestRefusesUnknownBeads(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		dir := w.repo("garden", true)
		water := w.seed(dir, "Water the plants")
		var chore string
		if w.real {
			chore = strings.TrimSpace(beadstest.Run(t, dir, "bd", "create", "Agent chore", "--silent"))
		} else {
			chore = "tst-chore"
			w.bd.(*fakebeads.Fake).Put(dir, beads.Issue{ID: chore, Title: "Agent chore", Status: "open"})
		}
		acts := []api.Action{
			action("u-chore", api.KindDone, "garden", chore, api.Payload{}),
			action("u-flag", api.KindNote, "garden", "-f/etc/hostname", api.Payload{Text: water}),
			action("u-prof", api.KindDone, "garden", "--mem-profile=x", api.Payload{}),
			action("u-water", api.KindNote, "garden", water, api.Payload{Text: "Bought soil"}),
		}
		for _, a := range acts {
			w.hub.Enqueue(us, a)
		}
		w.tick(w.start(""))
		for _, id := range []string{aid("u-chore"), aid("u-flag"), aid("u-prof")} {
			if rs := w.hub.Results(id); len(rs) != 1 || rs[0].OK || rs[0].Error != ErrUnknownBead {
				t.Errorf("%s: results = %+v", id, rs)
			}
		}
		w.oneOK(aid("u-water"))
		if w.show(dir, chore).Status == "closed" {
			t.Error("closed a bead the bridge never listed")
		}
		if n := len(w.show(dir, water).Comments); n != 1 {
			t.Errorf("water has %d comments, want 1", n)
		}
	})
}

func TestIgnoresActionsForAnotherBridge(t *testing.T) {
	w := newWorld(t, false)
	dir := w.repo("garden", true)
	water := w.seed(dir, "Water the plants")
	a := action("o-1", api.KindDone, "garden", water, api.Payload{})
	a.Bridge = "other"
	w.hub.Enqueue(us, a) // the hub misroutes it to us
	w.tick(w.start(""))
	if rs := w.hub.Results(aid("o-1")); len(rs) != 0 {
		t.Errorf("results = %+v", rs)
	}
	if w.show(dir, water).Status == "closed" {
		t.Error("applied an action owned by another bridge")
	}
}

func TestSyncsOnlyWhenSomethingChanged(t *testing.T) {
	w := newWorld(t, false)
	dir := w.repo("garden", true)
	water := w.seed(dir, "Water the plants")
	b := w.start("")
	w.tick(b)
	w.tick(b)
	if n := w.hub.ItemPuts(us, "garden"); n != 1 {
		t.Fatalf("puts after two quiet rounds = %d", n)
	}
	// An agent comments: updated_at stays, comment_count moves.
	if err := w.bd.AddComment(w.ctx, dir, water, "agent", "Use rain water"); err != nil {
		t.Fatal(err)
	}
	w.tick(b)
	items, _ := w.hub.Items(us, "garden")
	if n := w.hub.ItemPuts(us, "garden"); n != 2 || len(items) != 1 || len(items[0].Comments) != 1 {
		t.Fatalf("after a comment: puts %d items %+v", n, items)
	}
	// A deleted bead drops out of the set.
	w.bd.(*fakebeads.Fake).Put(dir, beads.Issue{ID: water, Title: "Water the plants", Status: "open"}) // no human label
	w.tick(b)
	if items, _ := w.hub.Items(us, "garden"); len(items) != 0 {
		t.Fatalf("item without the human label still sent: %+v", items)
	}
	// After RefreshEvery a full push goes out even with no change.
	b.repos["garden"].pushed = time.Now().Add(-RefreshEvery)
	w.tick(b)
	if n := w.hub.ItemPuts(us, "garden"); n != 4 {
		t.Fatalf("puts after refresh = %d", n)
	}
}

func TestOneRepoFailingDoesNotStopOthers(t *testing.T) {
	w := newWorld(t, false)
	bad := w.repo("broken", true)
	good := w.repo("garden", true)
	w.seed(good, "Water the plants")
	w.bd.(*fakebeads.Fake).FailDir[bad] = errors.New("database is locked")
	w.tick(w.start(""))
	if items, _ := w.hub.Items(us, "garden"); len(items) != 1 {
		t.Errorf("garden items = %+v", items)
	}
}

func TestHubDownThenBack(t *testing.T) {
	w := newWorld(t, false)
	dir := w.repo("garden", true)
	w.seed(dir, "Water the plants")
	b := w.start("")
	w.hub.SetDown(true)
	if err := b.Tick(w.ctx, true); err == nil {
		t.Fatal("tick with the hub down returned no error")
	}
	w.hub.SetDown(false)
	w.tick(b)
	if items, _ := w.hub.Items(us, "garden"); len(items) != 1 {
		t.Errorf("items = %+v", items)
	}
	hs := w.hub.Hellos()
	if len(hs) != 1 || hs[0].Version != "test" || hs[0].BdVersion != "1.3.1" {
		t.Errorf("hellos = %+v", hs)
	}
}

func TestRefusesOldBD(t *testing.T) {
	w := newWorld(t, false)
	w.bd.(*fakebeads.Fake).Ver = "1.2.2"
	err := w.start("").Run(w.ctx)
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("Run with bd 1.2.2 = %v", err)
	}
	if len(w.hub.Hellos()) != 0 {
		t.Error("an old bd still greeted the hub")
	}
}

func TestRunStopsWithContext(t *testing.T) {
	w := newWorld(t, false)
	w.repo("garden", true)
	ctx, cancel := context.WithCancel(w.ctx)
	done := make(chan error, 1)
	go func() { done <- w.start("").Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(w.hub.Repos(us)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(w.hub.Repos(us)) != 1 {
		t.Error("Run did not report repos")
	}
}

func TestIgnoresActionsWithoutAUUID(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		dir := w.repo("garden", true)
		water := w.seed(dir, "Water the plants")
		b := w.start("")
		w.tick(b)
		for _, id := range []string{"", "not-a-uuid", strings.Repeat("a", 5<<20)} {
			a := action("x", api.KindNote, "garden", water, api.Payload{Text: "Hello"})
			a.ID = id
			w.hub.Enqueue(us, a)
		}
		w.tick(b)
		if got := w.show(dir, water).Comments; len(got) != 0 {
			t.Fatalf("applied an action without a valid id: %+v", got)
		}
		if _, ok := b.Journal.Get(""); ok {
			t.Fatal("journaled an empty action id")
		}
	})
}

func TestScanCadence(t *testing.T) {
	w := newWorld(t, false)
	w.repo("garden", true)
	b := w.start("")
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	if err := b.Tick(w.ctx, false); err != nil {
		t.Fatal(err)
	}
	w.repo("kitchen", true)
	now = now.Add(ScanEvery - time.Second)
	if err := b.Tick(w.ctx, false); err != nil {
		t.Fatal(err)
	}
	if got := w.hub.Repos(us); len(got) != 1 {
		t.Fatalf("repos before the next scan = %+v", got)
	}
	now = now.Add(time.Second)
	if err := b.Tick(w.ctx, false); err != nil {
		t.Fatal(err)
	}
	if got := w.hub.Repos(us); len(got) != 2 {
		t.Fatalf("repos after the next scan = %+v", got)
	}
}

func TestSameNamedReposSyncSideBySide(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		home := w.repo("projects/garden", true)
		work := w.repo("work/garden", true)
		w.seed(home, "Water the plants")
		hedge := w.seed(work, "Trim the hedge")
		w.hub.Enqueue(us, action("s-done", api.KindDone, "work/garden", hedge, api.Payload{}))
		b := w.start("")
		w.tick(b)

		want := []api.Repo{{Name: "projects/garden", OptedIn: true}, {Name: "work/garden", OptedIn: true}}
		if got := w.hub.Repos(us); !slices.Equal(got, want) {
			t.Errorf("repos = %+v, want %+v", got, want)
		}
		if items, _ := w.hub.Items(us, "projects/garden"); len(items) != 1 || items[0].Title != "Water the plants" {
			t.Errorf("projects/garden items = %+v", items)
		}
		if items, _ := w.hub.Items(us, "work/garden"); len(items) != 1 || items[0].Title != "Trim the hedge" {
			t.Errorf("work/garden items = %+v", items)
		}
		w.oneOK(aid("s-done"))
		if w.show(work, hedge).Status != "closed" {
			t.Error("the action went to the wrong repo")
		}
	})
}

func TestSyncSwitch(t *testing.T) {
	both(t, func(t *testing.T, w *world) {
		garden := w.repo("projects/garden", false)
		w.seed(garden, "Water the plants")
		kitchen := w.repo("projects/kitchen", true)
		w.seed(kitchen, "Descale the kettle")
		saved := map[string]bool{}
		b := w.start("")
		b.SaveSwitch = func(repo string, on bool) error { saved[repo] = on; return nil }
		on, off := true, false
		w.hub.Enqueue(us, action("sw-on", api.KindSync, "projects/garden", "", api.Payload{On: &on}))
		w.hub.Enqueue(us, action("sw-off", api.KindSync, "projects/kitchen", "", api.Payload{On: &off}))
		// Repos the bridge did not find, and paths outside what it found, are refused.
		w.hub.Enqueue(us, action("sw-unknown", api.KindSync, "projects/shed", "", api.Payload{On: &on}))
		w.hub.Enqueue(us, action("sw-escape", api.KindSync, "projects/garden/../../..", "", api.Payload{On: &on}))
		w.tick(b)

		w.oneOK(aid("sw-on"))
		w.oneOK(aid("sw-off"))
		for _, name := range []string{"sw-unknown", "sw-escape"} {
			if rs := w.hub.Results(aid(name)); len(rs) != 1 || rs[0].OK || rs[0].Error != ErrUnknownRepo {
				t.Errorf("%s: results %+v, want %q", name, rs, ErrUnknownRepo)
			}
		}
		if !maps.Equal(saved, map[string]bool{"projects/garden": true, "projects/kitchen": false}) {
			t.Errorf("saved switches %v", saved)
		}
		want := []api.Repo{{Name: "projects/garden", OptedIn: true}, {Name: "projects/kitchen", OptedIn: false}}
		if got := w.hub.Repos(us); !slices.Equal(got, want) {
			t.Errorf("repos = %+v, want %+v", got, want)
		}
		if items, _ := w.hub.Items(us, "projects/garden"); len(items) != 1 {
			t.Errorf("garden items after switching on = %+v", items)
		}

		// A new process reads the saved switches, which beat the config file.
		b = w.start("")
		b.Switches = saved
		w.tick(b)
		if got := w.hub.Repos(us); !slices.Equal(got, want) {
			t.Errorf("repos after restart = %+v, want %+v", got, want)
		}
	})
}

func TestSyncSwitchRefusesThePersonalRepo(t *testing.T) {
	w := newWorld(t, false)
	personal := filepath.Join(t.TempDir(), "me")
	writeFile(t, filepath.Join(personal, ".beads", "metadata.json"), "{}\n")
	b := w.start(personal)
	b.SaveSwitch = func(string, bool) error { t.Error("saved a switch for the personal repo"); return nil }
	off := false
	w.hub.Enqueue(us, action("sw-me", api.KindSync, PersonalRepo, "", api.Payload{On: &off}))
	w.tick(b)
	if rs := w.hub.Results(aid("sw-me")); len(rs) != 1 || rs[0].Error != ErrPersonalSwitch {
		t.Errorf("results %+v", rs)
	}
}
