package hub

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// openSessionStream opens /api/events on ts with cookie and returns its lines,
// closed when the hub ends the stream.
func openSessionStream(t *testing.T, ts *httptest.Server, cookie string) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	req.Host = "localhost:8080"
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events: %d", resp.StatusCode)
	}
	lines := make(chan string, 64)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	return lines
}

// heartbeats waits for n heartbeats on lines.
func heartbeats(t *testing.T, lines <-chan string, n int) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for n > 0 {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("stream closed while its session was valid")
			}
			if strings.HasPrefix(l, ": heartbeat") {
				n--
			}
		case <-timeout:
			t.Fatal("no heartbeat")
		}
	}
}

// streamEnds waits for the hub to end the stream.
func streamEnds(t *testing.T, lines <-chan string) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-lines:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("stream still open")
		}
	}
}

func TestEventStreamEndsWithItsSession(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	h.srv.heartbeat = 20 * time.Millisecond
	ts := httptest.NewServer(h.srv)
	t.Cleanup(ts.Close)
	c := h.login("water the plants")
	other := h.login("water the plants")
	lines := openSessionStream(t, ts, c.Value)
	kept := openSessionStream(t, ts, other.Value)
	heartbeats(t, lines, 2)
	if w := h.do("POST", "/api/logout", "", c.Value); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	streamEnds(t, lines)
	heartbeats(t, kept, 2)
}

func TestEventStreamEndsWhenLoginTurnsOn(t *testing.T) {
	h := newLoginHarness(t, Config{})
	h.srv.heartbeat = 20 * time.Millisecond
	ts := httptest.NewServer(h.srv)
	t.Cleanup(ts.Close)
	lines := openSessionStream(t, ts, "")
	heartbeats(t, lines, 2)
	h.srv.setLogin(newLogin(cheapHash("water the plants"), false))
	streamEnds(t, lines)
}
