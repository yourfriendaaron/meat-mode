package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

type vapidKeys struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

// loadVAPID reads vapid.json from dir, generating it on first use.
// Keys are never rotated automatically.
func loadVAPID(dir string) (vapidKeys, error) {
	path := filepath.Join(dir, "vapid.json")
	b, err := os.ReadFile(path)
	if err == nil {
		var k vapidKeys
		if err := json.Unmarshal(b, &k); err != nil {
			return k, fmt.Errorf("read %s: %w", path, err)
		}
		if k.PrivateKey == "" || k.PublicKey == "" {
			return k, fmt.Errorf("read %s: missing keys", path)
		}
		return k, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return vapidKeys{}, err
	}
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return vapidKeys{}, err
	}
	k := vapidKeys{PrivateKey: priv, PublicKey: pub}
	b, _ = json.Marshal(k)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return k, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return k, err
	}
	return k, nil
}

// Registry stores Web Push subscriptions and sends to them.
type Registry struct {
	db      *sql.DB
	keys    vapidKeys
	subject string
	// Client is the HTTP client used for push services; nil means the default.
	Client *http.Client
}

// NewRegistry loads or creates the VAPID keys in dataDir and the
// subscription table in db. subject is a mailto: or https: contact.
func NewRegistry(dataDir string, db *sql.DB, subject string) (*Registry, error) {
	keys, err := loadVAPID(dataDir)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS push_subscriptions (
		endpoint TEXT PRIMARY KEY,
		p256dh   TEXT NOT NULL,
		auth     TEXT NOT NULL
	)`)
	if err != nil {
		return nil, err
	}
	subject = strings.TrimPrefix(subject, "mailto:") // the library adds it
	return &Registry{db: db, keys: keys, subject: subject}, nil
}

// PublicKey is the VAPID public key the browser subscribes with.
func (r *Registry) PublicKey() string { return r.keys.PublicKey }

// Subscribe stores a subscription, replacing any with the same endpoint.
func (r *Registry) Subscribe(ctx context.Context, s api.PushSubscription) error {
	if s.Endpoint == "" || s.Keys.P256dh == "" || s.Keys.Auth == "" {
		return errors.New("push subscription needs endpoint, p256dh and auth")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO push_subscriptions (endpoint, p256dh, auth)
		VALUES (?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth`,
		s.Endpoint, s.Keys.P256dh, s.Keys.Auth)
	return err
}

// Unsubscribe removes a subscription; an unknown endpoint is not an error.
func (r *Registry) Unsubscribe(ctx context.Context, endpoint string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

func (r *Registry) list(ctx context.Context) ([]webpush.Subscription, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT endpoint, p256dh, auth FROM push_subscriptions ORDER BY endpoint`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webpush.Subscription
	for rows.Next() {
		var s webpush.Subscription
		if err := rows.Scan(&s.Endpoint, &s.Keys.P256dh, &s.Keys.Auth); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Notify sends m to every subscription. A 404 or 410 from the push service
// removes that subscription. Other failures are returned joined and do not
// stop the remaining sends.
func (r *Registry) Notify(ctx context.Context, m Message) error {
	subs, err := r.list(ctx)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{
		"title": m.Title, "body": m.Body, "url": m.URL, "tag": m.Tag,
	})
	opts := &webpush.Options{
		Subscriber:      r.subject,
		VAPIDPublicKey:  r.keys.PublicKey,
		VAPIDPrivateKey: r.keys.PrivateKey,
		TTL:             86400,
		Urgency:         webpush.UrgencyHigh,
	}
	if r.Client != nil {
		opts.HTTPClient = r.Client
	}
	var errs []error
	for i := range subs {
		resp, err := webpush.SendNotificationWithContext(ctx, payload, &subs[i], opts)
		if err != nil {
			errs = append(errs, fmt.Errorf("web push: %w", err))
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			if err := r.Unsubscribe(ctx, subs[i].Endpoint); err != nil {
				errs = append(errs, err)
			}
		case resp.StatusCode/100 != 2:
			errs = append(errs, fmt.Errorf("web push: status %d", resp.StatusCode))
		}
	}
	return errors.Join(errs...)
}
