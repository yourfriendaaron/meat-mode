// Package bridge runs on the agent machine. It finds beads repos, sends their
// human items to the hub and replays the hub's actions with bd.
package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/netguard"
)

// Config is the bridge's settings file, bridge.json.
type Config struct {
	HubURL string `json:"hubURL"`
	Token  string `json:"token"`
	// Roots are the directories scanned for beads repos. Absent (null) means
	// the home folder; an empty list means none.
	Roots             []string `json:"roots"`
	AllowInsecureHTTP bool     `json:"allowInsecureHTTP,omitempty"`
	// BD is the bd executable; found on PATH when empty.
	BD string `json:"bd,omitempty"`
	// Sync holds the sync switches set in the app, by repo name. A switch
	// overrides the repo's own meat-mode.sync line.
	Sync map[string]bool `json:"sync,omitempty"`
	// Agents holds the agents switches set in the app, by repo name. Agents
	// run only in a repo switched on here; no repo file can do it.
	Agents map[string]bool `json:"agents,omitempty"`
	// TrustedNetworks is MEAT_MODE_TRUSTED_NETWORKS as `bridge pair` saw it:
	// networks outside the private set that the hub may be in. The
	// environment, when set, wins at run time.
	TrustedNetworks string `json:"trustedNetworks,omitempty"`
}

// Allowed is the set of addresses the hub may have: the private set plus the
// trusted networks.
func (c Config) Allowed() (netguard.Set, error) { return netguard.ParseTrusted(c.TrustedNetworks) }

// Paths are where the bridge keeps its files.
type Paths struct {
	Home   string // the user's home directory
	Config string // bridge.json
	State  string // directory for the replay journal
}

// DefaultPaths returns the platform locations: the config under
// os.UserConfigDir, the state under $XDG_STATE_HOME (or ~/.local/state), or
// ~/Library/Application Support on macOS.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Home:   home,
		Config: filepath.Join(cfg, "meat-mode", "bridge.json"),
		State:  stateDir(runtime.GOOS, home, os.Getenv("XDG_STATE_HOME")),
	}, nil
}

func stateDir(goos, home, xdg string) string {
	switch {
	case goos == "darwin":
		return filepath.Join(home, "Library", "Application Support", "meat-mode")
	case xdg != "" && filepath.IsAbs(xdg):
		return filepath.Join(xdg, "meat-mode")
	}
	return filepath.Join(home, ".local", "state", "meat-mode")
}

// applyOverrides returns c with the MEAT_MODE_HUB_URL,
// MEAT_MODE_ALLOW_INSECURE_HTTP and MEAT_MODE_TRUSTED_NETWORKS overrides
// from getenv applied.
func applyOverrides(c Config, getenv func(string) string) (Config, error) {
	if v := getenv("MEAT_MODE_HUB_URL"); v != "" {
		c.HubURL = v
	}
	if v := getenv("MEAT_MODE_ALLOW_INSECURE_HTTP"); v != "" {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("MEAT_MODE_ALLOW_INSECURE_HTTP must be true or false, not %q", v)
		}
		c.AllowInsecureHTTP = on
	}
	if v := getenv(netguard.EnvTrusted); v != "" {
		c.TrustedNetworks = v
	}
	return c, nil
}

// readConfigFile reads the config file as saved, without the environment
// overrides. A missing file gives an empty config. It logs a warning when
// the file, which holds the token, is readable by anyone but its owner.
func readConfigFile(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return c, err
	default:
		if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
			slog.Warn("the bridge config holds the hub token but others can read it; run chmod 600 on it",
				"file", path, "mode", fmt.Sprintf("%04o", fi.Mode().Perm()))
		}
		if err := json.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("read %s: %w", path, err)
		}
	}
	return c, nil
}

// SaveSwitch records one repo's sync switch in the config file at path. It
// rereads the file so environment overrides are never written back.
func SaveSwitch(path, repo string, on bool) error {
	return saveSwitch(path, func(c *Config) *map[string]bool { return &c.Sync }, repo, on)
}

// SaveAgents records one repo's agents switch in the config file at path.
func SaveAgents(path, repo string, on bool) error {
	return saveSwitch(path, func(c *Config) *map[string]bool { return &c.Agents }, repo, on)
}

func saveSwitch(path string, field func(*Config) *map[string]bool, repo string, on bool) error {
	return WithConfigLock(path, func() error {
		c, err := readConfigFile(path)
		if err != nil {
			return err
		}
		m := field(&c)
		if *m == nil {
			*m = map[string]bool{}
		}
		(*m)[repo] = on
		return c.Save(path)
	})
}

// LoadAgents reads the agents map as saved in the config file at path. The
// running bridge calls it before each repo report, so `meat-mode bridge
// agents` reaches the hub within one poll.
func LoadAgents(path string) (map[string]bool, error) {
	c, err := readConfigFile(path)
	return c.Agents, err
}

// WithConfigLock runs fn holding the lock that serializes every
// read-modify-write of the config file at path. It is a lock file of its own,
// not the journal's single-instance lock, so a command can change the config
// while the bridge runs.
func WithConfigLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close() // closing releases the lock
	if err := lockFileWait(f); err != nil {
		return fmt.Errorf("lock %s: %w", f.Name(), err)
	}
	return fn()
}

// Save writes the config with mode 0600, creating its directory.
func (c Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), 0o600)
}

// ResolveRoots returns the scan roots with ~ expanded. With no roots set it
// returns the home folder.
func (c Config) ResolveRoots(home string) []string {
	if c.Roots == nil {
		return []string{home}
	}
	out := make([]string, 0, len(c.Roots))
	for _, r := range c.Roots {
		out = append(out, expandHome(r, home))
	}
	return out
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return p
}

// CheckURL enforces the hub URL policy: https, or http only to a loopback
// host unless allowInsecure is set.
func CheckURL(raw string, allowInsecure bool) error {
	if raw == "" {
		return errors.New("no hub URL is set; run `meat-mode bridge pair <url> <token>`")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("hub URL %q is not a valid URL", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if allowInsecure || isLoopback(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("refusing plain HTTP to %s: the token would cross the network unencrypted. "+
			"Use an https URL, or set allowInsecureHTTP in the config (or MEAT_MODE_ALLOW_INSECURE_HTTP=true) "+
			"if this network is trusted", u.Host)
	}
	return fmt.Errorf("hub URL must start with https:// (or http:// for a local hub), not %q", u.Scheme+"://")
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
