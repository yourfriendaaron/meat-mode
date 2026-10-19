package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
	"github.com/yourfriendaaron/meat-mode/web"
)

// PushRegistry keeps Web Push subscriptions. internal/notify provides it.
type PushRegistry interface {
	PublicKey() string
	// Subscribe stores s for the session sessionID.
	Subscribe(ctx context.Context, sessionID string, s api.PushSubscription) error
	Unsubscribe(ctx context.Context, endpoint string) error
	// DropSession removes the subscriptions a session registered.
	DropSession(ctx context.Context, sessionID string) error
}

// ReminderTester sends a test reminder to the calling device's subscription
// and to ntfy. The PushRegistry from internal/notify provides it.
type ReminderTester interface {
	TestReminder(ctx context.Context, sessionID, endpoint, url string) api.ReminderTest
}

// Body size limits.
const (
	maxBody      = 1 << 20  // every JSON body except an item set
	maxItemsBody = 16 << 20 // PUT /bridge/v1/items: a full repo with comments
)

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; font-src 'self'; manifest-src 'self'; worker-src 'self'; " +
	"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// Server is the hub's HTTP handler.
type Server struct {
	cfg       Config
	store     *store.Store
	push      PushRegistry
	events    *broker
	guard     *guard
	now       func() time.Time
	heartbeat time.Duration
	mux       *http.ServeMux
	log       *slog.Logger

	lookup   dnsLookup     // public DNS, for the public DNS check
	dnsEvery time.Duration // between public DNS checks after start
	dnsMu    sync.Mutex
	dns      api.PublicDNS // the latest public DNS check; read it with publicDNS

	loginMu    sync.RWMutex
	login      *login     // nil: the hub has no login; read it with currentLogin
	verify     sync.Mutex // one password or setup code check at a time
	loginFails limiter    // wrong passwords
	codeFails  limiter    // wrong setup codes
}

// NewServer builds the handler. push may be nil, which turns the push
// routes off (404).
func NewServer(cfg Config, st *store.Store, push PushRegistry) *Server {
	static, err := fs.Sub(web.Static, "static")
	if err != nil {
		panic(err) // the embed always holds static/
	}
	return newServer(cfg, st, push, static)
}

func newServer(cfg Config, st *store.Store, push PushRegistry, static fs.FS) *Server {
	if cfg.User == "" {
		cfg.User = "user"
	}
	s := &Server{
		cfg:       cfg,
		store:     st,
		push:      push,
		events:    newBroker(),
		guard:     newGuard(cfg),
		now:       time.Now,
		heartbeat: 25 * time.Second,
		mux:       http.NewServeMux(),
		log:       slog.Default(),
		lookup:    lookupPublic,
		dnsEvery:  dnsEvery,
		dns:       api.PublicDNS{State: api.DNSUnchecked},
	}
	s.login = newLogin(cfg.PasswordHash, true)
	if s.login == nil {
		h, err := st.Settings()
		if err != nil {
			panic(err) // Run reads the settings before it builds the server
		}
		s.login = newLogin(h.PasswordHash, false)
	}
	st.Watch(func(store.Change) { s.events.publish() })

	m := s.mux
	m.HandleFunc("GET /api/session", s.handleSession)
	m.HandleFunc("POST /api/login", s.handleLogin)
	m.HandleFunc("POST /api/logout", s.handleLogout)
	m.HandleFunc("GET /api/state", s.handleState)
	m.HandleFunc("GET /api/events", s.handleEvents)
	m.HandleFunc("POST /api/actions", s.handleNewAction)
	m.HandleFunc("GET /api/actions", s.handleListActions)
	m.HandleFunc("POST /api/actions/{id}/cancel", s.handleCancel)
	m.HandleFunc("POST /api/items/{key}/seen", s.handleSeen)
	m.HandleFunc("GET /api/push/key", s.handlePushKey)
	m.HandleFunc("POST /api/push/subscriptions", s.handlePushSubscribe)
	m.HandleFunc("DELETE /api/push/subscriptions", s.handlePushUnsubscribe)
	m.HandleFunc("GET /api/bridges", s.handleListBridges)
	m.HandleFunc("POST /api/bridges", s.handleNewBridge)
	m.HandleFunc("DELETE /api/bridges/{name}", s.handleRevokeBridge)
	// Features in their own files register their routes here, one call each.
	s.setupRoutes(m)
	s.settingsRoutes(m)
	s.passwordRoutes(m)
	s.remindersRoutes(m)
	s.calendarRoutes(m)
	s.devicesRoutes(m)

	m.Handle("POST /bridge/v1/hello", s.bridgeAuth(s.handleHello))
	m.Handle("PUT /bridge/v1/repos", s.bridgeAuth(s.handlePutRepos))
	m.Handle("PUT /bridge/v1/items", s.bridgeAuth(s.handlePutItems))
	m.Handle("GET /bridge/v1/actions", s.bridgeAuth(s.handlePullActions))
	m.Handle("POST /bridge/v1/actions/{id}/result", s.bridgeAuth(s.handleResult))
	m.Handle("PUT /bridge/v1/runs", s.bridgeAuth(s.handlePutRuns))
	m.Handle("POST /bridge/v1/personal", s.tokenAuth(s.handleAddPersonal))
	m.Handle("GET /bridge/v1/personal/{id}", s.tokenAuth(s.handleGetPersonal))
	m.Handle("POST /bridge/v1/personal/{id}/actions", s.tokenAuth(s.handleChangePersonal))

	m.HandleFunc("GET /healthz", s.handleHealth)
	staticH := newStatic(static)
	m.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) || strings.HasPrefix(r.URL.Path, "/bridge/") {
			writeError(w, http.StatusNotFound, "no such route")
			return
		}
		staticH.ServeHTTP(w, r)
	})
	return s
}

// ServeHTTP adds security headers, applies the guard and the login gate,
// and dispatches to the routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	if !s.guard.check(w, r) {
		return
	}
	r, ok := s.gate(w, r)
	if !ok {
		return
	}
	s.mux.ServeHTTP(w, r)
}

// Close ends open event streams so a graceful shutdown can finish.
func (s *Server) Close() { s.events.close() }

// --- helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// readJSON decodes a JSON body of at most limit bytes into v. Anything but
// whitespace after the first JSON value is refused.
func readJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		} else {
			writeError(w, http.StatusBadRequest, "request body is not valid JSON: "+err.Error())
		}
		return false
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must hold exactly one JSON value")
		return false
	}
	return true
}

// storeError writes the HTTP response for an error from the store.
func (s *Server) storeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "the hub hit an internal error; see its log")
	}
}

// --- app API

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.State()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	st.User, err = s.userName()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleNewAction(w http.ResponseWriter, r *http.Request) {
	var na api.NewAction
	if !readJSON(w, r, maxBody, &na) {
		return
	}
	if deviceOf(r) != "" && !deviceKinds[na.Kind] {
		writeError(w, http.StatusUnauthorized, "a device token posts only ask, done and snooze")
		return
	}
	if deviceOf(r) != "" && na.Kind == api.KindAsk && strings.TrimSpace(na.Payload.Text) != "" {
		// A notification's Retry wakes the agent; words for it come from the app.
		writeError(w, http.StatusUnauthorized, "a device token asks without a note")
		return
	}
	user, err := s.userName()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	a, created, err := s.store.AppendAction(na, user+"-app")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, a)
}

func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive whole number")
			return
		}
		limit = min(n, 500)
	}
	as, err := s.store.RecentActions(limit)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.CancelAction(r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleSeen(w http.ResponseWriter, r *http.Request) {
	if err := s.store.MarkSeen(r.PathValue("key")); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, http.StatusNotFound, "push notifications are not set up on this hub")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": s.push.PublicKey()})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, http.StatusNotFound, "push notifications are not set up on this hub")
		return
	}
	var sub api.PushSubscription
	if !readJSON(w, r, maxBody, &sub) {
		return
	}
	if !strings.HasPrefix(sub.Endpoint, "https://") || sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "subscription needs an https endpoint and its p256dh and auth keys")
		return
	}
	var sessionID string
	if sess := sessionOf(r); sess != nil {
		sessionID = sess.ID
	}
	switch sub.Kind {
	case "", api.DeviceWeb:
	case api.DeviceApp:
		s.registerApp(w, r, sessionID, sub)
		return
	default:
		writeError(w, http.StatusBadRequest, `kind must be "web" or "app"`)
		return
	}
	if err := s.push.Subscribe(r.Context(), sessionID, sub); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, http.StatusNotFound, "push notifications are not set up on this hub")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	if body.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "endpoint is required")
		return
	}
	if err := s.push.Unsubscribe(r.Context(), body.Endpoint); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListBridges(w http.ResponseWriter, r *http.Request) {
	bs, err := s.store.Bridges()
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, bs)
}

// handleNewBridge adds a machine. Its token can wake agents, so it needs the
// password entered within reauthWindow in this session, or in the body
// (docs/api.md, Login, Password again).
func (s *Server) handleNewBridge(w http.ResponseWriter, r *http.Request) {
	var body api.NewBridge
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	if !s.passwordAgain(w, r, body.Password) {
		return
	}
	token, err := s.store.CreateBridge(body.Name)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, api.NewBridgeReply{Name: body.Name, Token: token})
}

func (s *Server) handleRevokeBridge(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RevokeBridge(r.PathValue("name")); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB().PingContext(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}

// --- bridge API

// bridgeAuth checks the bearer token, records the bridge's last-seen time
// and passes its name on.
func (s *Server) bridgeAuth(next func(http.ResponseWriter, *http.Request, string)) http.Handler {
	return s.authToken(next, true)
}

// tokenAuth checks the bearer token and passes the bridge's name on without
// touching its last-seen time: an agent using the token says nothing about
// whether the bridge runs.
func (s *Server) tokenAuth(next func(http.ResponseWriter, *http.Request, string)) http.Handler {
	return s.authToken(next, false)
}

func (s *Server) authToken(next func(http.ResponseWriter, *http.Request, string), touch bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="meat-mode"`)
			writeError(w, http.StatusUnauthorized, "missing bridge token")
			return
		}
		name, err := s.store.Authenticate(strings.TrimSpace(token))
		if err != nil {
			if errors.Is(err, store.ErrUnauthorized) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="meat-mode", error="invalid_token"`)
				writeError(w, http.StatusUnauthorized, "unknown or revoked bridge token")
				return
			}
			s.storeError(w, r, err)
			return
		}
		if touch {
			if err := s.store.Touch(name); err != nil {
				s.log.Warn("record bridge last-seen", "bridge", name, "err", err)
			}
		}
		next(w, r, name)
	})
}

func (s *Server) handleHello(w http.ResponseWriter, r *http.Request, bridge string) {
	var h api.Hello
	if !readJSON(w, r, maxBody, &h) {
		return
	}
	if err := s.store.Hello(bridge, h); err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.HelloReply{Bridge: bridge, ServerTime: time.Now().UTC()})
}

func (s *Server) handlePutRepos(w http.ResponseWriter, r *http.Request, bridge string) {
	var repos []api.Repo
	if !readJSON(w, r, maxBody, &repos) {
		return
	}
	if err := s.store.ReplaceRepos(bridge, repos); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePutItems(w http.ResponseWriter, r *http.Request, bridge string) {
	// The repo is a query parameter because it is a path (projects/garden),
	// and proxies may decode a %2F in the URL path.
	repo := r.URL.Query().Get("repo")
	if !store.ValidRepoName(repo) {
		writeError(w, http.StatusBadRequest, "repo name is not valid")
		return
	}
	var items []api.Item
	if !readJSON(w, r, maxItemsBody, &items) {
		return
	}
	if err := s.store.ReplaceItems(bridge, repo, items); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePullActions(w http.ResponseWriter, r *http.Request, bridge string) {
	as, err := s.store.PendingActions(bridge)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

func (s *Server) handleResult(w http.ResponseWriter, r *http.Request, bridge string) {
	var res api.ActionResult
	if !readJSON(w, r, maxBody, &res) {
		return
	}
	if err := s.store.ReportResult(bridge, r.PathValue("id"), res); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePutRuns(w http.ResponseWriter, r *http.Request, bridge string) {
	var runs []api.RunReport
	if !readJSON(w, r, maxBody, &runs) {
		return
	}
	if err := s.store.ReplaceRuns(bridge, runs); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAddPersonal lets an agent add to the personal list with any bridge
// token, with no bridge running.
func (s *Server) handleAddPersonal(w http.ResponseWriter, r *http.Request, bridge string) {
	var t api.PersonalTodo
	if !readJSON(w, r, maxBody, &t) {
		return
	}
	key, created, err := s.store.AddPersonal(t, bridge)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	_, _, id, _ := store.SplitKey(key)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, api.PersonalTodoReply{Key: key, ID: id})
}

// handleGetPersonal lets an agent read a personal to-do its machine added.
func (s *Server) handleGetPersonal(w http.ResponseWriter, r *http.Request, bridge string) {
	it, err := s.store.PersonalItem(bridge, r.PathValue("id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// handleChangePersonal lets an agent change a personal to-do its machine
// added.
func (s *Server) handleChangePersonal(w http.ResponseWriter, r *http.Request, bridge string) {
	var c api.PersonalChange
	if !readJSON(w, r, maxBody, &c) {
		return
	}
	a, created, err := s.store.ChangePersonal(bridge, r.PathValue("id"), c)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, a)
}
