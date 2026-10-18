package hub

import (
	"net/http"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func str(s string) *string { return &s }

func TestSettingsDefaultToTheEnvironment(t *testing.T) {
	h := newHarness(t, nil, false)
	got := decode[api.Settings](t, h.do("GET", "/api/settings", nil, ""))
	if got != (api.Settings{Name: "alex"}) {
		t.Fatalf("fresh hub: %+v", got)
	}
}

func TestSettingsUpdateAndValidation(t *testing.T) {
	h := newHarness(t, nil, false)
	w := h.do("PUT", "/api/settings", api.SettingsUpdate{Name: str(" Sam "), TimeZone: str("Europe/Paris")}, "")
	h.expect(w, http.StatusOK)
	want := api.Settings{Name: "Sam", TimeZone: "Europe/Paris"}
	if got := decode[api.Settings](t, w); got != want {
		t.Fatalf("reply: %+v", got)
	}

	// A field left out stays; a refused one changes nothing.
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{TimeZone: str("America/New_York")}, ""), http.StatusOK)
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{Name: str("Casey"), TimeZone: str("Mars/Olympus")}, ""), http.StatusBadRequest)
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{TimeZone: str("")}, ""), http.StatusBadRequest)
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{TimeZone: str("Local")}, ""), http.StatusBadRequest)
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{Name: str("Al\nex")}, ""), http.StatusBadRequest)
	want = api.Settings{Name: "Sam", TimeZone: "America/New_York"}
	if got := decode[api.Settings](t, h.do("GET", "/api/settings", nil, "")); got != want {
		t.Fatalf("after refusals: %+v", got)
	}

	// An empty name goes back to MEAT_MODE_USER.
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{Name: str("")}, ""), http.StatusOK)
	if got := decode[api.Settings](t, h.do("GET", "/api/settings", nil, "")); got.Name != "alex" {
		t.Fatalf("after clearing the name: %+v", got)
	}
}

func TestNameSignsNewNotes(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	note := func(id, text string) api.Action {
		w := h.do("POST", "/api/actions", api.NewAction{ID: id, Kind: api.KindNote, Key: "laptop/garden/gd-1", Payload: api.Payload{Text: text}}, "")
		h.expect(w, http.StatusCreated)
		return decode[api.Action](t, w)
	}
	if a := note(aid1, "First"); a.Author != "alex-app" {
		t.Fatalf("before the change: %+v", a)
	}
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{Name: str("Sam")}, ""), http.StatusOK)
	if a := note(aid2, "Second"); a.Author != "Sam-app" {
		t.Fatalf("after the change: %+v", a)
	}
	// The app shows the new name too, and the first note keeps its author.
	if st := decode[api.State](t, h.do("GET", "/api/state", nil, "")); st.User != "Sam" {
		t.Fatalf("state user %q", st.User)
	}
	if got := decode[api.Setup](t, h.do("GET", "/api/setup", nil, "")); got.User != "Sam" {
		t.Fatalf("setup user %q", got.User)
	}
	as := decode[[]api.Action](t, h.do("GET", "/api/actions", nil, ""))
	authors := map[string]string{}
	for _, a := range as {
		authors[a.Payload.Text] = a.Author
	}
	if authors["First"] != "alex-app" || authors["Second"] != "Sam-app" {
		t.Fatalf("authors %v", authors)
	}
}

func TestBridgeLearnsTheTimeZone(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	w := h.do("GET", "/bridge/v1/settings", nil, h.token)
	h.expect(w, http.StatusOK)
	if got := decode[api.Settings](t, w); got.TimeZone != "" {
		t.Fatalf("before one is chosen: %+v", got)
	}
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{TimeZone: str("Europe/Paris")}, ""), http.StatusOK)
	w = h.do("GET", "/bridge/v1/settings", nil, h.token)
	h.expect(w, http.StatusOK)
	if got := decode[api.Settings](t, w); got.TimeZone != "Europe/Paris" {
		t.Fatalf("bridge settings %+v", got)
	}
	h.expect(h.do("GET", "/bridge/v1/settings", nil, ""), http.StatusUnauthorized)
}

func minutes(n int) *int { return &n }

func TestReminderLeadSetting(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	w := h.do("PUT", "/api/settings", api.SettingsUpdate{ReminderLead: minutes(30)}, "")
	h.expect(w, http.StatusOK)
	if got := decode[api.Settings](t, w); got.ReminderLead != 30 || got.Name != "alex" {
		t.Fatalf("reply: %+v", got)
	}
	// Out of range, or beside a refused field, nothing changes.
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{ReminderLead: minutes(-1)}, ""), http.StatusBadRequest)
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{ReminderLead: minutes(api.MaxLeadMinutes + 1)}, ""), http.StatusBadRequest)
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{Name: str("Sam"), ReminderLead: minutes(60), TimeZone: str("Mars/Olympus")}, ""), http.StatusBadRequest)
	want := api.Settings{Name: "alex", ReminderLead: 30}
	if got := decode[api.Settings](t, h.do("GET", "/api/settings", nil, "")); got != want {
		t.Fatalf("after refusals: %+v", got)
	}
	w = h.do("GET", "/bridge/v1/settings", nil, h.token)
	h.expect(w, http.StatusOK)
	if got := decode[api.Settings](t, w); got.ReminderLead != 30 {
		t.Fatalf("bridge settings %+v", got)
	}
	h.expect(h.do("PUT", "/api/settings", api.SettingsUpdate{ReminderLead: minutes(0)}, ""), http.StatusOK)
	if got := decode[api.Settings](t, h.do("GET", "/api/settings", nil, "")); got.ReminderLead != 0 {
		t.Fatalf("after turning it off: %+v", got)
	}
}
