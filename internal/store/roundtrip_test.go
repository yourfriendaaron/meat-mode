package store

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// An export of a hub in use restores into a fresh hub, whose own export
// then holds the same lists, log, reminders and origins: what one side
// writes, the other reads in full.
func TestExportRestoresIntoAFreshHub(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	must(t, s.UpdateProfile(ptr("Sam"), ptr("Europe/Paris"), ptr(30)))
	_, err := s.NewCalendarToken()
	must(t, err)

	due := c.t.Add(2 * time.Hour)
	add := addPersonal(t, s, id1, "Water the plants", &due)
	key := hubKey(add.BeadID)
	c.tick()
	_, _, err = s.AddPersonal(api.PersonalTodo{Title: "Approve the footer text", Note: "The short one."}, "laptop", garden)
	must(t, err)
	c.tick()
	must(t, s.MarkSeen(key))
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindNote, Key: key, Payload: api.Payload{Text: "Use the rain water"}}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id3, Kind: api.KindNote, Key: "laptop/garden/gd-1", Payload: api.Payload{Text: "Check the soil"}}, "alex-app")
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO personal_origins (bridge, bead_id, item_id) VALUES ('laptop', 'bd-7', ?)`, add.BeadID)
	must(t, err)
	_, err = s.db.Exec(`CREATE TABLE reminder_fired (item_key TEXT NOT NULL, fire_at INTEGER NOT NULL, PRIMARY KEY (item_key, fire_at));
		CREATE TABLE reminder_early (item_key TEXT NOT NULL, due_at INTEGER NOT NULL, PRIMARY KEY (item_key, due_at));
		INSERT INTO reminder_early (item_key, due_at) VALUES (?, ?)`, key, due.UnixMilli())
	must(t, err)

	first, err := s.Export()
	must(t, err)
	file, err := json.Marshal(first)
	must(t, err)

	fresh, _ := open(t)
	res, err := fresh.Restore(bytes.NewReader(file))
	must(t, err)
	if res.Items != len(first.Items) || res.Actions != len(first.Actions) || res.Reminders != 1 || res.Cancelled != 1 ||
		!res.CalendarFeed || len(res.Machines) != len(first.Machines) {
		t.Errorf("restore result %+v for an export of %d items and %d actions", res, len(first.Items), len(first.Actions))
	}
	second, err := fresh.Export()
	must(t, err)

	// What a restore changes on purpose: the feed is off, no machine is
	// paired, and the change waiting for a machine is cancelled.
	want := first
	want.ExportedAt = second.ExportedAt
	want.Settings.CalendarFeed = false
	want.Machines = []string{}
	want.Actions = append([]api.Action{}, first.Actions...)
	for i, a := range want.Actions {
		if a.ID == id3 {
			a.Status, a.Error = api.StatusCancelled, CancelledByRestore
			want.Actions[i] = a
		}
	}
	wantJSON, err := json.Marshal(want)
	must(t, err)
	gotJSON, err := json.Marshal(second)
	must(t, err)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("export after restore\n got %s\nwant %s", gotJSON, wantJSON)
	}
}

// Items moved off a machine's personal repo keep the bead's labels as they
// were, which the tag rules may refuse. The hub's own export must still
// restore.
func TestExportOfMovedLabelsRestores(t *testing.T) {
	s, c := open(t)
	legacySeed(t, s, "laptop", api.Item{ID: "lp-1", Title: "Water the plants", Status: "open", Priority: 2,
		CreatedAt: c.t, UpdatedAt: c.t, Labels: []string{"human", "two words", "garden"}})
	must(t, s.TransferPersonal())
	ex, err := s.Export()
	must(t, err)
	file, err := json.Marshal(ex)
	must(t, err)
	fresh, _ := open(t)
	_, err = fresh.Restore(bytes.NewReader(file))
	must(t, err)
	for _, it := range mustState(t, fresh).Items {
		if it.Title == "Water the plants" && !slices.Equal(it.Labels, []string{"garden"}) {
			t.Fatalf("labels %q, want [garden]", it.Labels)
		}
	}
}
