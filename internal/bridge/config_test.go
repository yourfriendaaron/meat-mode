package bridge

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
	c := Config{HubURL: "https://hub.example.com", Token: "secret", Roots: []string{}}
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
	got, err := readConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.HubURL != c.HubURL || got.Token != "secret" {
		t.Errorf("loaded %+v", got)
	}
	if got.Roots == nil || len(got.ResolveRoots(t.TempDir())) != 0 {
		t.Errorf("an empty roots list must mean none, got %#v", got.Roots)
	}
}

// settingsEnv is an Env whose environment is vars and whose config file
// holds c.
func settingsEnv(t *testing.T, c Config, vars map[string]string) Env {
	t.Helper()
	home := t.TempDir()
	env := Env{
		Paths:  Paths{Home: home, Config: filepath.Join(home, "bridge.json"), State: filepath.Join(home, "state")},
		Getenv: func(k string) string { return vars[k] },
	}
	if err := c.Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestLoadSettings(t *testing.T) {
	saved := Config{HubURL: "https://hub.example.com", Token: "secret", Roots: []string{"~/code"}}
	env := settingsEnv(t, saved, map[string]string{
		"MEAT_MODE_HUB_URL":             "http://192.0.2.10:8080",
		"MEAT_MODE_ALLOW_INSECURE_HTTP": "true",
		"MEAT_MODE_TRUSTED_NETWORKS":    "198.51.100.0/24",
		"MEAT_MODE_BRIDGE_TOKEN":        "  pairing-token\n",
		"MEAT_MODE_BRIDGE_POLL":         "200ms",
		"MEAT_MODE_AGENT_NAME":          "helper",
	})
	s, err := LoadSettings(env)
	if err != nil {
		t.Fatal(err)
	}
	if s.Config.HubURL != "http://192.0.2.10:8080" || !s.Config.AllowInsecureHTTP || s.Config.TrustedNetworks != "198.51.100.0/24" || s.Config.Token != "secret" {
		t.Errorf("effective config %+v", s.Config)
	}
	if s.Saved.HubURL != saved.HubURL || s.Saved.AllowInsecureHTTP || s.Saved.TrustedNetworks != "" {
		t.Errorf("saved config picked up the overrides: %+v", s.Saved)
	}
	if !slices.Equal(s.Roots, []string{filepath.Join(env.Paths.Home, "code")}) {
		t.Errorf("roots %v", s.Roots)
	}
	if s.Token != "pairing-token" || s.Poll != 200*time.Millisecond || s.Agent.Name != "helper" {
		t.Errorf("token %q, poll %v, agent %+v", s.Token, s.Poll, s.Agent)
	}
	if err := s.Paired(); err != nil {
		t.Errorf("paired with the insecure opt-in: %v", err)
	}
	if h := s.Hub(); h.URL != s.Config.HubURL || h.Token != "secret" {
		t.Errorf("hub %+v", h)
	}

	// Defaults: no variables, no file.
	empty := Env{Paths: Paths{Home: t.TempDir(), Config: filepath.Join(t.TempDir(), "absent.json")}, Getenv: func(string) string { return "" }}
	if s, err = LoadSettings(empty); err != nil {
		t.Fatal(err)
	}
	if s.Poll != 0 || !slices.Equal(s.Agent.Command, DefaultAgentConfig().Command) || !slices.Equal(s.Roots, []string{empty.Paths.Home}) {
		t.Errorf("defaults: poll %v, agent %v, roots %v", s.Poll, s.Agent.Command, s.Roots)
	}
}

// TestLoadSettingsRefusals: a bad override the hub client depends on fails
// every command; a bad one only the running bridge reads fails only New.
func TestLoadSettingsRefusals(t *testing.T) {
	ok := Config{HubURL: "https://hub.example.com", Token: "secret"}
	for k, v := range map[string]string{
		"MEAT_MODE_ALLOW_INSECURE_HTTP": "maybe",
		"MEAT_MODE_TRUSTED_NETWORKS":    "0.0.0.0/0",
	} {
		if _, err := LoadSettings(settingsEnv(t, ok, map[string]string{k: v})); err == nil {
			t.Errorf("%s=%q was accepted", k, v)
		}
	}
	for _, kv := range [][2]string{
		{"MEAT_MODE_BRIDGE_POLL", "fast"},
		{"MEAT_MODE_BRIDGE_POLL", "0s"},
		{"MEAT_MODE_AGENT_TIMEOUT", "-1s"},
		{"MEAT_MODE_AGENT_BUDGET_USD", "free"},
	} {
		k, v := kv[0], kv[1]
		s, err := LoadSettings(settingsEnv(t, ok, map[string]string{k: v}))
		if err != nil {
			t.Errorf("%s=%q failed the settings: %v", k, v, err)
			continue
		}
		if s.Paired() != nil {
			t.Errorf("%s=%q unpaired the settings", k, v)
		}
		b, err := New(s, quiet)
		if err == nil || !strings.Contains(err.Error(), k) {
			if b != nil {
				b.Journal.Close()
			}
			t.Errorf("New with %s=%q = %v, want an error naming it", k, v, err)
		}
	}
}

func TestSettingsPaired(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"no file", Config{}, "not paired"},
		{"no token", Config{HubURL: "https://hub.example.com"}, "not paired"},
		{"no URL", Config{Token: "secret"}, "not paired"},
		{"plain http", Config{HubURL: "http://192.0.2.10:8080", Token: "secret"}, "refusing plain HTTP"},
		{"paired", Config{HubURL: "https://hub.example.com", Token: "secret"}, ""},
	}
	for _, c := range cases {
		s, err := LoadSettings(settingsEnv(t, c.cfg, nil))
		if err != nil {
			t.Fatal(err)
		}
		err = s.Paired()
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: Paired() = %v, want %q", c.name, err, c.want)
		}
		if c.want == "" {
			continue
		}
		if b, err := New(s, quiet); err == nil {
			b.Journal.Close()
			t.Errorf("%s: New built a bridge", c.name)
		}
	}
}

func TestSettingsRepos(t *testing.T) {
	env := settingsEnv(t, Config{Roots: []string{"~/projects"}, Sync: map[string]bool{"projects/garden": false, "projects/kitchen": true}}, nil)
	root := filepath.Join(env.Paths.Home, "projects")
	writeRepo(t, filepath.Join(root, "garden"), "meat-mode.sync: true\n")
	writeRepo(t, filepath.Join(root, "kitchen"), "")
	writeRepo(t, filepath.Join(root, "shed"), "meat-mode.sync: true\n")
	s, err := LoadSettings(env)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range s.Repos() {
		got[r.Name] = r.Synced()
	}
	want := map[string]bool{"projects/garden": false, "projects/kitchen": true, "projects/shed": true}
	if !maps.Equal(got, want) {
		t.Errorf("synced = %v, want %v", got, want)
	}
}

func TestConfigDropsTheOldPersonalRepo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	if err := os.WriteFile(path, []byte(`{"hubURL":"https://hub.example.com","token":"secret","roots":null,"personal":"/srv/me"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := readConfigFile(path)
	if err != nil {
		t.Fatalf("a config from an older bridge: %v", err)
	}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "personal") {
		t.Errorf("saved config still names the personal repo: %s", b)
	}
}

func TestResolveRoots(t *testing.T) {
	home := t.TempDir()
	if r := (Config{}).ResolveRoots(home); !slices.Equal(r, []string{home}) {
		t.Errorf("default roots %v, want the home folder", r)
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

func TestSaveSwitchKeepsOverridesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	if err := (Config{HubURL: "https://hub.example.com", Token: "secret"}).Save(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEAT_MODE_HUB_URL", "https://other.example.com")
	if err := SaveSwitch(path, "projects/garden", true); err != nil {
		t.Fatal(err)
	}
	if err := SaveSwitch(path, "projects/kitchen", false); err != nil {
		t.Fatal(err)
	}
	c, err := readConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.HubURL != "https://hub.example.com" || c.Token != "secret" {
		t.Errorf("config after saving switches %+v", c)
	}
	if !maps.Equal(c.Sync, map[string]bool{"projects/garden": true, "projects/kitchen": false}) {
		t.Errorf("switches %v", c.Sync)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode after saving: %v %v", fi.Mode(), err)
	}
}
