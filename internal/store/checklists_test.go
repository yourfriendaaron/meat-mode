package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// setChecklist writes a checklist into an item's stored data, as schema 13
// kept it.
func setChecklist(t *testing.T, s *Store, repo, id string, checks []legacyCheck) {
	t.Helper()
	b, err := json.Marshal(checks)
	must(t, err)
	_, err = s.db.Exec(`UPDATE items SET data = json_set(data, '$.checklist', json(?)) WHERE bridge = ? AND repo = ? AND id = ?`,
		string(b), HubBridge, repo, id)
	must(t, err)
}

func hubRow(t *testing.T, s *Store, repo, id string) api.Item {
	t.Helper()
	tx, err := s.db.Begin()
	must(t, err)
	defer tx.Rollback()
	it, err := hubItem(tx, repo, id)
	must(t, err)
	return it
}

func TestMigrationFourteenTurnsChecklistsIntoSubtasks(t *testing.T) {
	s, c := open(t)
	water := addPersonal(t, s, uuid.NewString(), "Water the plants", nil).BeadID
	seeds := strings.TrimPrefix(addSubtask(t, s, "Buy seeds", water), "hub/personal/")
	done := addPersonal(t, s, uuid.NewString(), "Renew the library card", nil).BeadID
	act(t, s, api.KindDone, hubKey(done), api.Payload{})
	plain := addPersonal(t, s, uuid.NewString(), "Call Sam", nil).BeadID
	garden := "projects/garden"
	_, err := s.CreateBridge("laptop")
	must(t, err)
	compostKey, _, err := s.AddPersonal(api.PersonalTodo{Title: "Order compost"}, "laptop", garden)
	must(t, err)
	_, _, compost, _ := SplitKey(compostKey)
	// An id the conversion would pick is already taken.
	tx, err := s.db.Begin()
	must(t, err)
	must(t, putHubItem(tx, PersonalRepo, api.Item{ID: water + ".1", Title: "Fill the can", Status: "open", Priority: 2,
		CreatedAt: c.t, UpdatedAt: c.t}, c.t, nil))
	must(t, tx.Commit())

	setChecklist(t, s, PersonalRepo, water, []legacyCheck{{"c1", "Ferns", true}, {"c2", " ", false}, {"c3", "Tomatoes", false}})
	setChecklist(t, s, PersonalRepo, seeds, []legacyCheck{{"c1", "Carrots", true}, {"c2", "Beans", false}})
	setChecklist(t, s, PersonalRepo, done, []legacyCheck{{"c1", "Find the card", false}})
	setChecklist(t, s, garden, compost, []legacyCheck{{"c1", "Measure the beds", false}})
	_, err = s.db.Exec(`UPDATE items SET data = json_set(data, '$.description', 'Two packets.') WHERE repo = ? AND id = ?`, PersonalRepo, seeds)
	must(t, err)

	c.tick()
	_, err = s.db.Exec(`PRAGMA user_version = 13`)
	must(t, err)
	must(t, s.migrate())

	var left int
	must(t, s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE json_extract(data, '$.checklist') IS NOT NULL`).Scan(&left))
	if left != 0 {
		t.Fatalf("%d items still hold a checklist", left)
	}
	// A top-level open item gets one sub-task per entry, in order, done ones
	// closed as done; an empty entry is dropped and a taken id skipped.
	ferns, tomatoes := hubRow(t, s, PersonalRepo, water+".1-2"), hubRow(t, s, PersonalRepo, water+".2")
	if ferns.Title != "Ferns" || ferns.Parent != water || ferns.Status != "closed" || ferns.CloseReason != ReasonDone ||
		!ferns.ClosedAt.Equal(c.t) || !ferns.CreatedAt.Equal(c.t) {
		t.Fatalf("done entry %+v", ferns)
	}
	if tomatoes.Title != "Tomatoes" || tomatoes.Parent != water || tomatoes.Status != "open" || tomatoes.ClosedAt != nil {
		t.Fatalf("open entry %+v", tomatoes)
	}
	if it := hubRow(t, s, PersonalRepo, water); it.Description != "" || !it.UpdatedAt.Equal(c.t) {
		t.Fatalf("parent %+v", it)
	}
	// A sub-task, and a closed item, get the entries as a Markdown list.
	if d := hubRow(t, s, PersonalRepo, seeds).Description; d != "Two packets.\n\n- ~~Carrots~~\n- Beans" {
		t.Fatalf("sub-task's description %q", d)
	}
	if d := hubRow(t, s, PersonalRepo, done).Description; d != "- Find the card" {
		t.Fatalf("closed item's description %q", d)
	}
	// A project item's sub-task stays in the project.
	if sub := hubRow(t, s, garden, compost+".1"); sub.Title != "Measure the beds" || sub.Parent != compost {
		t.Fatalf("project sub-task %+v", sub)
	}
	// The state shows them.
	st := mustState(t, s)
	item(t, st, hubKey(water+".2"))
	if it := item(t, st, hubKey(plain)); it.Description != "" {
		t.Fatalf("an item with no checklist changed: %+v", it)
	}

	// Run again, it changes nothing.
	before := snapshot(t, s)
	_, err = s.db.Exec(`PRAGMA user_version = 13`)
	must(t, err)
	c.tick()
	must(t, s.migrate())
	if after := snapshot(t, s); after != before {
		t.Fatalf("a second run changed the data:\n%s\nwant\n%s", after, before)
	}
}

func TestConvertChecklistPadsIDsToOneWidth(t *testing.T) {
	checks := make([]legacyCheck, 12)
	for i := range checks {
		checks[i] = legacyCheck{Text: "Step"}
	}
	_, subs, err := convertChecklist(api.Item{ID: "mm-4", Status: "open"}, checks, time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), func(string) (bool, error) { return false, nil })
	must(t, err)
	if len(subs) != 12 || subs[0].ID != "mm-4.01" || subs[11].ID != "mm-4.12" {
		t.Fatalf("ids %s … %s of %d", subs[0].ID, subs[len(subs)-1].ID, len(subs))
	}
}

// A version 3 file names no checklist, so one that does is refused.
func TestRestoreRefusesAChecklistInVersionThree(t *testing.T) {
	s, _ := open(t)
	file := changeExport(t, func(ex map[string]any) { ex["version"] = 3 })
	_, err := s.Restore(strings.NewReader(string(file)))
	if err == nil || !strings.Contains(err.Error(), `unknown field "checklist"`) {
		t.Fatalf("%v", err)
	}
}

// Edits in a version 2 file's action log lose their checklist, so the file
// reads as version 3.
func TestRestoreV2DropsChecklistsFromEdits(t *testing.T) {
	s, _ := open(t)
	file := changeExport(t, func(ex map[string]any) {
		ex["version"] = 2
		ex["actions"] = append(ex["actions"].([]any), map[string]any{
			"id": "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a99", "seq": 100, "kind": "edit", "bridge": "hub", "repo": "personal", "beadId": "mm-1",
			"payload": map[string]any{
				"set": map[string]any{"checklist": []any{map[string]any{"id": "ch-1", "text": "Ferns", "done": true}}, "priority": 1},
				"was": map[string]any{"checklist": []any{}, "priority": 2},
			},
			"author": "alex-app", "status": "applied", "createdAt": "2026-10-08T09:00:00Z", "appliedAt": "2026-10-08T09:00:00Z",
		})
	})
	res, err := s.Restore(strings.NewReader(string(file)))
	must(t, err)
	if res.Actions != 7 {
		t.Fatalf("result %+v", res)
	}
	a, err := s.Action("0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a99")
	must(t, err)
	if raw := mustJSON(t, a.Payload); strings.Contains(raw, "checklist") || !strings.Contains(raw, `"priority":1`) {
		t.Fatalf("payload %s", raw)
	}
}
