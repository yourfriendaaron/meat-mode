package hub

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// pngBytes starts like a PNG file, so the hub sniffs image/png.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR the rest of a picture of the ferns")

// send sends body with the given Content-Type and the harness's session.
func (h *harness) send(method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Host = "localhost:8080"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	h.addCookie(req)
	w := httptest.NewRecorder()
	h.srv.ServeHTTP(w, req)
	return w
}

// addTodo adds a to-do to the personal list and returns its key.
func (h *harness) addTodo(title string) string {
	h.t.Helper()
	w := h.do("POST", "/api/actions", api.NewAction{ID: uuid.NewString(), Kind: api.KindAdd,
		Bridge: "hub", Repo: "personal", Payload: api.Payload{Title: title}}, "")
	h.expect(w, http.StatusCreated)
	a := decode[api.Action](h.t, w)
	return store.ItemKey(a.Bridge, a.Repo, a.BeadID)
}

// rev is the live updates' revision now.
func (h *harness) rev() int64 {
	h.srv.events.mu.Lock()
	defer h.srv.events.mu.Unlock()
	return h.srv.events.rev
}

// bumped checks that the revision moved on from was, and returns it.
func (h *harness) bumped(was int64) int64 {
	h.t.Helper()
	now := h.rev()
	if now <= was {
		h.t.Fatalf("revision %d did not move on from %d; live updates would not refresh", now, was)
	}
	return now
}

func uploadPath(key, name string) string {
	return "/api/items/" + url.PathEscape(key) + "/attachments?name=" + url.QueryEscape(name)
}

func (h *harness) upload(key, name string, body []byte) api.Attachment {
	h.t.Helper()
	w := h.send("POST", uploadPath(key, name), "application/octet-stream", body)
	h.expect(w, http.StatusCreated)
	return decode[api.Attachment](h.t, w)
}

func stateItem(t *testing.T, h *harness, key string) api.Item {
	t.Helper()
	for _, it := range decode[api.State](t, h.do("GET", "/api/state", nil, "")).Items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("no item %s", key)
	return api.Item{}
}

func TestAttachmentRoutes(t *testing.T) {
	h := newHarness(t, nil, false)
	key := h.addTodo("Water the plants")
	rev := h.rev()

	a := h.upload(key, "ferns.png", pngBytes)
	if a.Name != "ferns.png" || a.Type != "image/png" || a.Size != int64(len(pngBytes)) || len(a.ID) != 32 {
		t.Fatalf("attachment %+v", a)
	}
	rev = h.bumped(rev)
	if got := stateItem(t, h, key).Attachments; len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("state attachments %+v", got)
	}

	// An image is served inline as itself, under a policy of its own.
	w := h.do("GET", "/api/attachments/"+a.ID, nil, "")
	h.expect(w, http.StatusOK)
	if !bytes.Equal(w.Body.Bytes(), pngBytes) {
		t.Fatal("served bytes differ")
	}
	for k, want := range map[string]string{
		"Content-Type":            "image/png",
		"Content-Disposition":     `inline; filename="ferns.png"; filename*=UTF-8''ferns.png`,
		"Content-Security-Policy": "default-src 'none'; sandbox",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "private, max-age=86400",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s: %q, want %q", k, got, want)
		}
	}

	// Anything else downloads: HTML, SVG and PDF among them.
	for name, body := range map[string]string{
		"page.html":   "<html><script>alert(1)</script></html>",
		"logo.svg":    `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"quote.pdf":   "%PDF-1.4 a quote for compost",
		"Säen 1\".gz": "\x1f\x8b\x08 seeds",
	} {
		b := h.upload(key, name, []byte(body))
		rev = h.bumped(rev)
		w := h.do("GET", "/api/attachments/"+b.ID, nil, "")
		h.expect(w, http.StatusOK)
		if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Errorf("%s served as %q", name, ct)
		}
		if d := w.Header().Get("Content-Disposition"); !strings.HasPrefix(d, "attachment; ") {
			t.Errorf("%s disposition %q", name, d)
		}
		if name == "Säen 1\".gz" {
			if d, want := w.Header().Get("Content-Disposition"), `attachment; filename="S_en 1_.gz"; filename*=UTF-8''S%C3%A4en%201%22.gz`; d != want {
				t.Errorf("disposition %q, want %q", d, want)
			}
		}
	}

	// Remove, then Undo.
	h.expect(h.do("DELETE", "/api/attachments/"+a.ID, nil, ""), http.StatusNoContent)
	rev = h.bumped(rev)
	h.expect(h.do("GET", "/api/attachments/"+a.ID, nil, ""), http.StatusNotFound)
	for _, at := range stateItem(t, h, key).Attachments {
		if at.ID == a.ID {
			t.Fatal("a removed file is still on its item")
		}
	}
	w = h.do("POST", "/api/attachments/"+a.ID+"/restore", nil, "")
	h.expect(w, http.StatusOK)
	h.bumped(rev)
	if back := decode[api.Attachment](t, w); back.ID != a.ID {
		t.Fatalf("restored %+v", back)
	}
	if got := stateItem(t, h, key).Attachments; got[0].ID != a.ID {
		t.Fatalf("restored file not back in its place: %+v", got)
	}

	h.expect(h.do("GET", "/api/attachments/"+strings.Repeat("0", 32), nil, ""), http.StatusNotFound)
	h.expect(h.do("POST", "/api/attachments/"+strings.Repeat("0", 32)+"/restore", nil, ""), http.StatusNotFound)
	h.expect(h.do("DELETE", "/api/attachments/nothing", nil, ""), http.StatusNotFound)

	// Like every app route, they need a session.
	h.cookie = ""
	h.expect(h.do("GET", "/api/attachments/"+a.ID, nil, ""), http.StatusUnauthorized)
	h.expect(h.send("POST", uploadPath(key, "a.txt"), "application/octet-stream", []byte("a")), http.StatusUnauthorized)
}

func TestAttachmentUploadRefusals(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	key := h.addTodo("Water the plants")

	h.expect(h.send("POST", uploadPath("laptop/garden/gd-1", "a.txt"), "application/octet-stream", []byte("a")), http.StatusBadRequest)
	h.expect(h.send("POST", uploadPath("hub/personal/mm-99", "a.txt"), "application/octet-stream", []byte("a")), http.StatusNotFound)
	h.expect(h.send("POST", "/api/items/"+url.PathEscape(key)+"/attachments?name=a%FF", "application/octet-stream", []byte("a")), http.StatusBadRequest)

	// Past the per-file cap, by its declared size: refused before it is
	// read, with the sizes the app shows.
	big := make([]byte, api.MaxAttachmentBytes+1)
	w := h.send("POST", uploadPath(key, "Vacation.mov"), "application/octet-stream", big)
	h.expect(w, http.StatusRequestEntityTooLarge)
	if msg := decode[map[string]string](t, w)["error"]; msg != "Vacation.mov is 25.1 MB; a file can be at most 25 MB." {
		t.Fatalf("413 says %q", msg)
	}
	// With no size given, it is cut off at the cap.
	req := httptest.NewRequest("POST", uploadPath(key, "Vacation.mov"), io.MultiReader(bytes.NewReader(big)))
	req.Host = "localhost:8080"
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/octet-stream")
	h.addCookie(req)
	w = httptest.NewRecorder()
	h.srv.ServeHTTP(w, req)
	h.expect(w, http.StatusRequestEntityTooLarge)

	// The item's cap.
	for range api.MaxAttachmentsPerItem {
		h.upload(key, "note.txt", []byte("x"))
	}
	w = h.send("POST", uploadPath(key, "one-more.txt"), "application/octet-stream", []byte("x"))
	h.expect(w, http.StatusRequestEntityTooLarge)
	if msg := decode[map[string]string](t, w)["error"]; msg != "An item can hold at most 20 files." {
		t.Fatalf("413 says %s", w.Body)
	}
	// Putting a removed one back past that cap is a conflict.
	first := stateItem(t, h, key).Attachments[0]
	h.expect(h.do("DELETE", "/api/attachments/"+first.ID, nil, ""), http.StatusNoContent)
	h.upload(key, "note.txt", []byte("y"))
	h.expect(h.do("POST", "/api/attachments/"+first.ID+"/restore", nil, ""), http.StatusConflict)
}

// The two routes that take a file take application/octet-stream; no other
// route does, and neither takes a type a form can send.
func TestGuardTakesOctetStreamOnlyForFiles(t *testing.T) {
	h := newHarness(t, nil, false)
	key := h.addTodo("Water the plants")
	upload := uploadPath(key, "a.txt")
	for _, c := range []struct {
		method, path, ct string
		want             int
	}{
		{"POST", upload, "application/octet-stream", http.StatusCreated},
		{"POST", upload, "application/json", http.StatusUnsupportedMediaType},
		{"POST", upload, "multipart/form-data; boundary=x", http.StatusUnsupportedMediaType},
		{"POST", upload, "text/plain", http.StatusUnsupportedMediaType},
		{"POST", upload, "application/octet-stream; x=y", http.StatusUnsupportedMediaType},
		{"POST", "/api/restore", "application/octet-stream", http.StatusConflict}, // past the guard, to a hub with data
		{"POST", "/api/restore", "application/json", http.StatusConflict},
		{"POST", "/api/restore", "text/plain", http.StatusUnsupportedMediaType},
		{"POST", "/api/restore", "multipart/form-data; boundary=x", http.StatusUnsupportedMediaType},
		{"POST", "/api/actions", "application/octet-stream", http.StatusUnsupportedMediaType},
		{"POST", "/api/items/" + url.PathEscape(key) + "/seen", "application/octet-stream", http.StatusUnsupportedMediaType},
		{"PUT", upload, "application/octet-stream", http.StatusUnsupportedMediaType},
		{"DELETE", "/api/attachments/" + strings.Repeat("0", 32), "application/octet-stream", http.StatusUnsupportedMediaType},
		{"POST", "/api/items/a/b/attachments", "application/octet-stream", http.StatusUnsupportedMediaType},
	} {
		if w := h.send(c.method, c.path, c.ct, []byte("a")); w.Code != c.want {
			t.Errorf("%s %s as %q: %d %s, want %d", c.method, c.path, c.ct, w.Code, w.Body, c.want)
		}
	}
}

// exportOf reads export.json out of an export's zip.
func exportOf(t *testing.T, file []byte) api.Export {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	if zr.File[0].Name != api.ExportJSONName {
		t.Fatalf("first entry %q", zr.File[0].Name)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var e api.Export
	if err := json.NewDecoder(rc).Decode(&e); err != nil {
		t.Fatal(err)
	}
	return e
}

// An export with files restores into a fresh hub through the routes: the
// same files, byte for byte, and the same metadata.
func TestExportThenRestoreWithAttachments(t *testing.T) {
	h := newHarness(t, nil, false)
	key := h.addTodo("Water the plants")
	other := h.addTodo("Buy seeds")
	files := map[string][]byte{}
	for _, f := range []struct {
		key, name string
		body      []byte
	}{
		{key, "ferns.png", pngBytes},
		{key, "Seed list ö.txt", []byte("Tomatoes, basil, chard")},
		{other, "quote.pdf", []byte("%PDF-1.4 a quote for seeds")},
		{other, "removed.txt", []byte("gone")},
	} {
		a := h.upload(f.key, f.name, f.body)
		files[a.ID] = f.body
	}
	removed := stateItem(t, h, other).Attachments[1]
	h.expect(h.do("DELETE", "/api/attachments/"+removed.ID, nil, ""), http.StatusNoContent)
	delete(files, removed.ID)

	w := h.do("GET", "/api/export", nil, "")
	h.expect(w, http.StatusOK)
	file := w.Body.Bytes()
	first := exportOf(t, file)

	fresh := newHarness(t, nil, false)
	w = fresh.send("POST", "/api/restore", "application/octet-stream", file)
	fresh.expect(w, http.StatusOK)
	if res := decode[api.RestoreResult](t, w); res.Attachments != 3 || res.Items != 2 {
		t.Fatalf("result %+v", res)
	}
	for id, body := range files {
		w := fresh.do("GET", "/api/attachments/"+id, nil, "")
		fresh.expect(w, http.StatusOK)
		if !bytes.Equal(w.Body.Bytes(), body) {
			t.Errorf("file %s differs after the restore", id)
		}
	}
	fresh.expect(fresh.do("GET", "/api/attachments/"+removed.ID, nil, ""), http.StatusNotFound)

	w = fresh.do("GET", "/api/export", nil, "")
	fresh.expect(w, http.StatusOK)
	second := exportOf(t, w.Body.Bytes())
	for i := range first.Items {
		a, _ := json.Marshal(first.Items[i])
		b, _ := json.Marshal(second.Items[i])
		if string(a) != string(b) {
			t.Errorf("item after the restore\n got %s\nwant %s", b, a)
		}
	}

	// The same zip into a hub with data is refused.
	h.expect(h.send("POST", "/api/restore", "application/octet-stream", file), http.StatusConflict)
}

// A file the export lists but the hub lost stops the export with a 500
// naming it, rather than a zip without it.
func TestExportStopsOnAMissingFile(t *testing.T) {
	h := newHarness(t, nil, false)
	key := h.addTodo("Water the plants")
	a := h.upload(key, "ferns.png", pngBytes)
	must(t, os.Remove(filepath.Join(h.dir, store.AttachmentDir, a.ID)))
	w := h.do("GET", "/api/export", nil, "")
	h.expect(w, http.StatusInternalServerError)
	if !strings.Contains(w.Body.String(), "ferns.png") {
		t.Fatalf("500 says %s", w.Body)
	}
}

// A bare JSON export of version 2 listing files is refused, naming how
// many; a version 1 file still restores (TestRestoreRoute).
func TestRestoreRouteRefusesBareJSONWithFiles(t *testing.T) {
	h := newHarness(t, nil, false)
	key := h.addTodo("Water the plants")
	h.upload(key, "ferns.png", pngBytes)
	w := h.do("GET", "/api/export", nil, "")
	h.expect(w, http.StatusOK)
	js, err := json.Marshal(exportOf(t, w.Body.Bytes()))
	must(t, err)

	fresh := newHarness(t, nil, false)
	w = fresh.do("POST", "/api/restore", string(js), "")
	fresh.expect(w, http.StatusBadRequest)
	if !strings.Contains(w.Body.String(), "lists 1 attached files") {
		t.Fatalf("400 says %s", w.Body)
	}
}

func TestSweepRunsAtStart(t *testing.T) {
	h := newHarness(t, nil, false)
	stray := filepath.Join(h.dir, store.AttachmentDir, strings.Repeat("ab", 16))
	must(t, os.WriteFile(stray, []byte("x"), 0o640))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.srv.sweepAttachments(ctx, time.Hour)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(stray); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sweep did not run at start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
