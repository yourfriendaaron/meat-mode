package bridge

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCheckURL(t *testing.T) {
	cases := []struct {
		url      string
		insecure bool
		ok       bool
	}{
		{"https://hub.example.com", false, true},
		{"http://localhost:8080", false, true},
		{"http://127.0.0.5:8080", false, true},
		{"http://[::1]:8080", false, true},
		{"http://192.0.2.10:8080", false, false},
		{"http://hub.example.com", false, false},
		{"http://192.0.2.10:8080", true, true},
		{"ftp://hub.example.com", true, false},
		{"hub.example.com", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		err := CheckURL(c.url, c.insecure)
		if (err == nil) != c.ok {
			t.Errorf("CheckURL(%q, %v) = %v, want ok=%v", c.url, c.insecure, err, c.ok)
		}
	}
	err := CheckURL("http://192.0.2.10:8080", false)
	if !strings.Contains(err.Error(), "refusing plain HTTP") || !strings.Contains(err.Error(), "allowInsecureHTTP") {
		t.Errorf("message does not say what to do: %v", err)
	}
}

func TestConfigSaveLoadAndEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meat-mode", "bridge.json")
	c := Config{HubURL: "https://hub.example.com", Token: "secret", Roots: []string{}, Personal: "/srv/me"}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v, want 0600", fi.Mode().Perm())
	}
	t.Setenv("MEAT_MODE_HUB_URL", "http://192.0.2.10:8080")
	t.Setenv("MEAT_MODE_ALLOW_INSECURE_HTTP", "true")
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.HubURL != "http://192.0.2.10:8080" || !got.AllowInsecureHTTP || got.Token != "secret" {
		t.Errorf("loaded %+v", got)
	}
	if got.Roots == nil || len(got.ResolveRoots(t.TempDir())) != 0 {
		t.Errorf("an empty roots list must mean none, got %#v", got.Roots)
	}
	t.Setenv("MEAT_MODE_ALLOW_INSECURE_HTTP", "maybe")
	if _, err := LoadConfig(path); err == nil {
		t.Error("a bad MEAT_MODE_ALLOW_INSECURE_HTTP was accepted")
	}
}

func TestResolveRoots(t *testing.T) {
	home := t.TempDir()
	if r := (Config{}).ResolveRoots(home); r != nil {
		t.Errorf("no ~/projects: roots %v", r)
	}
	if err := os.Mkdir(filepath.Join(home, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := (Config{}).ResolveRoots(home); !slices.Equal(r, []string{filepath.Join(home, "projects")}) {
		t.Errorf("default roots %v", r)
	}
	r := Config{Roots: []string{"~/code", "/srv/repos"}}.ResolveRoots(home)
	if !slices.Equal(r, []string{filepath.Join(home, "code"), "/srv/repos"}) {
		t.Errorf("expanded roots %v", r)
	}
}

func TestStateDir(t *testing.T) {
	home := "/srv/people/alex"
	cases := map[string]string{
		stateDir("linux", home, ""):                 "/srv/people/alex/.local/state/meat-mode",
		stateDir("linux", home, "/var/state"):       "/var/state/meat-mode",
		stateDir("linux", home, "relative"):         "/srv/people/alex/.local/state/meat-mode",
		stateDir("darwin", home, "/ignored/on/mac"): "/srv/people/alex/Library/Application Support/meat-mode",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("stateDir = %q, want %q", got, want)
		}
	}
}
