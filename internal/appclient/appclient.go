// Package appclient is the terminal viewer's client for the hub's app API
// (docs/api.md, App API): it logs in with the hub's password, keeps the
// session cookie in a file, reads the state and the epics, follows the event
// stream and sends actions. It dials only through netguard.Dialer, as the
// bridge does, so the password never goes to a host outside the private
// network.
package appclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// The hub's two names for its session cookie (internal/hub, session.go).
const (
	sessionCookie       = "meat_mode_session"        // on a plain http:// hub
	secureSessionCookie = "__Host-meat_mode_session" // on an https:// hub
)

// callTimeout bounds one ordinary request; the event stream has none.
const callTimeout = 30 * time.Second

// NormalizeHub turns what the user typed into the form Session.Hub keeps:
// https:// when no scheme is given, no trailing slash or path. It refuses a
// scheme other than http and https, and http other than localhost,
// 127.0.0.1 and [::1], as the hub does (docs/api.md, App API). An explicit
// port is kept, a default one dropped. The hub answers 421 to a Host other
// than localhost, its public URL's host or MEAT_MODE_ALLOWED_HOSTS, so a hub
// typed as a bare IP address can fail there; HTTPError 421 says so.
func NormalizeHub(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("give the hub's URL, such as https://hub.example.com")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("%q is not a hub URL such as https://hub.example.com", raw)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("the hub's URL must start with https://, not %s://", u.Scheme)
	}
	if u.User != nil {
		return "", errors.New("leave the user name and password out of the hub's URL")
	}
	host := strings.ToLower(u.Hostname())
	if scheme == "http" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return "", fmt.Errorf("the hub takes plain http:// only on localhost; use https://%s", u.Host)
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("%q is not a port", port)
		}
		if scheme == "https" && n == 443 || scheme == "http" && n == 80 {
			port = ""
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host, nil
}

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

	mu   sync.Mutex // guards session
	http *http.Client
	// sleep waits between event stream attempts; tests replace it so they
	// do not wait for seconds.
	sleep func(ctx context.Context, d time.Duration) error
	// idle is how long an open event stream may stay silent before it
	// counts as dropped; the hub sends a heartbeat every 25 seconds.
	idle time.Duration
}

// New makes a client for hub (already normalised) that dials only inside
// allowed. s may be nil before Login. onSession, when not nil, is called
// with the new session whenever the hub sets the cookie (Login, and the
// daily renewal on any request), so the caller can save it.
func New(hub string, allowed netguard.Set, s *Session, onSession func(Session)) (*Client, error) {
	n, err := NormalizeHub(hub)
	if err != nil {
		return nil, err
	}
	c := &Client{
		hub:       n,
		dialer:    netguard.Dialer{Allowed: allowed},
		onSession: onSession,
		sleep:     sleepCtx,
		idle:      70 * time.Second,
	}
	if s != nil {
		cp := *s
		cp.Hub = n
		c.session = &cp
	}
	// The client never follows a redirect, which would carry the cookie or
	// the password to wherever the Location points, and never uses a proxy
	// from the environment, which would dial for it.
	c.http = &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           c.dial,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c, nil
}

// dial goes through netguard.Dialer, which refuses a hub outside the allowed
// set and dials the address it checked. A plain http hub is allowed only on
// this machine, so it is dialed only at a loopback address: "localhost" is
// never looked up, since a resolver could answer it with another private
// address and the password would cross the network in clear text.
func (c *Client) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if !strings.HasPrefix(c.hub, "http://") {
		return c.dialer.DialContext(ctx, network, addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []string
	if host == "localhost" {
		ips = []string{"127.0.0.1", "::1"}
	} else if ip, err := netip.ParseAddr(host); err == nil && ip.Unmap().IsLoopback() {
		ips = []string{ip.String()}
	} else {
		return nil, fmt.Errorf("refusing the hub at %s: plain http is allowed only on this machine", host)
	}
	var last error
	for _, ip := range ips {
		conn, err := c.dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

// Hub is the client's hub URL.
func (c *Client) Hub() string { return c.hub }

// current returns a copy of the session, or nil.
func (c *Client) current() *Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return nil
	}
	cp := *c.session
	return &cp
}

// request makes one request with the session cookie when withCookie, and
// takes a cookie the hub sets. body, when not nil, is sent as JSON.
func (c *Client) request(ctx context.Context, method, path string, body []byte, withCookie bool) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.hub+path, r)
	if err != nil {
		return nil, err
	}
	// Every write is JSON, a bodiless one too: the hub refuses any other
	// type on a non-GET /api/ route. No Origin header: the hub checks one
	// only when it is there.
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	if withCookie {
		if s := c.current(); s != nil && s.Cookie != "" {
			req.AddCookie(&http.Cookie{Name: s.CookieName, Value: s.Cookie})
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	c.takeCookie(resp)
	return resp, nil
}

// takeCookie keeps a session cookie the hub set, under either name, and
// tells onSession. A cookie being cleared (logout) is not kept.
func (c *Client) takeCookie(resp *http.Response) {
	for _, ck := range resp.Cookies() {
		if ck.Name != sessionCookie && ck.Name != secureSessionCookie {
			continue
		}
		if ck.Value == "" || ck.MaxAge < 0 {
			continue
		}
		s := Session{Hub: c.hub, CookieName: ck.Name, Cookie: ck.Value}
		c.mu.Lock()
		c.session = &s
		c.mu.Unlock()
		if c.onSession != nil {
			c.onSession(s)
		}
		return
	}
}

// call makes one request with a JSON body from in when it is not nil, and
// decodes a JSON reply into out when out is not nil.
func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	} else if method != http.MethodGet {
		body = []byte{}
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	resp, err := c.request(ctx, method, path, body, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if err := statusError(resp, data); err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("the hub's reply to %s %s is not what this viewer reads: %w", method, path, err)
		}
	}
	return nil
}

// statusError maps a non-2xx reply to ErrAuth, RetryError or HTTPError, and
// a 2xx one to nil.
func statusError(resp *http.Response, data []byte) error {
	code := resp.StatusCode
	if code/100 == 2 {
		return nil
	}
	if code/100 == 3 {
		return &HTTPError{Status: code, Msg: fmt.Sprintf(
			"the hub replied %d with a redirect to %q; the viewer does not follow redirects, so give the final address",
			code, safeMessage(resp.Header.Get("Location")))}
	}
	msg := hubMessage(data)
	if msg == "" {
		msg = http.StatusText(code)
	}
	switch code {
	case http.StatusUnauthorized:
		return ErrAuth
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return &RetryError{Status: code, After: retryAfter(resp.Header.Get("Retry-After")), Msg: msg}
	}
	return &HTTPError{Status: code, Msg: fmt.Sprintf("the hub replied %d: %s", code, msg)}
}

// maxMessageRunes is the most of the hub's text an error message carries;
// anything longer is cut, so a huge body cannot flood the terminal.
const maxMessageRunes = 2000

// safeMessage makes one line of the hub's text safe to print: no control
// or bidi character survives, and it stops at maxMessageRunes runes.
func safeMessage(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= maxMessageRunes {
			break
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// hubMessage is the hub's {"error"} text, or the body as it is, made
// safe to print.
func hubMessage(data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return safeMessage(e.Error)
	}
	return safeMessage(string(data))
}

// retryAfter reads Retry-After as seconds or an HTTP date; 0 when absent
// or unreadable. It is clamped to 5 minutes either way: a hub that asks
// for a century, or names a date that far off, would otherwise overflow
// the Duration the caller multiplies it into.
func retryAfter(v string) int {
	const maxWait = 300
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		return min(max(n, 0), maxWait)
	}
	if t, err := http.ParseTime(v); err == nil {
		return min(max(int(math.Ceil(time.Until(t).Seconds())), 0), maxWait)
	}
	return 0
}

// Login sends POST /api/login and, on 204, returns the new session (the
// client uses it from then on). The caller saves it.
func (c *Client) Login(ctx context.Context, password string) (Session, error) {
	body, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		return Session{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	resp, err := c.request(ctx, http.MethodPost, "/api/login", body, false)
	if err != nil {
		return Session{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Session{}, err
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return Session{}, WrongPassword
	case http.StatusNotFound:
		return Session{}, ErrNoPassword
	}
	if err := statusError(resp, data); err != nil {
		return Session{}, err
	}
	s := c.current()
	if s == nil || !hasSessionCookie(resp) {
		return Session{}, errors.New("the hub took the password but set no session cookie")
	}
	return *s, nil
}

func hasSessionCookie(resp *http.Response) bool {
	for _, ck := range resp.Cookies() {
		if (ck.Name == sessionCookie || ck.Name == secureSessionCookie) && ck.Value != "" {
			return true
		}
	}
	return false
}

// Logout sends POST /api/logout and forgets the session.
func (c *Client) Logout(ctx context.Context) error {
	err := c.call(ctx, http.MethodPost, "/api/logout", nil, nil)
	c.mu.Lock()
	c.session = nil
	c.mu.Unlock()
	if errors.Is(err, ErrAuth) {
		return nil // the session had already ended
	}
	return err
}

// CheckSession is GET /api/session.
func (c *Client) CheckSession(ctx context.Context) (login, authenticated bool, err error) {
	var v struct {
		Login         bool `json:"login"`
		Authenticated bool `json:"authenticated"`
	}
	if err := c.call(ctx, http.MethodGet, "/api/session", nil, &v); err != nil {
		return false, false, err
	}
	return v.Login, v.Authenticated, nil
}

// State is GET /api/state.
func (c *Client) State(ctx context.Context) (api.State, error) {
	var s api.State
	err := c.call(ctx, http.MethodGet, "/api/state", nil, &s)
	return s, err
}

// Epics is GET /api/epics.
func (c *Client) Epics(ctx context.Context) (api.Epics, error) {
	var e api.Epics
	err := c.call(ctx, http.MethodGet, "/api/epics", nil, &e)
	return e, err
}

// Act is POST /api/actions with one action and returns the stored action
// (with `was` for an edit, and the hub's record of a repeating done). a.ID
// is filled with a new UUID when empty.
func (c *Client) Act(ctx context.Context, a api.NewAction) (api.Action, error) {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	var out api.Action
	err := c.call(ctx, http.MethodPost, "/api/actions", a, &out)
	return out, err
}

// sleepCtx waits d or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
