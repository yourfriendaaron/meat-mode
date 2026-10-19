package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// runConfig is a config Run accepts, on a free local port.
func runConfig(dir string) Config {
	return Config{Listen: "127.0.0.1:0", DataDir: dir, PublicURL: "http://localhost:8080", User: "alex"}
}

// stopped is a context that has already ended, so Run starts and at once
// shuts down.
func stopped() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestRunPassesTheValidatedConfigToOnStart(t *testing.T) {
	good := cheapHash("water the plants")
	file := filepath.Join(t.TempDir(), "hash")
	if err := os.WriteFile(file, []byte(good+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := runConfig(t.TempDir())
	cfg.PasswordHashFile = file
	errStop := errors.New("stop here")
	var got Config
	err := Run(stopped(), cfg, Options{OnStart: func(_ context.Context, c Config, _ *store.Store) error {
		got = c
		return errStop
	}})
	if !errors.Is(err, errStop) {
		t.Fatalf("Run: %v", err)
	}
	if got.PasswordHash != good || got.PasswordHashFile != "" {
		t.Errorf("OnStart config hash %q, file %q; want the hash loaded from the file", got.PasswordHash, got.PasswordHashFile)
	}
}

func TestRunEndsSessionsOfAnOldEnvironmentHash(t *testing.T) {
	const endpoint = "https://updates.push.services.mozilla.com/wpush/v2/a"
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := newLogin(cheapHash("water the plants"), true)
	token, _, err := st.CreateSession(old.tag, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	push := &fakePush{subs: map[string]api.PushSubscription{}}
	push.Subscribe(context.Background(), store.SessionID(token), api.PushSubscription{Endpoint: endpoint})

	cfg := runConfig(dir)
	cfg.PasswordHash = cheapHash("feed the cat")
	err = Run(stopped(), cfg, Options{Push: func(Config, *store.Store) (PushRegistry, error) { return push, nil }})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(push.subs) != 0 {
		t.Errorf("subscription of a session under the old hash kept: %v", push.subs)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil || n != 0 {
		t.Errorf("sessions left after the hash changed: %d, %v", n, err)
	}
}

func TestRestartKeepsSessionsOfTheSamePassword(t *testing.T) {
	h := newLoginHarness(t, Config{PasswordHash: cheapHash("water the plants")})
	c := h.login("water the plants")
	h.srv = h.server(Config{PasswordHash: cheapHash("water the plants")})
	if err := h.srv.endOtherLogins(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w := h.do("GET", "/api/state", "", c.Value); w.Code != 200 {
		t.Errorf("session after a restart with the same password: %d, want 200", w.Code)
	}

	// Turning login off ends every session.
	h.srv = h.server(Config{})
	h.push.Subscribe(context.Background(), store.SessionID(c.Value), api.PushSubscription{Endpoint: "https://push.example.com/a"})
	if err := h.srv.endOtherLogins(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.push.subs) != 0 {
		t.Errorf("subscription kept after login was turned off: %v", h.push.subs)
	}
}

func TestRequestsOutliveTheShutdownSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), sessionKey{}, "kept"))
	srv := newHTTPServer(ctx, newHarness(t, nil, false).srv)
	cancel()
	base := srv.BaseContext(nil)
	if base.Err() != nil {
		t.Errorf("request context ended with the signal: %v", base.Err())
	}
	if base.Value(sessionKey{}) != "kept" {
		t.Error("request context lost the base context's values")
	}
}
