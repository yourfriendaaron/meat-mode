package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// TokenPrefix starts every bridge token, so a leaked one is easy to spot.
const TokenPrefix = "mm_"

var bridgeNameRE = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// ValidBridgeName reports whether name fits [a-z0-9-]{1,32}.
func ValidBridgeName(name string) bool { return bridgeNameRE.MatchString(name) }

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateBridge adds a bridge and returns its token, which is not stored and
// cannot be shown again. A revoked bridge's name can be reused: it gets a new
// token and keeps its repos and items until the bridge sends fresh ones.
func (s *Store) CreateBridge(name string) (string, error) {
	if !ValidBridgeName(name) {
		return "", invalidf("bridge name must be 1 to 32 lowercase letters, digits or hyphens")
	}
	if err := reservedBridge(name); err != nil {
		return "", err
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	now := fmtTime(s.now())
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var revoked sql.NullString
	err = tx.QueryRow(`SELECT revoked_at FROM bridges WHERE name = ?`, name).Scan(&revoked)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.Exec(`INSERT INTO bridges (name, token_hash, created_at) VALUES (?, ?, ?)`,
			name, hashToken(token), now)
	case err != nil:
		return "", err
	case !revoked.Valid:
		return "", conflictf("a bridge named %q already exists", name)
	default:
		_, err = tx.Exec(`UPDATE bridges SET token_hash = ?, created_at = ?, revoked_at = NULL,
			last_seen = NULL, version = '', bd_version = '' WHERE name = ?`,
			hashToken(token), now, name)
	}
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	s.emit(Change{Kind: "bridges", Bridge: name})
	return token, nil
}

// RevokeBridge revokes a bridge's token. Its items leave the app's view.
func (s *Store) RevokeBridge(name string) error {
	res, err := s.db.Exec(`UPDATE bridges SET revoked_at = ? WHERE name = ? AND name != ? AND revoked_at IS NULL`,
		fmtTime(s.now()), name, HubBridge)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFoundf("no bridge named %q", name)
	}
	s.emit(Change{Kind: "bridges", Bridge: name})
	return nil
}

// Authenticate returns the name of the active bridge that owns token.
func (s *Store) Authenticate(token string) (string, error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return "", ErrUnauthorized
	}
	want := hashToken(token)
	var name string
	var got []byte
	err := s.db.QueryRow(`SELECT name, token_hash FROM bridges WHERE token_hash = ? AND revoked_at IS NULL`,
		want).Scan(&name, &got)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	if subtle.ConstantTimeCompare(want, got) != 1 {
		return "", ErrUnauthorized
	}
	return name, nil
}

// Touch records that the bridge was heard from. It emits no change.
func (s *Store) Touch(name string) error {
	_, err := s.db.Exec(`UPDATE bridges SET last_seen = ? WHERE name = ?`, fmtTime(s.now()), name)
	return err
}

// Hello records the bridge's versions and last-seen time.
func (s *Store) Hello(name string, h api.Hello) error {
	_, err := s.db.Exec(`UPDATE bridges SET last_seen = ?, version = ?, bd_version = ? WHERE name = ?`,
		fmtTime(s.now()), truncRunes(h.Version, maxVersionLen), truncRunes(h.BdVersion, maxVersionLen), name)
	if err != nil {
		return err
	}
	s.emit(Change{Kind: "bridges", Bridge: name})
	return nil
}

// Bridges lists active bridges by name, without the hub's own row.
func (s *Store) Bridges() ([]api.Bridge, error) {
	rows, err := s.db.Query(`SELECT name, created_at, last_seen, version, bd_version
		FROM bridges WHERE revoked_at IS NULL AND name != ? ORDER BY name`, HubBridge)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.Bridge{}
	for rows.Next() {
		var b api.Bridge
		var created string
		var last sql.NullString
		if err := rows.Scan(&b.Name, &created, &last, &b.Version, &b.BdVersion); err != nil {
			return nil, err
		}
		b.CreatedAt = parseTime(created)
		b.LastSeen = parseTimePtr(last)
		out = append(out, b)
	}
	return out, rows.Err()
}
