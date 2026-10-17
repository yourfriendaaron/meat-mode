package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

// Session is a logged-in browser. ID is the hex SHA-256 of its token, safe
// to keep alongside other rows; the token itself is never stored.
type Session struct {
	ID        string
	ExpiresAt time.Time
	// Renewed is set when CheckSession moved the expiry forward, so the
	// caller sends the cookie again with the new lifetime.
	Renewed bool
}

// sessionRenewAfter is how long a session goes before CheckSession moves
// its expiry forward again.
const sessionRenewAfter = 24 * time.Hour

// CreateSession starts a session under tag that lasts ttl, and returns its
// token. It also deletes expired sessions.
func (s *Store) CreateSession(tag string, now time.Time, ttl time.Duration) (string, Session, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	id := hashToken(token)
	exp := now.Add(ttl)
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now.Unix()); err != nil {
		return "", Session{}, err
	}
	if _, err := s.db.Exec(`INSERT INTO sessions (id, tag, created_at, renewed_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		id, tag, now.Unix(), now.Unix(), exp.Unix()); err != nil {
		return "", Session{}, err
	}
	return token, Session{ID: SessionID(token), ExpiresAt: time.Unix(exp.Unix(), 0)}, nil
}

// CheckSession returns the session for token if it exists under tag and has
// not expired, or nil. Once a day of use it moves the expiry to now+ttl.
func (s *Store) CheckSession(token, tag string, now time.Time, ttl time.Duration) (*Session, error) {
	if token == "" {
		return nil, nil
	}
	id := hashToken(token)
	var renewed, expires int64
	err := s.db.QueryRow(`SELECT renewed_at, expires_at FROM sessions WHERE id = ? AND tag = ? AND expires_at > ?`,
		id, tag, now.Unix()).Scan(&renewed, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sess := &Session{ID: SessionID(token), ExpiresAt: time.Unix(expires, 0)}
	if now.Sub(time.Unix(renewed, 0)) >= sessionRenewAfter {
		exp := now.Add(ttl).Unix()
		if _, err := s.db.Exec(`UPDATE sessions SET renewed_at = ?, expires_at = ? WHERE id = ?`,
			now.Unix(), exp, id); err != nil {
			return nil, err
		}
		sess.ExpiresAt = time.Unix(exp, 0)
		sess.Renewed = true
	}
	return sess, nil
}

// SessionID is the ID of the session token names, whether or not it exists.
func SessionID(token string) string { return hex.EncodeToString(hashToken(token)) }

// DeleteSession ends the session for token; an unknown token is not an
// error.
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, hashToken(token))
	return err
}
