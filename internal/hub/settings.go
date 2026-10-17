package hub

import (
	"net/http"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// settingsRoutes registers the routes for the name and time zone kept in
// the app (docs/api.md, Settings).
func (s *Server) settingsRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/settings", s.handleSettings)
	m.HandleFunc("PUT /api/settings", s.handleUpdateSettings)
	m.Handle("GET /bridge/v1/settings", s.tokenAuth(func(w http.ResponseWriter, r *http.Request, _ string) {
		s.handleSettings(w, r)
	}))
}

// userName is the name kept in the app, or MEAT_MODE_USER.
func (s *Server) userName() (string, error) {
	h, err := s.store.Settings()
	return s.nameFrom(h), err
}

func (s *Server) nameFrom(h store.HubSettings) string {
	if h.UserName != "" {
		return h.UserName
	}
	return s.cfg.User
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Settings()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.Settings{Name: s.nameFrom(h), TimeZone: h.TimeZone})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body api.SettingsUpdate
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	if err := s.store.UpdateProfile(body.Name, body.TimeZone); err != nil {
		s.storeError(w, r, err)
		return
	}
	s.handleSettings(w, r)
}
