package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

const (
	id4 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a54"
	id5 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a55"
	id6 = "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a56"
)

func hubKey(id string) string { return ItemKey(HubBridge, PersonalRepo, id) }

func addPersonal(t *testing.T, s *Store, id, title string, due *time.Time) api.Action {
	t.Helper()
	a, _, err := s.AppendAction(api.NewAction{ID: id, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: title, Due: due}}, "alex-app")
	must(t, err)
	return a
}

func TestPersonalListWorksWithNoBridge(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	c := &clock{t: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	s.now = c.now

	st := mustState(t, s)
	if len(st.Repos) != 1 || !reflect.DeepEqual(st.Repos[0], api.Repo{Bridge: HubBridge, Name: PersonalRepo, Personal: true, OptedIn: true}) {
		t.Fatalf("a new hub's repos = %+v, want only the personal list", st.Repos)
	}

	due := c.t.Add(3 * time.Hour)
	a := addPersonal(t, s, id1, "Water the plants", &due)
	if a.Status != api.StatusApplied || a.BeadID == "" || a.AppliedAt == nil {
		t.Fatalf("add on the hub = %+v, want applied with the new item's id", a)
	}
	key := hubKey(a.BeadID)
	it := item(t, mustState(t, s), key)
	if rk, first, err := s.ReminderKey(context.Background(), it); err != nil || rk != key || !first.Equal(it.FirstSeen) {
		t.Errorf("reminder key = %q, %v, %v; want the item's own key and first-seen time", rk, first, err)
	}
	if it.Title != "Water the plants" || it.Status != "open" || it.Pending || it.DueAt == nil || !it.DueAt.Equal(due) || it.CreatedBy != "alex-app" {
		t.Fatalf("added item %+v", it)
	}

	until := c.t.Add(24 * time.Hour)
	for _, na := range []api.NewAction{
		{ID: id2, Kind: api.KindSnooze, Key: key, Payload: api.Payload{Until: &until}},
		{ID: id3, Kind: api.KindNote, Key: key, Payload: api.Payload{Text: "Use the rain barrel"}},
		{ID: id4, Kind: api.KindDone, Key: key},
	} {
		c.tick()
		got, created, err := s.AppendAction(na, "alex-app")
		must(t, err)
		if !created || got.Status != api.StatusApplied {
			t.Fatalf("%s = %+v", na.Kind, got)
		}
	}
	st = mustState(t, s)
	it = item(t, st, key)
	if it.Status != "closed" || it.CloseReason != ReasonDone || it.DeferUntil == nil || !it.DeferUntil.Equal(until) ||
		len(it.Comments) != 1 || it.Comments[0].Text != "Use the rain barrel" || it.Comments[0].ID != id3 || it.Pending {
		t.Fatalf("item after snooze, note and done: %+v", it)
	}
	if st.Unapplied != 0 {
		t.Errorf("unapplied = %d, want 0", st.Unapplied)
	}
	// Undo of an applied action is the inverse action, as for a bead.
	if _, err := s.CancelAction(id4); !errors.Is(err, ErrConflict) {
		t.Errorf("cancel an applied personal action: %v", err)
	}
	_, _, err = s.AppendAction(api.NewAction{ID: id5, Kind: api.KindReopen, Key: key}, "alex-app")
	must(t, err)
	// A repeated id changes nothing.
	again, created, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: "Water the plants"}}, "alex-app")
	must(t, err)
	if created || again.BeadID != a.BeadID {
		t.Errorf("repeated add = %+v, created %v", again, created)
	}
	// Won't do closes it as dismissed.
	other := addPersonal(t, s, id6, "Sort the recycling", nil)
	_, _, err = s.AppendAction(api.NewAction{ID: "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a57", Kind: api.KindDismiss, Key: hubKey(other.BeadID)}, "alex-app")
	must(t, err)
	if it := item(t, mustState(t, s), hubKey(other.BeadID)); it.Status != "closed" || it.CloseReason != ReasonDismissed {
		t.Fatalf("dismissed item %+v", it)
	}
	s.Close()

	// It all survives a restart.
	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	st = mustState(t, s)
	// The reopened store runs on the real clock, so the item dismissed on
	// the test clock is past its week and hidden.
	if len(st.Items) != 1 {
		t.Fatalf("items after restart = %+v", st.Items)
	}
	if it := item(t, st, key); it.Key != key || it.Status != "open" || len(it.Comments) != 1 {
		t.Fatalf("item after restart = %+v", it)
	}
}

func TestPersonalListRefusesWhatABridgeRepoWould(t *testing.T) {
	s, _ := open(t)
	on := true
	for _, na := range []api.NewAction{
		{ID: id1, Kind: api.KindSync, Bridge: HubBridge, Repo: PersonalRepo, Payload: api.Payload{On: &on}},
		{ID: id1, Kind: api.KindDone, Key: hubKey("mm-404")},
		{ID: id1, Kind: api.KindAdd, Bridge: HubBridge, Repo: "garden", Payload: api.Payload{Title: "Rake"}},
	} {
		if _, _, err := s.AppendAction(na, "alex-app"); err == nil {
			t.Errorf("accepted %+v", na)
		}
	}
}

func TestHubNameIsReserved(t *testing.T) {
	s, _ := open(t)
	if _, err := s.CreateBridge(HubBridge); !errors.Is(err, ErrInvalid) {
		t.Errorf("create a bridge named hub: %v", err)
	}
	if err := s.RevokeBridge(HubBridge); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke the hub: %v", err)
	}
	if bs, _ := s.Bridges(); len(bs) != 0 {
		t.Errorf("bridges = %+v, want none", bs)
	}
	if _, err := s.Authenticate(TokenPrefix); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("authenticate: %v", err)
	}
}

func TestMigrationStopsOnAMachineNamedHub(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	// Wind the database back to version 1 with a machine called hub.
	for _, q := range []string{
		`DROP TABLE personal_origins`,
		`DELETE FROM bridges WHERE name = 'hub'`,
		`INSERT INTO bridges (name, token_hash, created_at) VALUES ('hub', x'00', '2026-03-01T09:00:00Z')`,
		`PRAGMA user_version = 1`,
	} {
		_, err := s.DB().Exec(q)
		must(t, err)
	}
	s.Close()
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), `named "hub"`) {
		t.Fatalf("open = %v, want the reserved-name error", err)
	}
}

func TestHubHidesClosedPersonalItemsAfterAWeek(t *testing.T) {
	s, c := open(t)
	a := addPersonal(t, s, id1, "Buy stamps", nil)
	_, _, err := s.AppendAction(api.NewAction{ID: id2, Kind: api.KindDone, Key: hubKey(a.BeadID)}, "alex-app")
	must(t, err)
	c.t = c.t.Add(6 * 24 * time.Hour)
	if !hasItem(mustState(t, s), hubKey(a.BeadID)) {
		t.Fatal("closed item gone after six days")
	}
	c.t = c.t.Add(2 * 24 * time.Hour)
	if hasItem(mustState(t, s), hubKey(a.BeadID)) {
		t.Fatal("closed item still shown after eight days")
	}
}

// legacySeed builds what a hub held before the personal list moved: bridge
// with personal repo "personal" holding the given items, written
// straight to the tables as the older hub did.
func legacySeed(t *testing.T, s *Store, bridge string, items ...api.Item) {
	t.Helper()
	if bs, _ := s.Bridges(); !slices.ContainsFunc(bs, func(b api.Bridge) bool { return b.Name == bridge }) {
		_, err := s.CreateBridge(bridge)
		must(t, err)
	}
	_, err := s.DB().Exec(`INSERT INTO repos (bridge, name, personal, opted_in) VALUES (?, 'personal', 1, 1)
		ON CONFLICT DO NOTHING`, bridge)
	must(t, err)
	for _, it := range items {
		if it.Comments == nil {
			it.Comments = []api.Comment{}
		}
		tx, err := s.DB().Begin()
		must(t, err)
		must(t, putHubItem(tx, PersonalRepo, it, s.now(), nil))
		_, err = tx.Exec(`UPDATE items SET bridge = ? WHERE bridge = 'hub' AND repo = 'personal' AND id = ?`, bridge, it.ID)
		must(t, err)
		must(t, tx.Commit())
	}
}

// legacyAction appends a pending action for the bridge's personal repo, as
// the older hub did.
func legacyAction(t *testing.T, s *Store, bridge, id, kind, beadID string, p api.Payload) {
	t.Helper()
	_, err := s.DB().Exec(`INSERT INTO actions (id, kind, bridge, repo, bead_id, payload, author, status, created_at)
		VALUES (?, ?, ?, 'personal', ?, ?, 'alex-app', 'pending', ?)`, id, kind, bridge, beadID,
		mustJSON(t, p), fmtTime(s.now()))
	must(t, err)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	must(t, err)
	return string(b)
}

func TestTransferPersonalMovesEverythingOnce(t *testing.T) {
	s, c := open(t)
	closed := c.t.Add(-time.Hour)
	legacySeed(t, s, "laptop",
		api.Item{ID: "lp-1", Title: "Water the plants", Status: "open", Priority: 2, CreatedAt: c.t, UpdatedAt: c.t,
			Comments: []api.Comment{{ID: "c1", Author: "alex", Text: "Both pots"}}},
		api.Item{ID: "lp-2", Title: "Renew the library card", Status: "open", Priority: 2},
		api.Item{ID: "lp-3", Title: "Sweep the porch", Status: "closed", ClosedAt: &closed, CloseReason: "Done in Meat Mode"},
	)
	_, err := s.DB().Exec(`UPDATE items SET seen_at = ? WHERE bridge = 'laptop' AND id = 'lp-1'`, fmtTime(c.t))
	must(t, err)
	// Taps made while the laptop was off, and one on a bead the hub lost.
	legacyAction(t, s, "laptop", id1, api.KindDone, "lp-2", api.Payload{})
	legacyAction(t, s, "laptop", id2, api.KindAdd, "", api.Payload{Title: "Buy stamps"})
	legacyAction(t, s, "laptop", id3, api.KindDone, "lp-9", api.Payload{})
	_, err = s.DB().Exec(`CREATE TABLE reminder_fired (item_key TEXT NOT NULL, fire_at INTEGER NOT NULL, PRIMARY KEY (item_key, fire_at))`)
	must(t, err)
	_, err = s.DB().Exec(`INSERT INTO reminder_fired VALUES ('laptop/personal/lp-1', 1)`)
	must(t, err)
	_, err = s.DB().Exec(`CREATE TABLE reminder_early (item_key TEXT NOT NULL, due_at INTEGER NOT NULL, PRIMARY KEY (item_key, due_at));
		INSERT INTO reminder_early VALUES ('laptop/personal/lp-1', 1)`)
	must(t, err)

	c.tick()
	must(t, s.TransferPersonal())
	must(t, s.TransferPersonal()) // a rerun changes nothing

	st := mustState(t, s)
	var titles []string
	for _, it := range st.Items {
		if it.Bridge != HubBridge || it.Repo != PersonalRepo {
			t.Errorf("item left outside the personal list: %s", it.Key)
		}
		titles = append(titles, it.Title)
	}
	slices.Sort(titles)
	if want := []string{"Buy stamps", "Renew the library card", "Sweep the porch", "Water the plants"}; !slices.Equal(titles, want) {
		t.Fatalf("titles = %v, want %v", titles, want)
	}
	if it := item(t, st, hubKey("lp-1")); len(it.Comments) != 1 || it.Comments[0].Text != "Both pots" || it.SeenAt == nil {
		t.Errorf("lp-1 lost its comments or seen time: %+v", it)
	}
	if it := item(t, st, hubKey("lp-2")); it.Status != "closed" || it.Pending {
		t.Errorf("the waiting done was not applied: %+v", it)
	}
	if it := item(t, st, hubKey("lp-3")); it.Status != "closed" || it.ClosedAt == nil {
		t.Errorf("closed item %+v", it)
	}
	if len(st.Repos) != 1 || st.Unapplied != 0 {
		t.Errorf("repos %+v, unapplied %d", st.Repos, st.Unapplied)
	}
	if pa, _ := s.PendingActions("laptop"); len(pa) != 0 {
		t.Errorf("laptop still has actions waiting: %+v", pa)
	}
	for id, want := range map[string]string{id1: api.StatusApplied, id2: api.StatusApplied, id3: api.StatusCancelled} {
		a, err := s.Action(id)
		must(t, err)
		if a.Status != want {
			t.Errorf("action %s = %s, want %s", a.Kind, a.Status, want)
		}
	}
	keys, err := queryStrings(s.DB(), `SELECT item_key FROM reminder_fired UNION ALL SELECT item_key FROM reminder_early`)
	must(t, err)
	if !slices.Equal(keys, []string{hubKey("lp-1"), hubKey("lp-1")}) {
		t.Errorf("reminder keys = %v", keys)
	}

	// An older bridge still sending its personal repo adds only beads the
	// hub has not had; the hub's copy of the others stays as it is.
	c.tick()
	must(t, s.ReplaceItems("laptop", "personal", []api.Item{
		{ID: "lp-1", Title: "Water the plants twice", Status: "open"},
		{ID: "lp-2", Title: "Renew the library card", Status: "open"},
		{ID: "lp-4", Title: "Call Sam", Status: "open"},
	}))
	st = mustState(t, s)
	if len(st.Items) != 5 {
		t.Fatalf("items after the older bridge's push: %d", len(st.Items))
	}
	if it := item(t, st, hubKey("lp-1")); it.Title != "Water the plants" {
		t.Errorf("a re-sent bead overwrote the hub's copy: %+v", it)
	}
	if it := item(t, st, hubKey("lp-2")); it.Status != "closed" {
		t.Errorf("a re-sent bead reopened the hub's copy: %+v", it)
	}
	item(t, st, hubKey("lp-4"))
	// And a bridge's personal repo takes no new action.
	if _, _, err := s.AppendAction(api.NewAction{ID: id4, Kind: api.KindAdd, Bridge: "laptop", Repo: "personal",
		Payload: api.Payload{Title: "Rake"}}, "alex-app"); !errors.Is(err, ErrNotFound) {
		t.Errorf("add to a bridge's personal repo: %v", err)
	}
}

func TestTransferKeepsTwoBridgesApart(t *testing.T) {
	s, _ := open(t)
	legacySeed(t, s, "laptop", api.Item{ID: "td-1", Title: "Water the plants", Status: "open"})
	legacySeed(t, s, "desk", api.Item{ID: "td-1", Title: "Call Sam", Status: "open"})
	must(t, s.TransferPersonal())
	st := mustState(t, s)
	if len(st.Items) != 2 {
		t.Fatalf("items = %+v", st.Items)
	}
	a, b := item(t, st, hubKey("td-1")), item(t, st, hubKey("td-1-2"))
	if a.Title == b.Title || a.ID != "td-1" || b.ID != "td-1-2" {
		t.Errorf("items %+v and %+v", a, b)
	}
	// The renamed one still takes actions.
	_, _, err := s.AppendAction(api.NewAction{ID: id6, Kind: api.KindDone, Key: b.Key}, "alex-app")
	must(t, err)
	// A rerun from either bridge adds nothing.
	must(t, s.ReplaceItems("desk", "personal", []api.Item{{ID: "td-1", Title: "Call Sam", Status: "open"}}))
	must(t, s.ReplaceItems("laptop", "personal", []api.Item{{ID: "td-1", Title: "Water the plants", Status: "open"}}))
	if n := len(mustState(t, s).Items); n != 2 {
		t.Errorf("items after rerun = %d", n)
	}
}

func TestTransferDropsABeadsRepeat(t *testing.T) {
	s, c := open(t)
	due := c.t.Add(time.Hour)
	legacySeed(t, s, "laptop", api.Item{ID: "lp-1", Title: "Water the plants", Status: "open", DueAt: &due,
		Repeat: &api.Repeat{Every: api.EveryDay, TimeZone: "UTC", Until: "someday"}})
	must(t, s.TransferPersonal())
	key := hubKey("lp-1")
	if it := item(t, mustState(t, s), key); it.Repeat != nil {
		t.Fatalf("imported repeat %+v", it.Repeat)
	}
	if a := act(t, s, api.KindDone, key, api.Payload{}); a.Payload.Was != nil {
		t.Fatalf("done %+v", a)
	}
}

// A bridge's repo is the personal list only when it is named personal.
func TestOnlyTheRepoNamedPersonalIsPersonal(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	must(t, s.ReplaceRepos("laptop", []api.Repo{
		{Name: "projects/garden", Personal: true, OptedIn: true},
		{Name: "personal", Personal: true, OptedIn: true},
	}))
	rows, err := s.DB().Query(`SELECT name, personal FROM repos WHERE bridge = 'laptop'`)
	must(t, err)
	defer rows.Close()
	for rows.Next() {
		var name string
		var personal bool
		must(t, rows.Scan(&name, &personal))
		if personal != (name == PersonalRepo) {
			t.Errorf("repo %s personal = %v", name, personal)
		}
	}
	must(t, rows.Err())
}

// A retry after the hub died between the add and its note writes the note
// once; a plain retry adds none.
func TestAddPersonalRetryRestoresALostNote(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	todo := api.PersonalTodo{ID: id1, Title: "Water the plants", Note: "The fern first"}
	key, _, err := s.AddPersonal(todo, "laptop", "")
	must(t, err)
	notes := func() int {
		return len(item(t, mustState(t, s), key).Comments)
	}
	if _, _, err := s.AddPersonal(todo, "laptop", ""); err != nil || notes() != 1 {
		t.Fatalf("plain retry: %v, %d notes", err, notes())
	}
	_, err = s.db.Exec(`DELETE FROM actions WHERE kind = ?`, api.KindNote)
	must(t, err)
	_, err = s.db.Exec(`UPDATE items SET data = json_remove(data, '$.comments') WHERE bridge = ?`, HubBridge)
	must(t, err)
	if notes() != 0 {
		t.Fatalf("note still there: %d", notes())
	}
	if k, created, err := s.AddPersonal(todo, "laptop", ""); err != nil || created || k != key || notes() != 1 {
		t.Fatalf("retry = %q, %v, %v, %d notes", k, created, err, notes())
	}
	if _, _, err := s.AddPersonal(todo, "laptop", ""); err != nil || notes() != 1 {
		t.Fatalf("second retry: %v, %d notes", err, notes())
	}
	// With the note lost and the item gone, a retry still answers.
	_, err = s.db.Exec(`DELETE FROM actions WHERE kind = ?`, api.KindNote)
	must(t, err)
	_, err = s.db.Exec(`DELETE FROM items WHERE bridge = ?`, HubBridge)
	must(t, err)
	if k, _, err := s.AddPersonal(todo, "laptop", ""); err != nil || k != key {
		t.Fatalf("retry of a deleted item = %q, %v", k, err)
	}
}
