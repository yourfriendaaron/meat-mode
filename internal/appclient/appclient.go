// Package appclient is the terminal viewer's client for the hub's app API
// (docs/api.md, App API): it logs in with the hub's password, keeps the
// session cookie in a file, reads the state and the epics, follows the event
// stream and sends actions. It dials only through netguard.Dialer, as the
// bridge does, so the password never goes to a host outside the private
// network.
package appclient

import (
	"context"
	"errors"
	"fmt"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// SessionFile is the file, beside bridge.json in the same directory
// (filepath.Dir of bridge.DefaultPaths().Config), that holds one Session per
// hub URL, mode 0600:
//
//	{"version": 1, "sessions": {"<Session.Hub>": {"hub", "cookieName", "cookie"}}}
//
// Writes go to a temporary file renamed over it; two viewers saving at once
// leave the last writer's file, which loses at most the other's renewal.
const SessionFile = "tui.json"

// Session is a logged-in app session on one hub. It is a full app login:
// anything the app can do, a holder of the cookie can do.
type Session struct {
	Hub string `json:"hub"` // the hub URL as the user gave it, normalised: scheme, host, port, no path
	// CookieName is the name the hub's Set-Cookie used, read by hand rather
	// than through a cookie jar: the hub names it after MEAT_MODE_PUBLIC_URL's
	// scheme (__Host-meat_mode_session on https, meat_mode_session on http),
	// not after the URL the viewer dials, and a jar drops a Secure cookie
	// over http://localhost. Either name is accepted.
	CookieName string `json:"cookieName"`
	Cookie     string `json:"cookie"`
}

// LoadSession reads the session for hub from dir/SessionFile. ok is false
// when the file or the hub's entry does not exist.
func LoadSession(dir, hub string) (s Session, ok bool, err error) {
	return Session{}, false, errNotBuilt
}

// SaveSession writes s into dir/SessionFile, replacing that hub's entry and
// keeping the others, atomically and with mode 0600.
func SaveSession(dir string, s Session) error { return errNotBuilt }

// DeleteSession removes hub's entry; the file goes when it is the last.
// A missing entry is not an error.
func DeleteSession(dir, hub string) error { return errNotBuilt }

// NormalizeHub turns what the user typed into the form Session.Hub keeps:
// https:// when no scheme is given, no trailing slash or path. It refuses a
// scheme other than http and https, and http other than localhost,
// 127.0.0.1 and [::1], as the hub does (docs/api.md, App API). An explicit
// port is kept, a default one dropped. The hub answers 421 to a Host other
// than localhost, its public URL's host or MEAT_MODE_ALLOWED_HOSTS, so a hub
// typed as a bare IP address can fail there; HTTPError 421 says so.
func NormalizeHub(raw string) (string, error) { return "", errNotBuilt }

// ErrAuth means the hub answered 401: the session ended, or Login's
// password was wrong (WrongPassword tells which).
var ErrAuth = errors.New("not logged in to the hub")

// WrongPassword is Login's error for a 401; it wraps ErrAuth.
var WrongPassword = fmt.Errorf("wrong password: %w", ErrAuth)

// ErrNoPassword means the hub has no password yet (POST /api/login answers
// 404, CheckSession reports login false): it must be set in the app first.
var ErrNoPassword = errors.New("this hub has no password yet: set one in the app first")

// RetryError is a 429 or 503 with the wait the hub asked for.
type RetryError struct {
	Status int
	After  int // seconds, from Retry-After; 0 when absent
	Msg    string
}

func (e *RetryError) Error() string { return e.Msg }

// HTTPError is any other non-2xx answer, with the hub's {"error"} text.
type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string { return e.Msg }

// Client talks to one hub. Its methods are safe for concurrent use (a mutex
// guards the session). A refused dial comes back wrapping a
// *netguard.RefusedError.
type Client struct {
	hub       string
	session   *Session
	dialer    netguard.Dialer
	onSession func(Session)
}

// New makes a client for hub (already normalised) that dials only inside
// allowed. s may be nil before Login. onSession, when not nil, is called
// with the new session whenever the hub sets the cookie (Login, and the
// daily renewal on any request), so the caller can save it.
func New(hub string, allowed netguard.Set, s *Session, onSession func(Session)) (*Client, error) {
	return nil, errNotBuilt
}

// Hub is the client's hub URL.
func (c *Client) Hub() string { return c.hub }

// Login sends POST /api/login and, on 204, returns the new session (the
// client uses it from then on). The caller saves it.
func (c *Client) Login(ctx context.Context, password string) (Session, error) {
	return Session{}, errNotBuilt
}

// Logout sends POST /api/logout and forgets the session.
func (c *Client) Logout(ctx context.Context) error { return errNotBuilt }

// CheckSession is GET /api/session.
func (c *Client) CheckSession(ctx context.Context) (login, authenticated bool, err error) {
	return false, false, errNotBuilt
}

// State is GET /api/state.
func (c *Client) State(ctx context.Context) (api.State, error) { return api.State{}, errNotBuilt }

// Epics is GET /api/epics.
func (c *Client) Epics(ctx context.Context) (api.Epics, error) { return api.Epics{}, errNotBuilt }

// Act is POST /api/actions with one action and returns the stored action
// (with `was` for an edit, and the hub's record of a repeating done). a.ID
// is filled with a new UUID when empty.
func (c *Client) Act(ctx context.Context, a api.NewAction) (api.Action, error) {
	return api.Action{}, errNotBuilt
}

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

// Events follows GET /api/events until ctx ends, calling fn from one
// goroutine. It reconnects after a drop with backoff from 1 s to 30 s,
// waiting Retry-After on a 503. On a 401, or a drop followed by
// CheckSession reporting not authenticated, it sends EventAuth and returns
// ErrAuth. Other calls (State, Epics, Act) return a 401 as ErrAuth; the
// caller checks the session, asks for the password, then starts Events
// again.
func (c *Client) Events(ctx context.Context, fn func(Event)) error { return errNotBuilt }

var errNotBuilt = errors.New("appclient: not built yet")
