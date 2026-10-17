package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/auth"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Login settings (docs/api.md, Login).
const (
	sessionCookie = "meat_mode_session"
	sessionTTL    = 90 * 24 * time.Hour
	loginFailures = 5           // failed logins allowed per loginWindow
	loginWindow   = time.Minute // for the whole hub: a proxy hides client addresses
	maxLoginBody  = 4 << 10
)

// login is the hub's password. A nil *login means the hub has no login.
type login struct {
	hash *auth.Hash
	tag  string // names the hash, so a new password ends the old sessions
	env  bool   // set by MEAT_MODE_PASSWORD_HASH or _FILE, not in the app
}

func newLogin(phc string, env bool) *login {
	if phc == "" {
		return nil
	}
	h, err := auth.ParseHash(phc)
	if err != nil {
		panic(err) // Config.Validate and the store refuse it before it gets here
	}
	sum := sha256.Sum256([]byte(phc))
	return &login{hash: h, tag: hex.EncodeToString(sum[:16]), env: env}
}

// currentLogin is the hub's password now, or nil. Claiming the hub and
// changing the password replace it while the hub runs.
func (s *Server) currentLogin() *login {
	s.loginMu.RLock()
	defer s.loginMu.RUnlock()
	return s.login
}

func (s *Server) setLogin(l *login) {
	s.loginMu.Lock()
	s.login = l
	s.loginMu.Unlock()
}

// limiter counts failed tries within the last loginWindow, for the whole
// hub.
type limiter struct {
	mu    sync.Mutex
	fails []time.Time
}

// retryAfter is how long until another try may be made, or 0.
func (l *limiter) retryAfter(now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	for len(l.fails) > 0 && now.Sub(l.fails[0]) >= loginWindow {
		l.fails = l.fails[1:]
	}
	if len(l.fails) < loginFailures {
		return 0
	}
	return l.fails[0].Add(loginWindow).Sub(now)
}

func (l *limiter) failed(now time.Time) {
	l.mu.Lock()
	l.fails = append(l.fails, now)
	l.mu.Unlock()
}

// attempt runs check unless too many recent tries failed under lim, and
// returns how long to wait when they did. check reports a wrong secret as
// false, which counts as a failure. Tries run one at a time, so requests
// sent together cannot all slip past the limit, and only one Argon2 run at
// a time bounds memory.
func (s *Server) attempt(lim *limiter, check func() (bool, error)) (ok bool, wait time.Duration, err error) {
	s.verify.Lock()
	defer s.verify.Unlock()
	if wait := lim.retryAfter(s.now()); wait > 0 {
		return false, wait, nil
	}
	ok, err = check()
	if err == nil && !ok {
		lim.failed(s.now())
	}
	return ok, 0, err
}

// tooMany answers 429 with Retry-After.
func tooMany(w http.ResponseWriter, wait time.Duration, msg string) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	writeError(w, http.StatusTooManyRequests, msg)
}

type sessionKey struct{}

// sessionOf returns the session the gate found for r, or nil (login off).
func sessionOf(r *http.Request) *store.Session {
	s, _ := r.Context().Value(sessionKey{}).(*store.Session)
	return s
}

// gate lets an /api/ request through only with a valid session while login
// is on, and returns the request carrying that session. Pages, the bridge
// API and /healthz are not gated.
func (s *Server) gate(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	p := r.URL.Path
	l := s.currentLogin()
	if l == nil || !strings.HasPrefix(p, "/api/") {
		return r, true
	}
	switch p {
	case "/api/session", "/api/login", "/api/logout", "/api/setup/claim":
		return r, true
	}
	sess, err := s.session(w, r, l)
	if err != nil {
		s.storeError(w, r, err)
		return r, false
	}
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "log in first")
		return r, false
	}
	return r.WithContext(context.WithValue(r.Context(), sessionKey{}, sess)), true
}

// session returns r's valid session under l, or nil, sending the cookie
// again when its expiry moved.
func (s *Server) session(w http.ResponseWriter, r *http.Request, l *login) (*store.Session, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, nil
	}
	now := s.now()
	sess, err := s.store.CheckSession(c.Value, l.tag, now, sessionTTL)
	if err != nil || sess == nil {
		return nil, err
	}
	if sess.Renewed {
		s.setSessionCookie(w, c.Value, sess.ExpiresAt.Sub(now))
	}
	return sess, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	maxAge := int(ttl / time.Second)
	if ttl < 0 {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   strings.HasPrefix(strings.ToLower(s.cfg.PublicURL), "https://"),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	authed := true
	l := s.currentLogin()
	if l != nil {
		sess, err := s.session(w, r, l)
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		authed = sess != nil
	}
	writeJSON(w, http.StatusOK, map[string]bool{"login": l != nil, "authenticated": authed})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	l := s.currentLogin()
	if l == nil {
		writeError(w, http.StatusNotFound, "this hub has no login")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, maxLoginBody, &body) {
		return
	}
	ok, wait, _ := s.attempt(&s.loginFails, func() (bool, error) { return l.hash.Verify(body.Password), nil })
	if wait > 0 {
		tooMany(w, wait, "too many wrong passwords; try again in a minute")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "wrong password")
		return
	}
	if err := s.startSession(w, l); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startSession logs this browser in under l: a new session and its cookie.
func (s *Server) startSession(w http.ResponseWriter, l *login) error {
	token, sess, err := s.store.CreateSession(l.tag, s.now(), sessionTTL)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, token, sess.ExpiresAt.Sub(s.now()))
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.store.DeleteSession(c.Value); err != nil {
			s.storeError(w, r, err)
			return
		}
		if s.push != nil {
			if err := s.push.DropSession(r.Context(), store.SessionID(c.Value)); err != nil {
				s.storeError(w, r, err)
				return
			}
		}
		s.setSessionCookie(w, "", -1)
	}
	w.WriteHeader(http.StatusNoContent)
}
