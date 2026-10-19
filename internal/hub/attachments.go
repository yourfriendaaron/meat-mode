package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// fileReadTime is how long the two routes that take a file, an upload and
// a restore, may take to read their body, past the server's ReadTimeout.
const fileReadTime = 30 * time.Minute

// attachmentCSP replaces the page's policy on a served file, so nothing in
// it runs or loads on the hub's origin.
const attachmentCSP = "default-src 'none'; sandbox"

// inlineTypes are the types served as themselves, inline, so the app shows
// them; every other file downloads as application/octet-stream.
var inlineTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// attachmentRoutes registers the attachments (docs/api.md, Attachments).
// The login gate keeps them behind the session.
func (s *Server) attachmentRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/items/{key}/attachments", s.handleAddAttachment)
	m.HandleFunc("GET /api/attachments/{id}", s.handleGetAttachment)
	m.HandleFunc("DELETE /api/attachments/{id}", s.handleRemoveAttachment)
	m.HandleFunc("POST /api/attachments/{id}/restore", s.handleRestoreAttachment)
}

// allowSlowBody gives a request fileReadTime to send its body. A writer
// that cannot set deadlines, as in tests, keeps the server's.
func allowSlowBody(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(fileReadTime))
}

// bodyReader remembers the first error reading a request body, so a body
// that did not arrive whole answers 400 rather than an internal error.
type bodyReader struct {
	r   io.Reader
	err error
}

func (b *bodyReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && err != io.EOF && b.err == nil {
		b.err = err
	}
	return n, err
}

// fileError writes the answer for an error from a route that took a file.
func (s *Server) fileError(w http.ResponseWriter, r *http.Request, body *bodyReader, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, store.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.As(body.err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "The file is larger than this route takes.")
	case body.err != nil:
		writeError(w, http.StatusBadRequest, "the file did not arrive whole: "+body.err.Error())
	default:
		s.storeError(w, r, err)
	}
}

// handleAddAttachment streams the body to disk as a file on the item. The
// store refuses a declared size past a cap before reading, and cuts off a
// body that runs past one.
func (s *Server) handleAddAttachment(w http.ResponseWriter, r *http.Request) {
	allowSlowBody(w)
	body := &bodyReader{r: http.MaxBytesReader(w, r.Body, api.MaxAttachmentBytes+1)}
	a, err := s.store.AddAttachment(r.PathValue("key"), r.URL.Query().Get("name"), r.ContentLength, body)
	if err != nil {
		s.fileError(w, r, body, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

// handleGetAttachment serves a file under its own policy: images inline as
// themselves, anything else as a download that never renders on the hub's
// origin.
func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	a, f, err := s.store.OpenAttachment(r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Security-Policy", attachmentCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("ETag", `"`+a.SHA256+`"`)
	disposition, ct := "attachment", "application/octet-stream"
	if inlineTypes[a.Type] {
		disposition, ct = "inline", a.Type
	}
	h.Set("Content-Type", ct)
	h.Set("Content-Disposition", contentDisposition(disposition, a.Name))
	http.ServeContent(w, r, "", a.CreatedAt, f)
}

// contentDisposition names a file both ways: an ASCII filename, with any
// other character as "_", and filename* with the UTF-8 name percent-escaped
// (RFC 6266, RFC 8187).
func contentDisposition(disposition, name string) string {
	var plain, escaped strings.Builder
	for _, c := range name {
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			plain.WriteByte('_')
		} else {
			plain.WriteRune(c)
		}
	}
	for _, b := range []byte(name) {
		if attrChar(b) {
			escaped.WriteByte(b)
		} else {
			fmt.Fprintf(&escaped, "%%%02X", b)
		}
	}
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, plain.String(), escaped.String())
}

// attrChar reports whether b may stand unescaped in an RFC 8187 value.
func attrChar(b byte) bool {
	switch {
	case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", b) >= 0
}

func (s *Server) handleRemoveAttachment(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RemoveAttachment(r.PathValue("id")); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestoreAttachment(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.RestoreAttachment(r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// sweepAttachments runs the store's sweep at once and then every interval
// until ctx ends.
func (s *Server) sweepAttachments(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.sweepOnce()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sweepOnce runs one sweep and logs what it did.
func (s *Server) sweepOnce() {
	rep, err := s.store.SweepAttachments()
	if err != nil {
		s.log.Error("attachment sweep failed", "err", err)
		return
	}
	if rep.Purged > 0 || len(rep.Strays) > 0 {
		s.log.Info("attachment sweep", "purged", rep.Purged, "stray files deleted", len(rep.Strays))
	}
	for _, id := range rep.Missing {
		s.log.Warn("an attachment's file is missing from the data directory", "id", id)
	}
}
