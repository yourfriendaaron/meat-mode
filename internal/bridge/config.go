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
)

// Config is the bridge's settings file, bridge.json.
type Config struct {
	HubURL string `json:"hubURL"`
	Token  string `json:"token"`
	// Roots are the directories scanned for beads repos. Absent (null) means
	// the home folder; an empty list means none.
	Roots []string `json:"roots"`
	// Personal is the personal beads repo directory; optional.
	Personal          string `json:"personal,omitempty"`
	AllowInsecureHTTP bool   `json:"allowInsecureHTTP,omitempty"`
	// BD is the bd executable; found on PATH when empty.
	BD string `json:"bd,omitempty"`
}

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

// LoadConfig reads the config file and applies the MEAT_MODE_HUB_URL and
// MEAT_MODE_ALLOW_INSECURE_HTTP overrides. A missing file gives an empty
// config. It logs a warning when the file, which holds the token, is
// readable by anyone but its owner.
func LoadConfig(path string) (Config, error) {
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
	if v := os.Getenv("MEAT_MODE_HUB_URL"); v != "" {
		c.HubURL = v
	}
	if v := os.Getenv("MEAT_MODE_ALLOW_INSECURE_HTTP"); v != "" {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("MEAT_MODE_ALLOW_INSECURE_HTTP must be true or false, not %q", v)
		}
		c.AllowInsecureHTTP = on
	}
	return c, nil
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

// ResolvePersonal returns the personal repo path with ~ expanded.
func (c Config) ResolvePersonal(home string) string {
	return expandHome(c.Personal, home)
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
