package notify

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"time"
)

// httpTimeout bounds every notification delivery, so one hung connection
// cannot stall the reminder scheduler.
const httpTimeout = 20 * time.Second

// FromEnv builds the Web Push registry and the combined notifier from the
// MEAT_MODE_VAPID_SUBJECT, MEAT_MODE_NTFY_URL, MEAT_MODE_NTFY_TOPIC and
// MEAT_MODE_NTFY_TOKEN variables. ntfy is on only when its URL is set.
func FromEnv(dataDir string, db *sql.DB) (*Registry, Notifier, error) {
	reg, err := NewRegistry(dataDir, db, os.Getenv("MEAT_MODE_VAPID_SUBJECT"))
	if err != nil {
		return nil, nil, err
	}
	client := &http.Client{Timeout: httpTimeout}
	reg.Client = client
	multi := Multi{reg}
	if u := os.Getenv("MEAT_MODE_NTFY_URL"); u != "" {
		topic := os.Getenv("MEAT_MODE_NTFY_TOPIC")
		if topic == "" {
			return nil, nil, errors.New("MEAT_MODE_NTFY_TOPIC is not set: on a shared ntfy server the topic name works like a password, so pick a long random one (a token alone does not hide the topic)")
		}
		multi = append(multi, &Ntfy{URL: u, Topic: topic, Token: os.Getenv("MEAT_MODE_NTFY_TOKEN"), Client: client})
	}
	return reg, multi, nil
}
