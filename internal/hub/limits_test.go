package hub

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// openStream opens /api/events on base and returns its lines.
func openStream(t *testing.T, ctx context.Context, base, cookie string) (*http.Response, <-chan string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/events", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	return resp, lines
}

func waitLine(t *testing.T, lines <-chan string, prefix string, within time.Duration) string {
	t.Helper()
	timeout := time.After(within)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed before a %q line", prefix)
			}
			if strings.HasPrefix(l, prefix) {
				return l
			}
		case <-timeout:
			t.Fatalf("no %q line within %v", prefix, within)
		}
	}
}

func TestEventStreamsAreCapped(t *testing.T) {
	h := newHarness(t, nil, false)
	ts := httptest.NewServer(h.srv)
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var open []*http.Response
	for range maxEventStreams {
		resp, lines := openStream(t, ctx, ts.URL, h.cookie)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream %d: status %d", len(open)+1, resp.StatusCode)
		}
		waitLine(t, lines, "data:", 3*time.Second)
		open = append(open, resp)
	}
	over, _ := openStream(t, ctx, ts.URL, h.cookie)
	if over.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stream past the cap: status %d", over.StatusCode)
	}
	over.Body.Close()

	// Closing one frees a slot.
	open[0].Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for h.srv.events.count() >= maxEventStreams {
		if time.Now().After(deadline) {
			t.Fatal("slot not freed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	again, _ := openStream(t, ctx, ts.URL, h.cookie)
	if again.StatusCode != http.StatusOK {
		t.Fatalf("stream after one closed: status %d", again.StatusCode)
	}
}

// TestReadTimeoutSparesEventStreams runs the hub's own http.Server with a
// short ReadTimeout: a stream must outlive it, while a slow request body
// must not.
func TestReadTimeoutSparesEventStreams(t *testing.T) {
	h := newHarness(t, nil, false)
	h.srv.heartbeat = 50 * time.Millisecond
	srv := newHTTPServer(context.Background(), h.srv)
	if srv.ReadTimeout != 60*time.Second || srv.WriteTimeout != 0 {
		t.Fatalf("ReadTimeout %v, WriteTimeout %v", srv.ReadTimeout, srv.WriteTimeout)
	}
	srv.ReadTimeout = 300 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()
	base := "http://" + ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, lines := openStream(t, ctx, base, h.cookie)
	waitLine(t, lines, "data:", 3*time.Second)
	time.Sleep(3 * srv.ReadTimeout)
	h.do("POST", "/api/bridges", map[string]string{"name": "laptop"}, "")
	if l := waitLine(t, lines, "data:", 3*time.Second); l != `data: {"rev": 1}` {
		t.Fatalf("event after the read timeout %q", l)
	}

	// A request whose body never arrives is cut off.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("POST /api/bridges HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{"))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	buf := make([]byte, 512)
	for {
		if _, err := conn.Read(buf); err != nil {
			break
		}
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("slow body held the connection for %v", d)
	}
}
