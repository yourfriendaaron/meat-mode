package reminders

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// restored opens a fresh hub, restores the store package's test export
// into it, changed by fn, and starts a scheduler on it at now.
func restored(t *testing.T, now time.Time, fn func(ex map[string]any)) (*Scheduler, *fakeNotifier) {
	t.Helper()
	file, err := os.ReadFile("../store/testdata/export-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var ex map[string]any
	if err := json.Unmarshal(file, &ex); err != nil {
		t.Fatal(err)
	}
	fn(ex)
	if file, err = json.Marshal(ex); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.Restore(file); err != nil {
		t.Fatal(err)
	}
	n := &fakeNotifier{}
	s, err := New(st.DB(), storeItems{st}, n, &fakeClock{now: now}, "https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	s.KeyOf = st.ReminderKey
	return s, n
}

// A reminder the export records as sent is not sent again by the restored
// hub; without the record, the same tick sends it.
func TestRestoredRemindersAreNotSentAgain(t *testing.T) {
	water := func(ex map[string]any) map[string]any { return ex["items"].([]any)[0].(map[string]any) }
	drop := func(ex map[string]any) { ex["reminders"] = []any{} }
	cases := []struct {
		name   string
		now    time.Time
		change func(map[string]any)
	}{
		// "Water the plants" is due at 18:00 with a 15-minute early reminder.
		{"early", time.Date(2026, 10, 14, 17, 50, 0, 0, time.UTC), func(map[string]any) {}},
		// The reminder at the due time, a week before.
		{"due", time.Date(2026, 10, 7, 18, 2, 0, 0, time.UTC), func(ex map[string]any) { water(ex)["dueAt"] = "2026-10-07T18:00:00Z" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, n := restored(t, c.now, c.change)
			if err := s.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(n.got) != 0 {
				t.Fatalf("sent again after the restore: %+v", n.got)
			}

			s, n = restored(t, c.now, func(ex map[string]any) { c.change(ex); drop(ex) })
			if err := s.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(n.got) != 1 || n.got[0].Title != "Water the plants" {
				t.Fatalf("without the record: %+v", n.got)
			}
		})
	}
}
