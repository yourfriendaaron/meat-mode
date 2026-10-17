package hub

import (
	"net/http"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

const testSub = `{"endpoint":"https://push.example.com/send/abc","keys":{"p256dh":"BP","auth":"AU"}}`

func TestTestReminderNeedsPush(t *testing.T) {
	h := newHarness(t, nil, false)
	h.expect(h.do("POST", "/api/reminders/test", nil, ""), http.StatusNotFound)
}

func TestTestReminder(t *testing.T) {
	h := newHarness(t, nil, true)
	w := h.do("POST", "/api/reminders/test", nil, "")
	h.expect(w, http.StatusOK)
	if got := decode[api.ReminderTest](t, w); got.Push != "none" || got.Ntfy != "off" {
		t.Fatalf("before subscribing: %+v", got)
	}
	h.expect(h.do("POST", "/api/push/subscriptions", testSub, ""), http.StatusNoContent)
	if got := decode[api.ReminderTest](t, h.do("POST", "/api/reminders/test", nil, "")); got.Push != "sent" {
		t.Fatalf("after subscribing: %+v", got)
	}
}

func TestTestReminderIsPerSession(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	a, b := h.login("water the plants"), h.login("water the plants")
	if w := h.do("POST", "/api/push/subscriptions", testSub, a.Value); w.Code != http.StatusNoContent {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body)
	}
	for cookie, want := range map[string]string{a.Value: "sent", b.Value: "none"} {
		w := h.do("POST", "/api/reminders/test", "", cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("test reminder: %d %s", w.Code, w.Body)
		}
		if got := decode[api.ReminderTest](t, w); got.Push != want {
			t.Errorf("push = %q, want %q", got.Push, want)
		}
	}
}
