package appclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/hub/hubtest"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// sessions records what onSession was called with.
type sessions struct {
	mu   sync.Mutex
	list []Session
}

func (r *sessions) add(s Session) {
	r.mu.Lock()
	r.list = append(r.list, s)
	r.mu.Unlock()
}

func (r *sessions) all() []Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Session(nil), r.list...)
}

// sleeps records the waits Events asks for and returns at once.
type sleeps struct {
	mu   sync.Mutex
	list []time.Duration
}

func (r *sleeps) sleep(ctx context.Context, d time.Duration) error {
	r.mu.Lock()
	r.list = append(r.list, d)
	r.mu.Unlock()
	return ctx.Err()
}

func (r *sleeps) all() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.list...)
}

func newClient(t *testing.T, hub string, s *Session, on func(Session)) *Client {
	t.Helper()
	c, err := New(hub, netguard.Set{}, s, on)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = (&sleeps{}).sleep
	return c
}

func login(t *testing.T, c *Client) Session {
	t.Helper()
	s, err := c.Login(context.Background(), hubtest.Password)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLoginAndSavedSession(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	h.Seed(t)
	var got sessions
	c := newClient(t, h.URL, nil, got.add)
	s := login(t, c)
	if s.Hub != h.URL || s.CookieName != sessionCookie || s.Cookie == "" {
		t.Fatalf("session %+v", s)
	}
	if l := got.all(); len(l) != 1 || l[0] != s {
		t.Errorf("onSession got %+v, want the login's session", l)
	}

	dir := t.TempDir()
	if err := SaveSession(dir, s); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := LoadSession(dir, h.URL)
	if err != nil || !ok || loaded != s {
		t.Fatalf("LoadSession = %+v, %v, %v", loaded, ok, err)
	}
	c2 := newClient(t, h.URL, &loaded, nil)
	if login, authed, err := c2.CheckSession(context.Background()); err != nil || !login || !authed {
		t.Errorf("CheckSession with the saved session = %v, %v, %v", login, authed, err)
	}
	if _, err := c2.State(context.Background()); err != nil {
		t.Errorf("State with the saved session: %v", err)
	}
}

func TestLoginSecureCookieName(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{PublicURL: "https://hub.example.com"})
	c := newClient(t, h.URL, nil, nil)
	s := login(t, c)
	if s.CookieName != secureSessionCookie {
		t.Fatalf("cookie name %q, want %q", s.CookieName, secureSessionCookie)
	}
	if _, err := c.State(context.Background()); err != nil {
		t.Errorf("State under the secure name: %v", err)
	}
}

func TestWrongPassword(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	var got sessions
	c := newClient(t, h.URL, nil, got.add)
	_, err := c.Login(context.Background(), "feed the cat")
	if !errors.Is(err, WrongPassword) || !errors.Is(err, ErrAuth) {
		t.Fatalf("Login = %v, want WrongPassword", err)
	}
	if len(got.all()) != 0 {
		t.Error("onSession called for a wrong password")
	}
}

func TestNoPasswordYet(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{NoPassword: true})
	c := newClient(t, h.URL, nil, nil)
	if _, err := c.Login(context.Background(), hubtest.Password); !errors.Is(err, ErrNoPassword) {
		t.Fatalf("Login = %v, want ErrNoPassword", err)
	}
	if login, authed, err := c.CheckSession(context.Background()); err != nil || login || authed {
		t.Errorf("CheckSession = %v, %v, %v; want false, false", login, authed, err)
	}
}

func TestEndedSession(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	c := newClient(t, h.URL, nil, nil)
	s := login(t, c)
	h.EndSession(t, s.Cookie)
	if _, err := c.State(context.Background()); !errors.Is(err, ErrAuth) {
		t.Fatalf("State = %v, want ErrAuth", err)
	}
	if login, authed, err := c.CheckSession(context.Background()); err != nil || !login || authed {
		t.Errorf("CheckSession = %v, %v, %v; want true, false", login, authed, err)
	}
}

func TestStateAndEpics(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	h.Seed(t)
	c := newClient(t, h.URL, nil, nil)
	login(t, c)
	st, err := c.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, it := range st.Items {
		titles[it.Key] = it.Title
	}
	if titles[hubtest.HumanKey] != hubtest.HumanTitle || titles[hubtest.PlainKey] != hubtest.PlainTitle ||
		titles[h.PersonalKey] != hubtest.PersonalTitle {
		t.Errorf("state items %v", titles)
	}
	e, err := c.Epics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Repos) != 1 || e.Repos[0].Repo != hubtest.Repo || len(e.Repos[0].Rows) != len(hubtest.EpicRows()) {
		t.Errorf("epics %+v", e)
	}
}

func TestActDone(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	h.Seed(t)
	c := newClient(t, h.URL, nil, nil)
	login(t, c)
	a, err := c.Act(context.Background(), api.NewAction{Kind: api.KindDone, Key: h.PersonalKey})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Kind != api.KindDone || a.Seq == 0 || a.Author != "alex-app" {
		t.Fatalf("action %+v", a)
	}
	again, err := c.Act(context.Background(), api.NewAction{ID: a.ID, Kind: api.KindDone, Key: h.PersonalKey})
	if err != nil || again.Seq != a.Seq {
		t.Errorf("the same id again = %+v, %v; want the stored action", again, err)
	}
	_, err = c.Act(context.Background(), api.NewAction{Kind: "no-such-kind", Key: h.PersonalKey})
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusBadRequest || !strings.Contains(he.Msg, "400") {
		t.Errorf("a bad kind = %v, want an HTTPError 400", err)
	}
}

func TestLogout(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	c := newClient(t, h.URL, nil, nil)
	s := login(t, c)
	if err := c.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.State(context.Background()); !errors.Is(err, ErrAuth) {
		t.Errorf("State after logout = %v, want ErrAuth", err)
	}
	// The hub ended the session too: the old cookie no longer works.
	old := newClient(t, h.URL, &s, nil)
	if _, err := old.State(context.Background()); !errors.Is(err, ErrAuth) {
		t.Errorf("State with the old cookie = %v, want ErrAuth", err)
	}
}

// collect runs Events in the background. stop ends its context and
// returns what Events returned; the test's cleanup calls it too.
func collect(t *testing.T, c *Client) (ch chan Event, stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch = make(chan Event, 64)
	done := make(chan error, 1)
	go func() { done <- c.Events(ctx, func(e Event) { ch <- e }) }()
	var once sync.Once
	var result error
	stop = func() error {
		once.Do(func() {
			cancel()
			select {
			case result = <-done:
			case <-time.After(5 * time.Second):
				t.Error("Events did not return after its context ended")
			}
		})
		return result
	}
	t.Cleanup(func() { stop() })
	return ch, stop
}

// waitFor reads events until one of kind, failing on any of stop first.
func waitFor(t *testing.T, ch chan Event, kind EventKind, stop ...EventKind) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Kind == kind {
				return e
			}
			for _, s := range stop {
				if e.Kind == s {
					t.Fatalf("got %s (%v) while waiting for %s", e.Kind, e.Err, kind)
				}
			}
		case <-deadline:
			t.Fatalf("no %s event", kind)
		}
	}
}

func TestEventsChangeAndEpics(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	h.Seed(t)
	c := newClient(t, h.URL, nil, nil)
	login(t, c)
	ch, stop := collect(t, c)
	waitFor(t, ch, EventConnected)
	waitFor(t, ch, EventChange) // the hub's change on connect

	h.SetEpics(t, hubtest.EpicRows()[:1])
	waitFor(t, ch, EventEpics, EventChange, EventDown)
	h.Touch(t, "Take out the bins")
	waitFor(t, ch, EventChange, EventEpics, EventDown)

	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Errorf("Events returned %v, want context.Canceled", err)
	}
}

func TestEventsReconnectAfterDrop(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	h.Seed(t)
	c := newClient(t, h.URL, nil, nil)
	var waits sleeps
	c.sleep = waits.sleep
	login(t, c)
	ch, _ := collect(t, c)
	waitFor(t, ch, EventConnected)
	waitFor(t, ch, EventChange)

	h.DropStreams()
	if e := waitFor(t, ch, EventDown, EventAuth); e.Err == nil {
		t.Error("EventDown without a reason")
	}
	waitFor(t, ch, EventConnected, EventAuth)
	if w := waits.all(); len(w) != 1 || w[0] != minBackoff {
		t.Errorf("waits %v, want one of %v", w, minBackoff)
	}
	h.Touch(t, "Take out the bins")
	waitFor(t, ch, EventChange, EventDown)
}

func TestEventsSessionEnds(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	c := newClient(t, h.URL, nil, nil)
	s := login(t, c)
	ch, stop := collect(t, c)
	waitFor(t, ch, EventConnected)

	// The hub would end the stream at its next heartbeat; drop it now.
	h.EndSession(t, s.Cookie)
	h.DropStreams()
	waitFor(t, ch, EventAuth, EventConnected)
	if err := stop(); !errors.Is(err, ErrAuth) {
		t.Errorf("Events returned %v, want ErrAuth", err)
	}

	// Starting again without a session: the 401 alone says so.
	ch, stop = collect(t, c)
	waitFor(t, ch, EventAuth, EventConnected)
	if err := stop(); !errors.Is(err, ErrAuth) {
		t.Errorf("Events returned %v, want ErrAuth", err)
	}
}

// fakeHub serves handler on loopback; its URL passes NormalizeHub.
func fakeHub(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func stream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": heartbeat\n\nevent: change\ndata: {\"rev\": 1}\n\n")
	w.(http.Flusher).Flush()
	<-r.Context().Done()
}

func TestEventsRetryAfter(t *testing.T) {
	var n atomic.Int32
	url := fakeHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events" {
			http.NotFound(w, r)
			return
		}
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error": "too many open event streams"}`)
			return
		}
		stream(w, r)
	})
	c := newClient(t, url, &Session{Hub: url, CookieName: sessionCookie, Cookie: "c"}, nil)
	var waits sleeps
	c.sleep = waits.sleep
	ch, _ := collect(t, c)
	e := waitFor(t, ch, EventDown, EventConnected)
	var re *RetryError
	if !errors.As(e.Err, &re) || re.Status != 503 || re.After != 7 || re.Msg != "too many open event streams" {
		t.Errorf("EventDown error %#v", e.Err)
	}
	waitFor(t, ch, EventConnected)
	waitFor(t, ch, EventChange)
	if w := waits.all(); len(w) != 1 || w[0] != 7*time.Second {
		t.Errorf("waits %v, want [7s]", w)
	}
}

func TestEventsBackoff(t *testing.T) {
	url := fakeHub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": "busy"}`, http.StatusInternalServerError)
	})
	c := newClient(t, url, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waits []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		if len(waits) == 8 {
			cancel()
		}
		return ctx.Err()
	}
	var downs int
	err := c.Events(ctx, func(e Event) {
		if e.Kind == EventDown {
			downs++
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Events = %v", err)
	}
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30, 30}
	for i := range want {
		want[i] *= time.Second
	}
	if fmt.Sprint(waits) != fmt.Sprint(want) || downs != 8 {
		t.Errorf("waits %v (downs %d), want %v", waits, downs, want)
	}
}

func TestCookieRenewal(t *testing.T) {
	var seen atomic.Value
	url := fakeHub(t, func(w http.ResponseWriter, r *http.Request) {
		if ck, err := r.Cookie(sessionCookie); err == nil {
			seen.Store(ck.Value)
		}
		if r.URL.Path == "/api/state" {
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "renewed", Path: "/", MaxAge: 3600, HttpOnly: true})
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items": [], "login": true, "authenticated": true}`)
	})
	var got sessions
	c := newClient(t, url, &Session{Hub: url, CookieName: sessionCookie, Cookie: "first"}, got.add)
	if _, err := c.State(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.Load() != "first" {
		t.Errorf("first request carried %v", seen.Load())
	}
	want := Session{Hub: url, CookieName: sessionCookie, Cookie: "renewed"}
	if l := got.all(); len(l) != 1 || l[0] != want {
		t.Errorf("onSession got %+v, want %+v", l, want)
	}
	if _, _, err := c.CheckSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.Load() != "renewed" {
		t.Errorf("after renewal the request carried %v", seen.Load())
	}
}

type fakeResolver struct{ addrs []netip.Addr }

func (f fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return f.addrs, nil
}

func TestPublicHubRefused(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().String()
	port = port[strings.LastIndex(port, ":"):]
	hub := "https://hub.example.com" + port

	loop := netip.AddrFrom4([4]byte{127, 0, 0, 1})
	public := netip.AddrFrom4([4]byte{203, 0, 113, 7}) // documentation range, outside the private set
	for name, addrs := range map[string][]netip.Addr{
		"public":                  {public},
		"one public among others": {loop, public},
	} {
		c := newClient(t, hub, nil, nil)
		c.dialer.Resolver = fakeResolver{addrs}
		_, err := c.Login(context.Background(), hubtest.Password)
		var re *netguard.RefusedError
		if !errors.As(err, &re) || re.IP != public || re.Host != "hub.example.com" {
			t.Errorf("%s: Login = %v, want a RefusedError for %s", name, err, public)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("the hub got %d requests", hits.Load())
	}
}

func TestNormalizeHub(t *testing.T) {
	for in, want := range map[string]string{
		"hub.example.com":                 "https://hub.example.com",
		" HTTPS://Hub.Example.com/ ":      "https://hub.example.com",
		"https://hub.example.com:443/app": "https://hub.example.com",
		"https://hub.example.com:8443":    "https://hub.example.com:8443",
		"http://localhost:8080/":          "http://localhost:8080",
		"http://127.0.0.1:80":             "http://127.0.0.1",
		"http://[::1]:8080":               "http://[::1]:8080",
		"https://[2001:db8::1]":           "https://[2001:db8::1]",
		"hub.example.com/?x=1#top":        "https://hub.example.com",
	} {
		if got, err := NormalizeHub(in); err != nil || got != want {
			t.Errorf("NormalizeHub(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "ftp://hub.example.com", "http://hub.example.com", "http://192.0.2.1",
		"https://alex@" + "hub.example.com", "https://", "https://hub.example.com:0", "https://hub.example.com:99999",
	} {
		if got, err := NormalizeHub(in); err == nil {
			t.Errorf("NormalizeHub(%q) = %q, want an error", in, got)
		}
	}
}

func TestSessionFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "meat-mode")
	if _, ok, err := LoadSession(dir, "https://hub.example.com"); ok || err != nil {
		t.Fatalf("LoadSession with no file = %v, %v", ok, err)
	}
	a := Session{Hub: "https://hub.example.com", CookieName: secureSessionCookie, Cookie: "a"}
	b := Session{Hub: "http://localhost:8080", CookieName: sessionCookie, Cookie: "b"}
	for _, s := range []Session{a, b} {
		if err := SaveSession(dir, s); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, SessionFile)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"version": 1`) {
		t.Errorf("file %s lacks its version", data)
	}
	a2 := a
	a2.Cookie = "a2"
	if err := SaveSession(dir, a2); err != nil {
		t.Fatal(err)
	}
	for _, want := range []Session{a2, b} {
		if got, ok, err := LoadSession(dir, want.Hub); err != nil || !ok || got != want {
			t.Errorf("LoadSession(%s) = %+v, %v, %v; want %+v", want.Hub, got, ok, err, want)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir holds %d files, want only %s", len(entries), SessionFile)
	}

	if err := DeleteSession(dir, a.Hub); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := LoadSession(dir, a.Hub); ok {
		t.Error("a deleted session loads")
	}
	if _, ok, _ := LoadSession(dir, b.Hub); !ok {
		t.Error("deleting one hub's session dropped another's")
	}
	if err := DeleteSession(dir, a.Hub); err != nil {
		t.Errorf("deleting a missing entry: %v", err)
	}
	if err := DeleteSession(dir, b.Hub); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file outlived its last session: %v", err)
	}

	// A newer version is left alone; a broken file is replaced.
	os.WriteFile(path, []byte(`{"version": 2, "sessions": {}}`), 0o600)
	if err := SaveSession(dir, a); err == nil {
		t.Error("SaveSession overwrote a newer version's file")
	}
	os.WriteFile(path, []byte(`{"version": 1, "sess`), 0o600)
	if _, _, err := LoadSession(dir, a.Hub); err == nil {
		t.Error("LoadSession read a broken file")
	}
	if err := SaveSession(dir, a); err != nil {
		t.Errorf("SaveSession over a broken file: %v", err)
	}
}
