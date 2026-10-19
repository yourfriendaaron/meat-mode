package hub

import (
	"net/http"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestSetupStatusAndRecord(t *testing.T) {
	h := newHarness(t, nil, false)
	w := h.do("GET", "/api/setup", nil, "")
	h.expect(w, http.StatusOK)
	got := decode[api.Setup](t, w)
	want := api.Setup{Status: "new", User: "alex", Password: "env"}
	if got != want {
		t.Fatalf("fresh hub: %+v", got)
	}

	h.expect(h.do("POST", "/api/setup", api.SetupRecord{Status: "later"}, ""), http.StatusBadRequest)
	h.expect(h.do("POST", "/api/setup", api.SetupRecord{Status: "dismissed"}, ""), http.StatusNoContent)
	if got := decode[api.Setup](t, h.do("GET", "/api/setup", nil, "")); got.Status != "dismissed" {
		t.Fatalf("after dismissing: %+v", got)
	}
}

func TestSetupOnAHubInUse(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	if got := decode[api.Setup](t, h.do("GET", "/api/setup", nil, "")); got.Status != "in-use" {
		t.Fatalf("hub with a machine: %+v", got)
	}
}

func TestSetupReportsWhereThePasswordComesFrom(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("pw")})
	cookie := h.login("pw")
	w := h.do("GET", "/api/setup", "", cookie.Value)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if got := decode[api.Setup](t, w); got.Password != "env" {
		t.Fatalf("password from the environment: %+v", got)
	}

	if err := h.st.SetPasswordHash(cheapHash("other")); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetUserName("Sam"); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetTimeZone("America/New_York"); err != nil {
		t.Fatal(err)
	}
	h.srv = h.server(Config{})
	cookie = h.login("other")
	got := decode[api.Setup](t, h.do("GET", "/api/setup", "", cookie.Value))
	if got.Password != "app" || got.User != "Sam" || got.TimeZone != "America/New_York" {
		t.Fatalf("set in the app: %+v", got)
	}
}
