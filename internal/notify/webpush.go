package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
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
		// A file left world-readable (by a copy or a restore) is tightened.
		if fi, statErr := os.Stat(path); statErr == nil && fi.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(path, 0o600)
		}
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
	if err := writeVAPID(path, b); err != nil {
		return k, err
	}
	return k, nil
}

// writeVAPID writes b to path through a temp file in the same directory
// (fsync, then rename), so a crash cannot leave a partial or readable key
// file behind.
func writeVAPID(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vapid-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	write := func() error {
		if _, err := tmp.Write(b); err != nil {
			return err
		}
		if err := tmp.Chmod(0o600); err != nil {
			return err
		}
		return tmp.Sync()
	}
	err = write()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// PushHosts are the push services a subscription endpoint may point at: the
// host is one of these or a subdomain of one. Any other endpoint could make
// the hub send requests, and the subscriber's data, wherever it names.
var PushHosts = []string{
	"push.services.mozilla.com",
	"fcm.googleapis.com",
	"push.apple.com",
	"web.push.apple.com",
	"notify.windows.com",
}

// MaxSubscriptions is how many subscriptions the registry keeps; a new one
// past it drops the oldest.
const MaxSubscriptions = 20

// SubscriptionTTL is how long a subscription lasts without being registered
// again. The app registers again each time it opens.
const SubscriptionTTL = 60 * 24 * time.Hour

// ErrBadEndpoint is returned for an endpoint that is not an https URL of a
// known push service.
var ErrBadEndpoint = fmt.Errorf("%w: push endpoint must be an https URL of a known push service", store.ErrInvalid)

// Registry stores Web Push subscriptions and sends to them.
type Registry struct {
	db      *sql.DB
	keys    vapidKeys
	subject string
	hosts   []string // PushHosts; tests add their own
	now     func() time.Time
	// Client is the HTTP client used for push services; nil means the default.
	Client *http.Client
	// RequireSession is set when the hub has a login: every subscription
	// then belongs to a session (the store's sessions table, in the same
	// database) and stops receiving when that session ends.
	RequireSession bool
	// Ntfy also receives test reminders; nil when ntfy is not set up.
	Ntfy Notifier
}

// NewRegistry loads or creates the VAPID keys in dataDir and the
// subscription table in db. subject is a mailto: or https: contact.
func NewRegistry(dataDir string, db *sql.DB, subject string) (*Registry, error) {
	if !validSubject(subject) {
		return nil, fmt.Errorf("MEAT_MODE_VAPID_SUBJECT must be a mailto: address containing @ or an https:// URL, not %q", subject)
	}
	keys, err := loadVAPID(dataDir)
	if err != nil {
		return nil, err
	}
	if err := createSubscriptions(db, time.Now()); err != nil {
		return nil, err
	}
	subject, _ = strings.CutPrefix(subject, "mailto:") // the library adds mailto: back unless the value starts with https:
	return &Registry{db: db, keys: keys, subject: subject, hosts: PushHosts, now: time.Now}, nil
}

// createSubscriptions creates the subscription table, or adds the session
// and renewal columns to one from before the login. Existing rows count as
// registered now, so they get the full SubscriptionTTL to register again.
// session_id is the hex session ID, or NULL for a hub without a login;
// renewed_at is Unix seconds.
func createSubscriptions(db *sql.DB, now time.Time) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS push_subscriptions (
		endpoint   TEXT PRIMARY KEY,
		p256dh     TEXT NOT NULL,
		auth       TEXT NOT NULL,
		session_id TEXT,
		renewed_at INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT name FROM pragma_table_info('push_subscriptions')`)
	if err != nil {
		return err
	}
	cols := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		cols[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["session_id"] {
		if _, err := db.Exec(`ALTER TABLE push_subscriptions ADD COLUMN session_id TEXT`); err != nil {
			return err
		}
	}
	if !cols["renewed_at"] {
		if _, err := db.Exec(`ALTER TABLE push_subscriptions ADD COLUMN renewed_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`UPDATE push_subscriptions SET renewed_at = ? WHERE renewed_at = 0`, now.Unix())
	return err
}

// validSubject reports whether the VAPID subject is a mailto: address with
// an @ in it, or an https:// URL; anything else fails every push later.
func validSubject(s string) bool {
	if rest, ok := strings.CutPrefix(s, "mailto:"); ok {
		return strings.Contains(rest, "@")
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// PublicKey is the VAPID public key the browser subscribes with.
func (r *Registry) PublicKey() string { return r.keys.PublicKey }

// allowed reports whether endpoint is an https URL of a known push service.
func (r *Registry) allowed(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && port != "443" {
		// Push services listen on 443. A host:port entry (only tests add
		// one) allows exactly that address.
		return slices.Contains(r.hosts, host+":"+port)
	}
	for _, h := range r.hosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// Subscribe stores a subscription as the newest, registered now by the
// session sessionID ("" for a hub without a login), replacing any with the
// same endpoint, and drops the oldest past MaxSubscriptions.
func (r *Registry) Subscribe(ctx context.Context, sessionID string, s api.PushSubscription) error {
	if s.Endpoint == "" || s.Keys.P256dh == "" || s.Keys.Auth == "" {
		return fmt.Errorf("%w: push subscription needs endpoint, p256dh and auth", store.ErrInvalid)
	}
	if r.RequireSession && sessionID == "" {
		return fmt.Errorf("%w: log in before turning on reminders", store.ErrUnauthorized)
	}
	var session any
	if sessionID != "" {
		session = sessionID
	}
	if !r.allowed(s.Endpoint) {
		return ErrBadEndpoint
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Delete and insert, so a renewed subscription gets the newest rowid.
	if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ?`, s.Endpoint); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO push_subscriptions (endpoint, p256dh, auth, session_id, renewed_at) VALUES (?, ?, ?, ?, ?)`,
		s.Endpoint, s.Keys.P256dh, s.Keys.Auth, session, r.now().Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE rowid NOT IN
		(SELECT rowid FROM push_subscriptions ORDER BY rowid DESC LIMIT ?)`, MaxSubscriptions); err != nil {
		return err
	}
	return tx.Commit()
}

// Unsubscribe removes a subscription; an unknown endpoint is not an error.
func (r *Registry) Unsubscribe(ctx context.Context, endpoint string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

// DropSession removes the subscriptions the session sessionID registered,
// when it logs out.
func (r *Registry) DropSession(ctx context.Context, sessionID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE session_id = ?`, sessionID)
	return err
}

// prune deletes subscriptions not registered again within SubscriptionTTL
// and, with a login, those whose session has ended or that belong to none.
func (r *Registry) prune(ctx context.Context) error {
	now := r.now()
	if _, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE renewed_at <= ?`,
		now.Add(-SubscriptionTTL).Unix()); err != nil {
		return err
	}
	if !r.RequireSession {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE session_id IS NULL OR NOT EXISTS
		(SELECT 1 FROM sessions WHERE sessions.id = unhex(push_subscriptions.session_id) AND sessions.expires_at > ?)`,
		now.Unix())
	return err
}

func (r *Registry) list(ctx context.Context) ([]webpush.Subscription, error) {
	return r.query(ctx, `SELECT endpoint, p256dh, auth FROM push_subscriptions ORDER BY endpoint`)
}

// query prunes, then reads the subscriptions the statement selects.
func (r *Registry) query(ctx context.Context, stmt string, args ...any) ([]webpush.Subscription, error) {
	if err := r.prune(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, stmt, args...)
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

// Notify sends m to every live subscription (see prune). A 404 or 410 from the push service
// removes that subscription. Other failures are returned joined and do not
// stop the remaining sends.
func (r *Registry) Notify(ctx context.Context, m Message) error {
	subs, err := r.list(ctx)
	if err != nil {
		return err
	}
	return r.send(ctx, subs, m)
}

// NotifySession sends m to the subscriptions the session sessionID
// registered ("" for a hub without a login) and reports how many there were.
func (r *Registry) NotifySession(ctx context.Context, sessionID string, m Message) (int, error) {
	var session any
	if sessionID != "" {
		session = sessionID
	}
	subs, err := r.query(ctx, `SELECT endpoint, p256dh, auth FROM push_subscriptions WHERE session_id IS ? ORDER BY endpoint`, session)
	if err != nil {
		return 0, err
	}
	return len(subs), r.send(ctx, subs, m)
}

func (r *Registry) send(ctx context.Context, subs []webpush.Subscription, m Message) error {
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
		if !r.allowed(subs[i].Endpoint) {
			// Stored before endpoints were checked; never send to it.
			if err := r.Unsubscribe(ctx, subs[i].Endpoint); err != nil {
				errs = append(errs, err)
			}
			continue
		}
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

// TestReminder sends a test reminder to the session's own subscriptions and
// to ntfy when it is set up. A failure is reported in the result, not
// returned, so the caller can say which way did not work.
func (r *Registry) TestReminder(ctx context.Context, sessionID, url string) api.ReminderTest {
	m := Message{Title: "Test reminder", Body: "Reminders from Meat Mode are working.", URL: url, Tag: "test"}
	out := api.ReminderTest{Push: "sent", Ntfy: "off"}
	if n, err := r.NotifySession(ctx, sessionID, m); err != nil {
		out.Push = "failed"
	} else if n == 0 {
		out.Push = "none"
	}
	if r.Ntfy != nil {
		out.Ntfy = "sent"
		if err := r.Ntfy.Notify(ctx, m); err != nil {
			out.Ntfy = "failed"
		}
	}
	return out
}
