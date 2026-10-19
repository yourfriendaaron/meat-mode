package store

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestExportOfAFreshHub(t *testing.T) {
	s, c := open(t)
	e, err := s.Export()
	must(t, err)
	if e.Format != api.ExportFormat || e.Version != api.ExportVersion || !e.ExportedAt.Equal(c.t) {
		t.Fatalf("export header %+v", e)
	}
	if e.Settings != (api.ExportSettings{}) {
		t.Errorf("settings %+v, want none chosen", e.Settings)
	}
	// Empty lists are [], never null, so a reader need not tell them apart.
	data, err := json.Marshal(e)
	must(t, err)
	for _, field := range []string{`"items":[]`, `"actions":[]`, `"reminders":[]`, `"origins":[]`, `"machines":[]`} {
		if !strings.Contains(string(data), field) {
			t.Errorf("fresh export lacks %s: %s", field, data)
		}
	}
}

func TestExportHoldsTheHubsLists(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	_, err := s.CreateBridge("desktop")
	must(t, err)
	_, err = s.CreateBridge("old-laptop")
	must(t, err)
	must(t, s.RevokeBridge("old-laptop"))
	must(t, s.UpdateProfile(ptr("Sam"), ptr("Europe/Paris"), ptr(30)))
	_, err = s.NewCalendarToken()
	must(t, err)

	due := c.t.Add(2 * time.Hour)
	add := addPersonal(t, s, id1, "Water the plants", &due)
	key := hubKey(add.BeadID)
	c.tick()
	agentKey, _, err := s.AddPersonal(api.PersonalTodo{Title: "Approve the footer text"}, "laptop", garden)
	must(t, err)
	c.tick()
	must(t, s.MarkSeen(key))
	c.tick()
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindDone, Key: key}, "alex-app")
	must(t, err)
	_, _, err = s.AppendAction(api.NewAction{ID: id3, Kind: api.KindNote, Key: "laptop/garden/gd-1", Payload: api.Payload{Text: "Use the rain water"}}, "alex-app")
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO personal_origins (bridge, bead_id, item_id) VALUES ('laptop', 'bd-7', ?)`, add.BeadID)
	must(t, err)

	e, err := s.Export()
	must(t, err)
	want := api.ExportSettings{Name: "Sam", TimeZone: "Europe/Paris", ReminderLead: 30, CalendarFeed: true}
	if e.Settings != want {
		t.Errorf("settings %+v, want %+v", e.Settings, want)
	}
	if strings.Join(e.Machines, ",") != "desktop,laptop" {
		t.Errorf("machines %v, want the paired ones without the hub or a revoked one", e.Machines)
	}

	byKey := map[string]api.ExportItem{}
	for _, it := range e.Items {
		if it.Bridge != HubBridge {
			t.Errorf("export holds a machine's item %+v", it)
		}
		byKey[it.Key] = it
	}
	if len(e.Items) != 2 {
		t.Fatalf("items %+v, want the personal item and the project's", e.Items)
	}
	// A closed item is exported with its times and no owner.
	it := byKey[key]
	if it.Title != "Water the plants" || it.Status != "closed" || it.Repo != PersonalRepo || it.Owner != "" ||
		it.FirstSeen.IsZero() || it.SeenAt == nil || it.Pending || it.Run != nil || it.Error != "" {
		t.Errorf("personal item %+v", it)
	}
	if ag := byKey[agentKey]; ag.Repo != garden || ag.Owner != "laptop" || ag.Title != "Approve the footer text" {
		t.Errorf("project item %+v, want owned by laptop", ag)
	}

	// The whole log, every machine's included, oldest first.
	var ids []string
	for i, a := range e.Actions {
		ids = append(ids, a.ID)
		if i > 0 && a.Seq <= e.Actions[i-1].Seq {
			t.Errorf("actions out of order: %v", e.Actions)
		}
	}
	if len(e.Actions) < 3 || e.Actions[0].ID != id1 || e.Actions[len(e.Actions)-1].ID != id3 {
		t.Errorf("action ids %v, want the add first and the note on the machine's repo last", ids)
	}

	if len(e.Origins) != 1 || e.Origins[0] != (api.ExportOrigin{Machine: "laptop", BeadID: "bd-7", ItemID: add.BeadID}) {
		t.Errorf("origins %+v", e.Origins)
	}
}

func TestExportRemindersWithAndWithoutTheirTables(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	add := addPersonal(t, s, id1, "Water the plants", nil)
	key := hubKey(add.BeadID)

	// A store used without the reminder scheduler has no reminder tables.
	e, err := s.Export()
	must(t, err)
	if len(e.Reminders) != 0 {
		t.Fatalf("reminders without tables: %+v", e.Reminders)
	}

	_, err = s.db.Exec(`CREATE TABLE reminder_fired (item_key TEXT NOT NULL, fire_at INTEGER NOT NULL, PRIMARY KEY (item_key, fire_at));
		CREATE TABLE reminder_early (item_key TEXT NOT NULL, due_at INTEGER NOT NULL, PRIMARY KEY (item_key, due_at))`)
	must(t, err)
	at := c.t.Add(time.Hour)
	for _, q := range []struct {
		table, key string
	}{
		{"reminder_fired", key},
		{"reminder_early", key},
		{"reminder_fired", "laptop/garden/gd-1"}, // a machine's item
		{"reminder_fired", hubKey("mm-gone")},    // no such item
	} {
		col := "fire_at"
		if q.table == "reminder_early" {
			col = "due_at"
		}
		_, err := s.db.Exec(`INSERT INTO `+q.table+` (item_key, `+col+`) VALUES (?, ?)`, q.key, at.UnixMilli())
		must(t, err)
	}
	e, err = s.Export()
	must(t, err)
	want := []api.ExportReminder{
		{Key: key, Kind: api.ReminderDue, At: at},
		{Key: key, Kind: api.ReminderEarly, At: at},
	}
	if len(e.Reminders) != len(want) {
		t.Fatalf("reminders %+v, want %+v", e.Reminders, want)
	}
	for i, r := range e.Reminders {
		if r.Key != want[i].Key || r.Kind != want[i].Kind || !r.At.Equal(want[i].At) {
			t.Errorf("reminder %d = %+v, want %+v", i, r, want[i])
		}
	}
}

func TestExportLeavesOutSecrets(t *testing.T) {
	s, c := open(t)
	seed(t, s)
	token, err := s.CreateBridge("desktop")
	must(t, err)
	must(t, s.SetPasswordHash(cheapHash("correct horse battery staple")))
	session, _, err := s.CreateSession("tag-1", c.t)
	must(t, err)
	code, err := s.NewSetupCode(time.Hour)
	must(t, err)
	feed, err := s.NewCalendarToken()
	must(t, err)
	_, err = s.db.Exec(`CREATE TABLE push_subscriptions (endpoint TEXT PRIMARY KEY, p256dh TEXT NOT NULL, auth TEXT NOT NULL);
		INSERT INTO push_subscriptions VALUES ('https://push.example.com/send/abc123', 'p256dh-key-value', 'auth-secret-value')`)
	must(t, err)
	addPersonal(t, s, id1, "Water the plants", nil)
	h, err := s.Settings()
	must(t, err)

	e, err := s.Export()
	must(t, err)
	data, err := json.Marshal(e)
	must(t, err)
	text := string(data)

	var secrets []string
	for _, v := range []string{token, session, code, normalSetupCode(code), feed} {
		sum := sha256.Sum256([]byte(v))
		secrets = append(secrets, v, hex.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(sum[:]))
	}
	secrets = append(secrets, h.PasswordHash, "push.example.com", "p256dh-key-value", "auth-secret-value")
	for _, v := range secrets {
		if strings.Contains(text, v) {
			t.Errorf("export holds the secret %q", v)
		}
	}
	// Nor anything of a machine's repo, nor the hub's runs.
	for _, v := range []string{"Repot the fern", "gd-1", "gd-2", "Which pot?"} {
		if strings.Contains(text, v) {
			t.Errorf("export holds %q from a machine's repo", v)
		}
	}
	if strings.Contains(text, `"run"`) || strings.Contains(text, `"pending"`) {
		t.Errorf("export holds run or pending state: %s", text)
	}
}
