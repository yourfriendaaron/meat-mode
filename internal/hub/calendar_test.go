package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// feed fetches path as a calendar app would: no cookie, cross-site
// headers, and an Accept header that names only text/calendar.
func feed(srv *Server, path, host string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.Host = host
	req.Header.Set("Accept", "text/calendar")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://calendar.example.org")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestCalendarFeedRoutes(t *testing.T) {
	h := newHarness(t, nil, false)
	h.srv.cfg.PublicURL = "https://app.example.com/"
	h.pair()
	due := time.Date(2030, 3, 2, 18, 30, 0, 0, time.UTC)
	h.expect(h.do("PUT", "/bridge/v1/items?repo=garden", []api.Item{
		{ID: "gd-1", Title: "Water the plants, then; the beds", Status: "open", DueAt: &due,
			UpdatedAt: time.Date(2030, 3, 1, 8, 0, 0, 0, time.UTC)},
		{ID: "gd-2", Title: "Buy seeds", Status: "open"},
		{ID: "gd-3", Title: "Sweep the porch", Status: "closed", DueAt: &due},
	}, h.token), http.StatusNoContent)
	rent := due.Add(-time.Hour)
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindAdd,
		Bridge: "hub", Repo: "personal", Payload: api.Payload{Title: "Pay the rent", Due: &rent}}, ""), http.StatusCreated)

	if got := decode[api.CalendarStatus](t, h.do("GET", "/api/calendar", nil, "")); got.On {
		t.Fatal("a new hub's feed is on")
	}
	h.expect(feed(h.srv, "/calendar/.ics", "localhost:8080"), http.StatusNotFound)

	w := h.do("POST", "/api/calendar", map[string]any{}, "")
	h.expect(w, http.StatusOK)
	link := decode[api.CalendarLink](t, w).URL
	path, ok := strings.CutPrefix(link, "https://app.example.com/calendar/")
	if !ok || !strings.HasSuffix(path, ".ics") || len(link) >= 256 {
		t.Fatalf("feed address %q", link)
	}
	if got := decode[api.CalendarStatus](t, h.do("GET", "/api/calendar", nil, "")); !got.On {
		t.Fatal("the feed is off after POST")
	}

	w = feed(h.srv, "/calendar/"+path, "localhost:8080")
	h.expect(w, http.StatusOK)
	if ct := w.Header().Get("Content-Type"); ct != "text/calendar; charset=utf-8" {
		t.Fatalf("Content-Type %q", ct)
	}
	body := w.Body.String()
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:",
		"X-WR-CALNAME:Meat Mode\r\n",
		"REFRESH-INTERVAL;VALUE=DURATION:PT1H\r\n",
		"X-PUBLISHED-TTL:PT1H\r\n",
		"UID:laptop/garden/gd-1@meat-mode\r\n",
		"DTSTAMP:20300301T080000Z\r\n",
		"DTSTART:20300302T183000Z\r\nDURATION:PT15M\r\n",
		`SUMMARY:Water the plants\, then\; the beds` + "\r\n",
		"URL:https://app.example.com/?item=laptop%2Fgarden%2Fgd-1\r\n",
		"DESCRIPTION:https://app.example.com/?item=laptop%2Fgarden%2Fgd-1\r\n",
		"SUMMARY:Pay the rent\r\n",
		"END:VCALENDAR\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("feed lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Buy seeds") || strings.Contains(body, "Sweep the porch") {
		t.Errorf("feed holds an undated or closed item:\n%s", body)
	}
	if strings.Count(body, "BEGIN:VEVENT") != 2 || strings.Index(body, "Pay the rent") > strings.Index(body, "Water the plants") {
		t.Errorf("want two events by due time:\n%s", body)
	}
	if strings.Contains(strings.ReplaceAll(body, "\r\n", ""), "\n") {
		t.Error("a bare line feed in the feed")
	}

	// The Host check still applies.
	h.expect(feed(h.srv, "/calendar/"+path, "attacker.example.net"), http.StatusMisdirectedRequest)

	// A new address replaces the old one; DELETE turns the feed off.
	w = h.do("POST", "/api/calendar", map[string]any{}, "")
	h.expect(w, http.StatusOK)
	path2 := strings.TrimPrefix(decode[api.CalendarLink](t, w).URL, "https://app.example.com/calendar/")
	h.expect(feed(h.srv, "/calendar/"+path, "localhost:8080"), http.StatusNotFound)
	h.expect(feed(h.srv, "/calendar/"+path2, "localhost:8080"), http.StatusOK)
	h.expect(feed(h.srv, "/calendar/"+strings.TrimSuffix(path2, ".ics"), "localhost:8080"), http.StatusNotFound)
	h.expect(h.do("DELETE", "/api/calendar", nil, ""), http.StatusNoContent)
	h.expect(feed(h.srv, "/calendar/"+path2, "localhost:8080"), http.StatusNotFound)
	if got := decode[api.CalendarStatus](t, h.do("GET", "/api/calendar", nil, "")); got.On {
		t.Fatal("the feed is on after DELETE")
	}
}

// With login on, the feed needs no session while the routes that manage it
// do.
func TestCalendarFeedNeedsNoLogin(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants"), PublicURL: "https://app.example.com"})
	if w := h.do("POST", "/api/calendar", `{}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("POST without a session: %d", w.Code)
	}
	cookie := h.login("water the plants")
	w := h.do("POST", "/api/calendar", `{}`, cookie.Value)
	if w.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", w.Code, w.Body)
	}
	path := strings.TrimPrefix(decode[api.CalendarLink](t, w).URL, "https://app.example.com")
	if w := feed(h.srv, path, "localhost:8080"); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "BEGIN:VCALENDAR") {
		t.Fatalf("feed without a session: %d %s", w.Code, w.Body)
	}
}

func TestEscapeText(t *testing.T) {
	for in, want := range map[string]string{
		`Water the plants`:        `Water the plants`,
		`a\b;c,d`:                 `a\\b\;c\,d`,
		"one\ntwo\r\nthree\rfour": `one\ntwo\nthree\nfour`,
		"tab\there\x00\x1b":       "tab\there",
		"Café: ok":                "Café: ok",
	} {
		if got := escapeText(in); got != want {
			t.Errorf("escapeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFoldLine(t *testing.T) {
	short := "SUMMARY:Water the plants"
	if got := foldLine(short); got != short {
		t.Fatalf("short line folded: %q", got)
	}
	for _, title := range []string{
		strings.Repeat("a", 200),
		strings.Repeat("é", 100), // two octets each
		strings.Repeat("水", 80),  // three octets each
		strings.Repeat("🌱", 50),  // four octets each
		"x" + strings.Repeat("🌱", 40),
	} {
		in := "SUMMARY:" + title
		got := foldLine(in)
		lines := strings.Split(got, "\r\n")
		for i, l := range lines {
			if len(l) > maxLineOctets {
				t.Errorf("line %d is %d octets", i, len(l))
			}
			if !utf8.ValidString(l) {
				t.Errorf("line %d splits a character: %q", i, l)
			}
			if i > 0 && !strings.HasPrefix(l, " ") {
				t.Errorf("continuation %d does not start with a space", i)
			}
		}
		// Unfolding gives the line back.
		if back := strings.ReplaceAll(got, "\r\n ", ""); back != in {
			t.Errorf("unfolded %q, want %q", back, in)
		}
	}
}
