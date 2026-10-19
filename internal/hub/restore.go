package hub

import (
	"errors"
	"io"
	"net/http"
)

// maxRestoreBody bounds POST /api/restore: a whole export, every closed
// item and the full action log included.
const maxRestoreBody = 64 << 20

// restoreRoutes registers restoring a fresh hub from an export (docs/api.md,
// Export and restore). The login gate keeps it behind the session.
func (s *Server) restoreRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/restore", s.handleRestore)
}

// handleRestore hands the file to the store as it was downloaded: the
// store reads its version before the rest. A hub with data answers 409, a
// file it refuses 400.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRestoreBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "the file is larger than an export can be (64 MiB)")
		} else {
			writeError(w, http.StatusBadRequest, "the file did not arrive whole: "+err.Error())
		}
		return
	}
	res, err := s.store.Restore(body)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	s.log.Info("hub restored from an export", "items", res.Items, "actions", res.Actions, "cancelled", res.Cancelled)
	writeJSON(w, http.StatusOK, res)
}
