package hub

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// broker fans a revision number out to every open event stream, and apart
// from it the epics signal, so an epics sync never re-fetches State.
type broker struct {
	mu     sync.Mutex
	rev    int64
	subs   map[*stream]struct{}
	closed chan struct{}
}

// stream is one subscriber's two slots: the latest revision, and whether
// epic rows changed since it last sent `event: epics`. Each holds at most
// one pending signal, so a burst of changes coalesces.
type stream struct {
	change chan int64
	epics  chan struct{}
}

func newBroker() *broker {
	return &broker{subs: map[*stream]struct{}{}, closed: make(chan struct{})}
}

// publish bumps the revision and tells every subscriber. It never blocks:
// a slow subscriber gets the latest revision when it catches up.
func (b *broker) publish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rev++
	for sub := range b.subs {
		select {
		case <-sub.change:
		default:
		}
		sub.change <- b.rev
	}
}

// publishEpics tells every subscriber that epic rows changed. It never
// blocks, and leaves the revision alone.
func (b *broker) publishEpics() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs {
		select {
		case sub.epics <- struct{}{}:
		default:
		}
	}
}

// maxEventStreams caps open event streams; one person needs a handful.
const maxEventStreams = 32

// subscribe adds a subscriber, or reports false when maxEventStreams are
// already open.
func (b *broker) subscribe() (*stream, int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) >= maxEventStreams {
		return nil, 0, false
	}
	sub := &stream{change: make(chan int64, 1), epics: make(chan struct{}, 1)}
	b.subs[sub] = struct{}{}
	return sub, b.rev, true
}

func (b *broker) unsubscribe(sub *stream) {
	b.mu.Lock()
	delete(b.subs, sub)
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

// streamAllowed reports whether an open event stream may go on: its session
// is still valid.
func (s *Server) streamAllowed(r *http.Request) bool {
	l, sess := s.currentLogin(), sessionOf(r)
	if l == nil || sess == nil {
		return false
	}
	live, err := s.store.SessionLive(sess.ID, l.tag, s.now())
	return err == nil && live
}

// handleEvents streams `event: change` with the current revision on connect
// and on every change, `event: epics` when epic rows change, plus a comment
// heartbeat. It ends at the first
// heartbeat after its session does. Past maxEventStreams it answers 503.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	sub, rev, ok := s.events.subscribe()
	if !ok {
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, "too many open event streams")
		return
	}
	defer s.events.unsubscribe(sub)
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
		case rev := <-sub.change:
			if !send(rev) {
				return
			}
		case <-sub.epics:
			if _, err := fmt.Fprintf(w, "event: %s\ndata: {}\n\n", api.EpicsEvent); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		case <-tick.C:
			if !s.streamAllowed(r) {
				return
			}
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		}
	}
}
