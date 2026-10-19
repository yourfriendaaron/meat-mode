package hub

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestExportRoute(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindAdd,
		Bridge: "hub", Repo: "personal", Payload: api.Payload{Title: "Water the plants"}}, ""), http.StatusCreated)

	// The file is named for the day in the hub's time zone, UTC while it
	// has none. Kiritimati is UTC+14 and Pago Pago UTC-11, so at any moment
	// at least one of them has a different date from UTC.
	fileDate := func(zone string) {
		t.Helper()
		if zone != "" {
			h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{TimeZone: str(zone)}, ""), http.StatusOK)
		}
		w := h.do("GET", "/api/export", nil, "")
		h.expect(w, http.StatusOK)
		e := exportOf(t, w.Body.Bytes())
		loc := time.UTC
		if zone != "" {
			loc, _ = time.LoadLocation(zone)
		}
		want := `attachment; filename="meat-mode-export-` + e.ExportedAt.In(loc).Format(time.DateOnly) + `.zip"`
		if got := w.Header().Get("Content-Disposition"); got != want {
			t.Errorf("zone %q: Content-Disposition %q, want %q", zone, got, want)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
			t.Errorf("Content-Type %q", ct)
		}
		if e.Format != api.ExportFormat || e.Version != api.ExportVersion || len(e.Items) != 1 ||
			e.Items[0].Title != "Water the plants" || strings.Join(e.Machines, ",") != "laptop" {
			t.Errorf("export %+v", e)
		}
	}
	fileDate("")
	fileDate("Pacific/Kiritimati")
	fileDate("Pacific/Pago_Pago")

	// Like every other app route, it needs a session.
	h.cookie = ""
	h.expect(h.do("GET", "/api/export", nil, ""), http.StatusUnauthorized)
}
