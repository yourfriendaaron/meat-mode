package store

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/auth"
)

// What the hub records about its setup wizard, in hub_settings.setup.
const (
	SetupDone      = "done"
	SetupDismissed = "dismissed"
)

// What SetupStatus reports for a hub with no record.
const (
	SetupNew   = "new"    // no items and no machines: the app offers the hub setup
	SetupInUse = "in-use" // already holds items or machines: the app does not
)

// maxUserName bounds the name kept in the app, in characters.
const maxUserName = 64

// HubSettings is the hub's one row of settings kept in the app rather than
// the environment.
type HubSettings struct {
	Setup   string     // "", SetupDone or SetupDismissed
	SetupAt *time.Time // when Setup was recorded
	// UserName is the user's name; "" means MEAT_MODE_USER.
	UserName string
	// TimeZone is an IANA name such as Europe/Paris; "" until chosen.
	TimeZone string
	// ReminderLead is how many minutes before a due time an early reminder
	// comes, 0 for none.
	ReminderLead int
	// PasswordHash is the Argon2id PHC string of a password set in the
	// app; "" for none. MEAT_MODE_PASSWORD_HASH wins over it.
	PasswordHash string
	// SetupCodeExpires is when the current setup code stops working, nil
	// when none was made. SetupCodeUsed is when it claimed the hub.
	SetupCodeExpires *time.Time
	SetupCodeUsed    *time.Time
}

// Settings returns the hub's settings.
func (s *Store) Settings() (HubSettings, error) {
	var h HubSettings
	var setupAt sql.NullString
	var expires, used sql.NullInt64
	err := s.db.QueryRow(`SELECT setup, setup_at, user_name, time_zone, reminder_lead, password_hash,
		setup_code_expires, setup_code_used FROM hub_settings WHERE id = 1`).Scan(
		&h.Setup, &setupAt, &h.UserName, &h.TimeZone, &h.ReminderLead, &h.PasswordHash, &expires, &used)
	if err != nil {
		return h, err
	}
	h.SetupAt = parseTimePtr(setupAt)
	h.SetupCodeExpires = unixPtr(expires)
	h.SetupCodeUsed = unixPtr(used)
	return h, nil
}

func unixPtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.Unix(n.Int64, 0).UTC()
	return &t
}

// SetupStatus is the recorded setup (SetupDone or SetupDismissed) or, with
// none, SetupNew for a hub with no items and no machines, revoked ones
// included, and SetupInUse otherwise.
func (s *Store) SetupStatus() (string, error) {
	var setup string
	var used bool
	err := s.db.QueryRow(`SELECT setup,
		EXISTS (SELECT 1 FROM items) OR EXISTS (SELECT 1 FROM bridges WHERE name != ?)
		FROM hub_settings WHERE id = 1`, HubBridge).Scan(&setup, &used)
	switch {
	case err != nil:
		return "", err
	case setup != "":
		return setup, nil
	case used:
		return SetupInUse, nil
	}
	return SetupNew, nil
}

// RecordSetup records the setup wizard as SetupDone or SetupDismissed.
func (s *Store) RecordSetup(status string) error {
	if status != SetupDone && status != SetupDismissed {
		return invalidf("setup status must be %q or %q", SetupDone, SetupDismissed)
	}
	return s.setSetting(`setup = ?, setup_at = ?`, status, fmtTime(s.now()))
}

// SetUserName keeps the user's name: up to 64 characters with no control
// characters, trimmed. An empty name goes back to MEAT_MODE_USER.
func (s *Store) SetUserName(name string) error {
	name, err := cleanUserName(name)
	if err != nil {
		return err
	}
	return s.setSetting(`user_name = ?`, name)
}

func cleanUserName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxUserName {
		return "", invalidf("name must be at most %d characters", maxUserName)
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", invalidf("name must not contain control characters")
	}
	return name, nil
}

// SetTimeZone keeps the user's IANA time zone, such as Europe/Paris.
func (s *Store) SetTimeZone(tz string) error {
	if err := checkTimeZone(tz); err != nil {
		return err
	}
	return s.setSetting(`time_zone = ?`, tz)
}

func checkTimeZone(tz string) error {
	if tz == "" || tz == "Local" {
		return invalidf("time zone must be an IANA name such as Europe/Paris")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return invalidf("unknown time zone %q; use an IANA name such as Europe/Paris", tz)
	}
	return nil
}

func checkReminderLead(minutes int) error {
	if minutes < 0 || minutes > api.MaxLeadMinutes {
		return invalidf("reminder lead time must be 0 to %d minutes", api.MaxLeadMinutes)
	}
	return nil
}

// UpdateProfile keeps the name, the time zone and the reminder lead time
// that are not nil, all or none: a refused one leaves the others unchanged.
func (s *Store) UpdateProfile(name, tz *string, lead *int) error {
	var set []string
	var args []any
	if name != nil {
		n, err := cleanUserName(*name)
		if err != nil {
			return err
		}
		set, args = append(set, "user_name = ?"), append(args, n)
	}
	if tz != nil {
		if err := checkTimeZone(*tz); err != nil {
			return err
		}
		set, args = append(set, "time_zone = ?"), append(args, *tz)
	}
	if lead != nil {
		if err := checkReminderLead(*lead); err != nil {
			return err
		}
		set, args = append(set, "reminder_lead = ?"), append(args, *lead)
	}
	if len(set) == 0 {
		return nil
	}
	return s.setSetting(strings.Join(set, ", "), args...)
}

// SetPasswordHash keeps the login password set in the app, as an Argon2id
// PHC string, or removes it with "".
func (s *Store) SetPasswordHash(phc string) error {
	if phc != "" {
		if _, err := auth.ParseHash(phc); err != nil {
			return invalidf("password hash is %v", err)
		}
	}
	return s.setSetting(`password_hash = ?`, phc)
}

func (s *Store) setSetting(set string, args ...any) error {
	_, err := s.db.Exec(`UPDATE hub_settings SET `+set+` WHERE id = 1`, args...)
	return err
}

// setupCodeAlphabet leaves out 0, 1, I and O, which read alike.
const setupCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// NewSetupCode makes a one-time setup code that lasts ttl, replacing any
// earlier one, and returns it as XXXX-XXXX-XXXX (60 random bits). Only its
// SHA-256 is kept.
func (s *Store) NewSetupCode(ttl time.Duration) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var code strings.Builder
	for i, c := range b {
		if i > 0 && i%4 == 0 {
			code.WriteByte('-')
		}
		code.WriteByte(setupCodeAlphabet[int(c)%len(setupCodeAlphabet)])
	}
	err := s.setSetting(`setup_code_hash = ?, setup_code_expires = ?, setup_code_used = NULL`,
		hashToken(normalSetupCode(code.String())), s.now().Add(ttl).Unix())
	return code.String(), err
}

// normalSetupCode drops spaces and dashes and upper-cases, so a code typed
// in any of those ways matches.
func normalSetupCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
}

// ClaimSetupCode sets the app's password hash phc if code is the current
// setup code, unexpired and unused, and uses the code up. A wrong, expired
// or used code is ErrUnauthorized; a hub that already has a password set
// in the app is ErrConflict.
func (s *Store) ClaimSetupCode(code, phc string) error {
	if _, err := auth.ParseHash(phc); err != nil {
		return invalidf("password hash is %v", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash []byte
	var expires, used sql.NullInt64
	var current string
	if err := tx.QueryRow(`SELECT setup_code_hash, setup_code_expires, setup_code_used, password_hash
		FROM hub_settings WHERE id = 1`).Scan(&hash, &expires, &used, &current); err != nil {
		return err
	}
	if current != "" {
		return conflictf("this hub already has a password")
	}
	if hash == nil || used.Valid || !expires.Valid || s.now().Unix() >= expires.Int64 ||
		subtle.ConstantTimeCompare(hashToken(normalSetupCode(code)), hash) != 1 {
		return &userError{ErrUnauthorized, "the setup code is wrong, expired or used; restart the hub for a new one"}
	}
	if _, err := tx.Exec(`UPDATE hub_settings SET password_hash = ?, setup_code_used = ? WHERE id = 1`,
		phc, s.now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
