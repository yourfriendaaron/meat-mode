package notify

import (
	"database/sql"
	"os"
)

// FromEnv builds the Web Push registry and the combined notifier from the
// MEAT_MODE_VAPID_SUBJECT, MEAT_MODE_NTFY_URL, MEAT_MODE_NTFY_TOPIC and
// MEAT_MODE_NTFY_TOKEN variables. ntfy is on only when its URL is set.
func FromEnv(dataDir string, db *sql.DB) (*Registry, Notifier, error) {
	reg, err := NewRegistry(dataDir, db, os.Getenv("MEAT_MODE_VAPID_SUBJECT"))
	if err != nil {
		return nil, nil, err
	}
	multi := Multi{reg}
	if u := os.Getenv("MEAT_MODE_NTFY_URL"); u != "" {
		topic := os.Getenv("MEAT_MODE_NTFY_TOPIC")
		if topic == "" {
			topic = "meat-mode"
		}
		multi = append(multi, &Ntfy{URL: u, Topic: topic, Token: os.Getenv("MEAT_MODE_NTFY_TOKEN")})
	}
	return reg, multi, nil
}
