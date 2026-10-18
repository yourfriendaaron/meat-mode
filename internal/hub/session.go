package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/auth"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Login settings (docs/api.md, Login). Session lifetimes are the store's
// (store.SessionIdle, store.SessionLifetime).
const (
	sessionCookie       = "meat_mode_session"        // on a plain http:// hub
	secureSessionCookie = "__Host-meat_mode_session" // on an https:// hub
	loginFailures       = 5                          // failed logins allowed per loginWindow
	loginWindow         = time.Minute                // for the whole hub: a proxy hides client addresses
	reauthWindow        = 10 * time.Minute           // how recent a password must be to add a machine
	maxLoginBody        = 4 << 10
)

// noPassword is the 401 for every /api/ route but setup while the hub has
// no password.
const noPassword = "this hub has no password yet: set one with the setup code"

// login is the hub's password. A nil *login means the hub has no password
// yet.
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

// sessionOf returns the session the gate found for r, or nil (a device
// request, or a route that needs no session).
func sessionOf(r *http.Request) *store.Session {
	s, _ := r.Context().Value(sessionKey{}).(*store.Session)
	return s
}

// gate lets an /api/ request through only with a valid session, and
// returns the request carrying that session. A hub with no password serves
// only its setup routes. A request with a device token goes through
// deviceGate instead. Pages, the bridge API and /healthz are not gated.
func (s *Server) gate(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	p := r.URL.Path
	if !isAPIPath(p) {
		return r, true
	}
	open := false
	switch p {
	case "/api/session", "/api/login", "/api/logout", "/api/setup/claim":
		open = true
	}
	l := s.currentLogin()
	if l == nil {
		if !open {
			writeError(w, http.StatusUnauthorized, noPassword)
		}
		return r, open
	}
	if _, ok := bearerToken(r); ok {
		// A device request; its cookie is ignored.
		return s.deviceGate(w, r, l)
	}
	if open {
		return r, true
	}
	if r.Method == http.MethodPost && confirmPath(p) {
		// The confirmation code is the credential.
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

// cookieName is the session cookie's name for the scheme of
// MEAT_MODE_PUBLIC_URL. The __Host- prefix makes the browser refuse it
// without Secure, with a Domain or off Path=/.
func (s *Server) cookieName() string {
	if s.secureCookie() {
		return secureSessionCookie
	}
	return sessionCookie
}

func (s *Server) secureCookie() bool {
	return strings.HasPrefix(strings.ToLower(s.cfg.PublicURL), "https://")
}

// session returns r's valid session under l, or nil, sending the cookie
// again when its expiry moved.
func (s *Server) session(w http.ResponseWriter, r *http.Request, l *login) (*store.Session, error) {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return nil, nil
	}
	now := s.now()
	sess, err := s.store.CheckSession(c.Value, l.tag, now)
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
		Name:     s.cookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secureCookie(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	authed := false
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
		writeError(w, http.StatusNotFound, "this hub has no password yet")
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

// startSession logs this browser in under l: a new session, with the
// password entered now, and its cookie.
func (s *Server) startSession(w http.ResponseWriter, l *login) error {
	token, sess, err := s.store.CreateSession(l.tag, s.now())
	if err != nil {
		return err
	}
	s.setSessionCookie(w, token, sess.ExpiresAt.Sub(s.now()))
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cookieName()); err == nil {
		if err := s.store.DeleteSession(c.Value); err != nil {
			s.storeError(w, r, err)
			return
		}
		if s.push != nil {
			// The session is already gone; a subscription left behind is
			// pruned once its session is seen to have ended.
			if err := s.push.DropSession(r.Context(), store.SessionID(c.Value)); err != nil {
				slog.Warn("could not drop the reminders of a logged-out session", "err", err)
			}
		}
		s.setSessionCookie(w, "", -1)
	}
	w.WriteHeader(http.StatusNoContent)
}

// endOtherLogins ends the sessions of any password but the current one, or
// every session while the hub has no password, with their reminders. Run calls it once at
// start: a hash changed in the environment leaves the old sessions behind.
func (s *Server) endOtherLogins(ctx context.Context) error {
	tag := ""
	if l := s.currentLogin(); l != nil {
		tag = l.tag
	}
	ended, err := s.store.EndOtherLogins(tag)
	if err != nil {
		return err
	}
	if s.push != nil {
		for _, id := range ended {
			// The session is already gone, so prune catches what this misses.
			if err := s.push.DropSession(ctx, id); err != nil {
				slog.Warn("could not drop the reminders of a logged-out session", "err", err)
			}
		}
	}
	return nil
}

// passwordAgain reports whether r's session may go on to a step that needs
// the password entered recently: it was entered within reauthWindow, or
// password is right, which counts as entering it now. Otherwise it answers
// 403 with "reauth": true, or 429 as for login. A wrong password counts
// with wrong logins.
func (s *Server) passwordAgain(w http.ResponseWriter, r *http.Request, password string) bool {
	sess, l := sessionOf(r), s.currentLogin()
	if sess == nil || l == nil {
		// The gate lets no request this far without both.
		writeError(w, http.StatusUnauthorized, "log in first")
		return false
	}
	if s.now().Sub(sess.VerifiedAt) < reauthWindow {
		return true
	}
	if password == "" {
		reauth(w, "enter your password to add a machine")
		return false
	}
	ok, wait, _ := s.attempt(&s.loginFails, func() (bool, error) { return l.hash.Verify(password), nil })
	if wait > 0 {
		tooMany(w, wait, "too many wrong passwords; try again in a minute")
		return false
	}
	if !ok {
		reauth(w, "wrong password")
		return false
	}
	if err := s.store.VerifySession(sess.ID, s.now()); err != nil {
		s.storeError(w, r, err)
		return false
	}
	return true
}

// reauth answers 403 asking for the password again.
func reauth(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusForbidden, map[string]any{"error": msg, "reauth": true})
}
