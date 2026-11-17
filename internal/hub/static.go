package hub

import (
	"bytes"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

const notBuiltPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Meat Mode</title></head>
<body><p>The Meat Mode frontend was not built into this binary. Run <code>pnpm --dir web build</code>, then build the hub again.</p></body></html>
`

// staticHandler serves the built frontend from fsys. Missing files under
// /assets/ are 404; any other GET that names no file gets index.html, so
// app routes load the app. A file gzip shrinks is also held gzipped, and
// served so to a request that accepts gzip and asks for no range.
type staticHandler struct {
	fsys  fs.FS
	index []byte              // nil when the frontend was not built
	gz    map[string]gzipFile // by file name
}

// gzipFile is a frontend file gzipped at startup, with the type of the
// plain file, since sniffing the gzipped bytes would name gzip.
type gzipFile struct {
	data  []byte
	ctype string
}

func newStatic(fsys fs.FS) *staticHandler {
	h := &staticHandler{fsys: fsys, gz: map[string]gzipFile{}}
	if b, err := fs.ReadFile(fsys, "index.html"); err == nil {
		h.index = b
	}
	fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || hidden(name) {
			return nil
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil
		}
		if z := gzipOnce(b); len(z) < len(b) {
			ctype := mime.TypeByExtension(path.Ext(name))
			if ctype == "" {
				ctype = http.DetectContentType(b)
			}
			h.gz[name] = gzipFile{data: z, ctype: ctype}
		}
		return nil
	})
	return h
}

// serve serves one file: gzipped when it has a gzipped copy and r accepts
// gzip without a Range, else plain from body. http.ServeContent answers
// HEAD and the conditional requests in both forms.
func (h *staticHandler) serve(w http.ResponseWriter, r *http.Request, name string, mod time.Time, body io.ReadSeeker) {
	z, ok := h.gz[name]
	if ok {
		w.Header().Add("Vary", "Accept-Encoding")
	}
	if !ok || r.Header.Get("Range") != "" || !acceptsGzip(r) {
		http.ServeContent(w, r, name, mod, body)
		return
	}
	w.Header().Set("Content-Type", z.ctype)
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Length", strconv.Itoa(len(z.data)))
	http.ServeContent(noRanges{w}, r, name, mod, bytes.NewReader(z.data))
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
					h.serve(w, r, name, st.ModTime(), rs)
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
	h.serve(w, r, "index.html", time.Time{}, bytes.NewReader(h.index))
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
