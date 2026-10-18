package hub

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// Private addresses are built from bytes so none appears as a literal.
var (
	lan     = netip.AddrFrom4([4]byte{10, 0, 0, 1}).String()
	lan2    = netip.AddrFrom4([4]byte{192, 168, 1, 20}).String()
	tailnet = netip.AddrFrom4([4]byte{100, 64, 0, 5}).String()
	ula     = netip.AddrFrom16([16]byte{0xfd, 0x7a, 0x11, 0x5c, 15: 1}).String()
	linkV6  = netip.AddrFrom16([16]byte{0xfe, 0x80, 15: 1}).WithZone("eth0").String()
)

// netReq sends a GET to path from peer with the header lines given, each
// as name and value, under the Host localhost.
func netReq(s *Server, path, peer string, lines ...[2]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.Host = "localhost:8080"
	req.RemoteAddr = peer
	for _, l := range lines {
		req.Header.Add(l[0], l[1])
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func hdr(name, value string) [2]string { return [2]string{name, value} }

func TestNetGuardHeaders(t *testing.T) {
	s := guardServer(t, Config{PublicURL: "https://meat-mode.example.com"})
	const peer = "127.0.0.1:41000"
	pass := map[string][][2]string{
		"no headers":            nil,
		"loopback":              {hdr("X-Forwarded-For", "127.0.0.1")},
		"comma list":            {hdr("X-Forwarded-For", lan+", "+tailnet+",127.0.0.1")},
		"trailing comma":        {hdr("X-Forwarded-For", lan+",")},
		"two lines":             {hdr("X-Forwarded-For", lan), hdr("X-Forwarded-For", lan2)},
		"IPv4-mapped private":   {hdr("X-Forwarded-For", "::ffff:"+lan)},
		"zoned link-local":      {hdr("X-Real-IP", linkV6)},
		"unique local":          {hdr("CF-Connecting-IP", ula)},
		"bracketed with port":   {hdr("True-Client-IP", "[::1]:8080")},
		"address with port":     {hdr("X-Forwarded-For", lan+":443")},
		"Forwarded quoted":      {hdr("Forwarded", `for="[::1]:4711";proto=https;by=203.0.113.9`)},
		"Forwarded list":        {hdr("Forwarded", "for="+lan+", For="+tailnet)},
		"Forwarded zone":        {hdr("Forwarded", `for="[`+strings.Replace(linkV6, "%", "%25", 1)+`]"`)},
		"Forwarded without for": {hdr("Forwarded", "proto=https;host=meat-mode.example.com")},
	}
	for name, lines := range pass {
		if w := netReq(s, "/healthz", peer, lines...); w.Code != http.StatusOK {
			t.Errorf("%s: %d %s, want 200", name, w.Code, w.Body)
		}
	}
	refuse := map[string][][2]string{
		"Funnel":                  {hdr("Tailscale-Funnel-Request", "?1")},
		"Funnel empty":            {hdr("Tailscale-Funnel-Request", "")},
		"public":                  {hdr("X-Forwarded-For", "203.0.113.5")},
		"public after private":    {hdr("X-Forwarded-For", "127.0.0.1, 203.0.113.5")},
		"public on a second line": {hdr("X-Forwarded-For", lan), hdr("X-Forwarded-For", "203.0.113.5")},
		"IPv4-mapped public":      {hdr("X-Forwarded-For", "::ffff:203.0.113.5")},
		"global IPv6":             {hdr("X-Forwarded-For", "2001:db8::1")},
		"bracketed public":        {hdr("X-Forwarded-For", "[2001:db8::1]:443")},
		"zoned public":            {hdr("X-Forwarded-For", "2001:db8::1%eth0")},
		"unknown":                 {hdr("X-Forwarded-For", "unknown")},
		"host name":               {hdr("X-Forwarded-For", "client.example.com")},
		"X-Real-IP":               {hdr("X-Real-IP", "198.51.100.7")},
		"CF-Connecting-IP":        {hdr("CF-Connecting-IP", "198.51.100.7")},
		"True-Client-IP":          {hdr("True-Client-IP", "2001:db8::7")},
		"Forwarded":               {hdr("Forwarded", "for=203.0.113.5")},
		"Forwarded quoted v6":     {hdr("Forwarded", `For="[2001:db8::1]:4711"`)},
		"Forwarded unknown":       {hdr("Forwarded", "for=unknown")},
		"Forwarded obfuscated":    {hdr("Forwarded", "for=_hidden")},
		"Forwarded second pair":   {hdr("Forwarded", "by=127.0.0.1;for=198.51.100.1")},
		"Forwarded second elem":   {hdr("Forwarded", "for=127.0.0.1, for=198.51.100.1")},
		"Forwarded second line":   {hdr("Forwarded", "for=127.0.0.1"), hdr("Forwarded", "for=198.51.100.1")},
	}
	for name, lines := range refuse {
		w := netReq(s, "/healthz", peer, lines...)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "private network") {
			t.Errorf("%s: %d %s, want 403", name, w.Code, w.Body)
		}
	}
}

func TestNetGuardPeer(t *testing.T) {
	s := guardServer(t, Config{PublicURL: "https://meat-mode.example.com"})
	for peer, ok := range map[string]bool{
		"127.0.0.1:41000":           true,
		"[::1]:41000":               true,
		lan + ":41000":              true,
		"[::ffff:" + lan + "]:4100": true,
		"[" + linkV6 + "]:41000":    true,
		"203.0.113.5:41000":         false,
		"[2001:db8::1]:41000":       false,
		"[::ffff:203.0.113.5]:4100": false,
		"":                          false,
		"@":                         false,
	} {
		w := netReq(s, "/healthz", peer)
		if got := w.Code == http.StatusOK; got != ok {
			t.Errorf("peer %q: %d %s", peer, w.Code, w.Body)
		}
	}
}

func TestNetGuardTrustedNetworks(t *testing.T) {
	s := guardServer(t, Config{PublicURL: "https://meat-mode.example.com", TrustedNetworks: "2001:db8:1::/48, 198.51.100.0/24"})
	for _, c := range []struct {
		peer, xff string
		ok        bool
	}{
		{"[2001:db8:1::20]:41000", "", true},
		{"198.51.100.9:41000", "2001:db8:1:2::5", true},
		{"127.0.0.1:41000", "198.51.100.200, " + lan, true},
		{"[2001:db8:2::20]:41000", "", false},
		{"127.0.0.1:41000", "198.51.101.1", false},
	} {
		var lines [][2]string
		if c.xff != "" {
			lines = append(lines, hdr("X-Forwarded-For", c.xff))
		}
		w := netReq(s, "/healthz", c.peer, lines...)
		if got := w.Code == http.StatusOK; got != c.ok {
			t.Errorf("peer %s, X-Forwarded-For %q: %d %s", c.peer, c.xff, w.Code, w.Body)
		}
	}
}

// The guard covers every route and runs before the Host, Origin and
// Content-Type checks, so a refused request learns nothing else.
func TestNetGuardEveryRouteFirst(t *testing.T) {
	s := guardServer(t, Config{PublicURL: "https://meat-mode.example.com"})
	public := hdr("X-Forwarded-For", "203.0.113.5")
	for _, path := range []string{"/", "/settings", "/healthz", "/bridge/v1/actions", "/calendar/feed.ics", "/api/state", "/api/session"} {
		w := netReq(s, path, "127.0.0.1:41000", public)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: %d %s, want 403", path, w.Code, w.Body)
		}
		if isAPIPath(path) {
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || !strings.Contains(body["error"], "203.0.113.5") {
				t.Errorf("%s: body %s, want a JSON error naming the address", path, w.Body)
			}
		} else if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") || !strings.Contains(w.Body.String(), "X-Forwarded-For") {
			t.Errorf("%s: %s %s, want plain text naming the header", path, ct, w.Body)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: refusal without the security headers", path)
		}
	}

	// A name the hub does not know, a foreign Origin and a form body would
	// each be refused later; the network guard answers first.
	req := httptest.NewRequest("POST", "/api/bridges", strings.NewReader("name=x"))
	req.Host = "attacker.example.net"
	req.RemoteAddr = "203.0.113.5:41000"
	req.Header.Set("Origin", "https://attacker.example.net")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "private network") {
		t.Errorf("POST from a public peer: %d %s", w.Code, w.Body)
	}
}

func TestNetGuardLogsOncePerKindPerMinute(t *testing.T) {
	s := guardServer(t, Config{PublicURL: "https://meat-mode.example.com"})
	var buf bytes.Buffer
	now := time.Date(2030, 3, 2, 9, 0, 0, 0, time.UTC)
	s.guard.net.log = slog.New(slog.NewTextHandler(&buf, nil))
	s.guard.net.now = func() time.Time { return now }
	lines := func() int { return strings.Count(buf.String(), "\n") }

	funnel := hdr("Tailscale-Funnel-Request", "?1")
	for range 3 {
		netReq(s, "/", "127.0.0.1:41000", funnel)
	}
	if n := lines(); n != 1 {
		t.Fatalf("%d lines after three Funnel requests, want 1:\n%s", n, &buf)
	}
	netReq(s, "/", "203.0.113.5:41000")
	netReq(s, "/", "127.0.0.1:41000", hdr("X-Real-IP", "198.51.100.7"))
	if n := lines(); n != 3 {
		t.Fatalf("%d lines after two other kinds, want 3:\n%s", n, &buf)
	}
	now = now.Add(59 * time.Second)
	netReq(s, "/", "127.0.0.1:41000", funnel)
	if n := lines(); n != 3 {
		t.Fatalf("logged again within the minute:\n%s", &buf)
	}
	now = now.Add(2 * time.Second)
	netReq(s, "/", "127.0.0.1:41000", funnel)
	if n := lines(); n != 4 {
		t.Fatalf("%d lines a minute later, want 4:\n%s", n, &buf)
	}
}

func TestConfigTrustedNetworks(t *testing.T) {
	cfg := Config{PublicURL: "https://meat-mode.example.com", TrustedNetworks: "2001:db8:1::/48, 198.51.100.0/24"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, bad := range []string{"0.0.0.0/0", "198.51.0.0/15", "2001:db8::/32", "not-a-network", "198.51.100.0/33"} {
		cfg.TrustedNetworks = bad
		_, want := netguard.ParseTrusted(bad)
		err := cfg.Validate()
		if err == nil || want == nil || err.Error() != want.Error() {
			t.Errorf("%q: Validate %v, want %v", bad, err, want)
		}
	}
	t.Setenv(netguard.EnvTrusted, "198.51.100.0/24")
	if got := ConfigFromEnv().TrustedNetworks; got != "198.51.100.0/24" {
		t.Errorf("ConfigFromEnv TrustedNetworks %q", got)
	}
}
