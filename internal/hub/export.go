package hub

import (
	"errors"
	"net/http"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// exportRoutes registers the export (docs/api.md, Export and restore).
func (s *Server) exportRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/export", s.handleExport)
}

// handleExport sends the export's zip as a file to save, named for the day
// it was made in the hub's time zone, writing it as it goes. A file the
// export lists but cannot hold stops it: before the answer starts, with an
// error naming the file; after, by cutting the download off, so a partial
// zip is never taken for a whole export.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.Export()
	if errors.Is(err, store.ErrFileMissing) {
		s.log.Error("export stopped", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	zone := time.UTC
	if loc, err := time.LoadLocation(e.Settings.TimeZone); err == nil && e.Settings.TimeZone != "" {
		zone = loc
	}
	name := "meat-mode-export-" + e.ExportedAt.In(zone).Format(time.DateOnly) + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := s.store.WriteExportZip(w, e); err != nil {
		s.log.Error("export failed partway; the download is cut off", "err", err)
		panic(http.ErrAbortHandler)
	}
}
