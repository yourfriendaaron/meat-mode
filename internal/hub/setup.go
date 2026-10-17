package hub

import (
	"net/http"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// setupRoutes registers the setup wizard's routes (docs/api.md, Setup).
func (s *Server) setupRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/setup", s.handleSetup)
	m.HandleFunc("POST /api/setup", s.handleRecordSetup)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	status, err := s.store.SetupStatus()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	h, err := s.store.Settings()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	out := api.Setup{
		Status:        status,
		User:          s.nameFrom(h),
		TimeZone:      h.TimeZone,
		Password:      "none",
		TailscaleOnly: h.TailscaleOnly,
	}
	switch {
	case s.cfg.PasswordHash != "":
		out.Password = "env"
	case h.PasswordHash != "":
		out.Password = "app"
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRecordSetup(w http.ResponseWriter, r *http.Request) {
	var body api.SetupRecord
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	if err := s.store.RecordSetup(body.Status); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
