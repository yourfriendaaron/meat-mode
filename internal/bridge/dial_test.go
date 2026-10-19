package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// fakeResolver answers every name with the given addresses and counts the
// lookups.
type fakeResolver struct {
	addrs []netip.Addr
	n     atomic.Int32
}

func (f *fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	f.n.Add(1)
	return f.addrs, nil
}

var (
	publicAddr = netip.AddrFrom4([4]byte{203, 0, 113, 7}) // documentation range, outside the private set
	loopback   = netip.AddrFrom4([4]byte{127, 0, 0, 1})
)

// hubAt serves a loopback hub; the URL's host is replaced by name so the
// resolver decides where it goes.
func hubAt(t *testing.T) (srv *httptest.Server, urlFor func(name string) string) {
	t.Helper()
	var hits atomic.Int32
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().String()
	port = port[strings.LastIndex(port, ":"):]
	return srv, func(name string) string { return "http://" + name + port }
}

func TestHubDialCheck(t *testing.T) {
	_, urlFor := hubAt(t)
	trusted, err := netguard.ParseTrusted(publicAddr.String() + "/24")
	if err != nil {
		t.Fatal(err)
	}
	h := func(allowed netguard.Set, addrs ...netip.Addr) *Hub {
		return &Hub{URL: urlFor("hub.example.com"), Token: "t", Allowed: allowed, Resolver: &fakeResolver{addrs: addrs}}
	}
	if _, err := h(netguard.Set{}, loopback).Actions(context.Background()); err != nil {
		t.Errorf("loopback hub: %v", err)
	}
	for name, hub := range map[string]*Hub{
		"public":                  h(netguard.Set{}, publicAddr),
		"one public among others": h(netguard.Set{}, loopback, publicAddr),
	} {
		if _, err := hub.Actions(context.Background()); err == nil || !strings.Contains(err.Error(), "outside the private network") {
			t.Errorf("%s: Actions = %v, want a refusal", name, err)
		}
	}
	// A trusted network passes the check. The context is already over so
	// the dial that follows opens no socket.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h(trusted, publicAddr).Actions(ctx); err == nil || strings.Contains(err.Error(), "outside the private network") {
		t.Errorf("trusted public address: Actions = %v, want a failure that is not a refusal", err)
	}
}

// TestHubDialsTheCheckedAddress: the name is looked up once per connection
// and the connection goes to what was checked.
func TestHubDialsTheCheckedAddress(t *testing.T) {
	_, urlFor := hubAt(t)
	res := &fakeResolver{addrs: []netip.Addr{loopback}}
	h := &Hub{URL: urlFor("hub.example.com"), Token: "t", Resolver: res}
	if _, err := h.Actions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res.n.Load() == 0 {
		t.Error("the host name was never resolved")
	}
}

func TestHubIgnoresProxyFromEnvironment(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { proxied.Add(1) }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	_, urlFor := hubAt(t)
	h := &Hub{URL: urlFor("hub.example.com"), Token: "t", Resolver: &fakeResolver{addrs: []netip.Addr{loopback}}}
	if _, err := h.Actions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if proxied.Load() != 0 {
		t.Error("the request went through the proxy from the environment")
	}
}

func TestPairRefusesAHubOutsideThePrivateSet(t *testing.T) {
	_, urlFor := hubAt(t)
	t.Setenv("MEAT_MODE_BRIDGE_TOKEN", "")
	env, _, _ := testEnv(t, fakeBD(t, "1.3.1"))
	env.Resolver = &fakeResolver{addrs: []netip.Addr{publicAddr}}
	// Plain HTTP to a name that is not loopback needs the opt-in; the dial
	// check comes after it.
	err := Main(context.Background(), []string{"pair", urlFor("hub.example.com"), token, "--allow-insecure-http", "--no-service"}, env)
	if err == nil || !strings.Contains(err.Error(), "outside the private network") {
		t.Fatalf("pair = %v, want a refusal", err)
	}
	if _, err := os.Stat(env.Paths.Config); !os.IsNotExist(err) {
		t.Error("config saved after a refused pairing")
	}
}

// TestPairSavesTrustedNetworks: the value pair saw in the environment is
// saved, for the service that does not get this shell's environment.
func TestPairSavesTrustedNetworks(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	t.Setenv("MEAT_MODE_BRIDGE_TOKEN", "")
	t.Setenv(netguard.EnvTrusted, "198.51.100.0/24")
	env, _, _ := testEnv(t, fakeBD(t, "1.3.1"))
	if err := Main(context.Background(), []string{"pair", hub.URL, token, "--no-service"}, env); err != nil {
		t.Fatal(err)
	}
	if c, _ := readConfigFile(env.Paths.Config); c.TrustedNetworks != "198.51.100.0/24" {
		t.Errorf("saved trustedNetworks = %q", c.TrustedNetworks)
	}
	t.Setenv(netguard.EnvTrusted, "0.0.0.0/0")
	err := Main(context.Background(), []string{"pair", hub.URL, token, "--no-service"}, env)
	if err == nil || !strings.Contains(err.Error(), netguard.EnvTrusted) {
		t.Errorf("pair with a bad trusted network = %v", err)
	}
}

func TestRunReadsTrustedNetworksFromEnvThenFile(t *testing.T) {
	env := Env{Paths: Paths{Config: t.TempDir() + "/bridge.json"}}
	must(t, Config{HubURL: "https://hub.example.com", Token: "t", TrustedNetworks: "198.51.100.0/24"}.Save(env.Paths.Config))
	s, err := LoadSettings(env)
	if err != nil || s.Config.TrustedNetworks != "198.51.100.0/24" {
		t.Fatalf("saved value = %+v, %v", s.Config, err)
	}
	t.Setenv(netguard.EnvTrusted, "203.0.113.0/24")
	if s, _ = LoadSettings(env); s.Config.TrustedNetworks != "203.0.113.0/24" || s.Saved.TrustedNetworks != "198.51.100.0/24" {
		t.Errorf("env value = %q, saved %q; want the env to win and the saved value kept", s.Config.TrustedNetworks, s.Saved.TrustedNetworks)
	}
	t.Setenv(netguard.EnvTrusted, "0.0.0.0/0")
	if _, err = LoadSettings(env); err == nil {
		t.Error("a trusted network wider than /16 was accepted")
	}
}

// TestRunLoopRetriesARefusedHub: a refusal is an error from the round, which
// the run loop logs and retries like any network error.
func TestRunLoopRetriesARefusedHub(t *testing.T) {
	_, urlFor := hubAt(t)
	h := &Hub{URL: urlFor("hub.example.com"), Token: "t", Resolver: &fakeResolver{addrs: []netip.Addr{publicAddr}}}
	b := &Bridge{Hub: h}
	b.bdVersion = "1.3.1"
	err := b.Tick(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "outside the private network") {
		t.Fatalf("Tick = %v", err)
	}
	if b.name != "" {
		t.Error("the bridge counts as connected")
	}
}
