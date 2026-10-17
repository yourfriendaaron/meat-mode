package hub

import (
	"net/http"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// remindersRoutes registers the test reminder (docs/api.md, Push).
func (s *Server) remindersRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/reminders/test", s.handleTestReminder)
}

func (s *Server) handleTestReminder(w http.ResponseWriter, r *http.Request) {
	t, ok := s.push.(ReminderTester)
	if !ok {
		writeError(w, http.StatusNotFound, "push notifications are not set up on this hub")
		return
	}
	var body api.ReminderTestRequest
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	var sessionID string
	if sess := sessionOf(r); sess != nil {
		sessionID = sess.ID
	}
	res := t.TestReminder(r.Context(), sessionID, body.Endpoint, s.cfg.PublicURL)
	writeJSON(w, http.StatusOK, res)
}
