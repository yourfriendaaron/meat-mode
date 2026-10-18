package store

import (
	"encoding/base64"
	"testing"
)

func TestCalendarToken(t *testing.T) {
	s, _ := open(t)
	if on, err := s.CalendarOn(); err != nil || on {
		t.Fatalf("a new hub's feed is on: %v, %v", on, err)
	}
	if ok, err := s.CalendarTokenOK(""); err != nil || ok {
		t.Fatalf("empty token with the feed off: %v, %v", ok, err)
	}
	first, err := s.NewCalendarToken()
	must(t, err)
	if b, err := base64.RawURLEncoding.DecodeString(first); err != nil || len(b) != 32 {
		t.Fatalf("token %q is not 32 bytes of base64url: %v", first, err)
	}
	var stored []byte
	must(t, s.DB().QueryRow(`SELECT calendar_hash FROM hub_settings`).Scan(&stored))
	if string(stored) == first || len(stored) != 32 {
		t.Fatalf("stored %q; want the token's SHA-256", stored)
	}
	if ok, _ := s.CalendarTokenOK(first); !ok {
		t.Fatal("the new token is refused")
	}
	if on, _ := s.CalendarOn(); !on {
		t.Fatal("the feed is off after a new token")
	}

	// A new token replaces the old one.
	second, err := s.NewCalendarToken()
	must(t, err)
	if second == first {
		t.Fatal("the same token twice")
	}
	for tok, want := range map[string]bool{first: false, second: true, "": false, second + "x": false} {
		if ok, err := s.CalendarTokenOK(tok); err != nil || ok != want {
			t.Errorf("token %q: %v, %v; want %v", tok, ok, err, want)
		}
	}

	must(t, s.StopCalendar())
	if ok, _ := s.CalendarTokenOK(second); ok {
		t.Fatal("the token works with the feed off")
	}
	if on, _ := s.CalendarOn(); on {
		t.Fatal("the feed is on after stopping it")
	}
}
