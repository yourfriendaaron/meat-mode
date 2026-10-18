package notify

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"testing"
	"time"
)

// TestAppPushThroughARealNtfy registers an app device whose endpoint is a
// real ntfy server's UnifiedPush topic, reads the confirmation and a
// reminder back from ntfy, and decrypts them as the app does. It needs a
// running ntfy, so it runs only with MEAT_MODE_TEST_NTFY=1:
//
//	docker run -d --rm --name meat-mode-test-ntfy -p 127.0.0.1:18091:80 docker.io/binwiederhier/ntfy serve
//	MEAT_MODE_TEST_NTFY=1 go test ./internal/notify -run RealNtfy
//	docker stop meat-mode-test-ntfy
//
// MEAT_MODE_TEST_NTFY_URL names another ntfy than http://127.0.0.1:18091.
// ntfy there speaks plain http, so the test reaches it through a TLS proxy
// that the guard resolves push.example.com to.
func TestAppPushThroughARealNtfy(t *testing.T) {
	if os.Getenv("MEAT_MODE_TEST_NTFY") != "1" {
		t.Skip("set MEAT_MODE_TEST_NTFY=1 with an ntfy server running; see the comment")
	}
	base := os.Getenv("MEAT_MODE_TEST_NTFY_URL")
	if base == "" {
		base = "http://127.0.0.1:18091"
	}
	ntfy, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(ntfy))
	t.Cleanup(proxy.Close)

	r, _ := newTestRegistry(t)
	r.Guard = NewEndpointGuard(nil, []netip.Prefix{loopbackNet})
	r.Guard.base = proxy.Client().Transport.(*http.Transport)
	r.Guard.lookup = func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{loopback4}, nil }
	ctx := context.Background()

	b := make([]byte, 8)
	rand.Read(b)
	topic := "upMeatModeTest" + hex.EncodeToString(b)
	_, port, _ := net.SplitHostPort(proxy.Listener.Addr().String())
	s := newAppSub(t, "https://push.example.com:"+port+"/"+topic+"?up=1")

	id, renewed, err := r.RegisterApp(ctx, "", s.PushSubscription)
	if err != nil || renewed {
		t.Fatalf("register: %v, renewed %v", err, renewed)
	}
	msgs := pollNtfy(t, base, topic, 1)
	var c struct {
		Confirm struct{ Device, Code string }
	}
	if err := json.Unmarshal(s.decrypt(t, msgs[0]), &c); err != nil || c.Confirm.Device != id {
		t.Fatalf("confirmation %+v, %v", c, err)
	}
	token, err := r.ConfirmDevice(ctx, id, c.Confirm.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.DeviceByToken(ctx, token); err != nil || got != id {
		t.Fatalf("token: %q, %v", got, err)
	}

	m := Message{Title: "Water the plants", Body: "Due now", URL: "https://meat.example.com/?item=home%2Fchores%2Fch-1",
		Tag: "home/chores/ch-1", Key: "home/chores/ch-1", Channel: ChannelReminders,
		Actions: []Action{{Action: ActionDone, Title: "Done"}, {Action: ActionSnooze, Title: "Snooze 1 hour"}}}
	if err := r.Notify(ctx, m); err != nil {
		t.Fatal(err)
	}
	msgs = pollNtfy(t, base, topic, 2)
	var p struct {
		Title, Channel, Key string
		Actions             []Action
	}
	if err := json.Unmarshal(s.decrypt(t, msgs[1]), &p); err != nil {
		t.Fatal(err)
	}
	if p.Title != m.Title || p.Channel != ChannelReminders || p.Key != m.Key || len(p.Actions) != 2 || p.Actions[0].Action != ActionDone {
		t.Fatalf("reminder %+v", p)
	}
	ds, err := r.Devices(ctx)
	if err != nil || len(ds) != 1 || !ds[0].Confirmed || ds[0].LastPush == nil {
		t.Fatalf("devices %+v, %v", ds, err)
	}
}

// pollNtfy waits up to 10 seconds for topic to hold n messages and returns
// their bodies, oldest first.
func pollNtfy(t *testing.T, base, topic string, n int) [][]byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/" + topic + "/json?poll=1")
		if err != nil {
			t.Fatal(err)
		}
		var out [][]byte
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			var m struct{ Event, Message, Encoding string }
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Fatalf("ntfy line %q: %v", sc.Text(), err)
			}
			if m.Event != "message" {
				continue
			}
			body := []byte(m.Message)
			if m.Encoding == "base64" {
				if body, err = base64.StdEncoding.DecodeString(m.Message); err != nil {
					t.Fatal(err)
				}
			}
			out = append(out, body)
		}
		resp.Body.Close()
		if len(out) >= n {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("ntfy holds %d messages on %s, want %d", len(out), topic, n)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
