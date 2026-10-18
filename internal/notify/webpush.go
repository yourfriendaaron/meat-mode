package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

// PushHosts are the push services a web subscription's endpoint may point
// at: the host is one of these or a subdomain of one. Any other endpoint
// could make the hub send requests, and the subscriber's data, wherever it
// names. App endpoints follow the endpoint rule instead (EndpointGuard).
var PushHosts = []string{
	"push.services.mozilla.com",
	"fcm.googleapis.com",
	"push.apple.com",
	"web.push.apple.com",
	"notify.windows.com",
}

// MaxSubscriptions is how many confirmed subscriptions, of both kinds, the
// registry keeps; a new one past it drops the oldest.
const MaxSubscriptions = 20

// MaxUnconfirmed is how many app registrations awaiting confirmation the
// registry keeps, outside MaxSubscriptions.
const MaxUnconfirmed = 3

// SubscriptionTTL is how long a subscription lasts without being registered
// again. The app registers again each time it opens.
const SubscriptionTTL = 60 * 24 * time.Hour

// ErrBadEndpoint is returned for a web endpoint that is not an https URL of
// a known push service.
var ErrBadEndpoint = fmt.Errorf("%w: push endpoint must be an https URL of a known push service", store.ErrInvalid)

// Registry stores push subscriptions, the devices they belong to, and sends
// to them (docs/api.md, Devices).
type Registry struct {
	db      *sql.DB
	keys    vapidKeys
	subject string
	hosts   []string // PushHosts; tests add their own
	now     func() time.Time
	// after runs f once d has passed; it schedules retries, and tests run
	// them by hand.
	after func(d time.Duration, f func())
	log   *slog.Logger

	mu    sync.Mutex
	holds map[string]time.Time // endpoint: no sends before this time (429)

	// Client is the HTTP client used for web push services; nil means the
	// default.
	Client *http.Client
	// Guard checks app endpoints and makes their connections.
	Guard *EndpointGuard
	// RequireSession reports whether the hub has a login: every
	// subscription then belongs to a session (the store's sessions table,
	// in the same database) and stops receiving when that session ends.
	// Nil means no login.
	RequireSession func() bool
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
	return &Registry{
		db: db, keys: keys, subject: subject, hosts: PushHosts, now: time.Now,
		after: func(d time.Duration, f func()) { time.AfterFunc(d, f) },
		log:   slog.Default(),
		holds: map[string]time.Time{},
		Guard: NewEndpointGuard(nil, nil),
	}, nil
}

// subscriptionColumns are the columns added since the first table, with
// their definitions, in the order they were added. Times are Unix seconds.
var subscriptionColumns = []struct{ name, def string }{
	{"session_id", "TEXT"},                       // hex session ID, NULL without a login
	{"renewed_at", "INTEGER NOT NULL DEFAULT 0"}, // last registration or renewal
	{"id", "TEXT"},                               // the device ID, 32 hex characters
	{"kind", "TEXT NOT NULL DEFAULT 'web'"},      // api.DeviceWeb or api.DeviceApp
	{"name", "TEXT NOT NULL DEFAULT ''"},         // "" shows as the kind's default name
	{"confirmed", "INTEGER NOT NULL DEFAULT 1"},  // 0 for an app awaiting its code
	{"created_at", "INTEGER NOT NULL DEFAULT 0"}, // first registration
	{"last_seen", "INTEGER NOT NULL DEFAULT 0"},  // last registration, renewal or token call
	{"last_push", "INTEGER"},                     // last push the push service accepted
	{"failing_since", "INTEGER"},                 // first failed send since the last delivery
	{"code_hash", "TEXT"},                        // hex SHA-256 of the confirmation code
	{"code_expires", "INTEGER"},                  // when the confirmation code stops working
	{"token_hash", "TEXT"},                       // hex SHA-256 of the device token
}

// createSubscriptions creates the subscription table, or adds the columns an
// older one lacks. Existing rows count as registered now, so they get the
// full SubscriptionTTL to register again, and become web devices with an ID.
func createSubscriptions(db *sql.DB, now time.Time) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS push_subscriptions (
		endpoint   TEXT PRIMARY KEY,
		p256dh     TEXT NOT NULL,
		auth       TEXT NOT NULL
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
	for _, c := range subscriptionColumns {
		if !cols[c.name] {
			if _, err := db.Exec(`ALTER TABLE push_subscriptions ADD COLUMN ` + c.name + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	for _, stmt := range []string{
		`UPDATE push_subscriptions SET renewed_at = ? WHERE renewed_at = 0`,
		`UPDATE push_subscriptions SET id = lower(hex(randomblob(16))) WHERE id IS NULL`,
		`UPDATE push_subscriptions SET created_at = renewed_at WHERE created_at = 0`,
		`UPDATE push_subscriptions SET last_seen = renewed_at WHERE last_seen = 0`,
		`CREATE UNIQUE INDEX IF NOT EXISTS push_subscriptions_id ON push_subscriptions (id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS push_subscriptions_token ON push_subscriptions (token_hash)`,
	} {
		var args []any
		if strings.Contains(stmt, "?") {
			args = append(args, now.Unix())
		}
		if _, err := db.Exec(stmt, args...); err != nil {
			return err
		}
	}
	return nil
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

func (r *Registry) requireSession() bool {
	return r.RequireSession != nil && r.RequireSession()
}

// Subscribe stores a web subscription as the newest, registered now by the
// session sessionID ("" for a hub without a login), replacing any with the
// same endpoint, and drops the oldest past MaxSubscriptions. A browser that
// subscribes again keeps its device ID.
func (r *Registry) Subscribe(ctx context.Context, sessionID string, s api.PushSubscription) error {
	if s.Kind != "" && s.Kind != api.DeviceWeb {
		return fmt.Errorf("%w: Subscribe takes web subscriptions only", store.ErrInvalid)
	}
	name, session, err := r.checkRegistration(sessionID, s)
	if err != nil {
		return err
	}
	if !r.allowed(s.Endpoint) {
		return ErrBadEndpoint
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	d, err := readDevice(tx, `endpoint = ?`, s.Endpoint)
	if err != nil {
		return err
	}
	if d == nil || d.Kind != api.DeviceWeb {
		if d, err = newDevice(api.DeviceWeb, r.now()); err != nil {
			return err
		}
	}
	d.Endpoint, d.P256dh, d.Auth, d.Name, d.Session = s.Endpoint, s.Keys.P256dh, s.Keys.Auth, name, session
	if err := r.writeDevice(tx, d); err != nil {
		return err
	}
	return tx.Commit()
}

// checkRegistration checks what every registration needs: keys, a session
// while login is on, and a name of at most 64 characters. It returns the
// trimmed name and the session as a column value.
func (r *Registry) checkRegistration(sessionID string, s api.PushSubscription) (string, any, error) {
	if s.Endpoint == "" || s.Keys.P256dh == "" || s.Keys.Auth == "" {
		return "", nil, fmt.Errorf("%w: push subscription needs endpoint, p256dh and auth", store.ErrInvalid)
	}
	if r.requireSession() && sessionID == "" {
		return "", nil, fmt.Errorf("%w: log in before turning on reminders", store.ErrUnauthorized)
	}
	name := strings.TrimSpace(s.Name)
	if utf8.RuneCountInString(name) > maxDeviceName {
		return "", nil, fmt.Errorf("%w: a device name is at most %d characters", store.ErrInvalid, maxDeviceName)
	}
	var session any
	if sessionID != "" {
		session = sessionID
	}
	return name, session, nil
}

// Unsubscribe removes a subscription; an unknown endpoint is not an error.
func (r *Registry) Unsubscribe(ctx context.Context, endpoint string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

// DropSession removes the subscriptions and devices the session sessionID
// registered, when it logs out.
func (r *Registry) DropSession(ctx context.Context, sessionID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE session_id = ?`, sessionID)
	return err
}

// prune deletes subscriptions not registered again within SubscriptionTTL,
// app registrations not confirmed within confirmTTL and, with a login, those
// whose session has ended or that belong to none.
func (r *Registry) prune(ctx context.Context) error {
	now := r.now()
	if _, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE renewed_at <= ?
		OR (confirmed = 0 AND created_at <= ?)`,
		now.Add(-SubscriptionTTL).Unix(), now.Add(-confirmTTL).Unix()); err != nil {
		return err
	}
	if !r.requireSession() {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE session_id IS NULL OR NOT EXISTS
		(SELECT 1 FROM sessions WHERE sessions.id = unhex(push_subscriptions.session_id) AND sessions.expires_at > ?)`,
		now.Unix())
	return err
}

// target is a subscription a push goes to.
type target struct {
	id, endpoint, p256dh, auth, kind string
}

func (r *Registry) list(ctx context.Context) ([]target, error) {
	return r.query(ctx, `WHERE confirmed = 1 ORDER BY endpoint`)
}

// query prunes, then reads the subscriptions the clause selects.
func (r *Registry) query(ctx context.Context, clause string, args ...any) ([]target, error) {
	if err := r.prune(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT coalesce(id, ''), endpoint, p256dh, auth, kind FROM push_subscriptions `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.endpoint, &t.p256dh, &t.auth, &t.kind); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Notify sends m to every live, confirmed subscription (see prune). Each
// send's result is handled as docs/api.md, Devices, says: a gone
// subscription is removed, and a server error or network failure is retried
// later. The failures of the first attempts come back joined and do not
// stop the remaining sends.
func (r *Registry) Notify(ctx context.Context, m Message) error {
	subs, err := r.list(ctx)
	if err != nil {
		return err
	}
	payload := pushPayload(m)
	var errs []error
	for _, t := range subs {
		if _, err := r.deliver(ctx, t, payload, messageTTL, 0, true); err != nil && !errors.Is(err, errHeld) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// notifyDevice sends m once to the subscription at endpoint and reports
// whether the hub had it and whether the push service accepted it. With a
// login it must belong to the session sessionID, so one session cannot test
// another's device.
func (r *Registry) notifyDevice(ctx context.Context, sessionID, endpoint string, m Message) (found, delivered bool, err error) {
	clause := `WHERE confirmed = 1 AND endpoint = ?`
	args := []any{endpoint}
	if r.requireSession() {
		clause += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	subs, err := r.query(ctx, clause, args...)
	if err != nil || len(subs) == 0 {
		return false, false, err
	}
	delivered, err = r.deliver(ctx, subs[0], pushPayload(m), messageTTL, 0, false)
	return true, delivered, err
}

// TestReminder sends a test reminder to the calling device's subscription,
// the one at endpoint, and to ntfy when it is set up. A failure is reported
// in the result, not returned, so the caller can say which way did not work.
// Only the first attempt counts: a test reminder is never retried.
func (r *Registry) TestReminder(ctx context.Context, sessionID, endpoint, url string) api.ReminderTest {
	m := Message{Title: "Test reminder", Body: "Reminders from Meat Mode are working.", URL: url, Tag: "test", Channel: ChannelTest}
	out := api.ReminderTest{Push: "sent", Ntfy: "off"}
	found, delivered, err := r.notifyDevice(ctx, sessionID, endpoint, m)
	switch {
	case delivered:
	case err != nil:
		out.Push = "failed"
	case !found:
		out.Push = "none"
	default:
		// The push service no longer knows the subscription, or its endpoint
		// is not allowed, so it was removed.
		out.Push = "expired"
	}
	if r.Ntfy != nil {
		out.Ntfy = "sent"
		if err := r.Ntfy.Notify(ctx, m); err != nil {
			out.Ntfy = "failed"
		}
	}
	return out
}
