package hub

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

const notBuiltPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Meat Mode</title></head>
<body><p>The Meat Mode frontend was not built into this binary. Run <code>pnpm --dir web build</code>, then build the hub again.</p></body></html>
`

// staticHandler serves the built frontend from fsys. Missing files under
// /assets/ are 404; any other GET that names no file gets index.html, so
// app routes load the app.
type staticHandler struct {
	fsys  fs.FS
	index []byte // nil when the frontend was not built
}

func newStatic(fsys fs.FS) *staticHandler {
	h := &staticHandler{fsys: fsys}
	if b, err := fs.ReadFile(fsys, "index.html"); err == nil {
		h.index = b
	}
	return h
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := path.Clean("/" + r.URL.Path)
	name := strings.TrimPrefix(p, "/")
	isAsset := p == "/assets" || strings.HasPrefix(p, "/assets/")

	if name != "" && name != "index.html" && !hidden(name) {
		if f, err := h.fsys.Open(name); err == nil {
			defer f.Close()
			if st, err := f.Stat(); err == nil && !st.IsDir() {
				if rs, ok := f.(io.ReadSeeker); ok {
					switch {
					case isAsset:
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					case name == "sw.js":
						w.Header().Set("Cache-Control", "no-cache")
					}
					http.ServeContent(w, r, name, st.ModTime(), rs)
					return
				}
			}
		}
	}
	if isAsset {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	if h.index == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, notBuiltPage)
		return
	}
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(h.index))
}

// hidden reports whether any path element is a dotfile such as .gitkeep.
func hidden(name string) bool {
	for _, el := range strings.Split(name, "/") {
		if strings.HasPrefix(el, ".") {
			return true
		}
	}
	return false
}
