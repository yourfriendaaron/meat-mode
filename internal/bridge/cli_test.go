package bridge

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		len(cfg.Roots) != 1 || cfg.Roots[0] != "~/code" {
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, svc, _ := testEnv(t, fakeBD(t, c.bd))
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
	if err := (Config{HubURL: hub.URL, Token: token}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	if err := Main(context.Background(), []string{"status"}, env); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Hub:      " + hub.URL, "bd:       1.3.1", "garden", "synced", "kitchen", "not opted in", `reachable, this bridge is "desk"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}
