package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// exportFile is the version 1 export in testdata, written by hand from the
// contract (docs/api.md, Export and restore).
func exportFile(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/export-v1.json")
	must(t, err)
	return b
}

// changeExport returns the test export with fn applied to it as plain JSON.
func changeExport(t *testing.T, fn func(ex map[string]any)) []byte {
	t.Helper()
	var ex map[string]any
	must(t, json.Unmarshal(exportFile(t), &ex))
	fn(ex)
	b, err := json.Marshal(ex)
	must(t, err)
	return b
}

func exportList(ex map[string]any, name string, i int) map[string]any {
	return ex[name].([]any)[i].(map[string]any)
}

// snapshot dumps every table a restore writes, so a test can tell that a
// refused restore changed nothing.
func snapshot(t *testing.T, s *Store) string {
	t.Helper()
	var b strings.Builder
	for _, q := range []string{
		`SELECT bridge, repo, id, data, first_seen, seen_at FROM items ORDER BY bridge, repo, id`,
		`SELECT ` + actionCols + ` FROM actions ORDER BY seq`,
		`SELECT name, revoked_at FROM bridges ORDER BY name`,
		`SELECT bridge, name, personal, opted_in FROM repos ORDER BY bridge, name`,
		`SELECT user_name, time_zone, reminder_lead, calendar_hash, setup FROM hub_settings`,
		`SELECT repo, item_id, bridge FROM personal_owners ORDER BY repo, item_id`,
		`SELECT bridge, bead_id, item_id FROM personal_origins ORDER BY bridge, bead_id`,
		`SELECT name FROM sqlite_master WHERE name LIKE 'reminder_%' ORDER BY name`,
	} {
		rows, err := s.db.Query(q)
		must(t, err)
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			must(t, rows.Scan(ptrs...))
			fmt.Fprintln(&b, vals...)
		}
		must(t, rows.Err())
		rows.Close()
	}
	return b.String()
}

func TestRestoreLoadsAnExport(t *testing.T) {
	s, _ := open(t)
	// Settings the wizard set before the restore are replaced.
	must(t, s.SetTimeZone("Pacific/Auckland"))
	_, err := s.NewCalendarToken()
	must(t, err)

	res, err := s.Restore(bytes.NewReader(exportFile(t)))
	must(t, err)
	want := api.RestoreResult{Items: 4, Actions: 6, Reminders: 2, Cancelled: 2, Machines: []string{"laptop"}, CalendarFeed: true}
	if fmt.Sprint(res) != fmt.Sprint(want) {
		t.Fatalf("result %+v, want %+v", res, want)
	}

	h, err := s.Settings()
	must(t, err)
	if h.UserName != "Alex" || h.TimeZone != "Europe/Lisbon" || h.ReminderLead != 30 || h.Setup != "" {
		t.Fatalf("settings %+v", h)
	}
	if on, err := s.CalendarOn(); err != nil || on {
		t.Fatalf("calendar feed on after a restore: %v %v", on, err)
	}
	if got := status(t, s); got != SetupInUse {
		t.Fatalf("setup status %q; a restore does not record the wizard", got)
	}

	tx, err := s.db.Begin()
	must(t, err)
	water, err := hubItem(tx, PersonalRepo, "mm-1")
	must(t, err)
	seeds, err := hubItem(tx, PersonalRepo, "mm-2")
	must(t, err)
	compost, err := hubItem(tx, "projects/garden", "mm-3")
	must(t, err)
	origin, ok, err := originOf(tx, "laptop", "pe-7")
	must(t, err)
	tx.Rollback()

	if water.Key != "hub/personal/mm-1" || water.Title != "Water the plants" || !water.DueAt.Equal(time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("item %+v", water)
	}
	if len(water.Comments) != 1 || water.Comments[0].Text != "The ferns need less water." || water.Comments[0].Author != "planner" {
		t.Fatalf("comments %+v", water.Comments)
	}
	if len(water.Checklist) != 2 || water.Checklist[0] != (api.Check{ID: "ch-1", Text: "Ferns", Done: true}) {
		t.Fatalf("checklist %+v", water.Checklist)
	}
	if r := water.Repeat; r == nil || r.Every != api.EveryWeek || r.TimeZone != "Europe/Lisbon" || r.Start == nil ||
		!r.Start.Equal(time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("repeat %+v", water.Repeat)
	}
	if water.RemindBefore == nil || *water.RemindBefore != 15 || water.CreatedBy != "planner" {
		t.Fatalf("item %+v", water)
	}
	if !water.FirstSeen.Equal(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)) || water.SeenAt == nil ||
		!water.SeenAt.Equal(time.Date(2026, 10, 7, 18, 1, 0, 0, time.UTC)) {
		t.Fatalf("times: first %v seen %v", water.FirstSeen, water.SeenAt)
	}
	if seeds.Parent != "mm-1" {
		t.Fatalf("parent %q", seeds.Parent)
	}
	if compost.Title != "Order compost for the beds" {
		t.Fatalf("project item %+v", compost)
	}
	if !ok || origin != "pe-7" {
		t.Fatalf("origin %q %v", origin, ok)
	}

	// The owner's agents may change the items it added, and only those.
	if _, err := s.PersonalItem("laptop", "", "mm-1"); err != nil {
		t.Fatalf("owner lost: %v", err)
	}
	if _, err := s.PersonalItem("laptop", "projects/garden", "mm-3"); err != nil {
		t.Fatalf("project owner lost: %v", err)
	}
	if _, err := s.PersonalItem("laptop", "", "mm-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an item added in the app answers %v", err)
	}

	// The project is a list again.
	repos, err := s.Repos()
	must(t, err)
	if !slices.ContainsFunc(repos, func(r api.Repo) bool { return r.Bridge == HubBridge && r.Name == "projects/garden" }) {
		t.Fatalf("repos %+v", repos)
	}

	// Actions keep their order and ids; those waiting for a machine are
	// cancelled, so a machine paired again never replays them.
	acts, err := s.queryActions(`SELECT ` + actionCols + ` FROM actions ORDER BY seq`)
	must(t, err)
	var seqs []int64
	for _, a := range acts {
		seqs = append(seqs, a.Seq)
	}
	if !slices.Equal(seqs, []int64{1, 2, 3, 5, 8, 9}) {
		t.Fatalf("seqs %v", seqs)
	}
	for _, a := range acts[4:] {
		if a.Status != api.StatusCancelled || a.Error != CancelledByRestore {
			t.Fatalf("pending bridge action restored as %s %q", a.Status, a.Error)
		}
	}
	if acts[2].Status != api.StatusApplied || acts[2].AppliedAt == nil || acts[0].Payload.Title != "Water the plants" {
		t.Fatalf("actions %+v", acts[:3])
	}
	_, err = s.CreateBridge("laptop")
	must(t, err)
	if p, err := s.PendingActions("laptop"); err != nil || len(p) != 0 {
		t.Fatalf("pending for the machine paired again: %v %v", p, err)
	}

	// The reminders sent are recorded where the scheduler looks.
	var fired, early int64
	must(t, s.db.QueryRow(`SELECT fire_at FROM reminder_fired WHERE item_key = 'hub/personal/mm-1'`).Scan(&fired))
	must(t, s.db.QueryRow(`SELECT due_at FROM reminder_early WHERE item_key = 'hub/personal/mm-1'`).Scan(&early))
	if fired != time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC).UnixMilli() || early != time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("reminders %d %d", fired, early)
	}

	// The next add carries on after the restored log, with a fresh id.
	a, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: "Sweep the porch"}}, "alex-app")
	must(t, err)
	if a.Seq != 10 || a.BeadID != "mm-10" {
		t.Fatalf("next add: seq %d id %q", a.Seq, a.BeadID)
	}
}

func TestRestoreRefusesAHubWithData(t *testing.T) {
	cases := map[string]func(t *testing.T, s *Store){
		"item": func(t *testing.T, s *Store) {
			_, err := s.db.Exec(`INSERT INTO items (bridge, repo, id, data, first_seen) VALUES ('hub', 'personal', 'mm-1', '{"id":"mm-1","title":"Water the plants","status":"open","comments":[]}', '2026-03-01T09:00:00Z')`)
			must(t, err)
		},
		"action": func(t *testing.T, s *Store) {
			_, err := s.db.Exec(`INSERT INTO actions (id, kind, bridge, repo, payload, author, status, created_at)
				VALUES (?, 'note', 'laptop', 'garden', '{}', 'alex-app', 'cancelled', '2026-03-01T09:00:00Z')`, id2)
			must(t, err)
		},
		"machine": func(t *testing.T, s *Store) {
			_, err := s.CreateBridge("laptop")
			must(t, err)
		},
		"revoked machine": func(t *testing.T, s *Store) {
			_, err := s.CreateBridge("laptop")
			must(t, err)
			must(t, s.RevokeBridge("laptop"))
		},
	}
	for name, add := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := open(t)
			add(t, s)
			before := snapshot(t, s)
			_, err := s.Restore(bytes.NewReader(exportFile(t)))
			if !errors.Is(err, ErrHubInUse) || !errors.Is(err, ErrConflict) {
				t.Fatalf("restore into a hub with data: %v", err)
			}
			if after := snapshot(t, s); after != before {
				t.Fatalf("the refused restore changed the hub:\n%s\nwas\n%s", after, before)
			}
		})
	}
}

func TestRestoreRefusesANewerVersion(t *testing.T) {
	s, _ := open(t)
	before := snapshot(t, s)
	_, err := s.Restore(bytes.NewReader(changeExport(t, func(ex map[string]any) { ex["version"] = api.ExportVersion + 1 })))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("newer version: %v", err)
	}
	for _, want := range []string{fmt.Sprintf("version %d", api.ExportVersion+1), fmt.Sprintf("version %d", api.ExportVersion), "Update the hub"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%q does not say %q", err, want)
		}
	}
	if snapshot(t, s) != before {
		t.Fatal("the refused restore changed the hub")
	}
}

func TestRestoreRefusesWhatIsNotAnExport(t *testing.T) {
	cases := map[string][]byte{
		"not JSON":       []byte("Water the plants"),
		"other format":   changeExport(t, func(ex map[string]any) { ex["format"] = "other-export" }),
		"no format":      changeExport(t, func(ex map[string]any) { delete(ex, "format") }),
		"version 0":      changeExport(t, func(ex map[string]any) { ex["version"] = 0 }),
		"version text":   changeExport(t, func(ex map[string]any) { ex["version"] = "1" }),
		"version 1.5":    changeExport(t, func(ex map[string]any) { ex["version"] = 1.5 }),
		"version absent": changeExport(t, func(ex map[string]any) { delete(ex, "version") }),
	}
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := open(t)
			_, err := s.Restore(bytes.NewReader(file))
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "not a Meat Mode export") {
				t.Fatalf("%v", err)
			}
		})
	}
}

// A file that fails any one check changes nothing, however much of it is
// valid.
func TestRestoreChecksTheWholeFileFirst(t *testing.T) {
	item := func(i int, field string, v any) func(map[string]any) {
		return func(ex map[string]any) {
			if v == nil {
				delete(exportList(ex, "items", i), field)
				return
			}
			exportList(ex, "items", i)[field] = v
		}
	}
	cases := map[string]struct {
		change func(map[string]any)
		says   string
	}{
		"no title":           {item(3, "title", " "), "title cannot be empty"},
		"unknown status":     {item(3, "status", "lost"), "unknown status"},
		"machine's item":     {item(3, "bridge", "laptop"), "only the lists the hub keeps"},
		"bad list":           {item(3, "repo", "Personal"), "neither the personal list nor a project"},
		"bad id":             {item(3, "id", "-mm-3"), "not valid"},
		"key mismatch":       {item(3, "key", "hub/personal/mm-9"), "does not match"},
		"priority":           {item(3, "priority", 7), "priority"},
		"long title":         {item(3, "title", strings.Repeat("a", MaxTitleLen+1)), "longer than"},
		"repeat without due": {item(0, "dueAt", nil), "repeat needs a due date"},
		"bad repeat":         {item(0, "repeat", map[string]any{"every": "fortnight", "timeZone": "Europe/Lisbon"}), "repeat.every"},
		"own parent":         {item(1, "parent", "mm-2"), "parent"},
		"remind before":      {item(0, "remindBefore", -1), "remindBefore"},
		"bad owner":          {item(3, "owner", "hub"), "owner"},
		"checklist no id":    {item(0, "checklist", []any{map[string]any{"text": "Ferns"}}), "checklist"},
		"too many checks":    {item(0, "checklist", manyChecks(MaxChecklist+1)), "at most"},
		"unknown field":      {item(3, "colour", "green"), "unknown field"},
		"item listed twice": {func(ex map[string]any) {
			ex["items"] = append(ex["items"].([]any), exportList(ex, "items", 3))
		}, "listed twice"},
		"action id":             {func(ex map[string]any) { exportList(ex, "actions", 5)["id"] = "act-1" }, "UUID"},
		"action listed twice":   {func(ex map[string]any) { exportList(ex, "actions", 5)["id"] = exportList(ex, "actions", 0)["id"] }, "listed twice"},
		"action out of order":   {func(ex map[string]any) { exportList(ex, "actions", 5)["seq"] = 4 }, "out of order"},
		"action status":         {func(ex map[string]any) { exportList(ex, "actions", 5)["status"] = "lost" }, "unknown status"},
		"reminder key":          {func(ex map[string]any) { exportList(ex, "reminders", 1)["key"] = "hub/personal/mm-9" }, "not an item in the file"},
		"reminder kind":         {func(ex map[string]any) { exportList(ex, "reminders", 1)["kind"] = "late" }, "unknown kind"},
		"origin":                {func(ex map[string]any) { exportList(ex, "origins", 0)["machine"] = "Laptop" }, "origin"},
		"machine":               {func(ex map[string]any) { ex["machines"] = []any{"Laptop"} }, "machine"},
		"time zone":             {func(ex map[string]any) { ex["settings"].(map[string]any)["timeZone"] = "Mars/Base" }, "time zone"},
		"reminder lead":         {func(ex map[string]any) { ex["settings"].(map[string]any)["reminderLead"] = api.MaxLeadMinutes + 1 }, "lead time"},
		"name":                  {func(ex map[string]any) { ex["settings"].(map[string]any)["name"] = "Alex\nSam" }, "control characters"},
		"unknown top-level key": {func(ex map[string]any) { ex["devices"] = []any{} }, "unknown field"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := open(t)
			before := snapshot(t, s)
			_, err := s.Restore(bytes.NewReader(changeExport(t, c.change)))
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("got %v, want an invalid error saying %q", err, c.says)
			}
			if snapshot(t, s) != before {
				t.Fatal("the refused restore changed the hub")
			}
		})
	}
}

func manyChecks(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = map[string]any{"id": fmt.Sprintf("ch-%d", i), "text": "Ferns"}
	}
	return out
}

// An export with nothing in it restores the settings alone, and an empty
// time zone stays unchosen.
func TestRestoreAnEmptyExport(t *testing.T) {
	s, _ := open(t)
	res, err := s.Restore(bytes.NewReader([]byte(`{"format":"meat-mode-export","version":1,"exportedAt":"2026-10-14T10:00:00Z",
		"settings":{"name":"","timeZone":"","reminderLead":0,"calendarFeed":false},
		"items":[],"actions":[],"reminders":[],"origins":[],"machines":[]}`)))
	must(t, err)
	if res.Items != 0 || res.Machines == nil || res.CalendarFeed {
		t.Fatalf("result %+v", res)
	}
	if got := status(t, s); got != SetupNew {
		t.Fatalf("status %q", got)
	}
}

// Each export version but the first has the upgrade step from the version
// before it.
func TestExportUpgradesReachTheCurrentVersion(t *testing.T) {
	if len(exportUpgrades) != api.ExportVersion-1 {
		t.Fatalf("%d upgrade steps for version %d; add the step from version %d", len(exportUpgrades), api.ExportVersion, api.ExportVersion-1)
	}
}
