package appclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// SessionFile is the file, beside bridge.json in the same directory
// (filepath.Dir of bridge.DefaultPaths().Config), that holds one Session per
// hub URL, mode 0600:
//
//	{"version": 1, "sessions": {"<Session.Hub>": {"hub", "cookieName", "cookie"}}}
//
// Writes go to a temporary file renamed over it; two viewers saving at once
// leave the last writer's file, which loses at most the other's renewal.
const SessionFile = "tui.json"

// sessionVersion is the version of SessionFile's shape this package writes.
const sessionVersion = 1

// Session is a logged-in app session on one hub. It is a full app login:
// anything the app can do, a holder of the cookie can do.
type Session struct {
	Hub string `json:"hub"` // the hub URL as the user gave it, normalised: scheme, host, port, no path
	// CookieName is the name the hub's Set-Cookie used, read by hand rather
	// than through a cookie jar: the hub names it after MEAT_MODE_PUBLIC_URL's
	// scheme (__Host-meat_mode_session on https, meat_mode_session on http),
	// not after the URL the viewer dials, and a jar drops a Secure cookie
	// over http://localhost. Either name is accepted.
	CookieName string `json:"cookieName"`
	Cookie     string `json:"cookie"`
}

type sessionFile struct {
	Version  int                `json:"version"`
	Sessions map[string]Session `json:"sessions"`
}

// readSessions reads dir/SessionFile; a missing file is an empty one.
func readSessions(dir string) (sessionFile, error) {
	f := sessionFile{Version: sessionVersion, Sessions: map[string]Session{}}
	path := filepath.Join(dir, SessionFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("%s is not a session file: %w; remove it and log in again", path, err)
	}
	if f.Version != sessionVersion {
		return f, fmt.Errorf("%s is version %d; this meat-mode reads version %d", path, f.Version, sessionVersion)
	}
	if f.Sessions == nil {
		f.Sessions = map[string]Session{}
	}
	return f, nil
}

// SavedHubs lists the hubs dir/SessionFile holds a session for, sorted. A
// missing file holds none.
func SavedHubs(dir string) ([]string, error) {
	f, err := readSessions(dir)
	if err != nil {
		return nil, err
	}
	hubs := make([]string, 0, len(f.Sessions))
	for h := range f.Sessions {
		hubs = append(hubs, h)
	}
	slices.Sort(hubs)
	return hubs, nil
}

// writeSessions writes f to dir/SessionFile through a temporary file in the
// same directory, so a reader never sees half a file.
func writeSessions(dir string, f sessionFile) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f.Version = sessionVersion
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tui-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // gone already after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, SessionFile))
}

// LoadSession reads the session for hub from dir/SessionFile. ok is false
// when the file or the hub's entry does not exist.
func LoadSession(dir, hub string) (s Session, ok bool, err error) {
	f, err := readSessions(dir)
	if err != nil {
		return Session{}, false, err
	}
	s, ok = f.Sessions[hub]
	if !ok || s.Cookie == "" {
		return Session{}, false, nil
	}
	return s, true, nil
}

// SaveSession writes s into dir/SessionFile, replacing that hub's entry and
// keeping the others, atomically and with mode 0600.
func SaveSession(dir string, s Session) error {
	if s.Hub == "" {
		return errors.New("a session needs its hub URL")
	}
	f, err := readSessions(dir)
	if err != nil {
		var se *json.SyntaxError
		if !errors.As(err, &se) {
			return err
		}
		// An unreadable file holds nothing a new login cannot get back.
		f = sessionFile{Sessions: map[string]Session{}}
	}
	f.Sessions[s.Hub] = s
	return writeSessions(dir, f)
}

// DeleteSession removes hub's entry; the file goes when it is the last.
// A missing entry is not an error.
func DeleteSession(dir, hub string) error {
	f, err := readSessions(dir)
	if err != nil {
		return err
	}
	if _, ok := f.Sessions[hub]; !ok {
		return nil
	}
	delete(f.Sessions, hub)
	if len(f.Sessions) == 0 {
		err := os.Remove(filepath.Join(dir, SessionFile))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeSessions(dir, f)
}
