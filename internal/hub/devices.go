package hub

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// DeviceRegistry keeps the devices reminders go to: browsers and the
// Android app (docs/api.md, Devices). The PushRegistry from internal/notify
// provides it.
type DeviceRegistry interface {
	// RegisterApp stores the app's subscription for the session sessionID.
	// renewed reports an endpoint that already
	// belongs to a confirmed device; otherwise id is a new device awaiting
	// confirmation.
	RegisterApp(ctx context.Context, sessionID string, s api.PushSubscription) (id string, renewed bool, err error)
	// ConfirmDevice trades a confirmation code for the device token.
	ConfirmDevice(ctx context.Context, id, code string) (token string, err error)
	// DeviceByToken returns the device a token belongs to and the session
	// that registered it, and records that it was seen;
	// store.ErrUnauthorized when it belongs to none.
	DeviceByToken(ctx context.Context, token string) (id, sessionID string, err error)
	// UpdateDevice replaces a device's endpoint and keys and renews it.
	UpdateDevice(ctx context.Context, id string, s api.PushSubscription) error
	// RevokeDevice removes a device; store.ErrNotFound for an unknown id.
	RevokeDevice(ctx context.Context, id string) error
	Devices(ctx context.Context) ([]api.Device, error)
}

// devicesRoutes registers the device routes (docs/api.md, Devices).
func (s *Server) devicesRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/devices", s.handleListDevices)
	m.HandleFunc("DELETE /api/devices/{id}", s.handleRevokeDevice)
	m.HandleFunc("POST /api/devices/{id}/confirm", s.handleConfirmDevice)
	m.HandleFunc("PUT /api/devices/self", s.handleUpdateSelf)
	m.HandleFunc("DELETE /api/devices/self", s.handleRevokeSelf)
}

type deviceKey struct{}

// deviceOf returns the device whose token the gate found on r, or "".
func deviceOf(r *http.Request) string {
	id, _ := r.Context().Value(deviceKey{}).(string)
	return id
}

// bearerToken returns the token of an Authorization: Bearer header.
func bearerToken(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return strings.TrimSpace(token), ok
}

// deviceRoute reports whether a device token opens the request's route:
// posting an action, and renewing or revoking the device itself. Which
// action kinds it may post, handleNewAction checks.
func deviceRoute(r *http.Request) bool {
	switch r.URL.Path {
	case "/api/actions":
		return r.Method == http.MethodPost
	case "/api/devices/self":
		return r.Method == http.MethodPut || r.Method == http.MethodDelete
	}
	return false
}

// deviceKinds are the action kinds a device token may post: the buttons on
// a notification.
var deviceKinds = map[string]bool{api.KindAsk: true, api.KindDone: true, api.KindSnooze: true}

// confirmPath reports whether p is /api/devices/{id}/confirm.
func confirmPath(p string) bool {
	rest, ok := strings.CutPrefix(p, "/api/devices/")
	if !ok {
		return false
	}
	id, ok := strings.CutSuffix(rest, "/confirm")
	return ok && id != "" && !strings.Contains(id, "/")
}

// deviceGate lets a request with a device token through to the routes the
// token opens, carrying the device, and answers 401 for anything else. It
// never looks at a session cookie. The call counts as use of the session
// that registered the device under l, so the session's idle end moves.
func (s *Server) deviceGate(w http.ResponseWriter, r *http.Request, l *login) (*http.Request, bool) {
	refuse := func(msg string) (*http.Request, bool) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="meat-mode", error="invalid_token"`)
		writeError(w, http.StatusUnauthorized, msg)
		return r, false
	}
	token, _ := bearerToken(r)
	if !deviceRoute(r) {
		return refuse("a device token opens only posting an action and renewing the device")
	}
	devs, ok := s.push.(DeviceRegistry)
	if !ok || token == "" {
		return refuse("unknown or revoked device token")
	}
	id, sessionID, err := devs.DeviceByToken(r.Context(), token)
	if errors.Is(err, store.ErrUnauthorized) {
		return refuse("unknown or revoked device token")
	}
	if err != nil {
		s.storeError(w, r, err)
		return r, false
	}
	live, err := s.store.UseSession(sessionID, l.tag, s.now())
	if err != nil {
		s.storeError(w, r, err)
		return r, false
	}
	if !live {
		// The session ended since the registry last pruned its devices.
		return refuse("unknown or revoked device token")
	}
	return r.WithContext(context.WithValue(r.Context(), deviceKey{}, id)), true
}

// devices returns the device registry, or answers 404 when push is off.
func (s *Server) devices(w http.ResponseWriter) (DeviceRegistry, bool) {
	d, ok := s.push.(DeviceRegistry)
	if !ok {
		writeError(w, http.StatusNotFound, "push notifications are not set up on this hub")
	}
	return d, ok
}

// registerApp handles POST /api/push/subscriptions with kind app.
func (s *Server) registerApp(w http.ResponseWriter, r *http.Request, sessionID string, sub api.PushSubscription) {
	devs, ok := s.devices(w)
	if !ok {
		return
	}
	id, renewed, err := devs.RegisterApp(r.Context(), sessionID, sub)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if renewed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusAccepted, api.DeviceRegistered{Device: id})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devs, ok := s.devices(w)
	if !ok {
		return
	}
	list, err := devs.Devices(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	devs, ok := s.devices(w)
	if !ok {
		return
	}
	if err := devs.RevokeDevice(r.Context(), r.PathValue("id")); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConfirmDevice(w http.ResponseWriter, r *http.Request) {
	devs, ok := s.devices(w)
	if !ok {
		return
	}
	var body api.DeviceConfirm
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	token, err := devs.ConfirmDevice(r.Context(), r.PathValue("id"), body.Code)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.DeviceToken{Token: token})
}

// selfDevice returns the calling device, or answers 401 for a request
// without a device token.
func (s *Server) selfDevice(w http.ResponseWriter, r *http.Request) (DeviceRegistry, string, bool) {
	devs, ok := s.devices(w)
	if !ok {
		return nil, "", false
	}
	id := deviceOf(r)
	if id == "" {
		writeError(w, http.StatusUnauthorized, "this route takes a device token")
		return nil, "", false
	}
	return devs, id, true
}

func (s *Server) handleUpdateSelf(w http.ResponseWriter, r *http.Request) {
	devs, id, ok := s.selfDevice(w, r)
	if !ok {
		return
	}
	var sub api.PushSubscription
	if !readJSON(w, r, maxBody, &sub) {
		return
	}
	if err := devs.UpdateDevice(r.Context(), id, sub); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRevokeSelf(w http.ResponseWriter, r *http.Request) {
	devs, id, ok := s.selfDevice(w, r)
	if !ok {
		return
	}
	if err := devs.RevokeDevice(r.Context(), id); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
