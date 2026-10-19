package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/beads"
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
	err := Main(context.Background(), []string{"pair", hub.URL, token, "--root", "~/code"}, env)
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
	cfg, err := readConfigFile(env.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HubURL != hub.URL || cfg.Token != token ||
		len(cfg.Roots) != 1 || cfg.Roots[0] != filepath.Join(env.Paths.Home, "code") {
		t.Errorf("config = %+v", cfg)
	}
	if !svc.installed || svc.exe != "/opt/meat-mode/meat-mode" || !strings.HasPrefix(svc.path, filepath.Dir(bd)+":") {
		t.Errorf("service = %+v", svc)
	}
	if hs := hub.Hellos(); len(hs) != 1 || hs[0].BdVersion != "1.3.1" ||
		!reflect.DeepEqual(hs[0].Trackers, []api.TrackerVersion{{Name: "beads", Title: "beads", Version: "1.3.1", Raise: (&beads.CLI{}).Info().Raise}}) {
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
	// A relative root and a bare ~ are stored as absolute paths.
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
	cfg, err := readConfigFile(env.Paths.Config)
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
			cfg, err := readConfigFile(env.Paths.Config)
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

// TestSettingsWarnOnBroadMode: a config others can read leaks the token.
func TestSettingsWarnOnBroadMode(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "bridge.json")
	env := Env{Paths: Paths{Config: path}}
	if err := (Config{HubURL: "https://meat-mode.example.com", Token: token}).Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(env); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatalf("warned about a 0600 config: %s", logs.String())
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(env)
	if err != nil || s.Config.Token != token {
		t.Fatalf("LoadSettings = %+v, %v; want it to load anyway", s.Config, err)
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
		{"personal repo, now kept on the hub", []string{hub.URL, token, "--personal", "/srv/me"}, "1.3.1", "-personal"},
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
	s, err := LoadSettings(env)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(s, quiet)
	if err != nil {
		t.Fatalf("with the opt-in: %v", err)
	}
	b.Journal.Close()
}

func TestNewWarnsWhenTheAgentIsNotOnPATH(t *testing.T) {
	env, _, _ := testEnv(t, fakeBD(t, "1.3.1"))
	must(t, Config{HubURL: "https://meat-mode.example.com", Token: token}.Save(env.Paths.Config))
	for _, found := range []bool{true, false} {
		env.LookPath = func(name string) (string, error) {
			if name == "claude" && !found {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + name, nil
		}
		var logs bytes.Buffer
		s, err := LoadSettings(env)
		if err != nil {
			t.Fatal(err)
		}
		b, err := New(s, slog.New(slog.NewTextHandler(&logs, nil)))
		if err != nil {
			t.Fatal(err)
		}
		b.Journal.Close()
		if warned := strings.Contains(logs.String(), "not on this bridge's PATH"); warned == found {
			t.Errorf("agent found %v: logs %q", found, logs.String())
		}
	}
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
	for _, want := range []string{"Hub:      " + hub.URL, "Trackers:\n  beads                    1.3.1\n", "garden", "synced", "kitchen", "not synced (switch it on in Settings)", journal, `reachable, this bridge is "desk"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestPollIntervals(t *testing.T) {
	b := &Bridge{}
	if b.actionEvery() != ActionEvery || b.syncEvery() != SyncEvery || b.scanEvery() != ScanEvery {
		t.Error("zero Poll should keep the defaults")
	}
	b.Poll = time.Second
	if b.actionEvery() != time.Second || b.syncEvery() != time.Second || b.scanEvery() != time.Second {
		t.Error("Poll should replace every interval")
	}
}

// TestPairDoesNotSaveInsecureEnvOverride keeps a one-time environment opt-in
// out of the saved config; only the flag, or the file itself, persists it.
func TestPairDoesNotSaveInsecureEnvOverride(t *testing.T) {
	t.Setenv("MEAT_MODE_ALLOW_INSECURE_HTTP", "true")
	t.Setenv("MEAT_MODE_HUB_URL", "")
	for _, c := range []struct {
		name string
		args []string
		want bool
	}{
		{"env only", nil, false},
		{"flag", []string{"--allow-insecure-http"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			hub := fakehub.New(map[string]string{token: us})
			defer hub.Close()
			env, _, out := testEnv(t, fakeBD(t, "1.3.1"))
			args := append([]string{"pair", hub.URL, token, "--no-service"}, c.args...)
			if err := Main(context.Background(), args, env); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			saved, err := readConfigFile(env.Paths.Config)
			if err != nil {
				t.Fatal(err)
			}
			if saved.AllowInsecureHTTP != c.want {
				t.Errorf("saved allowInsecureHTTP = %v, want %v", saved.AllowInsecureHTTP, c.want)
			}
		})
	}
}

// The service does not inherit the shell, so pair refuses to install one
// that could only reach the hub with this shell's opt-in to plain HTTP.
func TestPairRefusesAServiceThatNeedsTheInsecureEnv(t *testing.T) {
	t.Setenv("MEAT_MODE_ALLOW_INSECURE_HTTP", "true")
	t.Setenv("MEAT_MODE_HUB_URL", "")
	env, _, out := testEnv(t, fakeBD(t, "1.3.1"))
	err := Main(context.Background(), []string{"pair", "http://hub.example.com", token}, env)
	if err == nil || !strings.Contains(err.Error(), "--allow-insecure-http") {
		t.Fatalf("pair = %v\n%s", err, out)
	}
	if _, err := os.Stat(env.Paths.Config); !os.IsNotExist(err) {
		t.Errorf("config saved: %v", err)
	}
}
