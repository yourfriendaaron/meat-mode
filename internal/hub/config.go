// Package hub serves the app API, the bridge API and the embedded frontend.
// docs/api.md describes the routes.
package hub

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/auth"
)

// Config is the hub's settings, read from MEAT_MODE_* environment variables.
type Config struct {
	Listen    string // MEAT_MODE_LISTEN, default :8080
	DataDir   string // MEAT_MODE_DATA_DIR
	PublicURL string // MEAT_MODE_PUBLIC_URL
	User      string // MEAT_MODE_USER, default "user"; notes are authored <User>-app
	// AllowedHosts is MEAT_MODE_ALLOWED_HOSTS: comma-separated host names
	// the app answers to besides localhost and the PublicURL host.
	AllowedHosts string
	// PasswordHash is MEAT_MODE_PASSWORD_HASH: the Argon2id PHC string of
	// the login password. Login is on when it is set.
	PasswordHash string
	// PasswordHashFile is MEAT_MODE_PASSWORD_HASH_FILE, a file holding the
	// hash instead. LoadPasswordHash reads it into PasswordHash.
	PasswordHashFile string
	// NtfyURL is MEAT_MODE_NTFY_URL. The hub only reports whether it is
	// set, in the connection check; internal/notify does the sending.
	NtfyURL string
}

// ConfigFromEnv reads Config from the environment.
func ConfigFromEnv() Config {
	return Config{
		Listen:    envOr("MEAT_MODE_LISTEN", ":8080"),
		DataDir:   os.Getenv("MEAT_MODE_DATA_DIR"),
		PublicURL: os.Getenv("MEAT_MODE_PUBLIC_URL"),
		User:      envOr("MEAT_MODE_USER", "user"),

		AllowedHosts:     os.Getenv("MEAT_MODE_ALLOWED_HOSTS"),
		PasswordHash:     strings.TrimSpace(os.Getenv("MEAT_MODE_PASSWORD_HASH")),
		PasswordHashFile: os.Getenv("MEAT_MODE_PASSWORD_HASH_FILE"),
		NtfyURL:          strings.TrimSpace(os.Getenv("MEAT_MODE_NTFY_URL")),
	}
}

// LoadPasswordHash reads PasswordHashFile into PasswordHash. Setting both is
// an error, since it is unclear which one the operator meant.
func (c *Config) LoadPasswordHash() error {
	if c.PasswordHashFile == "" {
		return nil
	}
	if c.PasswordHash != "" {
		return errors.New("set MEAT_MODE_PASSWORD_HASH or MEAT_MODE_PASSWORD_HASH_FILE, not both")
	}
	b, err := os.ReadFile(c.PasswordHashFile)
	if err != nil {
		return fmt.Errorf("MEAT_MODE_PASSWORD_HASH_FILE: %w", err)
	}
	h := strings.TrimSpace(string(b))
	if h == "" {
		return fmt.Errorf("MEAT_MODE_PASSWORD_HASH_FILE %s is empty", c.PasswordHashFile)
	}
	c.PasswordHash, c.PasswordHashFile = h, ""
	return nil
}

// Validate refuses a hub without an HTTPS public address, or with a password
// hash it cannot read. Browsers allow Web Push and the installed app only in
// a secure context, and without a login the app is meant to be opened over a
// private network's HTTPS address (docs/deploy.md). Plain HTTP is accepted
// on loopback only, for development and tests.
func (c Config) Validate() error {
	if c.PasswordHash != "" && c.PasswordHashFile != "" {
		return errors.New("set MEAT_MODE_PASSWORD_HASH or MEAT_MODE_PASSWORD_HASH_FILE, not both")
	}
	if c.PasswordHash != "" {
		if _, err := auth.ParseHash(c.PasswordHash); err != nil {
			return fmt.Errorf("the login password hash is %w", err)
		}
	}
	if c.PublicURL == "" {
		return errors.New("MEAT_MODE_PUBLIC_URL is not set: set it to the HTTPS address you open the app at, such as https://meat-mode.example.com")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("MEAT_MODE_PUBLIC_URL %q is not a URL such as https://meat-mode.example.com", c.PublicURL)
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	}
	return fmt.Errorf("MEAT_MODE_PUBLIC_URL %q must start with https:// (plain http:// is accepted for localhost only)", c.PublicURL)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
