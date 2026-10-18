package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/auth"
)

// Session is a logged-in browser. ID is the hex SHA-256 of its token, safe
// to keep alongside other rows; the token itself is never stored.
type Session struct {
	ID        string
	ExpiresAt time.Time
	// VerifiedAt is when the password was last entered in this session.
	VerifiedAt time.Time
	// Renewed is set when CheckSession moved the expiry forward, so the
	// caller sends the cookie again with the new lifetime.
	Renewed bool
}

// Session lifetimes (docs/api.md, Login). A session ends SessionIdle after
// its last recorded use or SessionLifetime after it began, whichever comes
// first; expires_at holds the earlier of the two.
const (
	SessionIdle     = 14 * 24 * time.Hour
	SessionLifetime = 90 * 24 * time.Hour
)

// sessionRenewAfter is how long a session goes before a use of it is
// recorded again and its expiry moved forward.
const sessionRenewAfter = 24 * time.Hour

// sessionExpiry is when a session begun at created and last used at used
// ends.
func sessionExpiry(created, used time.Time) time.Time {
	idle, end := used.Add(SessionIdle), created.Add(SessionLifetime)
	if end.Before(idle) {
		return end
	}
	return idle
}

// CreateSession starts a session under tag, with the password entered now,
// and returns its token. It also deletes expired sessions.
func (s *Store) CreateSession(tag string, now time.Time) (string, Session, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	id := hashToken(token)
	exp := sessionExpiry(now, now)
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now.Unix()); err != nil {
		return "", Session{}, err
	}
	if _, err := s.db.Exec(`INSERT INTO sessions (id, tag, created_at, renewed_at, expires_at, verified_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, tag, now.Unix(), now.Unix(), exp.Unix(), now.Unix()); err != nil {
		return "", Session{}, err
	}
	return token, Session{ID: SessionID(token), ExpiresAt: time.Unix(exp.Unix(), 0), VerifiedAt: time.Unix(now.Unix(), 0)}, nil
}

// CheckSession returns the session for token if it exists under tag and has
// not ended, or nil, and records the use (see useSession).
func (s *Store) CheckSession(token, tag string, now time.Time) (*Session, error) {
	if token == "" {
		return nil, nil
	}
	return s.useSession(hashToken(token), tag, now)
}

// UseSession records a use of the session id names, as a device-token call
// of a device it registered does, and reports whether the session is still
// live under tag. Like CheckSession it records at most once a day.
func (s *Store) UseSession(id, tag string, now time.Time) (bool, error) {
	key, err := hex.DecodeString(id)
	if err != nil {
		return false, nil
	}
	sess, err := s.useSession(key, tag, now)
	return sess != nil, err
}

// useSession returns the live session whose key this is under tag, or nil.
// Once a day of use it records the use, which moves the expiry to SessionIdle
// from now, but never past SessionLifetime from the session's start.
func (s *Store) useSession(key []byte, tag string, now time.Time) (*Session, error) {
	var created, renewed, expires, verified int64
	err := s.db.QueryRow(`SELECT created_at, renewed_at, expires_at, verified_at FROM sessions
		WHERE id = ? AND tag = ? AND expires_at > ?`,
		key, tag, now.Unix()).Scan(&created, &renewed, &expires, &verified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sess := &Session{ID: hex.EncodeToString(key), ExpiresAt: time.Unix(expires, 0), VerifiedAt: time.Unix(verified, 0)}
	if now.Sub(time.Unix(renewed, 0)) >= sessionRenewAfter {
		exp := sessionExpiry(time.Unix(created, 0), now).Unix()
		if _, err := s.db.Exec(`UPDATE sessions SET renewed_at = ?, expires_at = ? WHERE id = ?`,
			now.Unix(), exp, key); err != nil {
			return nil, err
		}
		sess.Renewed = exp != expires
		sess.ExpiresAt = time.Unix(exp, 0)
	}
	return sess, nil
}

// VerifySession records that the password was entered now in the session
// id names.
func (s *Store) VerifySession(id string, now time.Time) error {
	key, err := hex.DecodeString(id)
	if err != nil {
		return invalidf("session id is not hex")
	}
	_, err = s.db.Exec(`UPDATE sessions SET verified_at = ? WHERE id = ?`, now.Unix(), key)
	return err
}

// SessionID is the ID of the session token names, whether or not it exists.
func SessionID(token string) string { return hex.EncodeToString(hashToken(token)) }

// SessionLive reports whether the session id names is still valid under
// tag at now. Unlike CheckSession it never renews the session.
func (s *Store) SessionLive(id, tag string, now time.Time) (bool, error) {
	key, err := hex.DecodeString(id)
	if err != nil {
		return false, nil
	}
	var n int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ? AND tag = ? AND expires_at > ?`,
		key, tag, now.Unix()).Scan(&n)
	return n > 0, err
}

// DeleteSession ends the session for token; an unknown token is not an
// error.
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, hashToken(token))
	return err
}

// ChangePassword sets the app's password hash phc, ends every session but
// token's and moves that one under tag, the new hash's, with the password
// entered at now, so the browser that changed the password stays logged in
// and every other one is logged out. It returns the IDs of the sessions it
// ended.
func (s *Store) ChangePassword(phc, token, tag string, now time.Time) ([]string, error) {
	if _, err := auth.ParseHash(phc); err != nil {
		return nil, invalidf("password hash is %v", err)
	}
	keep := hashToken(token)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE hub_settings SET password_hash = ? WHERE id = 1`, phc); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`DELETE FROM sessions WHERE id != ? RETURNING id`, keep)
	if err != nil {
		return nil, err
	}
	var ended []string
	for rows.Next() {
		var id []byte
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ended = append(ended, hex.EncodeToString(id))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE sessions SET tag = ?, verified_at = ? WHERE id = ?`, tag, now.Unix(), keep); err != nil {
		return nil, err
	}
	return ended, tx.Commit()
}

// EndOtherLogins ends every session not started under tag, the current
// password's, or every session when tag is "" (no password). The hub calls it
// at start, so a password hash changed outside the app logs out every
// device of the old one. It returns the IDs of the sessions it ended.
func (s *Store) EndOtherLogins(tag string) ([]string, error) {
	rows, err := s.db.Query(`DELETE FROM sessions WHERE ? = '' OR tag != ? RETURNING id`, tag, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ended []string
	for rows.Next() {
		var id []byte
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ended = append(ended, hex.EncodeToString(id))
	}
	return ended, rows.Err()
}
