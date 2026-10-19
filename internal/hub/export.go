package hub

import (
	"net/http"
	"time"
)

// exportRoutes registers the export (docs/api.md, Export and restore).
func (s *Server) exportRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/export", s.handleExport)
}

// handleExport sends the export as a file to save, named for the day it
// was made in the hub's time zone.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.Export()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	zone := time.UTC
	if loc, err := time.LoadLocation(e.Settings.TimeZone); err == nil && e.Settings.TimeZone != "" {
		zone = loc
	}
	name := "meat-mode-export-" + e.ExportedAt.In(zone).Format(time.DateOnly) + ".json"
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	writeJSON(w, http.StatusOK, e)
}
