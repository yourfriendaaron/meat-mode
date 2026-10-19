package hub

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// cheapHash is a PHC hash of password with tiny parameters, so tests stay
// fast; the hub accepts any parameters within its bounds.
func cheapHash(password string) string {
	salt := []byte("saltsaltsaltsalt")
	key := argon2.IDKey([]byte(password), salt, 1, 64, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=64,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

type loginHarness struct {
	t    *testing.T
	st   *store.Store
	srv  *Server
	push *fakePush
	now  time.Time
}

func newLoginHarness(t *testing.T, cfg Config) *loginHarness {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &loginHarness{t: t, st: st, now: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)}
	h.srv = h.server(cfg)
	return h
}

// server builds a server on the harness's store and clock, as a restarted
// hub would.
func (h *loginHarness) server(cfg Config) *Server {
	if cfg.User == "" {
		cfg.User = "alex"
	}
	h.push = &fakePush{subs: map[string]api.PushSubscription{}}
	s := newServer(cfg, h.st, h.push, fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}})
	s.now = func() time.Time { return h.now }
	h.t.Cleanup(s.Close)
	return s
}

func (h *loginHarness) do(method, path, body, cookie string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost:8080"
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: h.srv.cookieName(), Value: cookie})
	}
	w := httptest.NewRecorder()
	h.srv.ServeHTTP(w, req)
	return w
}

// login logs in and returns the session cookie.
func (h *loginHarness) login(password string) *http.Cookie {
	h.t.Helper()
	w := h.do("POST", "/api/login", `{"password":"`+password+`"}`, "")
	if w.Code != http.StatusNoContent {
		h.t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	return responseCookie(h.t, w)
}

// responseCookie returns the session cookie w sets, under either name.
func responseCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie || c.Name == secureSessionCookie {
			return c
		}
	}
	t.Fatalf("no session cookie in %v", w.Header()["Set-Cookie"])
	return nil
}

func sessionReply(t *testing.T, w *httptest.ResponseRecorder) (login, authed bool) {
	t.Helper()
	var v struct{ Login, Authenticated bool }
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("session reply %q: %v", w.Body, err)
	}
	return v.Login, v.Authenticated
}

// appRoutes is one request for every gated /api/ route.
var appRoutes = []struct{ method, path, body string }{
	{"GET", "/api/state", ""},
	{"GET", "/api/events", ""},
	{"POST", "/api/actions", `{}`},
	{"GET", "/api/actions", ""},
	{"POST", "/api/actions/" + aid1 + "/cancel", ""},
	{"POST", "/api/items/laptop%2Fgarden%2Fgd-1/seen", ""},
	{"GET", "/api/push/key", ""},
	{"POST", "/api/push/subscriptions", `{}`},
	{"DELETE", "/api/push/subscriptions", `{}`},
	{"GET", "/api/bridges", ""},
	{"POST", "/api/bridges", `{"name":"laptop"}`},
	{"DELETE", "/api/bridges/laptop", ""},
	{"POST", "/api/reminders/test", ""},
	{"GET", "/api/setup", ""},
	{"POST", "/api/setup", `{"status":"done"}`},
	{"GET", "/api/settings", ""},
	{"PUT", "/api/settings", `{"name":"Sam"}`},
	{"PUT", "/api/password", `{"current":"x","password":"feed the cat"}`},
	{"GET", "/api/setup/status", ""},
	{"GET", "/api/calendar", ""},
	{"POST", "/api/calendar", `{}`},
	{"DELETE", "/api/calendar", ""},
	{"POST", "/api/setup/test-event", ""},
	{"GET", "/api/devices", ""},
	{"DELETE", "/api/devices/0123456789abcdef0123456789abcdef", ""},
	{"PUT", "/api/devices/self", `{}`},
	{"DELETE", "/api/devices/self", ""},
	{"GET", "/api/export", ""},
	{"POST", "/api/restore", `{}`},
	{"GET", "/api/no-such-route", ""},
}

func TestLoginGatesEveryAppRoute(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	for _, rt := range appRoutes {
		w := h.do(rt.method, rt.path, rt.body, "")
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "log in first") {
			t.Errorf("%s %s without a session: %d %s, want 401", rt.method, rt.path, w.Code, w.Body)
		}
		w = h.do(rt.method, rt.path, rt.body, "not-a-session")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a made-up session: %d, want 401", rt.method, rt.path, w.Code)
		}
	}

	// Pages, the bridge API and /healthz answer as before.
	for _, rt := range []struct {
		path string
		want int
	}{
		{"/", http.StatusOK},
		{"/healthz", http.StatusOK},
		{"/bridge/v1/actions", http.StatusUnauthorized}, // the bridge's own bearer check
	} {
		if w := h.do("GET", rt.path, "", ""); w.Code != rt.want {
			t.Errorf("GET %s: %d, want %d", rt.path, w.Code, rt.want)
		}
	}
	if w := h.do("POST", "/bridge/v1/hello", `{}`, ""); strings.Contains(w.Body.String(), "log in first") {
		t.Errorf("bridge route answered with the app login: %s", w.Body)
	}

	if l, a := sessionReply(t, h.do("GET", "/api/session", "", "")); !l || a {
		t.Errorf("session before login = login %v, authenticated %v; want true, false", l, a)
	}

	c := h.login("water the plants")
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusOK {
		t.Errorf("state with a session: %d %s", w.Code, w.Body)
	}
	if l, a := sessionReply(t, h.do("GET", "/api/session", "", c.Value)); !l || !a {
		t.Errorf("session after login = login %v, authenticated %v; want true, true", l, a)
	}
}

func TestNoPasswordServesOnlySetup(t *testing.T) {
	h := newLoginHarness(t, Config{})
	for _, rt := range appRoutes {
		w := h.do(rt.method, rt.path, rt.body, "")
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), noPassword) {
			t.Errorf("%s %s with no password: %d %s, want 401", rt.method, rt.path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/setup/tailscale-only", "/api/devices/0123456789abcdef0123456789abcdef/confirm"} {
		if w := h.do("PUT", path, `{"on":true}`, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("PUT %s with no password: %d, want 401", path, w.Code)
		}
	}
	// A device token opens nothing either.
	if w := deviceDo(t, h.srv, "POST", "/api/actions", `{}`, "mmd_token", ""); w.Code != http.StatusUnauthorized ||
		!strings.Contains(w.Body.String(), noPassword) {
		t.Errorf("device request with no password: %d %s, want 401", w.Code, w.Body)
	}
	if l, a := sessionReply(t, h.do("GET", "/api/session", "", "")); l || a {
		t.Errorf("session with no password = login %v, authenticated %v; want false, false", l, a)
	}
	if w := h.do("POST", "/api/login", `{"password":"x"}`, ""); w.Code != http.StatusNotFound {
		t.Errorf("login with no password: %d, want 404", w.Code)
	}
	if w := h.do("POST", "/api/logout", "", ""); w.Code != http.StatusNoContent {
		t.Errorf("logout with no password: %d, want 204", w.Code)
	}
	// Claim needs no session; a wrong code is 403, not the gate's 401.
	if w := h.do("POST", "/api/setup/claim", `{"code":"AAAA-BBBB-CCCC","password":"water the plants"}`, ""); w.Code != http.StatusForbidden {
		t.Errorf("claim with no password: %d %s, want 403", w.Code, w.Body)
	}
	// Pages, the bridge API and /healthz answer as before.
	if w := h.do("GET", "/", "", ""); w.Code != http.StatusOK {
		t.Errorf("page with no password: %d", w.Code)
	}
	if w := h.do("GET", "/healthz", "", ""); w.Code != http.StatusOK {
		t.Errorf("healthz with no password: %d", w.Code)
	}
	if w := h.do("GET", "/bridge/v1/actions", "", ""); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), noPassword) {
		t.Errorf("bridge route with no password: %d %s, want its own bearer check", w.Code, w.Body)
	}
}

func TestTailscaleOnlyIsGone(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	if w := h.do("PUT", "/api/setup/tailscale-only", `{"on":true}`, c.Value); w.Code == http.StatusNoContent {
		t.Errorf("tailscale-only: %d, want no such route", w.Code)
	}
	if strings.Contains(h.do("GET", "/api/setup", "", c.Value).Body.String(), "tailscaleOnly") {
		t.Error("GET /api/setup still reports tailscaleOnly")
	}
}

func TestLoginCookie(t *testing.T) {
	for _, tc := range []struct {
		publicURL string
		name      string
		secure    bool
	}{
		{"https://meat-mode.example.com", secureSessionCookie, true},
		{"http://localhost:8080", sessionCookie, false},
	} {
		h := newLoginHarness(t, Config{PublicURL: tc.publicURL, PasswordHash: cheapHash("water the plants")})
		w := h.do("POST", "/api/login", `{"password":"water the plants"}`, "")
		cs := w.Result().Cookies()
		if len(cs) != 1 {
			t.Fatalf("%s: cookies %v", tc.publicURL, cs)
		}
		c := cs[0]
		if c.Name != tc.name || c.Value == "" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" ||
			c.Domain != "" || c.Secure != tc.secure || c.MaxAge != int(store.SessionIdle/time.Second) {
			t.Errorf("%s: cookie %+v", tc.publicURL, c)
		}

		// The hub reads only the name for its scheme.
		other := sessionCookie
		if tc.name == sessionCookie {
			other = secureSessionCookie
		}
		for name, want := range map[string]int{tc.name: http.StatusOK, other: http.StatusUnauthorized} {
			req := httptest.NewRequest("GET", "/api/state", nil)
			req.Host = "localhost:8080"
			req.AddCookie(&http.Cookie{Name: name, Value: c.Value})
			rw := httptest.NewRecorder()
			h.srv.ServeHTTP(rw, req)
			if rw.Code != want {
				t.Errorf("%s: state with cookie %s: %d, want %d", tc.publicURL, name, rw.Code, want)
			}
		}
		// Logging out clears the cookie under the same name.
		req := httptest.NewRequest("POST", "/api/logout", nil)
		req.Host = "localhost:8080"
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: tc.name, Value: c.Value})
		rw := httptest.NewRecorder()
		h.srv.ServeHTTP(rw, req)
		if cs := rw.Result().Cookies(); len(cs) != 1 || cs[0].Name != tc.name || cs[0].MaxAge >= 0 || cs[0].Secure != tc.secure {
			t.Errorf("%s: logout cookies %v", tc.publicURL, cs)
		}
	}
}

func TestWrongPasswordAndLimit(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	if w := h.do("POST", "/api/login", `{"password":"Water the plants"}`, ""); w.Code != http.StatusUnauthorized ||
		!strings.Contains(w.Body.String(), "wrong password") || len(w.Result().Cookies()) != 0 {
		t.Fatalf("wrong password: %d %s %v", w.Code, w.Body, w.Result().Cookies())
	}
	if w := h.do("POST", "/api/login", `{"password":`, ""); w.Code != http.StatusBadRequest {
		t.Errorf("broken body: %d, want 400", w.Code)
	}
	if w := h.do("POST", "/api/login", `{"password":"water the plants"}`, ""); w.Code != http.StatusNoContent {
		t.Errorf("login: %d", w.Code)
	}
	for range loginFailures - 1 {
		h.now = h.now.Add(time.Second)
		h.do("POST", "/api/login", `{"password":"nope"}`, "")
	}
	// Five failures in the minute: even the right password waits.
	w := h.do("POST", "/api/login", `{"password":"water the plants"}`, "")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth try: %d, want 429", w.Code)
	}
	// The first failure was 4 seconds ago, so 56 seconds remain.
	if ra := w.Header().Get("Retry-After"); ra != "56" {
		t.Errorf("Retry-After = %q, want 56", ra)
	}
	h.now = h.now.Add(56 * time.Second)
	if w := h.do("POST", "/api/login", `{"password":"water the plants"}`, ""); w.Code != http.StatusNoContent {
		t.Errorf("after the wait: %d, want 204", w.Code)
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	other := h.login("water the plants")
	w := h.do("POST", "/api/logout", "", c.Value)
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	if cleared := responseCookie(t, w); cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Errorf("logout cookie %+v, want it cleared", cleared)
	}
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusUnauthorized {
		t.Errorf("state after logout: %d, want 401", w.Code)
	}
	if w := h.do("GET", "/api/state", "", other.Value); w.Code != http.StatusOK {
		t.Errorf("another device after logout: %d, want 200", w.Code)
	}
}

func TestLogoutClearsTheCookieWhenRemindersCannotBeDropped(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	h.push.dropErr = errors.New("disk full")
	w := h.do("POST", "/api/logout", "", c.Value)
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s, want 204", w.Code, w.Body)
	}
	if cleared := responseCookie(t, w); cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Errorf("logout cookie %+v, want it cleared", cleared)
	}
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusUnauthorized {
		t.Errorf("state after logout: %d, want 401", w.Code)
	}
}

func TestNewPasswordEndsSessions(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	h.srv = h.server(Config{PasswordHash: cheapHash("feed the cat")})
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusUnauthorized {
		t.Errorf("old session under a new password: %d, want 401", w.Code)
	}
	h.login("feed the cat")
}

func TestSessionIdleEndAndRenewal(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")

	// Within a day of the last recorded use, no new cookie.
	h.now = h.now.Add(23 * time.Hour)
	w := h.do("GET", "/api/state", "", c.Value)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 0 {
		t.Fatalf("same day: %d, cookies %v", w.Code, w.Result().Cookies())
	}

	// After a day, the use is recorded and the idle end moves to 14 days
	// from now.
	h.now = h.now.Add(2 * time.Hour)
	w = h.do("GET", "/api/state", "", c.Value)
	if w.Code != http.StatusOK {
		t.Fatalf("next day: %d", w.Code)
	}
	if r := responseCookie(t, w); r.Value != c.Value || r.MaxAge != int(store.SessionIdle/time.Second) {
		t.Errorf("renewed cookie %+v", r)
	}

	// A second before the idle end it holds; at it the session is over.
	h.now = h.now.Add(store.SessionIdle - time.Second)
	if w := h.do("GET", "/api/session", "", c.Value); !strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Fatalf("a second before the idle end: %s", w.Body)
	}
	// That check recorded a use, so a new session shows the end itself.
	c = h.login("water the plants")
	h.now = h.now.Add(store.SessionIdle)
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusUnauthorized {
		t.Errorf("after 14 idle days: %d, want 401", w.Code)
	}
	if _, a := sessionReply(t, h.do("GET", "/api/session", "", c.Value)); a {
		t.Error("an idle session reports authenticated")
	}
}

func TestSessionAbsoluteEnd(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	start := h.now
	c := h.login("water the plants")
	// Used every few days, it still ends 90 days after the login.
	for h.now.Before(start.Add(store.SessionLifetime - 3*24*time.Hour)) {
		h.now = h.now.Add(3 * 24 * time.Hour)
		if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusOK {
			t.Fatalf("day %v: %d", h.now.Sub(start).Hours()/24, w.Code)
		}
	}
	h.now = start.Add(store.SessionLifetime - time.Second)
	w := h.do("GET", "/api/state", "", c.Value)
	if w.Code != http.StatusOK {
		t.Fatalf("a second before the absolute end: %d", w.Code)
	}
	// The cookie sent last never outlives the absolute end.
	h.now = start.Add(store.SessionLifetime)
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusUnauthorized {
		t.Errorf("at the absolute end: %d, want 401", w.Code)
	}
}

func TestDeviceCallsKeepTheSessionAlive(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	h.push.giveToken("mmd_phone", "dev1")
	h.push.owner = store.SessionID(c.Value)

	// A phone that lives on notification buttons: a device-token call
	// every 10 days, and no cookie use for 40 days.
	for day := 10; day <= 40; day += 10 {
		h.now = h.now.Add(10 * 24 * time.Hour)
		w := deviceDo(t, h.srv, "POST", "/api/actions", `{"kind":"done","key":"laptop/garden/gd-1"}`, "mmd_phone", "")
		if w.Code == http.StatusUnauthorized {
			t.Fatalf("device call on day %d: %d %s", day, w.Code, w.Body)
		}
	}
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusOK {
		t.Errorf("session after 40 days of device calls: %d, want 200", w.Code)
	}

	// Once the session has ended, the device token opens nothing.
	h.now = h.now.Add(store.SessionIdle)
	if w := deviceDo(t, h.srv, "POST", "/api/actions", `{"kind":"done","key":"laptop/garden/gd-1"}`, "mmd_phone", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("device call after its session ended: %d, want 401", w.Code)
	}
}

func TestLoginKeepsTheGuard(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"password":"water the plants"}`))
	req.Host = "localhost:8080"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.net")
	w := httptest.NewRecorder()
	h.srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-origin login: %d, want 403", w.Code)
	}
	req = httptest.NewRequest("POST", "/api/login", strings.NewReader(`password=water`))
	req.Host = "localhost:8080"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("form login: %d, want 415", w.Code)
	}
}

func TestConfigPasswordHash(t *testing.T) {
	good := cheapHash("water the plants")
	base := Config{PublicURL: "https://meat-mode.example.com"}

	c := base
	c.PasswordHash = good
	if err := c.Validate(); err != nil {
		t.Errorf("valid hash: %v", err)
	}
	c.PasswordHash = "hunter2"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "hash-password") {
		t.Errorf("plain password: %v, want an error naming hash-password", err)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "hash")
	if err := os.WriteFile(file, []byte(good+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c = base
	c.PasswordHashFile = file
	if err := c.LoadPasswordHash(); err != nil || c.PasswordHash != good || c.Validate() != nil {
		t.Errorf("hash file: %v, hash %q", err, c.PasswordHash)
	}

	c = base
	c.PasswordHash, c.PasswordHashFile = good, file
	if err := c.LoadPasswordHash(); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("both set: %v", err)
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted both")
	}

	empty := filepath.Join(dir, "empty")
	os.WriteFile(empty, []byte("\n"), 0o600)
	c = base
	c.PasswordHashFile = empty
	if err := c.LoadPasswordHash(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty file: %v", err)
	}
	c = base
	c.PasswordHashFile = filepath.Join(dir, "missing")
	if err := c.LoadPasswordHash(); err == nil {
		t.Error("missing file accepted")
	}

	t.Setenv("MEAT_MODE_PASSWORD_HASH", "  "+good+"\n")
	t.Setenv("MEAT_MODE_PASSWORD_HASH_FILE", "")
	if got := ConfigFromEnv().PasswordHash; got != good {
		t.Errorf("ConfigFromEnv hash = %q, want it trimmed", got)
	}
}

func TestPushSubscriptionsBelongToTheSession(t *testing.T) {
	const sub = `{"endpoint":"https://updates.push.services.mozilla.com/wpush/v2/a","keys":{"p256dh":"p","auth":"a"}}`
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	other := h.login("water the plants")
	if w := h.do("POST", "/api/push/subscriptions", sub, c.Value); w.Code != http.StatusNoContent {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body)
	}
	if got := h.push.sessions["https://updates.push.services.mozilla.com/wpush/v2/a"]; got != store.SessionID(c.Value) {
		t.Errorf("subscription session = %q, want %q", got, store.SessionID(c.Value))
	}
	// Logging out another device leaves it alone; logging out this one drops it.
	h.do("POST", "/api/logout", "", other.Value)
	if len(h.push.subs) != 1 {
		t.Fatalf("another device's logout dropped the subscription")
	}
	h.do("POST", "/api/logout", "", c.Value)
	if len(h.push.subs) != 0 {
		t.Errorf("subscription kept after its session logged out: %v", h.push.subs)
	}

}
