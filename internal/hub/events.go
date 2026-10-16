package hub

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// broker fans a revision number out to every open event stream.
type broker struct {
	mu     sync.Mutex
	rev    int64
	subs   map[chan int64]struct{}
	closed chan struct{}
}

func newBroker() *broker {
	return &broker{subs: map[chan int64]struct{}{}, closed: make(chan struct{})}
}

// publish bumps the revision and tells every subscriber. It never blocks:
// a slow subscriber gets the latest revision when it catches up.
func (b *broker) publish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rev++
	for ch := range b.subs {
		select {
		case <-ch:
		default:
		}
		ch <- b.rev
	}
}

// maxEventStreams caps open event streams; one person needs a handful.
const maxEventStreams = 32

// subscribe adds a subscriber, or reports false when maxEventStreams are
// already open.
func (b *broker) subscribe() (chan int64, int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) >= maxEventStreams {
		return nil, 0, false
	}
	ch := make(chan int64, 1)
	b.subs[ch] = struct{}{}
	return ch, b.rev, true
}

func (b *broker) unsubscribe(ch chan int64) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

func (b *broker) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// close ends every open stream, for shutdown.
func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
}

// handleEvents streams `event: change` with the current revision on connect
// and on every change, plus a comment heartbeat. Past maxEventStreams it
// answers 503.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ch, rev, ok := s.events.subscribe()
	if !ok {
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, "too many open event streams")
		return
	}
	defer s.events.unsubscribe(ch)
	rc := http.NewResponseController(w)
	// The stream has no body to read; the server's ReadTimeout is for
	// requests that do, so lift it here.
	_ = rc.SetReadDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(rev int64) bool {
		if _, err := fmt.Fprintf(w, "event: change\ndata: {\"rev\": %d}\n\n", rev); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send(rev) {
		return
	}
	tick := time.NewTicker(s.heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.events.closed:
			return
		case rev := <-ch:
			if !send(rev) {
				return
			}
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		}
	}
}
