package store

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
)

// NewCalendarToken turns the calendar feed on with a new token, 32 random
// bytes in base64url, and returns it. It replaces any earlier token, which
// stops working. Only its SHA-256 is kept.
func (s *Store) NewCalendarToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	return token, s.setSetting(`calendar_hash = ?`, hashToken(token))
}

// StopCalendar turns the calendar feed off.
func (s *Store) StopCalendar() error {
	return s.setSetting(`calendar_hash = NULL`)
}

// CalendarOn reports whether the calendar feed has a token.
func (s *Store) CalendarOn() (bool, error) {
	var on bool
	err := s.db.QueryRow(`SELECT calendar_hash IS NOT NULL FROM hub_settings WHERE id = 1`).Scan(&on)
	return on, err
}

// CalendarTokenOK reports whether token is the calendar feed's current
// token, comparing in constant time.
func (s *Store) CalendarTokenOK(token string) (bool, error) {
	var hash []byte
	if err := s.db.QueryRow(`SELECT calendar_hash FROM hub_settings WHERE id = 1`).Scan(&hash); err != nil {
		return false, err
	}
	return hash != nil && subtle.ConstantTimeCompare(hashToken(token), hash) == 1, nil
}
