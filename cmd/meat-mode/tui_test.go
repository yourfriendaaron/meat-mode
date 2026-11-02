package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/appclient"
	"github.com/yourfriendaaron/meat-mode/internal/hub/hubtest"
	"github.com/yourfriendaaron/meat-mode/internal/netguard"
	"github.com/yourfriendaaron/meat-mode/internal/tui"
)

func TestParseTUIArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want tuiArgs
		err  string
	}{
		{nil, tuiArgs{}, ""},
		{[]string{"--hub", "https://meat-mode.example.com"}, tuiArgs{hub: "https://meat-mode.example.com"}, ""},
		{[]string{"-hub=meat-mode.example.com"}, tuiArgs{hub: "meat-mode.example.com"}, ""},
		{[]string{"logout"}, tuiArgs{logout: true}, ""},
		{[]string{"logout", "--hub", "meat-mode.example.com"}, tuiArgs{logout: true, hub: "meat-mode.example.com"}, ""},
		{[]string{"--hub"}, tuiArgs{}, "flag needs an argument"},
		{[]string{"--port", "1"}, tuiArgs{}, "not defined"},
		{[]string{"meat-mode.example.com"}, tuiArgs{}, "unexpected"},
		{[]string{"--hub", "meat-mode.example.com", "logout"}, tuiArgs{}, "unexpected"},
	} {
		got, err := parseTUIArgs(c.args)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("parseTUIArgs(%q) error %v, want %q", c.args, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseTUIArgs(%q) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
}

// testEnv is a tuiEnv in a temporary config dir that answers the
// password prompt from passwords and records whether the viewer ran.
type testEnv struct {
	tuiEnv
	out, errOut bytes.Buffer
	asked       int
	ran         *tui.Options
}

func newTestEnv(t *testing.T, passwords ...string) *testEnv {
	e := &testEnv{}
	e.tuiEnv = tuiEnv{
		dir:      t.TempDir(),
		getenv:   func(string) string { return "" },
		readLine: func(string) (string, error) { return "", errors.New("no line to read") },
		readSecret: func(string) (string, error) {
			if e.asked >= len(passwords) {
				return "", errors.New("asked once too often")
			}
			e.asked++
			return passwords[e.asked-1], nil
		},
		out:    &e.out,
		errOut: &e.errOut,
		run: func(_ context.Context, o tui.Options) error {
			e.ran = &o
			return nil
		},
	}
	return e
}

func TestTUILogoutWithoutSession(t *testing.T) {
	e := newTestEnv(t)
	if err := tuiMain(context.Background(), []string{"logout"}, e.tuiEnv); err != nil {
		t.Fatal(err)
	}
	if got := e.out.String(); got != "No saved session; nothing to log out of.\n" {
		t.Errorf("output %q", got)
	}
	e.out.Reset()
	if err := tuiMain(context.Background(), []string{"logout", "--hub", "meat-mode.example.com"}, e.tuiEnv); err != nil {
		t.Fatal(err)
	}
	if got := e.out.String(); got != "No saved session for https://meat-mode.example.com; nothing to log out of.\n" {
		t.Errorf("output %q", got)
	}
}

func TestTUILoginThenLogout(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	e := newTestEnv(t, "feed the cat", hubtest.Password)
	if err := tuiMain(context.Background(), []string{"--hub", h.URL}, e.tuiEnv); err != nil {
		t.Fatal(err)
	}
	if e.ran == nil || e.ran.Client.Hub() != h.URL {
		t.Fatalf("the viewer did not run for %s", h.URL)
	}
	if e.asked != 2 || !strings.Contains(e.errOut.String(), "Wrong password.") {
		t.Errorf("asked %d times, said %q", e.asked, e.errOut.String())
	}
	s, ok, err := appclient.LoadSession(e.dir, h.URL)
	if err != nil || !ok || s.Cookie == "" {
		t.Fatalf("saved session %+v, %v, %v", s, ok, err)
	}
	if fi, err := os.Stat(filepath.Join(e.dir, appclient.SessionFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("session file mode %v, %v", fi.Mode(), err)
	}

	// The next start uses the saved session, the only one, without asking.
	e.ran, e.asked = nil, 0
	if err := tuiMain(context.Background(), nil, e.tuiEnv); err != nil {
		t.Fatal(err)
	}
	if e.ran == nil || e.asked != 0 {
		t.Errorf("second start: ran %v, asked %d", e.ran != nil, e.asked)
	}

	if err := tuiMain(context.Background(), []string{"logout"}, e.tuiEnv); err != nil {
		t.Fatal(err)
	}
	if got := e.out.String(); !strings.Contains(got, "Logged out of "+h.URL) {
		t.Errorf("output %q", got)
	}
	if _, ok, _ := appclient.LoadSession(e.dir, h.URL); ok {
		t.Error("the session is still saved")
	}
	c, err := appclient.New(h.URL, netguard.Set{}, &s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.State(context.Background()); !errors.Is(err, appclient.ErrAuth) {
		t.Errorf("the old session still works on the hub: %v", err)
	}
}

func TestTUIThreeWrongPasswords(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	e := newTestEnv(t, "a", "b", "c", "d")
	err := tuiMain(context.Background(), []string{"--hub", h.URL}, e.tuiEnv)
	if err == nil || err.Error() != "wrong password" || e.asked != 3 || e.ran != nil {
		t.Errorf("error %v, asked %d, ran %v", err, e.asked, e.ran != nil)
	}
}

func TestTUIHubWithoutPassword(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{NoPassword: true})
	e := newTestEnv(t, "anything")
	err := tuiMain(context.Background(), []string{"--hub", h.URL}, e.tuiEnv)
	if !errors.Is(err, appclient.ErrNoPassword) || e.ran != nil {
		t.Errorf("error %v, ran %v", err, e.ran != nil)
	}
}

func TestTUIAsksForTheHubWithThePairedOneOffered(t *testing.T) {
	h := hubtest.New(t, hubtest.Options{})
	e := newTestEnv(t, hubtest.Password)
	cfg := `{"hubURL": "` + h.URL + `", "token": "t", "trustedNetworks": "198.51.100.0/24"}`
	if err := os.WriteFile(filepath.Join(e.dir, "bridge.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	var prompt string
	e.readLine = func(p string) (string, error) { prompt = p; return "", nil }
	if err := tuiMain(context.Background(), nil, e.tuiEnv); err != nil {
		t.Fatal(err)
	}
	if prompt != "Hub URL ["+h.URL+"]: " || e.ran == nil || e.ran.Client.Hub() != h.URL {
		t.Errorf("prompt %q, ran %v", prompt, e.ran != nil)
	}
	set, err := e.allowed(h.URL)
	if err != nil || len(set.Trusted()) != 1 {
		t.Errorf("the paired hub's trusted networks: %v, %v", set.Trusted(), err)
	}
	if set, _ := e.allowed("https://meat-mode.example.com"); len(set.Trusted()) != 0 {
		t.Errorf("another hub got the paired hub's trusted networks: %v", set.Trusted())
	}
}
