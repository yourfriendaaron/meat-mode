package hub

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// fakeDNS answers every lookup with addrs and err, and counts the lookups.
type fakeDNS struct {
	addrs []netip.Addr
	err   error
	calls atomic.Int32
}

func (f *fakeDNS) lookup(_ context.Context, _ string) ([]netip.Addr, error) {
	f.calls.Add(1)
	return f.addrs, f.err
}

func addrs(ss ...string) []netip.Addr {
	var out []netip.Addr
	for _, s := range ss {
		out = append(out, netip.MustParseAddr(s))
	}
	return out
}

var errNoAnswer = errors.New("i/o timeout")

func TestCheckPublicDNS(t *testing.T) {
	trusted, err := netguard.ParseTrusted("2001:db8:1::/48")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, url string
		dns       *fakeDNS
		want      api.PublicDNS
		lookedUp  bool
	}{
		{"localhost", "http://localhost:8080", &fakeDNS{}, api.PublicDNS{State: api.DNSSkipped}, false},
		{"under localhost", "https://meat-mode.localhost", &fakeDNS{}, api.PublicDNS{State: api.DNSSkipped}, false},
		{"loopback", "http://127.0.0.1:8080", &fakeDNS{}, api.PublicDNS{State: api.DNSSkipped}, false},
		{"loopback v6", "http://[::1]:8080", &fakeDNS{}, api.PublicDNS{State: api.DNSSkipped}, false},
		{"private literal", "https://" + lan + ":8443", &fakeDNS{}, api.PublicDNS{State: api.DNSSkipped}, false},
		{"trusted literal", "https://[2001:db8:1::5]", &fakeDNS{}, api.PublicDNS{State: api.DNSSkipped}, false},
		{"public literal", "https://203.0.113.5", &fakeDNS{},
			api.PublicDNS{State: api.DNSPublic, Addresses: []string{"203.0.113.5"}}, false},
		{"no records", "https://meat-mode.example.com", &fakeDNS{}, api.PublicDNS{State: api.DNSPrivate}, true},
		{"private records", "https://meat-mode.example.com", &fakeDNS{addrs: addrs(lan, ula, "2001:db8:1::9")},
			api.PublicDNS{State: api.DNSPrivate}, true},
		{"public records", "https://meat-mode.example.com",
			&fakeDNS{addrs: addrs(lan, "203.0.113.7", "::ffff:198.51.100.3", "2001:db8::7")},
			api.PublicDNS{State: api.DNSPublic, Addresses: []string{"203.0.113.7", "198.51.100.3", "2001:db8::7"}}, true},
		{"no answer", "https://meat-mode.example.com", &fakeDNS{err: errNoAnswer}, api.PublicDNS{State: api.DNSUnchecked}, true},
	} {
		got, _ := checkPublicDNS(context.Background(), c.url, trusted, c.dns.lookup)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
		if n := c.dns.calls.Load(); (n > 0) != c.lookedUp {
			t.Errorf("%s: %d lookups, want lookup %v", c.name, n, c.lookedUp)
		}
	}
}

func publicConfig(dir string) Config {
	cfg := runConfig(dir)
	cfg.PublicURL = "https://meat-mode.example.com"
	return cfg
}

func TestRunStopsOnAPublicAddress(t *testing.T) {
	dns := &fakeDNS{addrs: addrs("203.0.113.7")}
	started := false
	err := Run(stopped(), publicConfig(t.TempDir()), Options{
		lookup:  dns.lookup,
		OnStart: func(context.Context, Config, *store.Store) error { started = true; return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "203.0.113.7") || !strings.Contains(err.Error(), "Funnel") {
		t.Fatalf("Run: %v, want the public address and how to fix it", err)
	}
	if started {
		t.Error("OnStart ran before the hub stopped")
	}
}

func TestRunStartsWhenDNSIsPrivateOrUnchecked(t *testing.T) {
	for _, dns := range []*fakeDNS{{}, {addrs: addrs(tailnet)}, {err: errNoAnswer}} {
		if err := Run(stopped(), publicConfig(t.TempDir()), Options{lookup: dns.lookup}); err != nil {
			t.Errorf("Run with %+v: %v", dns, err)
		}
		if dns.calls.Load() != 1 {
			t.Errorf("%d lookups, want 1", dns.calls.Load())
		}
	}
}

func TestRunSkipsTheLookupForLocalhost(t *testing.T) {
	dns := &fakeDNS{addrs: addrs("203.0.113.7")}
	if err := Run(stopped(), runConfig(t.TempDir()), Options{lookup: dns.lookup}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if dns.calls.Load() != 0 {
		t.Errorf("looked up localhost")
	}
}

// syncBuffer is a log sink a background goroutine can write to while the
// test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestWatchPublicDNSLogsAndKeepsRunning(t *testing.T) {
	h := newHarness(t, nil, false)
	h.srv.cfg.PublicURL = "https://meat-mode.example.com"
	dns := &fakeDNS{addrs: addrs("203.0.113.7")}
	var logs syncBuffer
	h.srv.lookup = dns.lookup
	h.srv.log = slog.New(slog.NewTextHandler(&logs, nil))
	h.srv.dnsEvery = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.srv.watchPublicDNS(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for dns.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if dns.calls.Load() < 3 {
		t.Fatalf("%d checks, want it to keep checking", dns.calls.Load())
	}
	if !strings.Contains(logs.String(), "203.0.113.7") {
		t.Errorf("public address not logged:\n%s", logs.String())
	}
	got := decode[api.SetupStatus](t, h.do("GET", "/api/setup/status", "", "")).PublicDNS
	if got.State != api.DNSPublic || !reflect.DeepEqual(got.Addresses, []string{"203.0.113.7"}) || got.CheckedAt == nil {
		t.Errorf("setup status publicDns %+v", got)
	}
}

func TestPublicDNSUncheckedIsLogged(t *testing.T) {
	h := newHarness(t, nil, false)
	h.srv.cfg.PublicURL = "https://meat-mode.example.com"
	var logs syncBuffer
	h.srv.lookup = (&fakeDNS{err: errNoAnswer}).lookup
	h.srv.log = slog.New(slog.NewTextHandler(&logs, nil))
	if d := h.srv.runPublicDNSCheck(context.Background()); d.State != api.DNSUnchecked || d.CheckedAt == nil {
		t.Errorf("check %+v", d)
	}
	if !strings.Contains(logs.String(), "could not check") {
		t.Errorf("log %q", logs.String())
	}
}
