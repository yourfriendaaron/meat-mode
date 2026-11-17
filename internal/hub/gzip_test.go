package hub

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// doHeader is h.do for a GET or HEAD with extra request headers.
func (h *harness) doHeader(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Host = "localhost:8080"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	h.addCookie(req)
	w := httptest.NewRecorder()
	h.srv.ServeHTTP(w, req)
	return w
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestAcceptsGzip(t *testing.T) {
	for in, want := range map[string]bool{
		"":                  false,
		"gzip":              true,
		"br, gzip, deflate": true,
		"GZIP;q=0.5":        true,
		"gzip;q=0":          false,
		"gzip; q=0.0":       false,
		"deflate, br":       false,
		"x-gzip":            false,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", in)
		if got := acceptsGzip(r); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
}

func TestStaticGzip(t *testing.T) {
	mod := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	js := strings.Repeat("console.log('Water the plants');\n", 200)
	page := "<!doctype html>" + strings.Repeat("<p>Water the plants</p>", 100)
	h := newHarness(t, fstest.MapFS{
		"index.html":       {Data: []byte(page), ModTime: mod},
		"assets/app-1a.js": {Data: []byte(js), ModTime: mod},
		"manifest.json":    {Data: []byte("{}"), ModTime: mod},
	}, false)
	gz := map[string]string{"Accept-Encoding": "gzip, deflate, br"}

	// Accepted: the gzipped copy, with the plain file's type and caching.
	w := h.doHeader("GET", "/assets/app-1a.js", gz)
	h.expect(w, http.StatusOK)
	hd := w.Header()
	if hd.Get("Content-Encoding") != "gzip" || hd.Get("Vary") != "Accept-Encoding" ||
		!strings.Contains(hd.Get("Content-Type"), "javascript") || !strings.Contains(hd.Get("Cache-Control"), "immutable") ||
		hd.Get("Accept-Ranges") != "" || hd.Get("Content-Length") != strconv.Itoa(w.Body.Len()) ||
		hd.Get("Last-Modified") != mod.Format(http.TimeFormat) {
		t.Fatalf("gzipped asset headers %v", hd)
	}
	if gunzip(t, w.Body.Bytes()) != js {
		t.Fatal("gzipped asset does not unpack to the file")
	}
	// App routes get the gzipped index.
	for _, p := range []string{"/", "/done"} {
		w := h.doHeader("GET", p, gz)
		if w.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") ||
			gunzip(t, w.Body.Bytes()) != page {
			t.Fatalf("%s: headers %v", p, w.Header())
		}
	}

	// Not accepted: the plain file, still marked as varying.
	for _, hdr := range []map[string]string{nil, {"Accept-Encoding": "gzip;q=0"}, {"Accept-Encoding": "br"}} {
		w := h.doHeader("GET", "/assets/app-1a.js", hdr)
		if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "Accept-Encoding" ||
			w.Header().Get("Accept-Ranges") != "bytes" || w.Body.String() != js {
			t.Fatalf("%v: headers %v", hdr, w.Header())
		}
	}

	// A range is of the plain file, even when gzip is accepted.
	w = h.doHeader("GET", "/assets/app-1a.js", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-6"})
	h.expect(w, http.StatusPartialContent)
	if w.Header().Get("Content-Encoding") != "" || w.Body.String() != "console" {
		t.Fatalf("range: headers %v body %q", w.Header(), w.Body.String())
	}

	// Conditional requests and HEAD hold in both forms.
	for _, hdr := range []map[string]string{nil, gz} {
		cond := map[string]string{"If-Modified-Since": mod.Format(http.TimeFormat)}
		for k, v := range hdr {
			cond[k] = v
		}
		w := h.doHeader("GET", "/assets/app-1a.js", cond)
		h.expect(w, http.StatusNotModified)
		if w.Header().Get("Content-Encoding") != "" || w.Body.Len() != 0 {
			t.Fatalf("304 %v: headers %v", hdr, w.Header())
		}
		w = h.doHeader("HEAD", "/assets/app-1a.js", hdr)
		h.expect(w, http.StatusOK)
		if w.Body.Len() != 0 || (hdr != nil) != (w.Header().Get("Content-Encoding") == "gzip") {
			t.Fatalf("HEAD %v: headers %v", hdr, w.Header())
		}
	}

	// A failed precondition has no body, so it announces none in either form.
	for _, hdr := range []map[string]string{nil, gz} {
		cond := map[string]string{"If-Match": `"x"`}
		for k, v := range hdr {
			cond[k] = v
		}
		w := h.doHeader("GET", "/assets/app-1a.js", cond)
		h.expect(w, http.StatusPreconditionFailed)
		if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Content-Length") != "" || w.Body.Len() != 0 {
			t.Fatalf("412 %v: headers %v", hdr, w.Header())
		}
	}

	// A file gzip would not shrink is served plain, with no Vary.
	w = h.doHeader("GET", "/manifest.json", gz)
	if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "" || w.Body.String() != "{}" {
		t.Fatalf("small file: headers %v", w.Header())
	}
}

func TestStateGzip(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	w := h.doHeader("GET", "/api/state", map[string]string{"Accept-Encoding": "gzip"})
	h.expect(w, http.StatusOK)
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" ||
		w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers %v", w.Header())
	}
	var st api.State
	if err := json.Unmarshal([]byte(gunzip(t, w.Body.Bytes())), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Items) != 1 || st.Items[0].Title != "Water the plants" {
		t.Fatalf("state %+v", st.Items)
	}
	w = h.doHeader("GET", "/api/state", nil)
	if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "Accept-Encoding" ||
		decode[api.State](t, w).Items[0].Title != "Water the plants" {
		t.Fatalf("plain state: headers %v", w.Header())
	}
}

// Replies that carry a token, the event stream and the export stay plain
// whatever the request accepts.
func TestNeverGzipped(t *testing.T) {
	h := newHarness(t, nil, false)
	req := httptest.NewRequest("POST", "/api/bridges", strings.NewReader(`{"name": "laptop"}`))
	req.Host = "localhost:8080"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	h.addCookie(req)
	w := httptest.NewRecorder()
	h.srv.ServeHTTP(w, req)
	h.expect(w, http.StatusCreated)
	if w.Header().Get("Content-Encoding") != "" || decode[api.NewBridgeReply](t, w).Token == "" {
		t.Fatalf("pairing reply: headers %v", w.Header())
	}

	w = h.doHeader("GET", "/api/export", map[string]string{"Accept-Encoding": "gzip"})
	h.expect(w, http.StatusOK)
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("export: headers %v", w.Header())
	}

	ts := httptest.NewServer(h.srv)
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ev, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	ev.Header.Set("Accept-Encoding", "gzip") // set by hand, so the client leaves the body as sent
	h.addCookie(ev)
	resp, err := http.DefaultClient.Do(ev)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events: headers %v", resp.Header)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || strings.HasPrefix(line, "\x1f\x8b") {
		t.Fatalf("events first line %q, %v", line, err)
	}
}
