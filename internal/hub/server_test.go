package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

const (
	aid1 = "6f1e2d3c-4b5a-4c6d-8e7f-90a1b2c3d4e1"
	aid2 = "6f1e2d3c-4b5a-4c6d-8e7f-90a1b2c3d4e2"
)

type fakePush struct {
	mu   sync.Mutex
	subs map[string]api.PushSubscription
}

func (f *fakePush) PublicKey() string { return "BExampleKey" }
func (f *fakePush) Subscribe(_ context.Context, s api.PushSubscription) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[s.Endpoint] = s
	return nil
}
func (f *fakePush) Unsubscribe(_ context.Context, endpoint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subs, endpoint)
	return nil
}

type harness struct {
	t     *testing.T
	srv   *Server
	push  *fakePush
	token string
}

func newHarness(t *testing.T, static fstest.MapFS, withPush bool) *harness {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &harness{t: t}
	var push PushRegistry
	if withPush {
		h.push = &fakePush{subs: map[string]api.PushSubscription{}}
		push = h.push
	}
	if static == nil {
		static = fstest.MapFS{}
	}
	h.srv = newServer(Config{User: "alex"}, st, push, static)
	t.Cleanup(h.srv.Close)
	return h
}

// do sends a request and returns the recorder. body may be a string or a
// value to marshal.
func (h *harness) do(method, path string, body any, token string) *httptest.ResponseRecorder {
	h.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			h.t.Fatal(err)
		}
		r = strings.NewReader(string(buf))
	}
	req := httptest.NewRequest(method, path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.srv.ServeHTTP(w, req)
	return w
}

func (h *harness) expect(w *httptest.ResponseRecorder, status int) {
	h.t.Helper()
	if w.Code != status {
		h.t.Fatalf("status %d, want %d; body %s", w.Code, status, w.Body.String())
	}
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return v
}

// pair adds bridge "laptop" with repo "garden" and one item through the API.
func (h *harness) pair() {
	h.t.Helper()
	w := h.do("POST", "/api/bridges", map[string]string{"name": "laptop"}, "")
	h.expect(w, http.StatusCreated)
	h.token = decode[api.NewBridgeReply](h.t, w).Token
	h.expect(h.do("POST", "/bridge/v1/hello", api.Hello{Version: "1.0.0", BdVersion: "1.3.0"}, h.token), http.StatusOK)
	h.expect(h.do("PUT", "/bridge/v1/repos", []api.Repo{{Name: "garden", OptedIn: true}, {Name: "chores"}}, h.token), http.StatusNoContent)
	h.expect(h.do("PUT", "/bridge/v1/repos/garden/items", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open", Priority: 2},
	}, h.token), http.StatusNoContent)
}

func TestBridgesRoutes(t *testing.T) {
	h := newHarness(t, nil, false)
	h.expect(h.do("POST", "/api/bridges", map[string]string{"name": "Not Valid"}, ""), http.StatusBadRequest)
	h.pair()
	h.expect(h.do("POST", "/api/bridges", map[string]string{"name": "laptop"}, ""), http.StatusConflict)

	w := h.do("GET", "/api/bridges", nil, "")
	h.expect(w, http.StatusOK)
	bs := decode[[]api.Bridge](t, w)
	if len(bs) != 1 || bs[0].Name != "laptop" || bs[0].BdVersion != "1.3.0" || bs[0].LastSeen == nil {
		t.Fatalf("bridges %+v", bs)
	}

	h.expect(h.do("DELETE", "/api/bridges/laptop", nil, ""), http.StatusNoContent)
	h.expect(h.do("DELETE", "/api/bridges/laptop", nil, ""), http.StatusNotFound)
	h.expect(h.do("GET", "/bridge/v1/actions", nil, h.token), http.StatusUnauthorized)
}

func TestBridgeAuth(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	for _, tok := range []string{"", "mm_unknown", "garbage"} {
		w := h.do("GET", "/bridge/v1/actions", nil, tok)
		h.expect(w, http.StatusUnauthorized)
		if !strings.Contains(w.Body.String(), `"error"`) {
			t.Errorf("no JSON error for %q", tok)
		}
	}
	routes := [][2]string{
		{"POST", "/bridge/v1/hello"}, {"PUT", "/bridge/v1/repos"}, {"PUT", "/bridge/v1/repos/garden/items"},
		{"GET", "/bridge/v1/actions"}, {"POST", "/bridge/v1/actions/" + aid1 + "/result"},
	}
	for _, rt := range routes {
		h.expect(h.do(rt[0], rt[1], "{}", ""), http.StatusUnauthorized)
	}
	w := h.do("POST", "/bridge/v1/hello", api.Hello{Version: "1.0.0"}, h.token)
	h.expect(w, http.StatusOK)
	if r := decode[api.HelloReply](t, w); r.Bridge != "laptop" || r.ServerTime.IsZero() {
		t.Fatalf("hello %+v", r)
	}
}

func TestActionFlowOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()

	na := api.NewAction{ID: aid1, Kind: api.KindNote, Key: "laptop/garden/gd-1", Payload: api.Payload{Text: "Used the rain barrel"}}
	w := h.do("POST", "/api/actions", na, "")
	h.expect(w, http.StatusCreated)
	a := decode[api.Action](t, w)
	if a.Author != "alex-app" || a.Status != api.StatusPending {
		t.Fatalf("action %+v", a)
	}
	// Same id again: 200 with the stored action.
	w = h.do("POST", "/api/actions", api.NewAction{ID: aid1, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, "")
	h.expect(w, http.StatusOK)
	if d := decode[api.Action](t, w); d.Kind != api.KindNote || d.Seq != a.Seq {
		t.Fatalf("duplicate %+v", d)
	}

	w = h.do("GET", "/api/state", nil, "")
	h.expect(w, http.StatusOK)
	st := decode[api.State](t, w)
	if st.User != "alex" || st.Unapplied != 1 || len(st.Items) != 1 || !st.Items[0].Pending || len(st.Items[0].Comments) != 1 {
		t.Fatalf("state %+v", st)
	}

	w = h.do("GET", "/bridge/v1/actions", nil, h.token)
	h.expect(w, http.StatusOK)
	if p := decode[[]api.Action](t, w); len(p) != 1 || p[0].ID != aid1 {
		t.Fatalf("pulled %+v", p)
	}
	h.expect(h.do("POST", "/bridge/v1/actions/"+aid1+"/result", api.ActionResult{OK: true}, h.token), http.StatusNoContent)
	h.expect(h.do("POST", "/bridge/v1/actions/"+aid2+"/result", api.ActionResult{OK: true}, h.token), http.StatusNotFound)

	w = h.do("GET", "/bridge/v1/actions", nil, h.token)
	if p := decode[[]api.Action](t, w); len(p) != 0 {
		t.Fatalf("pulled after apply %+v", p)
	}
	// Applied: cancel is a conflict.
	h.expect(h.do("POST", "/api/actions/"+aid1+"/cancel", nil, ""), http.StatusConflict)

	// A pending one cancels.
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: aid2, Kind: api.KindDone, Key: "laptop/garden/gd-1"}, ""), http.StatusCreated)
	w = h.do("POST", "/api/actions/"+aid2+"/cancel", nil, "")
	h.expect(w, http.StatusOK)
	if c := decode[api.Action](t, w); c.Status != api.StatusCancelled {
		t.Fatalf("cancel %+v", c)
	}
	h.expect(h.do("POST", "/api/actions/no-such-id/cancel", nil, ""), http.StatusNotFound)

	w = h.do("GET", "/api/actions?limit=1", nil, "")
	h.expect(w, http.StatusOK)
	if l := decode[[]api.Action](t, w); len(l) != 1 || l[0].ID != aid2 {
		t.Fatalf("recent %+v", l)
	}
	h.expect(h.do("GET", "/api/actions?limit=zero", nil, ""), http.StatusBadRequest)
	w = h.do("GET", "/api/actions", nil, "")
	if l := decode[[]api.Action](t, w); len(l) != 2 {
		t.Fatalf("recent default %+v", l)
	}
}

func TestActionValidationOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	cases := []struct {
		body   any
		status int
	}{
		{"not json", http.StatusBadRequest},
		{api.NewAction{ID: aid1, Kind: "launch", Key: "laptop/garden/gd-1"}, http.StatusBadRequest},
		{api.NewAction{ID: aid1, Kind: api.KindDone, Key: "laptop/garden/gd-404"}, http.StatusNotFound},
		{api.NewAction{ID: aid1, Kind: api.KindSnooze, Key: "laptop/garden/gd-1"}, http.StatusBadRequest},
		{api.NewAction{ID: aid1, Kind: api.KindNote, Key: "laptop/garden/gd-1", Payload: api.Payload{Text: strings.Repeat("x", 10001)}}, http.StatusBadRequest},
		{api.NewAction{ID: aid1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden"}, http.StatusBadRequest},
		{api.NewAction{ID: aid1, Kind: api.KindAdd, Bridge: "laptop", Repo: "chores", Payload: api.Payload{Title: "Mop"}}, http.StatusBadRequest},
		{`{"id":"` + aid1 + `","kind":"note","key":"laptop/garden/gd-1","payload":{"text":"` + strings.Repeat("x", 1<<20) + `"}}`, http.StatusRequestEntityTooLarge},
	}
	for i, tc := range cases {
		w := h.do("POST", "/api/actions", tc.body, "")
		if w.Code != tc.status {
			t.Errorf("case %d: status %d, want %d (%s)", i, w.Code, tc.status, w.Body.String())
		}
		if !strings.Contains(w.Header().Get("Content-Type"), "json") {
			t.Errorf("case %d: error is not JSON", i)
		}
	}
}

func TestAddOverHTTP(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	h.expect(h.do("POST", "/api/actions", api.NewAction{ID: aid1, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Buy seeds"}}, ""), http.StatusCreated)
	st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	if len(st.Items) != 2 || st.Items[1].ID != "pending-"+aid1 {
		t.Fatalf("state %+v", st.Items)
	}
	h.expect(h.do("POST", "/bridge/v1/actions/"+aid1+"/result", api.ActionResult{OK: true, BeadID: "gd-2"}, h.token), http.StatusNoContent)
	h.expect(h.do("PUT", "/bridge/v1/repos/garden/items", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open"}, {ID: "gd-2", Title: "Buy seeds", Status: "open"},
	}, h.token), http.StatusNoContent)
	st = decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	if len(st.Items) != 2 || st.Items[1].ID != "gd-2" || st.Items[1].Pending {
		t.Fatalf("state after bead %+v", st.Items)
	}
}

func TestItemsAndReposRoutes(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	h.expect(h.do("PUT", "/bridge/v1/repos/attic/items", []api.Item{}, h.token), http.StatusNotFound)
	h.expect(h.do("PUT", "/bridge/v1/repos/garden/items", []api.Item{{ID: ""}}, h.token), http.StatusBadRequest)
	h.expect(h.do("PUT", "/bridge/v1/repos", []api.Repo{{Name: "a/b"}}, h.token), http.StatusBadRequest)

	// Hub-only fields from a bridge are ignored.
	h.expect(h.do("PUT", "/bridge/v1/repos/garden/items", []api.Item{
		{ID: "gd-1", Title: "Water the plants", Status: "open", Pending: true, Error: "x", Key: "other/x/y"},
	}, h.token), http.StatusNoContent)
	st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	it := st.Items[0]
	if it.Pending || it.Error != "" || it.Key != "laptop/garden/gd-1" || it.Bridge != "laptop" || it.Repo != "garden" {
		t.Fatalf("item %+v", it)
	}
	if len(st.Repos) != 2 || st.Repos[0].Bridge != "laptop" {
		t.Fatalf("repos %+v", st.Repos)
	}
}

func TestSeenRoute(t *testing.T) {
	h := newHarness(t, nil, false)
	h.pair()
	key := url.PathEscape("laptop/garden/gd-1")
	h.expect(h.do("POST", "/api/items/"+key+"/seen", nil, ""), http.StatusNoContent)
	st := decode[api.State](t, h.do("GET", "/api/state", nil, ""))
	if st.Items[0].SeenAt == nil {
		t.Fatal("seenAt not set")
	}
	h.expect(h.do("POST", "/api/items/"+url.PathEscape("laptop/garden/gd-9")+"/seen", nil, ""), http.StatusNotFound)
	h.expect(h.do("POST", "/api/items/not-a-key/seen", nil, ""), http.StatusBadRequest)
}

func TestPushRoutes(t *testing.T) {
	off := newHarness(t, nil, false)
	off.expect(off.do("GET", "/api/push/key", nil, ""), http.StatusNotFound)
	off.expect(off.do("POST", "/api/push/subscriptions", "{}", ""), http.StatusNotFound)
	off.expect(off.do("DELETE", "/api/push/subscriptions", "{}", ""), http.StatusNotFound)

	h := newHarness(t, nil, true)
	w := h.do("GET", "/api/push/key", nil, "")
	h.expect(w, http.StatusOK)
	if k := decode[map[string]string](t, w); k["publicKey"] != "BExampleKey" {
		t.Fatalf("key %v", k)
	}
	sub := `{"endpoint":"https://push.example.com/send/abc","expirationTime":null,"keys":{"p256dh":"BP","auth":"AU"}}`
	h.expect(h.do("POST", "/api/push/subscriptions", sub, ""), http.StatusNoContent)
	if _, ok := h.push.subs["https://push.example.com/send/abc"]; !ok {
		t.Fatal("not subscribed")
	}
	h.expect(h.do("POST", "/api/push/subscriptions", `{"endpoint":"http://push.example.com/x"}`, ""), http.StatusBadRequest)
	h.expect(h.do("DELETE", "/api/push/subscriptions", `{"endpoint":"https://push.example.com/send/abc"}`, ""), http.StatusNoContent)
	if len(h.push.subs) != 0 {
		t.Fatal("not unsubscribed")
	}
	h.expect(h.do("DELETE", "/api/push/subscriptions", `{}`, ""), http.StatusBadRequest)
}

func TestUnknownAPIRoutesAreJSON404(t *testing.T) {
	h := newHarness(t, fstest.MapFS{"index.html": {Data: []byte("<p>app</p>")}}, false)
	for _, p := range []string{"/api/nothing", "/bridge/v2/hello"} {
		w := h.do("GET", p, nil, "")
		h.expect(w, http.StatusNotFound)
		if !strings.Contains(w.Header().Get("Content-Type"), "json") {
			t.Errorf("%s: not JSON", p)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t, nil, false)
	w := h.do("GET", "/api/state", nil, "")
	if w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		w.Header().Get("Referrer-Policy") == "" ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("headers %v", w.Header())
	}
}

func TestHealth(t *testing.T) {
	h := newHarness(t, nil, false)
	h.expect(h.do("GET", "/healthz", nil, ""), http.StatusOK)
}

func TestStaticFallback(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":         {Data: []byte("<p>app</p>")},
		"sw.js":              {Data: []byte("self.addEventListener('push', () => {})")},
		"manifest.json":      {Data: []byte("{}")},
		"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
		".gitkeep":           {Data: nil},
	}
	h := newHarness(t, fsys, false)
	cases := []struct {
		path, body, cache string
		status            int
	}{
		{"/", "<p>app</p>", "no-cache", 200},
		{"/index.html", "<p>app</p>", "no-cache", 200},
		{"/done", "<p>app</p>", "no-cache", 200},
		{"/item/laptop%2Fgarden%2Fgd-1", "<p>app</p>", "no-cache", 200},
		{"/sw.js", "self.addEventListener", "no-cache", 200},
		{"/manifest.json", "{}", "", 200},
		{"/assets/app-1a2b.js", "console.log(1)", "immutable", 200},
		{"/assets/app-old.js", "", "", 404},
		{"/assets/", "", "", 404},
		{"/.gitkeep", "<p>app</p>", "no-cache", 200},
	}
	for _, tc := range cases {
		w := h.do("GET", tc.path, nil, "")
		if w.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.path, w.Code, tc.status)
			continue
		}
		if tc.body != "" && !strings.Contains(w.Body.String(), tc.body) {
			t.Errorf("%s: body %q", tc.path, w.Body.String())
		}
		if tc.cache != "" && !strings.Contains(w.Header().Get("Cache-Control"), tc.cache) {
			t.Errorf("%s: Cache-Control %q", tc.path, w.Header().Get("Cache-Control"))
		}
	}
	if ct := h.do("GET", "/assets/app-1a2b.js", nil, "").Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("asset content type %q", ct)
	}
}

func TestStaticNotBuilt(t *testing.T) {
	h := newHarness(t, fstest.MapFS{".gitkeep": {}}, false)
	w := h.do("GET", "/", nil, "")
	h.expect(w, http.StatusOK)
	if !strings.Contains(w.Body.String(), "not built") {
		t.Fatalf("body %q", w.Body.String())
	}
	h.expect(h.do("GET", "/assets/app.js", nil, ""), http.StatusNotFound)
}

func TestEventsEmitOnChange(t *testing.T) {
	h := newHarness(t, nil, false)
	h.srv.heartbeat = 50 * time.Millisecond
	ts := httptest.NewServer(h.srv)
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func(prefix string) string {
		t.Helper()
		timeout := time.After(3 * time.Second)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatal("stream closed")
				}
				if strings.HasPrefix(l, prefix) {
					return l
				}
			case <-timeout:
				t.Fatalf("no %q line", prefix)
			}
		}
	}
	if l := next("data:"); l != `data: {"rev": 0}` {
		t.Fatalf("first event %q", l)
	}
	// A change bumps the revision.
	h.do("POST", "/api/bridges", map[string]string{"name": "laptop"}, "")
	if l := next("data:"); l != `data: {"rev": 1}` {
		t.Fatalf("change event %q", l)
	}
	next(": heartbeat")

	// Disconnecting removes the subscriber.
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for h.srv.events.count() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber not removed after disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
