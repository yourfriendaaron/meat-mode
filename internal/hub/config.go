// Package hub serves the app API, the bridge API and the embedded frontend.
// docs/api.md describes the routes.
package hub

import "os"

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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
