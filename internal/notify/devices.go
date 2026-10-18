package notify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Device settings (docs/api.md, Devices).
const (
	confirmTTL    = 10 * time.Minute // a confirmation code, and an unconfirmed registration
	maxDeviceName = 64
	// DeviceTokenPrefix starts every device token.
	DeviceTokenPrefix = "mmd_"
)

// Default device names, for a registration that names none.
const (
	defaultWebName = "Browser"
	defaultAppName = "Android app"
)

// ErrBadCode is a confirmation code that is wrong, used or expired, or one
// for a device the hub does not have.
var ErrBadCode = fmt.Errorf("%w: the confirmation code is wrong, used or expired; turn reminders on again in the app", store.ErrForbidden)

// device is one row of push_subscriptions.
type device struct {
	ID, Endpoint, P256dh, Auth, Kind, Name string
	Session                                any // hex session ID, or nil
	Confirmed                              bool
	CreatedAt                              int64
	LastPush, FailingSince, CodeExpires    sql.NullInt64
	CodeHash, TokenHash                    sql.NullString
}

// newDevice is a confirmed device of kind with a new ID, created now.
func newDevice(kind string, now time.Time) (*device, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &device{ID: hex.EncodeToString(b), Kind: kind, Confirmed: true, CreatedAt: now.Unix()}, nil
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// readDevice reads the device the condition selects, or nil.
func readDevice(q querier, cond string, args ...any) (*device, error) {
	var d device
	var session sql.NullString
	var id sql.NullString
	err := q.QueryRow(`SELECT id, endpoint, p256dh, auth, kind, name, session_id, confirmed, created_at,
		last_push, failing_since, code_expires, code_hash, token_hash FROM push_subscriptions WHERE `+cond, args...).
		Scan(&id, &d.Endpoint, &d.P256dh, &d.Auth, &d.Kind, &d.Name, &session, &d.Confirmed, &d.CreatedAt,
			&d.LastPush, &d.FailingSince, &d.CodeExpires, &d.CodeHash, &d.TokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !id.Valid {
		// A row written outside the registry; give it an ID now.
		nd, err := newDevice(d.Kind, time.Unix(d.CreatedAt, 0))
		if err != nil {
			return nil, err
		}
		id.String = nd.ID
	}
	d.ID = id.String
	if session.Valid {
		d.Session = session.String
	}
	return &d, nil
}

// writeDevice stores d as the newest row, registered and seen now, in place
// of any row with its ID or its endpoint, then keeps the newest
// MaxSubscriptions confirmed and MaxUnconfirmed unconfirmed rows.
func (r *Registry) writeDevice(tx *sql.Tx, d *device) error {
	// Delete and insert, so a renewed subscription gets the newest rowid.
	if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE id = ? OR endpoint = ?`, d.ID, d.Endpoint); err != nil {
		return err
	}
	now := r.now().Unix()
	if _, err := tx.Exec(`INSERT INTO push_subscriptions (endpoint, p256dh, auth, session_id, renewed_at, id, kind, name,
		confirmed, created_at, last_seen, last_push, failing_since, code_hash, code_expires, token_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Endpoint, d.P256dh, d.Auth, d.Session, now, d.ID, d.Kind, d.Name,
		d.Confirmed, d.CreatedAt, now, d.LastPush, d.FailingSince, d.CodeHash, d.CodeExpires, d.TokenHash); err != nil {
		return err
	}
	for _, c := range []struct {
		confirmed bool
		keep      int
	}{{true, MaxSubscriptions}, {false, MaxUnconfirmed}} {
		if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE confirmed = ? AND rowid NOT IN
			(SELECT rowid FROM push_subscriptions WHERE confirmed = ? ORDER BY rowid DESC LIMIT ?)`,
			c.confirmed, c.confirmed, c.keep); err != nil {
			return err
		}
	}
	return nil
}

// secret returns a random 32-byte secret, base64url, and its hex SHA-256.
func secret() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	s := base64.RawURLEncoding.EncodeToString(b)
	return s, hashSecret(s), nil
}

func hashSecret(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// RegisterApp registers the Android app's subscription s for the session
// sessionID ("" for a hub without a login). An endpoint that already belongs
// to a confirmed device renews that device and reports renewed. Any other
// becomes a new device awaiting confirmation: the hub pushes it a code at
// once and returns its ID.
func (r *Registry) RegisterApp(ctx context.Context, sessionID string, s api.PushSubscription) (id string, renewed bool, err error) {
	name, session, err := r.checkRegistration(sessionID, s)
	if err != nil {
		return "", false, err
	}
	if name == "" {
		name = defaultAppName
	}
	if err := r.Guard.Check(ctx, s.Endpoint); err != nil {
		return "", false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	d, err := readDevice(tx, `endpoint = ?`, s.Endpoint)
	if err != nil {
		return "", false, err
	}
	renewed = d != nil && d.Kind == api.DeviceApp && d.Confirmed
	var code string
	if !renewed {
		if d, err = newDevice(api.DeviceApp, r.now()); err != nil {
			return "", false, err
		}
		var hash string
		if code, hash, err = secret(); err != nil {
			return "", false, err
		}
		d.Confirmed = false
		d.CodeHash = sql.NullString{String: hash, Valid: true}
		d.CodeExpires = sql.NullInt64{Int64: r.now().Add(confirmTTL).Unix(), Valid: true}
	}
	d.Endpoint, d.P256dh, d.Auth, d.Name, d.Session = s.Endpoint, s.Keys.P256dh, s.Keys.Auth, name, session
	if err := r.writeDevice(tx, d); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	if !renewed {
		r.sendConfirmation(ctx, d, code)
	}
	return d.ID, renewed, nil
}

// sendConfirmation pushes the confirmation code to a new app device, once.
// The app is waiting for it; a code that does not arrive leaves the
// registration to expire, and the app shows reminders as off.
func (r *Registry) sendConfirmation(ctx context.Context, d *device, code string) {
	type confirm struct {
		Device string `json:"device"`
		Code   string `json:"code"`
	}
	payload := marshalPayload(struct {
		Confirm confirm `json:"confirm"`
	}{confirm{d.ID, code}})
	t := target{id: d.ID, endpoint: d.Endpoint, p256dh: d.P256dh, auth: d.Auth, kind: d.Kind}
	if _, err := r.deliver(ctx, t, payload, int(confirmTTL/time.Second), 0, false); err != nil {
		r.log.Warn("could not push the confirmation code to a new device", "device", d.ID, "err", err)
	}
}

// ConfirmDevice checks the code the app got in its confirmation push,
// confirms the device and returns its device token, which is not stored and
// cannot be shown again. A code works once, within confirmTTL.
func (r *Registry) ConfirmDevice(ctx context.Context, id, code string) (string, error) {
	if err := r.prune(ctx); err != nil {
		return "", err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	d, err := readDevice(tx, `id = ? AND kind = ? AND confirmed = 0`, id, api.DeviceApp)
	if err != nil {
		return "", err
	}
	if d == nil || !d.CodeHash.Valid || !d.CodeExpires.Valid || d.CodeExpires.Int64 <= r.now().Unix() ||
		subtle.ConstantTimeCompare([]byte(hashSecret(code)), []byte(d.CodeHash.String)) != 1 {
		return "", ErrBadCode
	}
	raw, _, err := secret()
	if err != nil {
		return "", err
	}
	token := DeviceTokenPrefix + raw
	d.Confirmed = true
	d.CodeHash, d.CodeExpires = sql.NullString{}, sql.NullInt64{}
	d.TokenHash = sql.NullString{String: hashSecret(token), Valid: true}
	if err := r.writeDevice(tx, d); err != nil {
		return "", err
	}
	return token, tx.Commit()
}

// DeviceByToken returns the ID of the confirmed, live device whose token
// this is, and records that it was seen. An unknown, revoked or dropped
// device's token is store.ErrUnauthorized.
func (r *Registry) DeviceByToken(ctx context.Context, token string) (string, error) {
	if err := r.prune(ctx); err != nil {
		return "", err
	}
	var id string
	err := r.db.QueryRowContext(ctx, `UPDATE push_subscriptions SET last_seen = ?
		WHERE token_hash = ? AND confirmed = 1 RETURNING id`, r.now().Unix(), hashSecret(token)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrUnauthorized
	}
	return id, err
}

// UpdateDevice replaces the endpoint and keys of the app device id, from
// PUT /api/devices/self, and renews it. The device keeps its token.
func (r *Registry) UpdateDevice(ctx context.Context, id string, s api.PushSubscription) error {
	if s.Endpoint == "" || s.Keys.P256dh == "" || s.Keys.Auth == "" {
		return fmt.Errorf("%w: push subscription needs endpoint, p256dh and auth", store.ErrInvalid)
	}
	if err := r.Guard.Check(ctx, s.Endpoint); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	d, err := readDevice(tx, `id = ? AND kind = ? AND confirmed = 1`, id, api.DeviceApp)
	if err != nil {
		return err
	}
	if d == nil {
		return store.ErrUnauthorized
	}
	if d.Endpoint != s.Endpoint {
		// A new endpoint has no failures behind it yet.
		d.FailingSince = sql.NullInt64{}
		r.mu.Lock()
		delete(r.holds, d.Endpoint)
		r.mu.Unlock()
	}
	d.Endpoint, d.P256dh, d.Auth = s.Endpoint, s.Keys.P256dh, s.Keys.Auth
	if err := r.writeDevice(tx, d); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeDevice removes the device id with its subscription and token.
func (r *Registry) RevokeDevice(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		if err == nil {
			err = fmt.Errorf("%w: no such device", store.ErrNotFound)
		}
		return err
	}
	return nil
}

// Devices lists the live devices, newest first, without endpoints or keys.
func (r *Registry) Devices(ctx context.Context) ([]api.Device, error) {
	if err := r.prune(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, kind, name, confirmed, created_at, last_seen, last_push
		FROM push_subscriptions WHERE id IS NOT NULL ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.Device{}
	for rows.Next() {
		var d api.Device
		var created, seen int64
		var push sql.NullInt64
		if err := rows.Scan(&d.ID, &d.Kind, &d.Name, &d.Confirmed, &created, &seen, &push); err != nil {
			return nil, err
		}
		if d.Name == "" {
			d.Name = defaultWebName
			if d.Kind == api.DeviceApp {
				d.Name = defaultAppName
			}
		}
		d.CreatedAt, d.LastSeen = time.Unix(created, 0).UTC(), time.Unix(seen, 0).UTC()
		if push.Valid {
			t := time.Unix(push.Int64, 0).UTC()
			d.LastPush = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
