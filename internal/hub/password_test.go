package hub

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// mintCode runs the start-up step that makes the setup code and returns
// the code from its file.
func mintCode(t *testing.T, h *loginHarness, cfg Config) string {
	t.Helper()
	if err := mintSetupCode(cfg, h.st); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.DataDir, setupCodeFile)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("setup code file mode %v, want 0600", fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func claimBody(code, password string) string {
	return `{"code":"` + code + `","password":"` + password + `"}`
}

func TestClaimTheHub(t *testing.T) {
	cfg := Config{DataDir: t.TempDir()}
	h := newLoginHarness(t, cfg)
	code := mintCode(t, h, cfg)
	if len(code) != 14 || code[4] != '-' || code[9] != '-' {
		t.Fatalf("setup code %q, want XXXX-XXXX-XXXX", code)
	}
	if err := h.st.SetTailscaleOnly(true); err != nil {
		t.Fatal(err)
	}

	if w := h.do("POST", "/api/setup/claim", claimBody(code, "short"), ""); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "at least 8") {
		t.Errorf("short password: %d %s", w.Code, w.Body)
	}
	if w := h.do("POST", "/api/setup/claim", claimBody("AAAA-BBBB-CCCC", "water the plants"), ""); w.Code != http.StatusForbidden ||
		!strings.Contains(w.Body.String(), "setup code is wrong") {
		t.Errorf("wrong code: %d %s", w.Code, w.Body)
	}

	// Typed without dashes and in lower case, the code still matches.
	typed := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	w := h.do("POST", "/api/setup/claim", claimBody(typed, "water the plants"), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("claim: %d %s", w.Code, w.Body)
	}
	c := responseCookie(t, w)
	if _, err := os.Stat(filepath.Join(cfg.DataDir, setupCodeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("setup code file after the claim: %v, want it removed", err)
	}
	if l, a := sessionReply(t, h.do("GET", "/api/session", "", c.Value)); !l || !a {
		t.Errorf("session after the claim = login %v, authenticated %v; want true, true", l, a)
	}
	if w := h.do("GET", "/api/state", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("another browser after the claim: %d, want 401", w.Code)
	}
	got := decode[api.Setup](t, h.do("GET", "/api/setup", "", c.Value))
	if got.Password != "app" || got.TailscaleOnly {
		t.Errorf("setup after the claim: %+v", got)
	}

	// The code works once, and a hub with a password cannot be claimed.
	if w := h.do("POST", "/api/setup/claim", claimBody(code, "feed the cat"), ""); w.Code != http.StatusConflict {
		t.Errorf("second claim: %d %s, want 409", w.Code, w.Body)
	}

	// After a restart the password from the app is the login, and no new
	// code is made.
	h.srv = h.server(cfg)
	if err := mintSetupCode(cfg, h.st); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, setupCodeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("setup code file on a hub with a password: %v", err)
	}
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusOK {
		t.Errorf("session after a restart: %d", w.Code)
	}
	h.login("water the plants")
}

func TestExpiredSetupCode(t *testing.T) {
	h := newLoginHarness(t, Config{})
	code, err := h.st.NewSetupCode(-time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if w := h.do("POST", "/api/setup/claim", claimBody(code, "water the plants"), ""); w.Code != http.StatusForbidden {
		t.Errorf("expired code: %d %s, want 403", w.Code, w.Body)
	}
	if l, _ := sessionReply(t, h.do("GET", "/api/session", "", "")); l {
		t.Error("an expired code turned login on")
	}
}

func TestWrongSetupCodeLimit(t *testing.T) {
	cfg := Config{DataDir: t.TempDir()}
	h := newLoginHarness(t, cfg)
	code := mintCode(t, h, cfg)
	for range loginFailures {
		h.now = h.now.Add(time.Second)
		if w := h.do("POST", "/api/setup/claim", claimBody("AAAA-BBBB-CCCC", "water the plants"), ""); w.Code != http.StatusForbidden {
			t.Fatalf("wrong code: %d %s", w.Code, w.Body)
		}
	}
	// Five wrong codes in the minute: even the right one waits.
	w := h.do("POST", "/api/setup/claim", claimBody(code, "water the plants"), "")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth try: %d, want 429", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra != "56" {
		t.Errorf("Retry-After = %q, want 56", ra)
	}
	h.now = h.now.Add(56 * time.Second)
	if w := h.do("POST", "/api/setup/claim", claimBody(code, "water the plants"), ""); w.Code != http.StatusNoContent {
		t.Errorf("after the wait: %d %s", w.Code, w.Body)
	}
}

func TestEnvironmentPasswordWins(t *testing.T) {
	cfg := Config{DataDir: t.TempDir(), PasswordHash: cheapHash("water the plants")}
	h := newLoginHarness(t, cfg)
	// A code file left from before the hash was set goes away.
	stale := filepath.Join(cfg.DataDir, setupCodeFile)
	if err := os.WriteFile(stale, []byte("AAAA-BBBB-CCCC\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mintSetupCode(cfg, h.st); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("setup code file with a password from the environment: %v", err)
	}
	code, err := h.st.NewSetupCode(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	w := h.do("POST", "/api/setup/claim", claimBody(code, "feed the cat"), "")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "environment") {
		t.Errorf("claim with a password from the environment: %d %s", w.Code, w.Body)
	}
	c := h.login("water the plants")
	w = h.do("PUT", "/api/password", `{"current":"water the plants","password":"feed the cat"}`, c.Value)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "environment") {
		t.Errorf("change with a password from the environment: %d %s", w.Code, w.Body)
	}
	// The environment also wins over a password kept in the app.
	if err := h.st.SetPasswordHash(cheapHash("feed the cat")); err != nil {
		t.Fatal(err)
	}
	h.srv = h.server(cfg)
	h.login("water the plants")
	if w := h.do("POST", "/api/login", `{"password":"feed the cat"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("the app's password under an environment one: %d, want 401", w.Code)
	}
}

func TestChangePassword(t *testing.T) {
	const sub = `{"endpoint":"https://updates.push.services.mozilla.com/wpush/v2/b","keys":{"p256dh":"p","auth":"a"}}`
	h := newLoginHarness(t, Config{})
	if w := h.do("PUT", "/api/password", `{"current":"","password":"feed the cat"}`, ""); w.Code != http.StatusConflict {
		t.Errorf("change on a hub with no password: %d %s, want 409", w.Code, w.Body)
	}

	if err := h.st.SetPasswordHash(cheapHash("water the plants")); err != nil {
		t.Fatal(err)
	}
	h.srv = h.server(Config{})
	c := h.login("water the plants")
	other := h.login("water the plants")
	if w := h.do("POST", "/api/push/subscriptions", sub, other.Value); w.Code != http.StatusNoContent {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body)
	}

	if w := h.do("PUT", "/api/password", `{"current":"water the plants","password":"short"}`, c.Value); w.Code != http.StatusBadRequest {
		t.Errorf("short password: %d %s", w.Code, w.Body)
	}
	if w := h.do("PUT", "/api/password", `{"current":"wrong","password":"feed the cat"}`, c.Value); w.Code != http.StatusForbidden ||
		!strings.Contains(w.Body.String(), "wrong password") {
		t.Errorf("wrong current password: %d %s, want 403", w.Code, w.Body)
	}
	w := h.do("PUT", "/api/password", `{"current":"water the plants","password":"feed the cat"}`, c.Value)
	if w.Code != http.StatusNoContent {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}

	// This browser stays logged in; the other one is logged out and stops
	// getting reminders.
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusOK {
		t.Errorf("this browser after the change: %d, want 200", w.Code)
	}
	if w := h.do("GET", "/api/state", "", other.Value); w.Code != http.StatusUnauthorized {
		t.Errorf("another browser after the change: %d, want 401", w.Code)
	}
	if len(h.push.subs) != 0 {
		t.Errorf("the other browser's subscription survived: %v", h.push.subs)
	}
	if w := h.do("POST", "/api/login", `{"password":"water the plants"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("old password: %d, want 401", w.Code)
	}
	h.login("feed the cat")

	// It survives a restart.
	h.srv = h.server(Config{})
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != http.StatusOK {
		t.Errorf("this browser after a restart: %d, want 200", w.Code)
	}
	h.login("feed the cat")
}

func TestWrongCurrentPasswordCountsAsAWrongLogin(t *testing.T) {
	h := newLoginHarness(t, Config{})
	if err := h.st.SetPasswordHash(cheapHash("water the plants")); err != nil {
		t.Fatal(err)
	}
	h.srv = h.server(Config{})
	c := h.login("water the plants")
	for range loginFailures {
		h.do("PUT", "/api/password", `{"current":"nope","password":"feed the cat"}`, c.Value)
	}
	if w := h.do("POST", "/api/login", `{"password":"water the plants"}`, ""); w.Code != http.StatusTooManyRequests {
		t.Errorf("login after five wrong current passwords: %d, want 429", w.Code)
	}
}

func TestTailscaleOnly(t *testing.T) {
	h := newLoginHarness(t, Config{})
	if w := h.do("PUT", "/api/setup/tailscale-only", `{"on":true}`, ""); w.Code != http.StatusNoContent {
		t.Fatalf("record: %d %s", w.Code, w.Body)
	}
	if got := decode[api.Setup](t, h.do("GET", "/api/setup", "", "")); !got.TailscaleOnly || got.Password != "none" {
		t.Errorf("setup: %+v", got)
	}
	if w := h.do("PUT", "/api/setup/tailscale-only", `{"on":false}`, ""); w.Code != http.StatusNoContent {
		t.Fatalf("clear: %d %s", w.Code, w.Body)
	}

	h = newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	if w := h.do("PUT", "/api/setup/tailscale-only", `{"on":true}`, c.Value); w.Code != http.StatusConflict {
		t.Errorf("record with a password: %d %s, want 409", w.Code, w.Body)
	}
}
