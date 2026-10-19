package hub

import (
	"net/http"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// maxOccurrenceWindow is the longest window GET /api/occurrences lists: a
// month grid of six weeks, with room to spare.
const maxOccurrenceWindow = 62 * 24 * time.Hour

// occurrencesRoutes registers the month view's repeat dates (docs/api.md,
// Month view).
func (s *Server) occurrencesRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/occurrences", s.handleOccurrences)
}

func (s *Server) handleOccurrences(w http.ResponseWriter, r *http.Request) {
	var t [2]time.Time
	for i, name := range []string{"from", "to"} {
		v, err := time.Parse(time.RFC3339, r.URL.Query().Get(name))
		if err != nil {
			writeError(w, http.StatusBadRequest, name+" must be a time, like 2026-11-01T00:00:00Z")
			return
		}
		t[i] = v
	}
	from, to := t[0], t[1]
	if !to.After(from) {
		writeError(w, http.StatusBadRequest, "to must be after from")
		return
	}
	if to.Sub(from) > maxOccurrenceWindow {
		writeError(w, http.StatusBadRequest, "the window from from to to can be at most 62 days")
		return
	}
	list, err := s.store.Occurrences(from, to)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.Occurrences{Occurrences: list})
}
