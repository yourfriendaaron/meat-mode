package hub

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Device methods of fakePush: device "dev1" has token "mmd_good", and the
// confirmation code "right" confirms any device. Every device belongs to
// the session owner.

const fakeDevice = "0123456789abcdef0123456789abcdef"

func (f *fakePush) RegisterApp(_ context.Context, sessionID string, s api.PushSubscription) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, renewed := f.subs[s.Endpoint]
	f.subs[s.Endpoint] = s
	return fakeDevice, renewed, nil
}

func (f *fakePush) ConfirmDevice(_ context.Context, id, code string) (string, error) {
	if code != "right" {
		return "", fmt.Errorf("%w: wrong code", store.ErrForbidden)
	}
	return "mmd_good", nil
}

// DeviceByToken reports every device as registered by the session owner.
func (f *fakePush) DeviceByToken(_ context.Context, token string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.tokens[token]; ok {
		return id, f.owner, nil
	}
	return "", "", store.ErrUnauthorized
}

func (f *fakePush) UpdateDevice(_ context.Context, id string, s api.PushSubscription) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updated == nil {
		f.updated = map[string]api.PushSubscription{}
	}
	f.updated[id] = s
	return nil
}

func (f *fakePush) RevokeDevice(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for tok, d := range f.tokens {
		if d == id {
			delete(f.tokens, tok)
		}
	}
	if id != fakeDevice {
		return fmt.Errorf("%w: no such device", store.ErrNotFound)
	}
	f.revoked = append(f.revoked, id)
	return nil
}

func (f *fakePush) Devices(context.Context) ([]api.Device, error) {
	return []api.Device{{ID: fakeDevice, Name: "Test phone", Kind: api.DeviceApp, Confirmed: true,
		CreatedAt: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), LastSeen: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)}}, nil
}

func (f *fakePush) giveToken(token, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokens == nil {
		f.tokens = map[string]string{}
	}
	f.tokens[token] = id
}

// deviceDo sends a request as the app's background code does: a device
// token, JSON, no Origin, and optionally a session cookie it should not
// have.
func deviceDo(t *testing.T, srv *Server, method, path, body, token, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost:8080"
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestDeviceTokenOpensOnlyItsRoutes(t *testing.T) {
	h := newHarness(t, nil, true)
	h.pair()
	h.push.giveToken("mmd_good", fakeDevice)
	key := "laptop/garden/gd-1"
	action := func(id, kind string) string {
		return fmt.Sprintf(`{"id":%q,"kind":%q,"key":%q}`, id, kind, key)
	}

	// Done and Snooze from a notification go through.
	w := deviceDo(t, h.srv, "POST", "/api/actions", action("0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a71", api.KindDone), "mmd_good", "")
	h.expect(w, http.StatusCreated)
	snooze := `{"id":"0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a72","kind":"snooze","key":"` + key + `","payload":{"until":"2030-01-01T10:00:00Z","previous":null}}`
	h.expect(deviceDo(t, h.srv, "POST", "/api/actions", snooze, "mmd_good", ""), http.StatusCreated)

	// Any other kind is refused, and nothing is stored for it.
	for i, kind := range []string{api.KindNote, api.KindDismiss, api.KindReopen, api.KindAdd} {
		id := fmt.Sprintf("0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a8%d", i)
		h.expect(deviceDo(t, h.srv, "POST", "/api/actions", action(id, kind), "mmd_good", ""), http.StatusUnauthorized)
	}
	if as := decode[[]api.Action](t, h.do("GET", "/api/actions", nil, "")); len(as) != 2 {
		t.Fatalf("%d actions stored, want the done and the snooze", len(as))
	}

	// It never reads state, and opens no other route.
	for _, rt := range appRoutes {
		if deviceRoute(httptest.NewRequest(rt.method, rt.path, nil)) {
			continue
		}
		if w := deviceDo(t, h.srv, rt.method, rt.path, rt.body, "mmd_good", ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a device token: %d, want 401", rt.method, rt.path, w.Code)
		}
	}
	for _, rt := range [][2]string{{"POST", "/api/logout"}, {"GET", "/api/session"}, {"POST", "/api/devices/" + fakeDevice + "/confirm"}} {
		if w := deviceDo(t, h.srv, rt[0], rt[1], `{}`, "mmd_good", ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a device token: %d, want 401", rt[0], rt[1], w.Code)
		}
	}

	// An unknown or empty token is refused even where login is off.
	for _, tok := range []string{"mmd_unknown", ""} {
		w := deviceDo(t, h.srv, "POST", "/api/actions", action("0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a91", api.KindDone), tok, "")
		h.expect(w, http.StatusUnauthorized)
	}

	// The device renews and revokes itself; a cookie request cannot.
	sub := `{"endpoint":"https://ntfy.example.com/upAbc?up=1","keys":{"p256dh":"k","auth":"a"}}`
	h.expect(deviceDo(t, h.srv, "PUT", "/api/devices/self", sub, "mmd_good", ""), http.StatusNoContent)
	if got := h.push.updated[fakeDevice]; got.Endpoint != "https://ntfy.example.com/upAbc?up=1" {
		t.Fatalf("updated %+v", h.push.updated)
	}
	h.expect(h.do("PUT", "/api/devices/self", sub, ""), http.StatusUnauthorized)
	h.expect(h.do("DELETE", "/api/devices/self", nil, ""), http.StatusUnauthorized)
	h.expect(deviceDo(t, h.srv, "DELETE", "/api/devices/self", "", "mmd_good", ""), http.StatusNoContent)
	if len(h.push.revoked) != 1 || h.push.revoked[0] != fakeDevice {
		t.Fatalf("revoked %v", h.push.revoked)
	}
	// Its token is gone with it.
	h.expect(deviceDo(t, h.srv, "PUT", "/api/devices/self", sub, "mmd_good", ""), http.StatusUnauthorized)
}

func TestDeviceRequestIgnoresTheSessionCookie(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	h.push.giveToken("mmd_good", fakeDevice)
	c := h.login("water the plants")
	h.push.owner = store.SessionID(c.Value)
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusOK {
		t.Fatalf("state with a session: %d", w.Code)
	}
	// A device request carrying a valid cookie still reads nothing.
	if w := deviceDo(t, h.srv, "GET", "/api/state", "", "mmd_good", c.Value); w.Code != http.StatusUnauthorized {
		t.Errorf("state with a device token and a cookie: %d, want 401", w.Code)
	}
	if w := deviceDo(t, h.srv, "GET", "/api/state", "", "mmd_unknown", c.Value); w.Code != http.StatusUnauthorized {
		t.Errorf("state with a bad device token and a cookie: %d, want 401", w.Code)
	}
	// The token opens its routes without a session cookie.
	sub := `{"endpoint":"https://ntfy.example.com/upAbc?up=1","keys":{"p256dh":"k","auth":"a"}}`
	if w := deviceDo(t, h.srv, "PUT", "/api/devices/self", sub, "mmd_good", ""); w.Code != http.StatusNoContent {
		t.Errorf("renew without a cookie: %d %s", w.Code, w.Body)
	}
}

func TestAppRegistrationAndConfirmation(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	app := `{"endpoint":"https://ntfy.example.com/upAbc?up=1","keys":{"p256dh":"k","auth":"a"},"kind":"app","name":"Test phone"}`

	// A new endpoint awaits confirmation; the same one again renews.
	w := h.do("POST", "/api/push/subscriptions", app, c.Value)
	if w.Code != http.StatusAccepted {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	if got := decode[api.DeviceRegistered](t, w); got.Device != fakeDevice {
		t.Fatalf("registered %+v", got)
	}
	if w := h.do("POST", "/api/push/subscriptions", app, c.Value); w.Code != http.StatusNoContent {
		t.Fatalf("renew: %d %s", w.Code, w.Body)
	}
	bad := strings.Replace(app, `"app"`, `"phone"`, 1)
	if w := h.do("POST", "/api/push/subscriptions", bad, c.Value); w.Code != http.StatusBadRequest {
		t.Errorf("unknown kind: %d, want 400", w.Code)
	}

	// The confirm call needs no session: the code is the credential.
	path := "/api/devices/" + fakeDevice + "/confirm"
	if w := h.do("POST", path, `{"code":"wrong"}`, ""); w.Code != http.StatusForbidden {
		t.Errorf("wrong code: %d, want 403", w.Code)
	}
	w = h.do("POST", path, `{"code":"right"}`, "")
	if w.Code != http.StatusOK || decode[api.DeviceToken](t, w).Token != "mmd_good" {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	// Other device routes still need the session.
	if w := h.do("POST", "/api/devices/"+fakeDevice+"/confirm/x", `{}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("a longer path took the confirm exemption: %d", w.Code)
	}

	// Settings lists and revokes devices.
	w = h.do("GET", "/api/devices", "", c.Value)
	if ds := decode[[]api.Device](t, w); w.Code != http.StatusOK || len(ds) != 1 || ds[0].Name != "Test phone" {
		t.Fatalf("devices: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "endpoint") {
		t.Errorf("the device list shows an endpoint: %s", w.Body)
	}
	if w := h.do("DELETE", "/api/devices/"+fakeDevice, "", c.Value); w.Code != http.StatusNoContent {
		t.Errorf("revoke: %d", w.Code)
	}
	if w := h.do("DELETE", "/api/devices/ffffffffffffffffffffffffffffffff", "", c.Value); w.Code != http.StatusNotFound {
		t.Errorf("revoke an unknown device: %d, want 404", w.Code)
	}
}

func TestDeviceRoutesWithoutPush(t *testing.T) {
	h := newHarness(t, nil, false)
	h.expect(h.do("GET", "/api/devices", nil, ""), http.StatusNotFound)
	h.expect(h.do("POST", "/api/devices/"+fakeDevice+"/confirm", `{"code":"x"}`, ""), http.StatusNotFound)
	h.expect(h.do("POST", "/api/push/subscriptions", `{"endpoint":"https://ntfy.example.com/up","keys":{"p256dh":"k","auth":"a"},"kind":"app"}`, ""), http.StatusNotFound)
	h.expect(deviceDo(t, h.srv, "POST", "/api/actions", `{}`, "mmd_good", ""), http.StatusUnauthorized)
}
