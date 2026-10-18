package hub

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// calendarRoutes registers the calendar feed (docs/api.md, Calendar feed).
// The feed itself is outside /api/: calendar apps cannot log in, so the
// token in its address is the only credential.
func (s *Server) calendarRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/calendar", s.handleCalendarStatus)
	m.HandleFunc("POST /api/calendar", s.handleNewCalendar)
	m.HandleFunc("DELETE /api/calendar", s.handleStopCalendar)
	m.HandleFunc("GET /calendar/{file}", s.handleCalendarFeed)
}

func (s *Server) handleCalendarStatus(w http.ResponseWriter, r *http.Request) {
	on, err := s.store.CalendarOn()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.CalendarStatus{On: on})
}

func (s *Server) handleNewCalendar(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	token, err := s.store.NewCalendarToken()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.CalendarLink{URL: s.publicBase() + "/calendar/" + token + ".ics"})
}

func (s *Server) handleStopCalendar(w http.ResponseWriter, r *http.Request) {
	if err := s.store.StopCalendar(); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// publicBase is MEAT_MODE_PUBLIC_URL without a trailing slash.
func (s *Server) publicBase() string { return strings.TrimRight(s.cfg.PublicURL, "/") }

// handleCalendarFeed serves the open items with a due date as iCalendar
// (RFC 5545), whatever the Accept header asks for.
func (s *Server) handleCalendarFeed(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutSuffix(r.PathValue("file"), ".ics")
	if ok && token != "" {
		var err error
		if ok, err = s.store.CalendarTokenOK(token); err != nil {
			s.storeError(w, r, err)
			return
		}
	}
	if !ok || token == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("No such calendar. Turn the feed on in the app's Settings for a new address.\n"))
		return
	}
	st, err := s.store.State()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="meat-mode.ics"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(calendar(st.Items, s.publicBase(), s.now())))
}

// eventLength is how long each item's event lasts.
const eventLength = "PT15M"

// calendar renders items as an iCalendar feed: one event per open item with
// a due date, at its due time, ordered by due time.
func calendar(items []api.Item, publicBase string, now time.Time) string {
	var dated []api.Item
	for _, it := range items {
		if it.Status != "closed" && it.DueAt != nil {
			dated = append(dated, it)
		}
	}
	slices.SortStableFunc(dated, func(a, b api.Item) int {
		if c := a.DueAt.Compare(*b.DueAt); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	var b strings.Builder
	line := func(name, value string) {
		b.WriteString(foldLine(name + ":" + value))
		b.WriteString("\r\n")
	}
	line("BEGIN", "VCALENDAR")
	line("VERSION", "2.0")
	line("PRODID", "-//Meat Mode//Meat Mode//EN")
	line("CALSCALE", "GREGORIAN")
	line("X-WR-CALNAME", "Meat Mode")
	line("REFRESH-INTERVAL;VALUE=DURATION", "PT1H")
	line("X-PUBLISHED-TTL", "PT1H")
	for _, it := range dated {
		link := publicBase + "/?item=" + url.QueryEscape(it.Key)
		stamp := it.UpdatedAt
		if stamp.IsZero() {
			stamp = now
		}
		line("BEGIN", "VEVENT")
		line("UID", escapeText(it.Key+"@meat-mode"))
		line("DTSTAMP", icalTime(stamp))
		line("DTSTART", icalTime(*it.DueAt))
		line("DURATION", eventLength)
		line("SUMMARY", escapeText(it.Title))
		line("URL", link)
		line("DESCRIPTION", escapeText(link))
		line("END", "VEVENT")
	}
	line("END", "VCALENDAR")
	return b.String()
}

// icalTime is a UTC DATE-TIME.
func icalTime(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// escapeText escapes an iCalendar TEXT value: backslash, semicolon, comma
// and line breaks. Other control characters, which TEXT does not allow, are
// dropped.
func escapeText(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == ';' || r == ',':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t' || !unicode.IsControl(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maxLineOctets is the longest content line iCalendar allows, CRLF left out.
const maxLineOctets = 75

// foldLine breaks a content line into lines of at most 75 octets, each
// continuation starting with a space, never inside a UTF-8 character.
func foldLine(s string) string {
	if len(s) <= maxLineOctets {
		return s
	}
	var b strings.Builder
	n := 0 // octets on the current line
	for len(s) > 0 {
		_, size := utf8.DecodeRuneInString(s)
		if n+size > maxLineOctets {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteString(s[:size])
		n += size
		s = s[size:]
	}
	return b.String()
}
