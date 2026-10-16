// Package hub serves the app API, the bridge API and the embedded frontend.
// docs/api.md describes the routes.
package hub

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
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
}

// ConfigFromEnv reads Config from the environment.
func ConfigFromEnv() Config {
	return Config{
		Listen:    envOr("MEAT_MODE_LISTEN", ":8080"),
		DataDir:   os.Getenv("MEAT_MODE_DATA_DIR"),
		PublicURL: os.Getenv("MEAT_MODE_PUBLIC_URL"),
		User:      envOr("MEAT_MODE_USER", "user"),

		AllowedHosts: os.Getenv("MEAT_MODE_ALLOWED_HOSTS"),
	}
}

// Validate refuses a hub without an HTTPS public address. Browsers allow
// Web Push and the installed app only in a secure context, and the app has
// no login, so it is meant to be opened over a private network's HTTPS
// address (docs/deploy.md). Plain HTTP is accepted on loopback only, for
// development and tests.
func (c Config) Validate() error {
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
