package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

const (
	id1 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a51"
	id2 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a52"
	id3 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a53"
)

// clock is a settable time source for tests.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }
func (c *clock) tick()          { c.t = c.t.Add(time.Second) }

func open(t *testing.T) (*Store, *clock) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c := &clock{t: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	s.now = c.now
	return s, c
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// seed creates bridge "laptop" with repo "garden" (opted in), "personal"
// and "chores" (not opted in), and two items in garden.
func seed(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.CreateBridge("laptop")
	must(t, err)
	must(t, s.ReplaceRepos("laptop", []api.Repo{
		{Name: "garden", OptedIn: true},
		{Name: "personal", Personal: true},
		{Name: "chores"},
	}))
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open", Priority: 2},
		{ID: "gd-2", Title: "Repot the fern", Status: "open", Priority: 1,
			Comments: []api.Comment{{ID: "c1", Author: "agent", Text: "Which pot?"}}},
	}))
}

func item(t *testing.T, st api.State, key string) api.Item {
	t.Helper()
	for _, it := range st.Items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("no item %s in state", key)
	return api.Item{}
}

func hasItem(st api.State, key string) bool {
	for _, it := range st.Items {
		if it.Key == key {
			return true
		}
	}
	return false
}

func TestOpenUsesWALAndSchemaVersion(t *testing.T) {
	s, _ := open(t)
	var mode string
	must(t, s.DB().QueryRow("PRAGMA journal_mode").Scan(&mode))
	if mode != "wal" {
		t.Fatalf("journal_mode = %q", mode)
	}
	var v int
	must(t, s.DB().QueryRow("PRAGMA user_version").Scan(&v))
	if v != schemaVersion {
		t.Fatalf("user_version = %d", v)
	}
	var fk int
	must(t, s.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk))
	if fk != 1 {
		t.Fatal("foreign keys off")
	}
}

func TestOpenReopensExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	_, err = s.CreateBridge("laptop")
	must(t, err)
	s.Close()
	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	bs, err := s.Bridges()
	must(t, err)
	if len(bs) != 1 {
		t.Fatalf("bridges after reopen = %v", bs)
	}
}

func TestOpenFailsOnReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := filepath.Join(t.TempDir(), "data")
	must(t, os.Mkdir(dir, 0o500))
	_, err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenIssueAuthenticateRevoke(t *testing.T) {
	s, _ := open(t)
	tok, err := s.CreateBridge("laptop")
	must(t, err)
	if !strings.HasPrefix(tok, "mm_") || len(tok) != 3+43 {
		t.Fatalf("token %q", tok)
	}
	var stored []byte
	must(t, s.DB().QueryRow(`SELECT token_hash FROM bridges`).Scan(&stored))
	if strings.Contains(string(stored), tok) || len(stored) != 32 {
		t.Fatal("token stored in the clear")
	}
	name, err := s.Authenticate(tok)
	must(t, err)
	if name != "laptop" {
		t.Fatalf("name %q", name)
	}
	if _, err := s.Authenticate(tok + "x"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong token: %v", err)
	}
	if _, err := s.CreateBridge("laptop"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.CreateBridge("Bad Name"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad name: %v", err)
	}
	must(t, s.RevokeBridge("laptop"))
	if _, err := s.Authenticate(tok); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked: %v", err)
	}
	if err := s.RevokeBridge("laptop"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke twice: %v", err)
	}
	// The name can be paired again with a new token.
	tok2, err := s.CreateBridge("laptop")
	must(t, err)
	if tok2 == tok {
		t.Fatal("token reused")
	}
	if _, err := s.Authenticate(tok2); err != nil {
		t.Fatal(err)
	}
}

func TestLogAppendPullApply(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	a, created, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, "alex-app")
	must(t, err)
	if !created || a.Status != api.StatusPending || a.Bridge != "laptop" || a.Repo != "garden" || a.BeadID != "gd-1" || a.Author != "alex-app" {
		t.Fatalf("appended %+v", a)
	}
	c.tick()
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindNote, Key: "laptop/garden/gd-2",
		Payload: api.Payload{Text: "The blue one"}}, "alex-app")
	must(t, err)

	pending, err := s.PendingActions("laptop")
	must(t, err)
	if len(pending) != 2 || pending[0].ID != id1 || pending[1].ID != id2 {
		t.Fatalf("pending %+v", pending)
	}

	// Duplicate id: stored action unchanged, nothing added.
	dup, created, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindSnooze, Key: "laptop/garden/gd-2"}, "alex-app")
	must(t, err)
	if created || dup.Kind != api.KindDone || dup.Seq != a.Seq {
		t.Fatalf("duplicate %+v created=%v", dup, created)
	}

	c.tick()
	must(t, s.ReportResult("laptop", id1, api.ActionResult{OK: true}))
	must(t, s.ReportResult("laptop", id2, api.ActionResult{OK: false, Error: "bd exited 1"}))
	pending, err = s.PendingActions("laptop")
	must(t, err)
	if len(pending) != 0 {
		t.Fatalf("still pending %+v", pending)
	}
	got, err := s.Action(id1)
	must(t, err)
	if got.Status != api.StatusApplied || got.AppliedAt == nil {
		t.Fatalf("applied %+v", got)
	}
	got, err = s.Action(id2)
	must(t, err)
	if got.Status != api.StatusFailed || got.Error != "bd exited 1" {
		t.Fatalf("failed %+v", got)
	}
	// A repeated result changes nothing.
	must(t, s.ReportResult("laptop", id2, api.ActionResult{OK: true}))
	got, _ = s.Action(id2)
	if got.Status != api.StatusFailed {
		t.Fatalf("status after retry %q", got.Status)
	}

	recent, err := s.RecentActions(10)
	must(t, err)
	if len(recent) != 2 || recent[0].ID != id2 {
		t.Fatalf("recent %+v", recent)
	}

	st, err := s.State()
	must(t, err)
	if e := item(t, st, "laptop/garden/gd-2").Error; e != "bd exited 1" {
		t.Fatalf("item error %q", e)
	}
}

func TestRoutingKeepsBridgesApart(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	tok, err := s.CreateBridge("desk")
	must(t, err)
	_ = tok
	must(t, s.ReplaceRepos("desk", []api.Repo{{Name: "garden", OptedIn: true}}))
	must(t, s.ReplaceItems("desk", "garden", []api.Item{{ID: "gd-1", Title: "Feed the cat", Status: "open"}}))

	_, _, err = s.AppendAction(api.NewAction{ID: id1, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindDone, Key: "desk/garden/gd-1"}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id3, Kind: api.KindAdd, Bridge: "desk", Repo: "garden",
		Payload: api.Payload{Title: "Buy seeds"}}, "alex-app")
	must(t, err)

	lp, _ := s.PendingActions("laptop")
	dp, _ := s.PendingActions("desk")
	if len(lp) != 1 || lp[0].ID != id1 {
		t.Fatalf("laptop got %+v", lp)
	}
	if len(dp) != 2 || dp[0].ID != id2 || dp[1].ID != id3 {
		t.Fatalf("desk got %+v", dp)
	}
	// A bridge cannot report a result for another bridge's action.
	if err := s.ReportResult("laptop", id2, api.ActionResult{OK: true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-bridge result: %v", err)
	}
}

func TestAppendValidation(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	until := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		na   api.NewAction
		want error
	}{
		{"bad id", api.NewAction{ID: "nope", Kind: api.KindDone, Key: "laptop/garden/gd-1"}, ErrInvalid},
		{"unknown kind", api.NewAction{ID: id1, Kind: "explode", Key: "laptop/garden/gd-1"}, ErrInvalid},
		{"missing key", api.NewAction{ID: id1, Kind: api.KindDone}, ErrInvalid},
		{"unknown item", api.NewAction{ID: id1, Kind: api.KindDone, Key: "laptop/garden/gd-9"}, ErrNotFound},
		{"unknown bridge", api.NewAction{ID: id1, Kind: api.KindDone, Key: "desk/garden/gd-1"}, ErrNotFound},
		{"snooze without until", api.NewAction{ID: id1, Kind: api.KindSnooze, Key: "laptop/garden/gd-1"}, ErrInvalid},
		{"empty note", api.NewAction{ID: id1, Kind: api.KindNote, Key: "laptop/garden/gd-1", Payload: api.Payload{Text: "  "}}, ErrInvalid},
		{"long note", api.NewAction{ID: id1, Kind: api.KindNote, Key: "laptop/garden/gd-1", Payload: api.Payload{Text: strings.Repeat("a", MaxNoteLen+1)}}, ErrInvalid},
		{"add without title", api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden"}, ErrInvalid},
		{"add to unknown repo", api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "attic", Payload: api.Payload{Title: "x"}}, ErrNotFound},
		{"add to repo not opted in", api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "chores", Payload: api.Payload{Title: "x"}}, ErrInvalid},
		{"add without bridge", api.NewAction{ID: id1, Kind: api.KindAdd, Repo: "garden", Payload: api.Payload{Title: "x"}}, ErrInvalid},
	}
	for _, tc := range cases {
		if _, _, err := s.AppendAction(tc.na, "alex-app"); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	// Valid ones pass, including a note of exactly the limit and an add to the personal repo.
	ok := []api.NewAction{
		{ID: id1, Kind: api.KindSnooze, Key: "laptop/garden/gd-1", Payload: api.Payload{Until: &until}},
		{ID: id2, Kind: api.KindNote, Key: "laptop/garden/gd-1", Payload: api.Payload{Text: strings.Repeat("a", MaxNoteLen)}},
		{ID: id3, Kind: api.KindAdd, Bridge: "laptop", Repo: "personal", Payload: api.Payload{Title: "Call Sam"}},
	}
	for _, na := range ok {
		if _, _, err := s.AppendAction(na, "alex-app"); err != nil {
			t.Errorf("%s: %v", na.Kind, err)
		}
	}
}

func TestOptimisticFolding(t *testing.T) {
	until := time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		kind  string
		setup func(t *testing.T, s *Store)
		check func(t *testing.T, it api.Item)
	}{
		{api.KindDone, nil, func(t *testing.T, it api.Item) {
			if it.Status != "closed" || it.CloseReason != "Done in Meat Mode" || it.ClosedAt == nil {
				t.Errorf("done: %+v", it)
			}
		}},
		{api.KindDismiss, nil, func(t *testing.T, it api.Item) {
			if it.Status != "closed" || it.CloseReason != "Dismissed" {
				t.Errorf("dismiss: %+v", it)
			}
		}},
		{api.KindReopen, func(t *testing.T, s *Store) {
			closed := time.Date(2026, 2, 28, 9, 0, 0, 0, time.UTC)
			must(t, s.ReplaceItems("laptop", "garden", []api.Item{
				{ID: "gd-1", Title: "Water the plants", Status: "closed", CloseReason: "Done", ClosedAt: &closed},
				{ID: "gd-2", Title: "Repot the fern", Status: "open"},
			}))
		}, func(t *testing.T, it api.Item) {
			if it.Status != "open" || it.CloseReason != "" || it.ClosedAt != nil {
				t.Errorf("reopen: %+v", it)
			}
		}},
		{api.KindSnooze, nil, func(t *testing.T, it api.Item) {
			if it.DeferUntil == nil || !it.DeferUntil.Equal(until) {
				t.Errorf("snooze: %+v", it)
			}
		}},
		{api.KindNote, nil, func(t *testing.T, it api.Item) {
			if n := len(it.Comments); n != 1 || it.Comments[0].Author != "alex-app" || it.Comments[0].Text != "Done, thanks" {
				t.Errorf("note: %+v", it.Comments)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			s, _ := open(t)
			seed(t, s)
			if tc.setup != nil {
				tc.setup(t, s)
			}
			_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: tc.kind, Key: "laptop/garden/gd-1",
				Payload: api.Payload{Until: &until, Text: "Done, thanks"}}, "alex-app")
			must(t, err)
			st, err := s.State()
			must(t, err)
			it := item(t, st, "laptop/garden/gd-1")
			if !it.Pending {
				t.Error("not marked pending")
			}
			tc.check(t, it)
			if st.Unapplied != 1 {
				t.Errorf("unapplied = %d", st.Unapplied)
			}
			if other := item(t, st, "laptop/garden/gd-2"); other.Pending {
				t.Error("other item marked pending")
			}
			// Once applied, the hub shows the bridge's copy again.
			must(t, s.ReportResult("laptop", id1, api.ActionResult{OK: true}))
			st, _ = s.State()
			if it := item(t, st, "laptop/garden/gd-1"); it.Pending {
				t.Errorf("still pending after apply: %+v", it)
			}
		})
	}
}

func TestCancel(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindDone, Key: "laptop/garden/gd-2"}, "alex-app")
	must(t, err)

	a, err := s.CancelAction(id1)
	must(t, err)
	if a.Status != api.StatusCancelled {
		t.Fatalf("cancel %+v", a)
	}
	if _, err := s.CancelAction(id1); err != nil {
		t.Fatalf("cancel twice: %v", err)
	}
	st, _ := s.State()
	if it := item(t, st, "laptop/garden/gd-1"); it.Pending || it.Status != "open" {
		t.Fatalf("cancelled action still folded: %+v", it)
	}
	if p, _ := s.PendingActions("laptop"); len(p) != 1 || p[0].ID != id2 {
		t.Fatalf("pending after cancel %+v", p)
	}

	must(t, s.ReportResult("laptop", id2, api.ActionResult{OK: true}))
	if _, err := s.CancelAction(id2); !errors.Is(err, ErrConflict) {
		t.Fatalf("cancel applied: %v", err)
	}
	if _, err := s.CancelAction(id3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel unknown: %v", err)
	}
}

func TestReplaceItems(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	st, _ := s.State()
	first := item(t, st, "laptop/garden/gd-2").FirstSeen
	must(t, s.MarkSeen("laptop/garden/gd-2"))

	c.tick()
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-2", Title: "Repot the fern in the blue pot", Status: "in_progress"},
		{ID: "gd-3", Title: "Sweep the porch", Status: "open"},
	}))
	st, _ = s.State()
	if hasItem(st, "laptop/garden/gd-1") {
		t.Fatal("dropped item still shown")
	}
	it := item(t, st, "laptop/garden/gd-2")
	if it.Title != "Repot the fern in the blue pot" || !it.FirstSeen.Equal(first) || it.SeenAt == nil {
		t.Fatalf("kept item lost state: %+v", it)
	}
	if it.Comments == nil {
		t.Fatal("comments is nil, want empty list")
	}
	if n := item(t, st, "laptop/garden/gd-3"); !n.FirstSeen.Equal(c.t) || n.SeenAt != nil {
		t.Fatalf("new item %+v", n)
	}

	// Items for a repo the bridge has not listed are refused.
	if err := s.ReplaceItems("laptop", "attic", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown repo: %v", err)
	}
	// Dropping a repo drops its items.
	must(t, s.ReplaceRepos("laptop", []api.Repo{{Name: "personal", Personal: true}}))
	st, _ = s.State()
	if len(st.Items) != 0 || len(st.Repos) != 1 {
		t.Fatalf("after dropping repo: %d items, %d repos", len(st.Items), len(st.Repos))
	}
}

func TestReplaceItemsReportsNewItems(t *testing.T) {
	s, _ := open(t)
	var changes []Change
	s.Watch(func(c Change) { changes = append(changes, c) })
	seed(t, s)
	last := changes[len(changes)-1]
	if last.Kind != "items" || len(last.NewItems) != 2 {
		t.Fatalf("change %+v", last)
	}
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{{ID: "gd-1", Status: "open"}, {ID: "gd-5", Status: "open"}}))
	last = changes[len(changes)-1]
	if len(last.NewItems) != 1 || last.NewItems[0] != "laptop/garden/gd-5" {
		t.Fatalf("new items %v", last.NewItems)
	}
}

func TestAddProvisionalLifecycle(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	due := time.Date(2026, 3, 3, 17, 0, 0, 0, time.UTC)
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Buy seeds", Due: &due}}, "alex-app")
	must(t, err)
	key := "laptop/garden/pending-" + id1

	st, _ := s.State()
	p := item(t, st, key)
	if !p.Pending || p.Title != "Buy seeds" || p.DueAt == nil || !p.DueAt.Equal(due) || p.Status != "open" {
		t.Fatalf("provisional %+v", p)
	}
	// Keyed actions on a provisional item are refused until it is real.
	if _, _, err := s.AppendAction(api.NewAction{ID: id2, Kind: api.KindDone, Key: key}, "alex-app"); !errors.Is(err, ErrConflict) {
		t.Fatalf("done on provisional: %v", err)
	}

	// Applied, but the bridge has not sent the repo yet: still shown.
	c.tick()
	must(t, s.ReportResult("laptop", id1, api.ActionResult{OK: true, BeadID: "gd-7"}))
	st, _ = s.State()
	if !hasItem(st, key) {
		t.Fatal("provisional item vanished before the bead arrived")
	}
	if a, _ := s.Action(id1); a.BeadID != "gd-7" {
		t.Fatalf("beadId not recorded: %+v", a)
	}

	// The real bead arrives: provisional item gone, real one shown.
	c.tick()
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{
		{ID: "gd-1", Status: "open"}, {ID: "gd-7", Title: "Buy seeds", Status: "open"},
	}))
	st, _ = s.State()
	if hasItem(st, key) {
		t.Fatal("provisional item still shown after the bead arrived")
	}
	if it := item(t, st, "laptop/garden/gd-7"); it.Pending {
		t.Fatal("real item marked pending")
	}
}

func TestAddProvisionalDropsAfterSyncWithoutBead(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Buy seeds"}}, "alex-app")
	must(t, err)
	c.tick()
	must(t, s.ReportResult("laptop", id1, api.ActionResult{OK: true, BeadID: "gd-7"}))
	c.tick()
	must(t, s.ReplaceItems("laptop", "garden", []api.Item{{ID: "gd-1", Status: "open"}}))
	st, _ := s.State()
	if hasItem(st, "laptop/garden/pending-"+id1) {
		t.Fatal("provisional item outlived a sync that did not include it")
	}
}

func TestAddResultNeedsBeadID(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Buy seeds"}}, "alex-app")
	must(t, err)
	if err := s.ReportResult("laptop", id1, api.ActionResult{OK: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestRevokedBridgeLeavesState(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, "alex-app")
	must(t, err)
	must(t, s.RevokeBridge("laptop"))
	st, err := s.State()
	must(t, err)
	if len(st.Items)+len(st.Repos)+len(st.Bridges)+st.Unapplied != 0 {
		t.Fatalf("revoked bridge still in state: %+v", st)
	}
}

func TestSplitKey(t *testing.T) {
	b, r, id, ok := SplitKey("laptop/garden/gd-1")
	if !ok || b != "laptop" || r != "garden" || id != "gd-1" {
		t.Fatal("split failed")
	}
	for _, bad := range []string{"", "laptop/garden", "Laptop/garden/gd-1", "laptop//gd-1", "laptop/garden/a/b", "laptop/../gd-1"} {
		if _, _, _, ok := SplitKey(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestReplaceReposDropsItemsWhenOptedOut(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	must(t, s.ReplaceRepos("laptop", []api.Repo{
		{Name: "garden"},
		{Name: "personal", Personal: true},
	}))
	st, _ := s.State()
	if hasItem(st, "laptop/garden/gd-1") {
		t.Fatal("items of a repo that opted out are still shown")
	}
}
