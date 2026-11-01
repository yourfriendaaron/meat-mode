package hub

import (
	"io"
	"net/http"
	"strconv"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// iconCSP replaces the page's policy on a served icon, so an SVG opened on
// its own runs nothing and loads nothing on the hub's origin.
const iconCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

// iconRoutes registers the repo icons (docs/api.md, List identity). The
// login gate keeps the app's route behind the session.
func (s *Server) iconRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/icons/{hash}", s.handleGetIcon)
	m.Handle("PUT /bridge/v1/icons", s.bridgeAuth(s.handlePutIcon))
}

func (s *Server) handleGetIcon(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !api.ValidIconHash(hash) {
		writeError(w, http.StatusBadRequest, "icon hash is not 32 lowercase hex digits")
		return
	}
	typ, data, err := s.store.Icon(hash)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Security-Policy", iconCSP)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handlePutIcon takes a repo's icon, or with an empty body clears it. The
// body is checked before the repo, as the contract orders the refusals.
func (s *Server) handlePutIcon(w http.ResponseWriter, r *http.Request, bridge string) {
	repo := r.URL.Query().Get("repo")
	if !store.ValidRepoName(repo) {
		writeError(w, http.StatusBadRequest, "repo name is not valid")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, api.MaxIconBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the icon: "+err.Error())
		return
	}
	if len(data) > api.MaxIconBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "the icon is larger than 64 KiB")
		return
	}
	if len(data) > 0 {
		if _, ok := api.SniffIcon(data); !ok {
			writeError(w, http.StatusBadRequest, "the icon is not an SVG, PNG or ICO")
			return
		}
	}
	if err := s.store.SetIcon(bridge, repo, data); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
