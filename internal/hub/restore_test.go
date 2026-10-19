package hub

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// restoreFile is the store package's hand-written version 1 export.
func restoreFile(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../store/testdata/export-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRestoreRoute(t *testing.T) {
	h := newHarness(t, nil, false)

	// Behind the session, like every app route.
	cookie := h.cookie
	h.cookie = ""
	if w := h.do("POST", "/api/restore", restoreFile(t), ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: %d %s", w.Code, w.Body)
	}
	h.cookie = cookie

	w := h.do("POST", "/api/restore", restoreFile(t), "")
	h.expect(w, http.StatusOK)
	got := decode[api.RestoreResult](t, w)
	if got.Items != 4 || got.Actions != 6 || got.Reminders != 2 || got.Cancelled != 2 ||
		len(got.Machines) != 1 || got.Machines[0] != "laptop" || !got.CalendarFeed {
		t.Fatalf("result %+v", got)
	}
	st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	titles := map[string]bool{}
	for _, it := range st.Items {
		titles[it.Title] = true
	}
	if !titles["Water the plants"] || !titles["Order compost for the beds"] || st.User != "Alex" {
		t.Fatalf("state after the restore: user %q, items %v", st.User, titles)
	}
	if s := decode[api.Setup](t, h.do("GET", "/api/setup", nil, "")); s.Status != "in-use" || s.TimeZone != "Europe/Lisbon" {
		t.Fatalf("setup after the restore: %+v", s)
	}

	// The hub now has data: a second restore is refused and changes nothing.
	w = h.do("POST", "/api/restore", restoreFile(t), "")
	h.expect(w, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "already has data") {
		t.Fatalf("409 body %s", w.Body)
	}
	if again := decode[api.State](t, h.do("GET", "/api/state", nil, "")); len(again.Items) != len(st.Items) {
		t.Fatalf("items %d after the refusal, were %d", len(again.Items), len(st.Items))
	}
}

func TestRestoreRouteRefusesAHubWithAMachine(t *testing.T) {
	h := newHarness(t, nil, false)
	h.expect(h.do("POST", "/api/bridges", map[string]string{"name": "laptop"}, ""), http.StatusCreated)
	h.expect(h.do("POST", "/api/restore", restoreFile(t), ""), http.StatusConflict)
}

func TestRestoreRouteRefusesBadFiles(t *testing.T) {
	h := newHarness(t, nil, false)
	newer := strings.Replace(restoreFile(t), `"version": 1,`, `"version": 3,`, 1)
	for name, c := range map[string]struct{ body, says string }{
		"newer":      {newer, "the file is version 3, and this hub reads version 2"},
		"not export": {`{"items": []}`, "not a Meat Mode export"},
		"not JSON":   {"Water the plants", "not a Meat Mode export"},
		"bad item":   {strings.Replace(restoreFile(t), `"title": "Buy seeds"`, `"title": ""`, 1), "title cannot be empty"},
	} {
		w := h.do("POST", "/api/restore", c.body, "")
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.says) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	// Nothing was loaded by any of them.
	if s := decode[api.Setup](t, h.do("GET", "/api/setup", nil, "")); s.Status != "new" {
		t.Fatalf("status after refused files: %q", s.Status)
	}

	w := h.do("POST", "/api/restore", strings.Repeat(" ", store.MaxExportJSON+1), "")
	h.expect(w, http.StatusRequestEntityTooLarge)
}
