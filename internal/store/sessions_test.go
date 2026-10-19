package store

import (
	"slices"
	"testing"
	"time"
)

func TestEndOtherLogins(t *testing.T) {
	s, c := open(t)
	old, _, err := s.CreateSession("old", c.now())
	must(t, err)
	cur, _, err := s.CreateSession("new", c.now())
	must(t, err)

	ended, err := s.EndOtherLogins("new")
	must(t, err)
	if want := []string{SessionID(old)}; !slices.Equal(ended, want) {
		t.Errorf("ended %v, want %v", ended, want)
	}
	if sess, err := s.CheckSession(cur, "new", c.now()); err != nil || sess == nil {
		t.Errorf("session under the current tag: %v, %v; want it kept", sess, err)
	}

	// No password ends every session.
	ended, err = s.EndOtherLogins("")
	must(t, err)
	if want := []string{SessionID(cur)}; !slices.Equal(ended, want) {
		t.Errorf("no password ended %v, want %v", ended, want)
	}
}

func TestSessionIdleEnd(t *testing.T) {
	s, c := open(t)
	start := c.now()
	token, sess, err := s.CreateSession("tag", start)
	must(t, err)
	if !sess.ExpiresAt.Equal(start.Add(SessionIdle)) || !sess.VerifiedAt.Equal(start) {
		t.Fatalf("new session %+v", sess)
	}

	// Within a day of the last recorded use, nothing moves.
	got, err := s.CheckSession(token, "tag", start.Add(23*time.Hour))
	must(t, err)
	if got == nil || got.Renewed || !got.ExpiresAt.Equal(start.Add(SessionIdle)) {
		t.Fatalf("same day: %+v", got)
	}
	// A day on, the use is recorded and the idle end moves.
	day := start.Add(24 * time.Hour)
	got, err = s.CheckSession(token, "tag", day)
	must(t, err)
	if got == nil || !got.Renewed || !got.ExpiresAt.Equal(day.Add(SessionIdle)) {
		t.Fatalf("next day: %+v", got)
	}
	// Just before the idle end it holds; at it the session is over.
	if got, _ := s.CheckSession(token, "tag", day.Add(SessionIdle-time.Second)); got == nil {
		t.Fatal("session ended before its idle end")
	}
	// That use was recorded, so start again from a fresh session.
	token, _, err = s.CreateSession("tag", start)
	must(t, err)
	if got, _ := s.CheckSession(token, "tag", start.Add(SessionIdle)); got != nil {
		t.Errorf("session live at its idle end: %+v", got)
	}
}

func TestSessionAbsoluteEnd(t *testing.T) {
	s, c := open(t)
	start := c.now()
	token, _, err := s.CreateSession("tag", start)
	must(t, err)
	// Used every day, the session still ends 90 days after it began.
	var last *Session
	for now := start.Add(24 * time.Hour); now.Before(start.Add(SessionLifetime)); now = now.Add(24 * time.Hour) {
		last, err = s.CheckSession(token, "tag", now)
		must(t, err)
		if last == nil {
			t.Fatalf("session ended early, at %v", now.Sub(start))
		}
	}
	if !last.ExpiresAt.Equal(start.Add(SessionLifetime)) {
		t.Errorf("expiry near the end = %v, want %v", last.ExpiresAt, start.Add(SessionLifetime))
	}
	if got, _ := s.CheckSession(token, "tag", start.Add(SessionLifetime)); got != nil {
		t.Errorf("session live at its absolute end: %+v", got)
	}
}

func TestUseSessionKeepsItAlive(t *testing.T) {
	s, c := open(t)
	start := c.now()
	token, _, err := s.CreateSession("tag", start)
	must(t, err)
	id := SessionID(token)
	// A device-token call every 10 days keeps a session with no cookie
	// use from its idle end.
	for days := 10; days <= 40; days += 10 {
		now := start.Add(time.Duration(days) * 24 * time.Hour)
		live, err := s.UseSession(id, "tag", now)
		must(t, err)
		if !live {
			t.Fatalf("day %d: session ended", days)
		}
	}
	if live, _ := s.UseSession(id, "other", start.Add(41*24*time.Hour)); live {
		t.Error("session live under another tag")
	}
	if live, _ := s.UseSession(id, "tag", start.Add(55*24*time.Hour)); live {
		t.Error("session live 15 days after its last use")
	}
	if live, err := s.UseSession("not hex", "tag", start); live || err != nil {
		t.Errorf("bad id: %v, %v", live, err)
	}
}

func TestVerifySession(t *testing.T) {
	s, c := open(t)
	token, _, err := s.CreateSession("tag", c.now())
	must(t, err)
	later := c.now().Add(time.Hour)
	must(t, s.VerifySession(SessionID(token), later))
	got, err := s.CheckSession(token, "tag", later)
	must(t, err)
	if got == nil || !got.VerifiedAt.Equal(later) {
		t.Errorf("after verify: %+v", got)
	}

	// Changing the password keeps the changing session, entered now.
	other, _, err := s.CreateSession("tag", c.now())
	must(t, err)
	changed := later.Add(time.Minute)
	ended, err := s.ChangePassword(cheapHash("feed the cat"), token, "new", changed)
	must(t, err)
	if !slices.Equal(ended, []string{SessionID(other)}) {
		t.Errorf("ended %v", ended)
	}
	got, err = s.CheckSession(token, "new", changed)
	must(t, err)
	if got == nil || !got.VerifiedAt.Equal(changed) {
		t.Errorf("after the change: %+v", got)
	}
}

func TestMigrationNineAddsVerifiedAt(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	// A session from before version 9, used 20 days after a start 30 days
	// ago, with the old 90-day expiry from its last use.
	now := time.Now()
	created, used := now.Add(-30*24*time.Hour).Unix(), now.Add(-20*24*time.Hour).Unix()
	_, err = s.DB().Exec(`ALTER TABLE sessions DROP COLUMN verified_at;
		ALTER TABLE repos DROP COLUMN tracker; ALTER TABLE repos DROP COLUMN actions;
		ALTER TABLE bridges DROP COLUMN trackers; ALTER TABLE bridges DROP COLUMN harness`)
	must(t, err)
	_, err = s.DB().Exec(`INSERT INTO sessions (id, tag, created_at, renewed_at, expires_at) VALUES (?, 'tag', ?, ?, ?)`,
		hashToken("old"), created, used, used+90*24*3600)
	must(t, err)
	_, err = s.DB().Exec(`PRAGMA user_version = 8`)
	must(t, err)
	s.Close()

	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	var verified, expires int64
	must(t, s.DB().QueryRow(`SELECT verified_at, expires_at FROM sessions`).Scan(&verified, &expires))
	if verified != 0 {
		t.Errorf("verified_at = %d, want 0", verified)
	}
	if want := used + int64(SessionIdle/time.Second); expires != want {
		t.Errorf("expires_at = %d, want the idle end %d", expires, want)
	}
	// 20 idle days is past the 14-day idle end.
	if got, _ := s.CheckSession("old", "tag", now); got != nil {
		t.Errorf("old session live: %+v", got)
	}
}
