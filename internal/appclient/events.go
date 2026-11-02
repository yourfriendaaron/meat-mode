package appclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// EventKind is what the event stream reported.
type EventKind string

const (
	EventChange    EventKind = "change"    // refetch the state
	EventEpics     EventKind = "epics"     // refetch the epics
	EventConnected EventKind = "connected" // the stream (re)opened: refetch both; the hub also sends a change at once, coalesce them
	EventDown      EventKind = "down"      // the stream dropped; Err says why, the client retries
	EventAuth      EventKind = "auth"      // the session ended; Events returns ErrAuth after sending it
)

// Event is one report from Events.
type Event struct {
	Kind EventKind
	Err  error // EventDown only
}

// The wait between attempts to open the stream doubles from minBackoff to
// maxBackoff, and starts again from minBackoff once a stream opened.
const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
)

// errStreamClosed is a stream the hub ended. It does so at the first
// heartbeat after the session ends, and when it shuts down.
var errStreamClosed = errors.New("the hub closed the event stream")

// Events follows GET /api/events until ctx ends, calling fn from one
// goroutine. It reconnects after a drop with backoff from 1 s to 30 s,
// waiting Retry-After on a 503. On a 401, or a drop followed by
// CheckSession reporting not authenticated, it sends EventAuth and returns
// ErrAuth. Other calls (State, Epics, Act) return a 401 as ErrAuth; the
// caller checks the session, asks for the password, then starts Events
// again.
func (c *Client) Events(ctx context.Context, fn func(Event)) error {
	var wait time.Duration
	for {
		opened, err := c.follow(ctx, fn)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrAuth) {
			fn(Event{Kind: EventAuth})
			return ErrAuth
		}
		if opened {
			// A hub ends the stream of a session that ended; ask before
			// trying again, so the user is asked for the password at once.
			if _, authed, cerr := c.CheckSession(ctx); cerr == nil && !authed || errors.Is(cerr, ErrAuth) {
				fn(Event{Kind: EventAuth})
				return ErrAuth
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			wait = 0
		}
		fn(Event{Kind: EventDown, Err: err})
		switch {
		case wait == 0:
			wait = minBackoff
		default:
			wait = min(wait*2, maxBackoff)
		}
		d := wait
		var re *RetryError
		if errors.As(err, &re) && re.After > 0 {
			d = time.Duration(re.After) * time.Second
		}
		if err := c.sleep(ctx, d); err != nil {
			return err
		}
	}
}

// follow opens one stream and reads it until it drops. opened reports
// whether the hub answered 200, in which case fn has had EventConnected.
func (c *Client) follow(ctx context.Context, fn func(Event)) (opened bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// The watchdog ends a stream that goes silent past c.idle: a connection
	// lost without a close sends nothing, not even an error.
	var silent atomic.Bool
	dog := time.AfterFunc(c.idle, func() { silent.Store(true); cancel() })
	defer dog.Stop()

	resp, err := c.request(ctx, http.MethodGet, "/api/events", nil, true)
	if err != nil {
		if silent.Load() {
			return false, fmt.Errorf("the hub did not answer within %s", c.idle)
		}
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err := statusError(resp, data); err != nil {
			return false, err
		}
		return false, &HTTPError{Status: resp.StatusCode, Msg: fmt.Sprintf("the hub replied %d to the event stream", resp.StatusCode)}
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "text/event-stream" {
		return false, fmt.Errorf("the hub's event stream came as %q, not text/event-stream", resp.Header.Get("Content-Type"))
	}
	fn(Event{Kind: EventConnected})

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	event := ""
	for sc.Scan() {
		dog.Reset(c.idle)
		line := sc.Text()
		switch {
		case line == "":
			switch event {
			case "change":
				fn(Event{Kind: EventChange})
			case api.EpicsEvent:
				fn(Event{Kind: EventEpics})
			}
			event = ""
		case strings.HasPrefix(line, ":"):
			// a comment: the hub's heartbeat
		default:
			field, value, _ := strings.Cut(line, ":")
			if field == "event" {
				event = strings.TrimPrefix(value, " ")
			}
		}
	}
	if silent.Load() {
		return true, fmt.Errorf("the hub's event stream was silent for %s", c.idle)
	}
	if err := sc.Err(); err != nil {
		return true, err
	}
	return true, errStreamClosed
}
