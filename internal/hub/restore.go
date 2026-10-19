package hub

import (
	"net/http"

	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// restoreRoutes registers restoring a fresh hub from an export (docs/api.md,
// Export and restore). The login gate keeps it behind the session.
func (s *Server) restoreRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/restore", s.handleRestore)
}

// handleRestore hands the file to the store as it arrives: the store tells
// a zip from bare JSON by its first bytes and holds each to its own limit.
// The body may take fileReadTime to arrive. A hub with data answers 409, a
// file past a limit 413, a file it refuses 400.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	allowSlowBody(w)
	body := &bodyReader{r: http.MaxBytesReader(w, r.Body, store.MaxRestoreFile+1)}
	res, err := s.store.Restore(body)
	if err != nil {
		s.fileError(w, r, body, err)
		return
	}
	s.log.Info("hub restored from an export", "items", res.Items, "actions", res.Actions, "cancelled", res.Cancelled,
		"attachments", res.Attachments)
	writeJSON(w, http.StatusOK, res)
}
