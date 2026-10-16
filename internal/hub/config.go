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
}

// ConfigFromEnv reads Config from the environment.
func ConfigFromEnv() Config {
	return Config{
		Listen:    envOr("MEAT_MODE_LISTEN", ":8080"),
		DataDir:   os.Getenv("MEAT_MODE_DATA_DIR"),
		PublicURL: os.Getenv("MEAT_MODE_PUBLIC_URL"),
		User:      envOr("MEAT_MODE_USER", "user"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
