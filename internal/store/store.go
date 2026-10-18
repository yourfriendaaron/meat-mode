// Package store keeps the hub's state in SQLite: bridges and their tokens,
// repos, items, the personal list, and the append-only action log.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Errors the hub maps to HTTP statuses. Wrapped errors carry a plain message.
var (
	ErrInvalid      = errors.New("invalid request")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrUnauthorized = errors.New("unauthorized")
)

// userError carries a plain message for the user and matches one sentinel.
type userError struct {
	kind error
	msg  string
}

func (e *userError) Error() string { return e.msg }
func (e *userError) Unwrap() error { return e.kind }

func invalidf(format string, a ...any) error {
	return &userError{ErrInvalid, fmt.Sprintf(format, a...)}
}

func notFoundf(format string, a ...any) error {
	return &userError{ErrNotFound, fmt.Sprintf(format, a...)}
}

func conflictf(format string, a ...any) error {
	return &userError{ErrConflict, fmt.Sprintf(format, a...)}
}

// DBFile is the database file name inside the data directory.
const DBFile = "meat-mode.db"

// Change describes one committed change, for listeners such as the SSE
// broadcaster and the notifier.
type Change struct {
	Kind   string // bridges, repos, items, actions, seen
	Bridge string
	Repo   string
	// NewItems lists items the hub received for the first time (Kind items).
	NewItems []string
}

// Store is the hub's database.
type Store struct {
	db  *sql.DB
	now func() time.Time

	mu        sync.Mutex
	listeners []func(Change)
}

// Open checks dataDir is writable, opens (or creates) the database in it,
// confirms WAL mode and applies the schema.
func Open(dataDir string) (*Store, error) {
	if dataDir == "" {
		return nil, errors.New("no data directory set (MEAT_MODE_DATA_DIR)")
	}
	if err := checkWritable(dataDir); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(filepath.Join(dataDir, DBFile)) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// One connection serialises writes, so no transaction ever hits a busy
	// upgrade. The hub serves one person; this is plenty.
	db.SetMaxOpenConns(1)
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database in %s: %w", dataDir, err)
	}
	if !strings.EqualFold(mode, "wal") {
		db.Close()
		return nil, fmt.Errorf("SQLite could not use WAL mode in %s (got %q); the data directory must be local storage, not NFS or SMB", dataDir, mode)
	}
	s := &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.TransferPersonal(); err != nil {
		db.Close()
		return nil, fmt.Errorf("move personal items to the hub: %w", err)
	}
	return s, nil
}

func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("cannot create data directory %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("data directory %s is not writable by user id %d: %w. "+
			"If it is a volume owned by root, give it to the user the hub runs as (chown) or run the hub as its owner",
			dir, os.Getuid(), err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// DB returns the underlying database so other packages can keep their own
// tables in it. It allows one connection at a time: close rows before
// running another query.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Watch registers fn to run after every committed change. fn must not block.
func (s *Store) Watch(fn func(Change)) {
	s.mu.Lock()
	s.listeners = append(s.listeners, fn)
	s.mu.Unlock()
}

func (s *Store) emit(c Change) {
	s.mu.Lock()
	ls := slices.Clone(s.listeners)
	s.mu.Unlock()
	for _, fn := range ls {
		fn(c)
	}
}

const schemaVersion = 5

var migrations = []string{
	// 1: initial schema.
	`
CREATE TABLE bridges (
	name       TEXT PRIMARY KEY,
	token_hash BLOB NOT NULL,
	created_at TEXT NOT NULL,
	last_seen  TEXT,
	version    TEXT NOT NULL DEFAULT '',
	bd_version TEXT NOT NULL DEFAULT '',
	revoked_at TEXT
);
CREATE UNIQUE INDEX bridges_token ON bridges(token_hash);

CREATE TABLE repos (
	bridge    TEXT NOT NULL REFERENCES bridges(name) ON DELETE CASCADE,
	name      TEXT NOT NULL,
	personal  INTEGER NOT NULL,
	opted_in  INTEGER NOT NULL,
	synced_at TEXT,
	PRIMARY KEY (bridge, name)
);

CREATE TABLE items (
	bridge     TEXT NOT NULL,
	repo       TEXT NOT NULL,
	id         TEXT NOT NULL,
	data       TEXT NOT NULL,
	first_seen TEXT NOT NULL,
	seen_at    TEXT,
	PRIMARY KEY (bridge, repo, id),
	FOREIGN KEY (bridge, repo) REFERENCES repos(bridge, name) ON DELETE CASCADE
);

CREATE TABLE actions (
	seq        INTEGER PRIMARY KEY AUTOINCREMENT,
	id         TEXT NOT NULL UNIQUE,
	kind       TEXT NOT NULL,
	bridge     TEXT NOT NULL,
	repo       TEXT NOT NULL,
	bead_id    TEXT NOT NULL DEFAULT '',
	payload    TEXT NOT NULL,
	author     TEXT NOT NULL,
	status     TEXT NOT NULL,
	error      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	applied_at TEXT
);
CREATE INDEX actions_bridge_status ON actions(bridge, status, seq);
CREATE INDEX actions_target ON actions(bridge, repo, bead_id, seq);
`,
	// 2: the personal list on the hub. The reserved bridge row has a random
	// token hash, so no token ever authenticates as it.
	`
INSERT INTO bridges (name, token_hash, created_at) VALUES ('hub', randomblob(32), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
INSERT INTO repos (bridge, name, personal, opted_in) VALUES ('hub', 'personal', 1, 1);

CREATE TABLE personal_origins (
	bridge  TEXT NOT NULL,
	bead_id TEXT NOT NULL,
	item_id TEXT NOT NULL,
	PRIMARY KEY (bridge, bead_id)
);
`,
	// 3: login sessions. id is the SHA-256 of the cookie's token; tag
	// names the password hash the session was made under. Times are Unix
	// seconds, so expiry compares in SQL.
	`
CREATE TABLE sessions (
	id         BLOB PRIMARY KEY,
	tag        TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	renewed_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_expires ON sessions(expires_at);
`,
	// 4: hub setup (setup.go), one row. setup is '', 'done' or
	// 'dismissed'. The setup code is kept only as its SHA-256; its times
	// are Unix seconds, so expiry compares in SQL.
	`
CREATE TABLE hub_settings (
	id                 INTEGER PRIMARY KEY CHECK (id = 1),
	setup              TEXT NOT NULL DEFAULT '',
	setup_at           TEXT,
	user_name          TEXT NOT NULL DEFAULT '',
	time_zone          TEXT NOT NULL DEFAULT '',
	password_hash      TEXT NOT NULL DEFAULT '',
	tailscale_only     INTEGER NOT NULL DEFAULT 0,
	setup_code_hash    BLOB,
	setup_code_expires INTEGER,
	setup_code_used    INTEGER
);
INSERT INTO hub_settings (id) VALUES (1);
`,
	// 5: which machine's token added each personal item through
	// POST /bridge/v1/personal, so its agents may change it. Items added in
	// the app or moved from a bridge's personal repo have no row.
	`
CREATE TABLE personal_owners (
	item_id TEXT PRIMARY KEY,
	bridge  TEXT NOT NULL
);
`,
}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if v > schemaVersion {
		return fmt.Errorf("database schema version %d is newer than this hub understands (%d); run a newer hub", v, schemaVersion)
	}
	if v == 1 {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM bridges WHERE name = ?`, HubBridge).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("a machine is named %q, which this hub now keeps for its own list; run the previous hub, revoke that machine and pair it again under another name", HubBridge)
		}
	}
	for ; v < schemaVersion; v++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrate schema to version %d: %w", v+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Times are stored as RFC 3339 text in UTC.
func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func fmtTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return fmtTime(*t)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func parseTimePtr(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	t := parseTime(s.String)
	return &t
}
