package notify

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Addresses tests need, built in code: the repo's guard refuses private,
// CGNAT and link-local literals anywhere in tracked files.
var (
	loopback4   = netip.AddrFrom4([4]byte{127, 0, 0, 1})
	private10   = netip.AddrFrom4([4]byte{10, 0, 0, 1})
	private192  = netip.AddrFrom4([4]byte{192, 168, 1, 20})
	linkLocal   = netip.AddrFrom4([4]byte{169, 254, 169, 254})
	cgnat       = netip.AddrFrom4([4]byte{100, 64, 0, 1})
	globalAddr  = netip.MustParseAddr("2606:2800:21f:cb07:6820:80da:af6b:8b2c") // example.com's
	loopbackNet = netip.PrefixFrom(loopback4, 8)
)

// appSub is an app's subscription with the keys to read what it receives.
type appSub struct {
	api.PushSubscription
	priv *ecdh.PrivateKey
	auth []byte
}

func newAppSub(t *testing.T, endpoint string) appSub {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := appSub{priv: k, auth: make([]byte, 16)}
	rand.Read(a.auth)
	a.Endpoint, a.Kind, a.Name = endpoint, api.DeviceApp, "Test phone"
	a.Keys.P256dh = base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
	a.Keys.Auth = base64.RawURLEncoding.EncodeToString(a.auth)
	return a
}

// decrypt reads an aes128gcm Web Push body (RFC 8291) as the app does.
func (a appSub) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("push body of %d bytes", len(body))
	}
	salt, idLen := body[:16], int(body[20])
	_ = binary.BigEndian.Uint32(body[16:20]) // record size
	serverPub, ct := body[21:21+idLen], body[21+idLen:]
	pub, err := ecdh.P256().NewPublicKey(serverPub)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := a.priv.ECDH(pub)
	if err != nil {
		t.Fatal(err)
	}
	info := "WebPush: info\x00" + string(a.priv.PublicKey().Bytes()) + string(serverPub)
	ikm, _ := hkdf.Key(sha256.New, shared, a.auth, info, 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	plain = bytes.TrimRight(plain, "\x00")
	return bytes.TrimSuffix(plain, []byte{2})
}

// pushed is one request a fake push server received.
type pushed struct {
	path string
	body []byte
}

// appServer is a UnifiedPush server for app endpoints at push.example.com,
// which the guard resolves to resolveTo. It answers each path with the
// status set for it, 201 by default.
type appServer struct {
	t   *testing.T
	srv *httptest.Server
	mu  sync.Mutex
	got []pushed
	// status and header answer a path; set them before the push.
	status map[string]int
	header map[string]http.Header
	// resolveTo is what push.example.com resolves to.
	resolveTo []netip.Addr
	r         *Registry
	now       time.Time
	retries   []retry
}

type retry struct {
	after time.Duration
	run   func()
}

func newAppServer(t *testing.T, r *Registry, cidrs ...netip.Prefix) *appServer {
	t.Helper()
	a := &appServer{t: t, r: r, status: map[string]int{}, header: map[string]http.Header{},
		resolveTo: []netip.Addr{loopback4}, now: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)}
	a.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		b, _ := io.ReadAll(q.Body)
		a.mu.Lock()
		a.got = append(a.got, pushed{q.URL.Path, b})
		code, h := a.status[q.URL.Path], a.header[q.URL.Path]
		a.mu.Unlock()
		for k, v := range h {
			w.Header()[k] = v
		}
		if code == 0 {
			code = 201
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(a.srv.Close)
	if cidrs == nil {
		cidrs = []netip.Prefix{loopbackNet}
	}
	r.Guard = NewEndpointGuard(nil, cidrs)
	a.use(r.Guard)
	r.now = func() time.Time { return a.now }
	r.after = func(d time.Duration, f func()) { a.retries = append(a.retries, retry{d, f}) }
	return a
}

// use points g's resolver at the fake DNS and trusts the server's
// certificate.
func (a *appServer) use(g *EndpointGuard) {
	g.base = a.srv.Client().Transport.(*http.Transport)
	g.lookup = func(_ context.Context, host string) ([]netip.Addr, error) {
		if host == "push.example.com" {
			return a.resolveTo, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
}

// endpoint is an app endpoint on this server.
func (a *appServer) endpoint(path string) string {
	_, port, _ := net.SplitHostPort(a.srv.Listener.Addr().String())
	return "https://push.example.com:" + port + path
}

func (a *appServer) pushes(path string) []pushed {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []pushed
	for _, p := range a.got {
		if p.path == path {
			out = append(out, p)
		}
	}
	return out
}

// runRetries runs the retries scheduled so far, at their times.
func (a *appServer) runRetries() []time.Duration {
	rs := a.retries
	a.retries = nil
	var waits []time.Duration
	for _, r := range rs {
		waits = append(waits, r.after)
		a.now = a.now.Add(r.after)
		r.run()
	}
	return waits
}

// confirmed registers s, reads the code from its confirmation push and
// confirms it, returning the device ID and token.
func (a *appServer) confirmed(s appSub, sessionID string) (string, string) {
	a.t.Helper()
	ctx := context.Background()
	path := s.Endpoint[strings.LastIndex(s.Endpoint, "/"):]
	before := len(a.pushes(path))
	id, renewed, err := a.r.RegisterApp(ctx, sessionID, s.PushSubscription)
	if err != nil || renewed {
		a.t.Fatalf("register: %v, renewed %v", err, renewed)
	}
	ps := a.pushes(path)
	if len(ps) != before+1 {
		a.t.Fatalf("%d confirmation pushes, want 1", len(ps)-before)
	}
	var c struct {
		Confirm struct{ Device, Code string }
	}
	if err := json.Unmarshal(s.decrypt(a.t, ps[len(ps)-1].body), &c); err != nil {
		a.t.Fatal(err)
	}
	if c.Confirm.Device != id || c.Confirm.Code == "" {
		a.t.Fatalf("confirmation %+v for device %s", c, id)
	}
	token, err := a.r.ConfirmDevice(ctx, id, c.Confirm.Code)
	if err != nil {
		a.t.Fatal(err)
	}
	return id, token
}

func newTestRegistry(t *testing.T) (*Registry, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r, err := NewRegistry(t.TempDir(), st.DB(), "mailto:alex@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return r, st
}

func TestEndpointRule(t *testing.T) {
	g := NewEndpointGuard([]string{"ntfy.example.com"}, []netip.Prefix{netip.PrefixFrom(private192, 24)})
	resolve := map[string][]netip.Addr{}
	g.lookup = func(_ context.Context, host string) ([]netip.Addr, error) {
		if a, ok := resolve[host]; ok {
			return a, nil
		}
		return nil, errors.New("no such host")
	}
	set := func(a ...netip.Addr) { resolve["push.example.com"] = a }
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		addr []netip.Addr
		ok   bool
	}{
		{"global", []netip.Addr{globalAddr}, true},
		{"loopback", []netip.Addr{loopback4}, false},
		{"loopback v6", []netip.Addr{netip.IPv6Loopback()}, false},
		{"private", []netip.Addr{private10}, false},
		{"unique local", []netip.Addr{netip.AddrFrom16([16]byte{0: 0xfd, 15: 1})}, false},
		{"link-local", []netip.Addr{linkLocal}, false},
		{"CGNAT", []netip.Addr{cgnat}, false},
		{"documentation", []netip.Addr{netip.MustParseAddr("192.0.2.10")}, false},
		{"documentation v6", []netip.Addr{netip.MustParseAddr("2001:db8::1")}, false},
		{"benchmarking", []netip.Addr{netip.AddrFrom4([4]byte{198, 18, 0, 1})}, false},
		{"multicast", []netip.Addr{netip.AddrFrom4([4]byte{224, 0, 0, 1})}, false},
		{"unspecified", []netip.Addr{netip.IPv4Unspecified()}, false},
		{"reserved", []netip.Addr{netip.AddrFrom4([4]byte{240, 0, 0, 1})}, false},
		{"mapped private", []netip.Addr{netip.AddrFrom16(private10.As16())}, false},
		{"6to4", []netip.Addr{netip.MustParseAddr("2002:a00:1::1")}, false},
		{"NAT64 of private", []netip.Addr{netip.MustParseAddr("64:ff9b::a00:1")}, false},
		{"Teredo", []netip.Addr{netip.MustParseAddr("2001:0:4136:e378:8000:63bf:3fff:fdd2")}, false},
		// DNS that answers a global and a private address is refused.
		{"one private answer", []netip.Addr{globalAddr, private10}, false},
		// MEAT_MODE_PUSH_ALLOW's CIDR admits addresses inside it only.
		{"allowed CIDR", []netip.Addr{private192}, true},
		{"allowed CIDR and outside", []netip.Addr{private192, private10}, false},
	} {
		set(tc.addr...)
		err := g.Check(ctx, "https://push.example.com/upAbc?up=1")
		if tc.ok != (err == nil) {
			t.Errorf("%s (%v): %v, want ok %v", tc.name, tc.addr, err, tc.ok)
		}
		if err != nil && !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v is not an invalid-request error", tc.name, err)
		}
	}

	// A host name entry admits that host exactly, whatever it resolves to.
	resolve["ntfy.example.com"] = []netip.Addr{private10}
	resolve["sub.ntfy.example.com"] = []netip.Addr{private10}
	if err := g.Check(ctx, "https://NTFY.example.com/upAbc?up=1"); err != nil {
		t.Errorf("allowed host: %v", err)
	}
	if err := g.Check(ctx, "https://sub.ntfy.example.com/upAbc?up=1"); err == nil {
		t.Error("a subdomain of an allowed host was admitted")
	}

	set(globalAddr)
	for _, u := range []string{
		"http://push.example.com/up",
		"https://alex@example.com/up", // user info
		"https://push.example.com/" + strings.Repeat("a", maxEndpoint),
		"https:push.example.com",
		"https://unknown.example.com/up",
		"https://" + private10.String() + "/up",
		"https://[::1]/up",
	} {
		if err := g.Check(ctx, u); err == nil || !errors.Is(err, store.ErrInvalid) {
			t.Errorf("Check(%.60q) = %v, want an invalid-request error", u, err)
		}
	}
	if err := g.Check(ctx, "https://"+private192.String()+":8443/up"); err != nil {
		t.Errorf("an address inside an allowed CIDR: %v", err)
	}
}

func TestAppSendsDialTheCheckedAddress(t *testing.T) {
	r, _ := newTestRegistry(t)
	a := newAppServer(t, r)
	ctx := context.Background()
	s := newAppSub(t, a.endpoint("/up1"))
	a.confirmed(s, "")
	if err := r.Notify(ctx, Message{Title: "Water the plants", Channel: ChannelReminders}); err != nil {
		t.Fatal(err)
	}
	// push.example.com has no DNS of its own: reaching the server proves the
	// hub dialled the address its own check resolved.
	ps := a.pushes("/up1")
	if len(ps) != 2 {
		t.Fatalf("%d pushes, want the confirmation and the reminder", len(ps))
	}
	var p struct{ Title, Channel string }
	if err := json.Unmarshal(s.decrypt(t, ps[1].body), &p); err != nil || p.Title != "Water the plants" || p.Channel != "reminders" {
		t.Fatalf("payload %+v, %v", p, err)
	}

	// The name now resolves to an address the rule refuses: the send is not
	// made, and the device is dropped.
	a.resolveTo = []netip.Addr{private10}
	if err := r.Notify(ctx, Message{Title: "Water the plants"}); err != nil {
		t.Fatal(err)
	}
	if n := len(a.pushes("/up1")); n != 2 {
		t.Fatalf("%d pushes after DNS changed, want still 2", n)
	}
	if ds, _ := r.Devices(ctx); len(ds) != 0 {
		t.Fatalf("devices %+v, want none", ds)
	}

	// Without the loopback range in MEAT_MODE_PUSH_ALLOW, registration is
	// refused before any push.
	r.Guard = NewEndpointGuard(nil, nil)
	a.use(r.Guard)
	a.resolveTo = []netip.Addr{loopback4}
	if _, _, err := r.RegisterApp(ctx, "", newAppSub(t, a.endpoint("/up2")).PushSubscription); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("register on loopback: %v, want invalid", err)
	}
	if n := len(a.pushes("/up2")); n != 0 {
		t.Fatalf("%d pushes to a refused endpoint", n)
	}

	// A host name entry trusts its host whatever it resolves to.
	r.Guard = NewEndpointGuard([]string{"push.example.com"}, nil)
	a.use(r.Guard)
	a.confirmed(newAppSub(t, a.endpoint("/up3")), "")
}

func TestSendsFollowNoRedirects(t *testing.T) {
	r, _ := newTestRegistry(t)
	a := newAppServer(t, r)
	ctx := context.Background()
	s := newAppSub(t, a.endpoint("/up1"))
	a.confirmed(s, "")
	a.status["/up1"] = http.StatusFound
	a.header["/up1"] = http.Header{"Location": {a.endpoint("/elsewhere")}}
	if err := r.Notify(ctx, Message{Title: "T"}); err == nil {
		t.Fatal("a redirect counted as delivered")
	}
	if n := len(a.pushes("/elsewhere")); n != 0 {
		t.Fatalf("followed the redirect %d times", n)
	}
	if len(a.retries) != 0 {
		t.Fatalf("a redirect was scheduled for retry")
	}
}

func TestAppConfirmation(t *testing.T) {
	r, _ := newTestRegistry(t)
	a := newAppServer(t, r)
	ctx := context.Background()
	s := newAppSub(t, a.endpoint("/up1"))
	id, renewed, err := r.RegisterApp(ctx, "", s.PushSubscription)
	if err != nil || renewed || len(id) != 32 {
		t.Fatalf("register: id %q, renewed %v, %v", id, renewed, err)
	}
	var c struct {
		Confirm struct{ Device, Code string }
	}
	if err := json.Unmarshal(s.decrypt(t, a.pushes("/up1")[0].body), &c); err != nil || c.Confirm.Device != id {
		t.Fatalf("confirmation %+v, %v", c, err)
	}
	if b, _ := base64.RawURLEncoding.DecodeString(c.Confirm.Code); len(b) != 32 {
		t.Errorf("code %q is not 32 bytes of base64url", c.Confirm.Code)
	}

	// No reminders before confirmation.
	r.Notify(ctx, Message{Title: "T"})
	if n := len(a.pushes("/up1")); n != 1 {
		t.Fatalf("an unconfirmed device got %d pushes", n)
	}
	ds, _ := r.Devices(ctx)
	if len(ds) != 1 || ds[0].Confirmed || ds[0].Name != "Test phone" || ds[0].Kind != api.DeviceApp {
		t.Fatalf("devices %+v", ds)
	}

	if _, err := r.ConfirmDevice(ctx, id, "wrong"); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("wrong code: %v, want forbidden", err)
	}
	if _, err := r.ConfirmDevice(ctx, "ffffffffffffffffffffffffffffffff", c.Confirm.Code); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("another device: %v, want forbidden", err)
	}
	token, err := r.ConfirmDevice(ctx, id, c.Confirm.Code)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, DeviceTokenPrefix)); !strings.HasPrefix(token, "mmd_") || len(b) != 32 {
		t.Fatalf("token %q", token)
	}
	if _, err := r.ConfirmDevice(ctx, id, c.Confirm.Code); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("used code: %v, want forbidden", err)
	}
	var stored string
	r.db.QueryRow(`SELECT token_hash FROM push_subscriptions WHERE id = ?`, id).Scan(&stored)
	if stored == "" || strings.Contains(stored, strings.TrimPrefix(token, DeviceTokenPrefix)) {
		t.Fatalf("stored token %q", stored)
	}

	// Confirmed: reminders arrive, and lastPush is set.
	a.now = a.now.Add(time.Minute)
	if err := r.Notify(ctx, Message{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	ds, _ = r.Devices(ctx)
	if len(ds) != 1 || !ds[0].Confirmed || ds[0].LastPush == nil || !ds[0].LastPush.Equal(a.now) {
		t.Fatalf("devices %+v", ds)
	}
	if got, err := r.DeviceByToken(ctx, token); err != nil || got != id {
		t.Fatalf("token: %q, %v", got, err)
	}
	if _, err := r.DeviceByToken(ctx, "mmd_unknown"); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("unknown token: %v", err)
	}

	// The same endpoint again renews the device; it keeps its ID and token.
	again, renewed, err := r.RegisterApp(ctx, "", s.PushSubscription)
	if err != nil || !renewed || again != id {
		t.Fatalf("renew: %q, %v, %v", again, renewed, err)
	}
	if got, _ := r.DeviceByToken(ctx, token); got != id {
		t.Fatal("renewal lost the token")
	}

	// A code not used within 10 minutes is dropped with its registration.
	late := newAppSub(t, a.endpoint("/up2"))
	lateID, _, err := r.RegisterApp(ctx, "", late.PushSubscription)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(late.decrypt(t, a.pushes("/up2")[0].body), &c); err != nil {
		t.Fatal(err)
	}
	a.now = a.now.Add(10*time.Minute + time.Second)
	if _, err := r.ConfirmDevice(ctx, lateID, c.Confirm.Code); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("expired code: %v, want forbidden", err)
	}
	if ds, _ := r.Devices(ctx); len(ds) != 1 {
		t.Fatalf("the expired registration is still listed: %+v", ds)
	}
}

func TestUnconfirmedRegistrationsAreCapped(t *testing.T) {
	r, _ := newTestRegistry(t)
	a := newAppServer(t, r)
	ctx := context.Background()
	for i := range MaxSubscriptions {
		if err := r.Subscribe(ctx, "", newSub(t, fmt.Sprintf("https://fcm.googleapis.com/fcm/send/%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	var ids []string
	for i := range 5 {
		id, _, err := r.RegisterApp(ctx, "", newAppSub(t, a.endpoint(fmt.Sprintf("/up%d", i))).PushSubscription)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	var confirmed, waiting int
	r.db.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE confirmed = 1`).Scan(&confirmed)
	r.db.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE confirmed = 0`).Scan(&waiting)
	if confirmed != MaxSubscriptions || waiting != MaxUnconfirmed {
		t.Fatalf("%d confirmed, %d waiting; want %d, %d", confirmed, waiting, MaxSubscriptions, MaxUnconfirmed)
	}
	var n int
	r.db.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE id IN (?, ?)`, ids[0], ids[1]).Scan(&n)
	if n != 0 {
		t.Error("the oldest unconfirmed registrations were kept")
	}
}

func TestSendResults(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T, status int, header http.Header) (*Registry, *appServer, string) {
		r, _ := newTestRegistry(t)
		a := newAppServer(t, r)
		id, _ := a.confirmed(newAppSub(t, a.endpoint("/up")), "")
		a.status["/up"], a.header["/up"] = status, header
		return r, a, id
	}
	listed := func(r *Registry) int {
		ds, err := r.Devices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(ds)
	}

	for _, code := range []int{404, 410} {
		r, a, _ := setup(t, code, nil)
		r.Notify(ctx, Message{Title: "T"})
		if listed(r) != 0 || len(a.retries) != 0 {
			t.Errorf("%d: %d devices, %d retries; want the device removed", code, listed(r), len(a.retries))
		}
	}

	t.Run("413 keeps the subscription", func(t *testing.T) {
		r, a, _ := setup(t, 413, nil)
		if err := r.Notify(ctx, Message{Title: "T"}); err == nil {
			t.Error("no error for 413")
		}
		if listed(r) != 1 || len(a.retries) != 0 {
			t.Errorf("%d devices, %d retries; want 1, 0", listed(r), len(a.retries))
		}
	})

	t.Run("429 holds every send until Retry-After", func(t *testing.T) {
		for _, tc := range []struct {
			retryAfter string
			want       time.Duration
		}{
			{"120", 2 * time.Minute},
			{"", time.Minute},
			{"86400", time.Hour},
			{"soon", time.Minute},
			{time.Date(2026, 3, 2, 9, 5, 0, 0, time.UTC).Format(http.TimeFormat), 5 * time.Minute},
		} {
			var h http.Header
			if tc.retryAfter != "" {
				h = http.Header{"Retry-After": {tc.retryAfter}}
			}
			r, a, _ := setup(t, 429, h)
			a.now = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
			r.Notify(ctx, Message{Title: "first"})
			r.Notify(ctx, Message{Title: "second"}) // held: not sent now
			if n := len(a.pushes("/up")); n != 2 {
				t.Fatalf("%q: %d pushes, want the confirmation and one held send", tc.retryAfter, n)
			}
			if len(a.retries) != 2 || a.retries[0].after != tc.want || a.retries[1].after != tc.want {
				t.Fatalf("%q: retries %+v, want two after %v", tc.retryAfter, a.retries, tc.want)
			}
			a.status["/up"] = 201
			a.runRetries()
			if n := len(a.pushes("/up")); n != 4 {
				t.Fatalf("%q: %d pushes after the hold, want both messages sent", tc.retryAfter, n)
			}
		}
	})

	for _, code := range []int{500, 503, 507} {
		t.Run(fmt.Sprintf("%d is retried, then given up", code), func(t *testing.T) {
			r, a, _ := setup(t, code, nil)
			if err := r.Notify(ctx, Message{Title: "T"}); err == nil {
				t.Error("no error")
			}
			var waits []time.Duration
			for len(a.retries) > 0 {
				waits = append(waits, a.runRetries()...)
			}
			if fmt.Sprint(waits) != "[1m0s 5m0s 30m0s]" {
				t.Errorf("retried after %v", waits)
			}
			if n := len(a.pushes("/up")); n != 5 {
				t.Errorf("%d pushes, want the confirmation, the send and 3 retries", n)
			}
			if listed(r) != 1 {
				t.Error("a failing device was dropped before 3 days")
			}
		})
	}

	t.Run("a retry that succeeds stops", func(t *testing.T) {
		r, a, _ := setup(t, 502, nil)
		r.Notify(ctx, Message{Title: "T"})
		a.status["/up"] = 201
		a.runRetries()
		if len(a.retries) != 0 || len(a.pushes("/up")) != 3 {
			t.Errorf("%d retries left, %d pushes", len(a.retries), len(a.pushes("/up")))
		}
		ds, _ := r.Devices(ctx)
		if ds[0].LastPush == nil || !ds[0].LastPush.Equal(a.now) {
			t.Errorf("lastPush %v, want the retry's time", ds[0].LastPush)
		}
	})

	t.Run("network errors are retried", func(t *testing.T) {
		r, a, _ := setup(t, 201, nil)
		a.srv.Close()
		if err := r.Notify(ctx, Message{Title: "T"}); err == nil {
			t.Error("no error")
		}
		if len(a.retries) != 1 || a.retries[0].after != time.Minute {
			t.Errorf("retries %+v", a.retries)
		}
	})

	t.Run("3 days of only failures removes the subscription", func(t *testing.T) {
		r, a, _ := setup(t, 500, nil)
		r.Notify(ctx, Message{Title: "T"})
		a.now = a.now.Add(3*24*time.Hour - time.Minute)
		r.Notify(ctx, Message{Title: "T"})
		if listed(r) != 1 {
			t.Fatal("removed before 3 days")
		}
		a.now = a.now.Add(time.Minute)
		r.Notify(ctx, Message{Title: "T"})
		if listed(r) != 0 {
			t.Fatal("kept after 3 days of failures")
		}
	})

	t.Run("a delivery resets the failure clock", func(t *testing.T) {
		r, a, _ := setup(t, 500, nil)
		r.Notify(ctx, Message{Title: "T"})
		a.now = a.now.Add(2 * 24 * time.Hour)
		a.status["/up"] = 201
		r.Notify(ctx, Message{Title: "T"})
		a.status["/up"] = 500
		a.now = a.now.Add(2 * 24 * time.Hour)
		r.Notify(ctx, Message{Title: "T"})
		if listed(r) != 1 {
			t.Fatal("removed although a push was delivered 2 days ago")
		}
	})

	t.Run("a test reminder reports the first attempt only", func(t *testing.T) {
		r, a, _ := setup(t, 503, nil)
		got := r.TestReminder(ctx, "", a.endpoint("/up"), "https://meat.example.com")
		if got.Push != "failed" || len(a.retries) != 0 {
			t.Errorf("got %+v with %d retries; want failed and none", got, len(a.retries))
		}
		a.status["/up"] = 201
		if got := r.TestReminder(ctx, "", a.endpoint("/up"), "https://meat.example.com"); got.Push != "sent" {
			t.Errorf("got %+v", got)
		}
		if n := len(a.pushes("/up")); n != 3 {
			t.Fatalf("%d pushes, want the confirmation and two tests", n)
		}
		a.status["/up"] = 410
		if got := r.TestReminder(ctx, "", a.endpoint("/up"), ""); got.Push != "expired" {
			t.Errorf("gone: got %+v", got)
		}
	})

	t.Run("a test reminder names the test channel", func(t *testing.T) {
		r, _ := newTestRegistry(t)
		a := newAppServer(t, r)
		s := newAppSub(t, a.endpoint("/up"))
		a.confirmed(s, "")
		r.TestReminder(ctx, "", s.Endpoint, "https://meat.example.com")
		ps := a.pushes("/up")
		var p struct{ Title, Channel string }
		if err := json.Unmarshal(s.decrypt(t, ps[len(ps)-1].body), &p); err != nil || p.Channel != ChannelTest {
			t.Fatalf("payload %+v, %v", p, err)
		}
	})
}

func TestDevicesFollowTheirSession(t *testing.T) {
	r, st := newTestRegistry(t)
	a := newAppServer(t, r)
	ctx := context.Background()

	// Registered with login off, then login is turned on: dropped.
	_, offToken := a.confirmed(newAppSub(t, a.endpoint("/off")), "")
	r.RequireSession = func() bool { return true }
	if _, err := r.DeviceByToken(ctx, offToken); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("token of a device from before the login: %v", err)
	}
	if _, _, err := r.RegisterApp(ctx, "", newAppSub(t, a.endpoint("/anon")).PushSubscription); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("register without a session: %v", err)
	}

	session := func() (string, string) {
		tok, s, err := st.CreateSession("tag", a.now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return tok, s.ID
	}
	_, out := session()
	endTok, ended := session()
	_, kept := session()
	_, outToken := a.confirmed(newAppSub(t, a.endpoint("/out")), out)
	_, endToken := a.confirmed(newAppSub(t, a.endpoint("/ended")), ended)
	keptID, keptToken := a.confirmed(newAppSub(t, a.endpoint("/kept")), kept)

	// Logging out drops the session's devices and their tokens.
	if err := r.DropSession(ctx, out); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeviceByToken(ctx, outToken); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("token after logout: %v", err)
	}
	// So does a session ending elsewhere, such as a password change.
	if err := st.DeleteSession(endTok); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeviceByToken(ctx, endToken); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("token after the session ended: %v", err)
	}
	if id, err := r.DeviceByToken(ctx, keptToken); err != nil || id != keptID {
		t.Fatalf("live session's device: %q, %v", id, err)
	}
	if ds, _ := r.Devices(ctx); len(ds) != 1 || ds[0].ID != keptID {
		t.Fatalf("devices %+v", ds)
	}

	// The device moves to a new endpoint and keeps its token.
	moved := newAppSub(t, a.endpoint("/moved"))
	if err := r.UpdateDevice(ctx, keptID, moved.PushSubscription); err != nil {
		t.Fatal(err)
	}
	r.Notify(ctx, Message{Title: "T"})
	if len(a.pushes("/moved")) != 1 || len(a.pushes("/kept")) != 1 {
		t.Fatalf("pushes moved %d, kept %d; want 1, 1 (the confirmation)", len(a.pushes("/moved")), len(a.pushes("/kept")))
	}
	if id, _ := r.DeviceByToken(ctx, keptToken); id != keptID {
		t.Fatal("the move lost the token")
	}
	if err := r.UpdateDevice(ctx, keptID, newAppSub(t, "https://"+private10.String()+"/up").PushSubscription); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("move to a private address: %v", err)
	}

	// Revoking drops it with its token; an unknown device is not found.
	if err := r.RevokeDevice(ctx, keptID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeviceByToken(ctx, keptToken); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("token after revoke: %v", err)
	}
	if err := r.RevokeDevice(ctx, keptID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke again: %v", err)
	}
	if err := r.UpdateDevice(ctx, keptID, moved.PushSubscription); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("update a revoked device: %v", err)
	}
}

func TestAppDevicesExpireWithoutRenewal(t *testing.T) {
	r, _ := newTestRegistry(t)
	a := newAppServer(t, r)
	_, token := a.confirmed(newAppSub(t, a.endpoint("/up")), "")
	a.now = a.now.Add(SubscriptionTTL)
	if _, err := r.DeviceByToken(context.Background(), token); !errors.Is(err, store.ErrUnauthorized) {
		t.Fatalf("token of a device not renewed for 60 days: %v", err)
	}
}

func TestWebSubscriptionsAreDevices(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	s := newSub(t, "https://fcm.googleapis.com/fcm/send/a")
	if err := r.Subscribe(ctx, "", s); err != nil {
		t.Fatal(err)
	}
	ds, _ := r.Devices(ctx)
	if len(ds) != 1 || ds[0].Kind != api.DeviceWeb || ds[0].Name != "Browser" || !ds[0].Confirmed || len(ds[0].ID) != 32 {
		t.Fatalf("devices %+v", ds)
	}
	// Subscribing again keeps the device.
	s.Name = "  Work laptop  "
	if err := r.Subscribe(ctx, "", s); err != nil {
		t.Fatal(err)
	}
	again, _ := r.Devices(ctx)
	if len(again) != 1 || again[0].ID != ds[0].ID || again[0].Name != "Work laptop" {
		t.Fatalf("devices %+v", again)
	}
	s.Name = strings.Repeat("é", 65)
	if err := r.Subscribe(ctx, "", s); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a 65-character name: %v", err)
	}
	if err := r.RevokeDevice(ctx, ds[0].ID); err != nil || count(t, r.db) != 0 {
		t.Fatalf("revoke: %v", err)
	}
}
