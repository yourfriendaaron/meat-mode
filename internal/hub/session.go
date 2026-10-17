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

// login holds the password and the failed-login count. A nil *login means
// the hub has no login.
type login struct {
	hash *auth.Hash
	tag  string // names the hash, so a new password ends the old sessions

	verify sync.Mutex // one Argon2 run at a time bounds memory

	mu    sync.Mutex
	fails []time.Time // within the last loginWindow
}

func newLogin(phc string) *login {
	if phc == "" {
		return nil
	}
	h, err := auth.ParseHash(phc)
	if err != nil {
		panic(err) // Config.Validate refuses it before the hub starts
	}
	sum := sha256.Sum256([]byte(phc))
	return &login{hash: h, tag: hex.EncodeToString(sum[:16])}
}

// retryAfter is how long until another login may be tried, or 0.
func (l *login) retryAfter(now time.Time) time.Duration {
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

func (l *login) failed(now time.Time) {
	l.mu.Lock()
	l.fails = append(l.fails, now)
	l.mu.Unlock()
}

// attempt checks password unless too many recent tries failed, and returns
// how long to wait when they did. Tries run one at a time, so requests sent
// together cannot all slip past the limit.
func (l *login) attempt(password string, now func() time.Time) (ok bool, wait time.Duration) {
	l.verify.Lock()
	defer l.verify.Unlock()
	if wait := l.retryAfter(now()); wait > 0 {
		return false, wait
	}
	if l.hash.Verify(password) {
		return true, 0
	}
	l.failed(now())
	return false, 0
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
	if s.login == nil || !strings.HasPrefix(p, "/api/") {
		return r, true
	}
	switch p {
	case "/api/session", "/api/login", "/api/logout":
		return r, true
	}
	sess, err := s.session(w, r)
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

// session returns r's valid session, or nil, sending the cookie again when
// its expiry moved.
func (s *Server) session(w http.ResponseWriter, r *http.Request) (*store.Session, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, nil
	}
	now := s.now()
	sess, err := s.store.CheckSession(c.Value, s.login.tag, now, sessionTTL)
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
	if s.login != nil {
		sess, err := s.session(w, r)
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		authed = sess != nil
	}
	writeJSON(w, http.StatusOK, map[string]bool{"login": s.login != nil, "authenticated": authed})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.login == nil {
		writeError(w, http.StatusNotFound, "this hub has no login")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, maxLoginBody, &body) {
		return
	}
	ok, wait := s.login.attempt(body.Password, s.now)
	if wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "too many wrong passwords; try again in a minute")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "wrong password")
		return
	}
	token, sess, err := s.store.CreateSession(s.login.tag, s.now(), sessionTTL)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	s.setSessionCookie(w, token, sess.ExpiresAt.Sub(s.now()))
	w.WriteHeader(http.StatusNoContent)
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
