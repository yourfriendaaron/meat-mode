package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yourfriendaaron/meat-mode/internal/store"
)

func guardServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := newServer(cfg, st, nil, fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}})
	t.Cleanup(s.Close)
	return s
}

type guardReq struct {
	method, path, host, ctype, origin, proto, site string
}

func (g guardReq) send(s *Server) *httptest.ResponseRecorder {
	body := ""
	if g.method != "GET" && g.method != "HEAD" {
		body = `{"name":"laptop"}`
	}
	req := httptest.NewRequest(g.method, g.path, strings.NewReader(body))
	req.Host = g.host
	if g.ctype != "" {
		req.Header.Set("Content-Type", g.ctype)
	}
	if g.origin != "" {
		req.Header.Set("Origin", g.origin)
	}
	if g.proto != "" {
		req.Header.Set("X-Forwarded-Proto", g.proto)
	}
	if g.site != "" {
		req.Header.Set("Sec-Fetch-Site", g.site)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func TestGuardHost(t *testing.T) {
	s := guardServer(t, Config{PublicURL: "https://meat-mode.example.com", AllowedHosts: "todo.example.com, Other.Example.com"})
	for host, ok := range map[string]bool{
		"localhost": true, "localhost:8080": true, "127.0.0.1:8080": true, "[::1]:8080": true, "[::1]": true,
		"meat-mode.example.com": true, "MEAT-MODE.example.com:443": true, "todo.example.com": true, "other.example.com": true,
		"attacker.example.net": false, "192.0.2.10:8080": false, "example.com": false, "": false,
	} {
		for _, path := range []string{"/api/state", "/", "/settings"} {
			w := guardReq{method: "GET", path: path, host: host}.send(s)
			if ok && w.Code == http.StatusMisdirectedRequest {
				t.Errorf("GET %s with Host %q refused: %s", path, host, w.Body)
			}
			if !ok {
				if w.Code != http.StatusMisdirectedRequest {
					t.Errorf("GET %s with Host %q = %d, want 421", path, host, w.Code)
				} else if !strings.Contains(w.Body.String(), "MEAT_MODE_ALLOWED_HOSTS") {
					t.Errorf("421 body does not name the setting: %s", w.Body)
				}
			}
		}
	}
	// The bridge API and the health check answer to any name.
	if w := (guardReq{method: "GET", path: "/healthz", host: "hub.internal"}).send(s); w.Code != http.StatusOK {
		t.Errorf("healthz = %d", w.Code)
	}
	if w := (guardReq{method: "GET", path: "/bridge/v1/actions", host: "hub.internal"}).send(s); w.Code != http.StatusUnauthorized {
		t.Errorf("bridge route = %d, want 401 from its own auth", w.Code)
	}
}

func TestGuardContentType(t *testing.T) {
	s := guardServer(t, Config{})
	for ctype, want := range map[string]int{
		"":                                    http.StatusUnsupportedMediaType,
		"text/plain":                          http.StatusUnsupportedMediaType,
		"application/x-www-form-urlencoded":   http.StatusUnsupportedMediaType,
		"multipart/form-data; boundary=x":     http.StatusUnsupportedMediaType,
		"application/json; boundary=x":        http.StatusUnsupportedMediaType,
		"application/json":                    http.StatusCreated,
		"application/json; charset=utf-8":     http.StatusConflict, // second add of the same name
		"Application/JSON; Charset=\"UTF-8\"": http.StatusConflict,
	} {
		w := guardReq{method: "POST", path: "/api/bridges", host: "localhost", ctype: ctype}.send(s)
		if want == http.StatusUnsupportedMediaType && w.Code != want {
			t.Errorf("Content-Type %q = %d, want 415", ctype, w.Code)
		}
		if want != http.StatusUnsupportedMediaType && w.Code == http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q refused", ctype)
		}
	}
	// Bodiless POST and DELETE need it too.
	for _, p := range []string{"/api/actions/x/cancel", "/api/items/a%2Fb%2Fc/seen"} {
		if w := (guardReq{method: "POST", path: p, host: "localhost"}).send(s); w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("POST %s without a type = %d", p, w.Code)
		}
	}
	if w := (guardReq{method: "DELETE", path: "/api/bridges/laptop", host: "localhost"}).send(s); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("DELETE without a type = %d", w.Code)
	}
	if w := (guardReq{method: "DELETE", path: "/api/bridges/laptop", host: "localhost", ctype: "application/json"}).send(s); w.Code != http.StatusNoContent {
		t.Errorf("DELETE with the type = %d", w.Code)
	}
}

func TestGuardOrigin(t *testing.T) {
	s := guardServer(t, Config{PublicURL: "https://meat-mode.example.com/"})
	cases := []struct {
		host, origin, proto string
		ok                  bool
	}{
		{"localhost:8080", "http://localhost:8080", "", true},
		{"localhost:8080", "", "", true},
		{"meat-mode.example.com", "https://meat-mode.example.com", "https", true},
		{"meat-mode.example.com", "https://meat-mode.example.com", "", true}, // the public URL
		{"localhost:8080", "https://meat-mode.example.com", "", true},
		{"localhost:8080", "http://localhost:9999", "", false},
		{"localhost:8080", "https://localhost:8080", "", false},
		{"localhost:8080", "http://attacker.example.net", "", false},
		{"localhost:8080", "null", "", false},
		{"meat-mode.example.com", "http://meat-mode.example.com", "https", false},
	}
	for i, c := range cases {
		w := guardReq{method: "POST", path: "/api/bridges", host: c.host, ctype: "application/json", origin: c.origin, proto: c.proto}.send(s)
		if c.ok && w.Code == http.StatusForbidden {
			t.Errorf("case %d: Origin %q on Host %q refused", i, c.origin, c.host)
		}
		if !c.ok && w.Code != http.StatusForbidden {
			t.Errorf("case %d: Origin %q on Host %q = %d, want 403", i, c.origin, c.host, w.Code)
		}
	}
	// An API GET with a foreign Origin is refused too: it could hold an
	// event stream slot.
	if w := (guardReq{method: "GET", path: "/api/events", host: "localhost", origin: "http://attacker.example.net"}).send(s); w.Code != http.StatusForbidden {
		t.Errorf("GET /api/events with a foreign Origin = %d, want 403", w.Code)
	}
	if w := (guardReq{method: "GET", path: "/api/state", host: "localhost", origin: "http://localhost"}).send(s); w.Code != http.StatusOK {
		t.Errorf("GET with its own Origin = %d", w.Code)
	}
}

func TestGuardSecFetchSite(t *testing.T) {
	s := guardServer(t, Config{})
	for site, ok := range map[string]bool{"": true, "same-origin": true, "none": true, "same-site": false, "cross-site": false} {
		// A no-cors fetch sends no Origin, only Sec-Fetch-Site.
		w := guardReq{method: "GET", path: "/api/state", host: "localhost", site: site}.send(s)
		if ok != (w.Code == http.StatusOK) {
			t.Errorf("GET /api/state with Sec-Fetch-Site %q = %d", site, w.Code)
		}
	}
	// A page may be opened from a link on another site.
	if w := (guardReq{method: "GET", path: "/", host: "localhost", site: "cross-site"}).send(s); w.Code != http.StatusOK {
		t.Errorf("GET / from another site = %d", w.Code)
	}
}

func TestGuardBareBridgePathIsAPage(t *testing.T) {
	s := guardServer(t, Config{})
	if w := (guardReq{method: "GET", path: "/bridge", host: "attacker.example.net"}).send(s); w.Code != http.StatusMisdirectedRequest {
		t.Errorf("GET /bridge under a foreign Host = %d, want 421", w.Code)
	}
}

func TestGuardBareAPIPathIsAPI(t *testing.T) {
	s := guardServer(t, Config{})
	w := guardReq{method: "GET", path: "/api", host: "attacker.example.net"}.send(s)
	if w.Code != http.StatusMisdirectedRequest || !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Errorf("GET /api under a foreign Host = %d %q, want a JSON 421", w.Code, w.Header().Get("Content-Type"))
	}
	if w := (guardReq{method: "GET", path: "/api", host: "localhost", site: "cross-site"}).send(s); w.Code != http.StatusForbidden {
		t.Errorf("GET /api from another site = %d, want 403", w.Code)
	}
}
