package hub

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestSetupStatusReportsTheHubSide(t *testing.T) {
	h := newLoginHarness(t, Config{
		PublicURL: "https://meat-mode.example.com",
		NtfyURL:   "https://ntfy.example.com",
	})
	got := decode[api.SetupStatus](t, h.do("GET", "/api/setup/status", "", ""))
	// The harness opens the app as localhost, which is allowed but is not the public address.
	want := api.SetupStatus{Login: "none", HostMatches: false, PublicURL: "https://meat-mode.example.com", PushKeys: true, Ntfy: true, Time: h.now,
		PublicDNS: api.PublicDNS{State: api.DNSUnchecked}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSetupStatusHostMatch(t *testing.T) {
	for _, c := range []struct {
		public, host string
		want         bool
	}{
		{"https://meat-mode.example.com", "meat-mode.example.com", true},
		{"https://Meat-Mode.example.com", "meat-mode.EXAMPLE.com", true},
		{"https://meat-mode.example.com:8443", "meat-mode.example.com:8443", true},
		{"https://meat-mode.example.com", "meat-mode.example.com:8443", false},
		{"https://meat-mode.example.com", "localhost:8080", false},
		{"http://localhost:8080", "localhost:8080", true},
		{"", "localhost:8080", false},
	} {
		h := newLoginHarness(t, Config{PublicURL: c.public, AllowedHosts: "meat-mode.example.com"})
		req := httptest.NewRequest("GET", "/api/setup/status", nil)
		req.Host = c.host
		w := httptest.NewRecorder()
		h.srv.ServeHTTP(w, req)
		if got := decode[api.SetupStatus](t, w).HostMatches; got != c.want {
			t.Errorf("public %q, host %q: %v, want %v", c.public, c.host, got, c.want)
		}
	}
}

func TestSetupStatusLoginSource(t *testing.T) {
	h := newLoginHarness(t, Config{})
	if got := decode[api.SetupStatus](t, h.do("GET", "/api/setup/status", "", "")).Login; got != "none" {
		t.Fatalf("no password: %q", got)
	}
	if err := h.st.SetPasswordHash(cheapHash("pw")); err != nil {
		t.Fatal(err)
	}
	if got := decode[api.SetupStatus](t, h.do("GET", "/api/setup/status", "", "")).Login; got != "app" {
		t.Fatalf("password set in the app: %q", got)
	}
	h.srv = h.server(Config{PasswordHash: cheapHash("env")})
	cookie := h.login("env")
	if got := decode[api.SetupStatus](t, h.do("GET", "/api/setup/status", "", cookie.Value)).Login; got != "environment" {
		t.Fatalf("password from the environment: %q", got)
	}
}

func TestSetupStatusWithoutPushKeys(t *testing.T) {
	h := newHarness(t, nil, false)
	w := h.do("GET", "/api/setup/status", nil, "")
	h.expect(w, http.StatusOK)
	if got := decode[api.SetupStatus](t, w); got.PushKeys || got.Ntfy {
		t.Fatalf("hub with no push and no ntfy: %+v", got)
	}
}

func TestTestEventReachesTheEventStream(t *testing.T) {
	h := newHarness(t, nil, false)
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
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func() string {
		t.Helper()
		for {
			select {
			case l := <-lines:
				if strings.HasPrefix(l, "data:") {
					return l
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no event")
			}
		}
	}
	if l := next(); l != `data: {"rev": 0}` {
		t.Fatalf("first event %q", l)
	}
	h.expect(h.do("POST", "/api/setup/test-event", nil, ""), http.StatusNoContent)
	if l := next(); l != `data: {"rev": 1}` {
		t.Fatalf("test event %q", l)
	}
}
