package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
)

// fakeService records installs without touching the system.
type fakeService struct {
	installed       bool
	exe, path, file string
}

func (f *fakeService) Install(exe, pathEnv string) error {
	f.installed, f.exe, f.path = true, exe, pathEnv
	return nil
}
func (f *fakeService) Uninstall() error { f.installed = false; return nil }
func (f *fakeService) Status() string   { return "fake" }
func (f *fakeService) File() string     { return f.file }

// fakeBD writes a script that answers `bd version --json`.
func fakeBD(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "bd")
	script := "#!/bin/sh\necho '{\"version\": \"" + version + "\", \"schema_version\": 1}'\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func testEnv(t *testing.T, bd string) (Env, *fakeService, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	out := &bytes.Buffer{}
	svc := &fakeService{file: filepath.Join(home, "unit")}
	return Env{
		Paths:      Paths{Home: home, Config: filepath.Join(home, ".config", "meat-mode", "bridge.json"), State: filepath.Join(home, "state")},
		Service:    svc,
		Executable: "/opt/meat-mode/meat-mode",
		LookPath:   func(string) (string, error) { return bd, nil },
		Stdout:     out,
		Stderr:     out,
		Version:    "test",
	}, svc, out
}

func TestPair(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	bd := fakeBD(t, "1.3.1")
	env, svc, out := testEnv(t, bd)
	personal := filepath.Join(env.Paths.Home, "me")
	err := Main(context.Background(), []string{"pair", hub.URL, token, "--personal", personal, "--root", "~/code"}, env)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	fi, err := os.Stat(env.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", fi.Mode().Perm())
	}
	cfg, err := LoadConfig(env.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HubURL != hub.URL || cfg.Token != token || cfg.BD != bd || cfg.Personal != personal ||
		len(cfg.Roots) != 1 || cfg.Roots[0] != filepath.Join(env.Paths.Home, "code") {
		t.Errorf("config = %+v", cfg)
	}
	if !svc.installed || svc.exe != "/opt/meat-mode/meat-mode" || !strings.HasPrefix(svc.path, filepath.Dir(bd)+":") {
		t.Errorf("service = %+v", svc)
	}
	if hs := hub.Hellos(); len(hs) != 1 || hs[0].BdVersion != "1.3.1" {
		t.Errorf("hellos = %+v", hs)
	}
	if !strings.Contains(out.String(), `as bridge "desk"`) {
		t.Errorf("output: %s", out)
	}

	if err := Main(context.Background(), []string{"uninstall"}, env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(env.Paths.Config); !os.IsNotExist(err) {
		t.Error("uninstall kept the config")
	}
	if svc.installed {
		t.Error("uninstall kept the service")
	}
}

func TestPairMakesRootsAbsolute(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	bd := fakeBD(t, "1.3.1")
	env, _, out := testEnv(t, bd)
	proj := filepath.Join(env.Paths.Home, "projects")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	// A relative root and a bare ~ are stored as absolute paths, like --personal.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	absRoot := filepath.Join(env.Paths.Home, "code")
	err = Main(context.Background(), []string{"pair", hub.URL, token, "--no-service",
		"--root", "rel/code", "--root", "~", "--root", absRoot}, env)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, err := LoadConfig(env.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(wd, "rel", "code"), env.Paths.Home, absRoot}
	if len(cfg.Roots) != len(want) {
		t.Fatalf("roots = %v, want %v", cfg.Roots, want)
	}
	for i := range want {
		if cfg.Roots[i] != want[i] {
			t.Errorf("root %d = %q, want %q", i, cfg.Roots[i], want[i])
		}
	}
}

// TestPairTokenSources: the token can come from the environment or stdin,
// so it need not sit in shell history or the process list.
func TestPairTokenSources(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	cases := []struct {
		name, env, stdin string
		args             []string
	}{
		{"environment", token, "", []string{hub.URL, "--no-service"}},
		{"stdin", "", token + "\n", []string{hub.URL, "-", "--no-service"}},
		{"stdin without newline", "", "  " + token, []string{"--no-service", hub.URL, "-"}},
		{"argument wins over environment", "wrong", "", []string{hub.URL, token, "--no-service"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MEAT_MODE_BRIDGE_TOKEN", c.env)
			env, _, out := testEnv(t, fakeBD(t, "1.3.1"))
			env.Stdin = strings.NewReader(c.stdin)
			if err := Main(context.Background(), append([]string{"pair"}, c.args...), env); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			cfg, err := LoadConfig(env.Paths.Config)
			if err != nil || cfg.Token != token {
				t.Fatalf("config token = %q, %v", cfg.Token, err)
			}
		})
	}
	t.Run("empty stdin", func(t *testing.T) {
		t.Setenv("MEAT_MODE_BRIDGE_TOKEN", "")
		env, _, _ := testEnv(t, fakeBD(t, "1.3.1"))
		env.Stdin = strings.NewReader("\n")
		if err := Main(context.Background(), []string{"pair", hub.URL, "-"}, env); err == nil {
			t.Fatal("paired with an empty token")
		}
	})
}

// TestLoadConfigWarnsOnBroadMode: a config others can read leaks the token.
func TestLoadConfigWarnsOnBroadMode(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "bridge.json")
	if err := (Config{HubURL: "https://meat-mode.example.com", Token: token}).Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatalf("warned about a 0600 config: %s", logs.String())
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil || cfg.Token != token {
		t.Fatalf("LoadConfig = %+v, %v; want it to load anyway", cfg, err)
	}
	if !strings.Contains(logs.String(), "chmod 600") {
		t.Errorf("no warning for a 0644 config: %q", logs.String())
	}
}

func TestPairRefusals(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	cases := []struct {
		name string
		args []string
		bd   string
		want string
	}{
		{"bad token", []string{hub.URL, "wrong"}, "1.3.1", "token"},
		{"plain http to another host", []string{"http://192.0.2.10:8080", token}, "1.3.1", "refusing plain HTTP"},
		{"old bd", []string{hub.URL, token}, "1.2.0", "too old"},
		{"missing token", []string{hub.URL}, "1.3.1", "needs a hub URL and a token"},
		{"token from stdin, none given", []string{hub.URL, "-"}, "1.3.1", "no token on standard input"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MEAT_MODE_BRIDGE_TOKEN", "")
			env, svc, _ := testEnv(t, fakeBD(t, c.bd))
			env.Stdin = strings.NewReader("")
			err := Main(context.Background(), append([]string{"pair"}, c.args...), env)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("pair = %v, want an error about %q", err, c.want)
			}
			if _, err := os.Stat(env.Paths.Config); !os.IsNotExist(err) {
				t.Error("config saved after a refused pairing")
			}
			if svc.installed {
				t.Error("service installed after a refused pairing")
			}
		})
	}
	if len(hub.Hellos()) != 0 {
		t.Error("a refused pairing reached the hub")
	}
}

func TestRunRefusesPlainHTTP(t *testing.T) {
	env, _, _ := testEnv(t, fakeBD(t, "1.3.1"))
	if err := (Config{HubURL: "http://192.0.2.10:8080", Token: token}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	err := Main(context.Background(), []string{"run"}, env)
	if err == nil || !strings.Contains(err.Error(), "refusing plain HTTP") {
		t.Fatalf("run = %v", err)
	}
	t.Setenv("MEAT_MODE_ALLOW_INSECURE_HTTP", "1")
	cfg, _ := LoadConfig(env.Paths.Config)
	b, err := New(cfg, env, quiet)
	if err != nil {
		t.Fatalf("with the opt-in: %v", err)
	}
	b.Journal.Close()
}

func TestStatus(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	env, _, out := testEnv(t, fakeBD(t, "1.3.1"))
	root := filepath.Join(env.Paths.Home, "projects")
	writeRepo(t, filepath.Join(root, "garden"), "meat-mode.sync: true\n")
	writeRepo(t, filepath.Join(root, "kitchen"), "")
	journal := filepath.Join(env.Paths.Home, "notes", "journal")
	writeRepo(t, journal, "")
	if err := (Config{HubURL: hub.URL, Token: token}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	if err := Main(context.Background(), []string{"status"}, env); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Hub:      " + hub.URL, "bd:       1.3.1", "garden", "synced", "kitchen", "not opted in", journal, `reachable, this bridge is "desk"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestPollOverride(t *testing.T) {
	if d, err := pollOverride(""); err != nil || d != 0 {
		t.Fatalf("empty: got %v, %v", d, err)
	}
	if d, err := pollOverride("200ms"); err != nil || d != 200*time.Millisecond {
		t.Fatalf("200ms: got %v, %v", d, err)
	}
	for _, bad := range []string{"fast", "0s", "-1s"} {
		if _, err := pollOverride(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	b := &Bridge{}
	if b.actionEvery() != ActionEvery || b.syncEvery() != SyncEvery || b.scanEvery() != ScanEvery {
		t.Error("zero Poll should keep the defaults")
	}
	b.Poll = time.Second
	if b.actionEvery() != time.Second || b.syncEvery() != time.Second || b.scanEvery() != time.Second {
		t.Error("Poll should replace every interval")
	}
}
