package hub

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Compression is scoped, not a wrapper over every route: the embedded
// frontend files are gzipped once at startup and GET /api/state per
// request. The event stream, attachments, the export and every reply that
// can carry a token stay plain, so no compressed body mixes a secret with
// text someone else can steer (BREACH).

// acceptsGzip reports whether r's Accept-Encoding takes gzip: listed with
// no q, or a q above zero.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		for _, p := range strings.Split(params, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if ok && strings.EqualFold(k, "q") {
				q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
				return err == nil && q > 0
			}
		}
		return true
	}
	return false
}

// gzipOnce compresses b at the best level, for a file served many times.
func gzipOnce(b []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression) // the level is valid
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

// fastGzip holds BestSpeed writers between requests; each one keeps
// several hundred KB of state.
var fastGzip = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed) // the level is valid
	return zw
}}

// writeJSONGzip is writeJSON, gzipped when r accepts it. Use it only for a
// body that holds no token.
func writeJSONGzip(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Add("Vary", "Accept-Encoding")
	if !acceptsGzip(r) {
		writeJSON(w, status, v)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(status)
	zw := fastGzip.Get().(*gzip.Writer)
	defer fastGzip.Put(zw)
	zw.Reset(w)
	json.NewEncoder(zw).Encode(v)
	zw.Close()
}

// noRanges drops the Accept-Ranges that http.ServeContent sets, for a
// gzipped body: a range request gets the plain file, so a range of the
// gzipped one would not match. An answer other than 200 carries no
// gzipped body, so it also drops the encoding and length set for one.
type noRanges struct{ http.ResponseWriter }

func (w noRanges) WriteHeader(code int) {
	w.Header().Del("Accept-Ranges")
	if code != http.StatusOK {
		w.Header().Del("Content-Encoding")
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(code)
}
