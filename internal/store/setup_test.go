package store

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// cheapHash is a PHC hash of password with tiny parameters, so tests stay
// fast.
func cheapHash(password string) string {
	salt := []byte("saltsaltsaltsalt")
	key := argon2.IDKey([]byte(password), salt, 1, 64, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=64,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func status(t *testing.T, s *Store) string {
	t.Helper()
	st, err := s.SetupStatus()
	must(t, err)
	return st
}

func TestSetupStatusFollowsItemsMachinesAndRecord(t *testing.T) {
	s, _ := open(t)
	if got := status(t, s); got != SetupNew {
		t.Fatalf("fresh hub: %q", got)
	}

	// A personal item makes the hub in use.
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: "add", Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: "Water the plants"}}, "alex-app")
	must(t, err)
	if got := status(t, s); got != SetupInUse {
		t.Fatalf("with an item: %q", got)
	}

	// So does a machine, even revoked and with no items.
	s2, _ := open(t)
	_, err = s2.CreateBridge("laptop")
	must(t, err)
	must(t, s2.RevokeBridge("laptop"))
	if got := status(t, s2); got != SetupInUse {
		t.Fatalf("with a revoked machine: %q", got)
	}

	// A record wins over both.
	must(t, s.RecordSetup(SetupDismissed))
	if got := status(t, s); got != SetupDismissed {
		t.Fatalf("after dismissing: %q", got)
	}
	must(t, s.RecordSetup(SetupDone))
	if got := status(t, s); got != SetupDone {
		t.Fatalf("after finishing: %q", got)
	}
	h, err := s.Settings()
	must(t, err)
	if h.Setup != SetupDone || h.SetupAt == nil {
		t.Fatalf("settings: %+v", h)
	}
	if err := s.RecordSetup("later"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown status: %v", err)
	}
}

func TestUpgradedHubWithDataIsInUse(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	seed(t, s)
	// Back to the schema before hub settings existed.
	_, err = s.DB().Exec(`DROP TABLE hub_settings; DROP TABLE personal_owners;
		ALTER TABLE repos DROP COLUMN agents_allowed; DROP TABLE runs;
		ALTER TABLE sessions DROP COLUMN verified_at;
		ALTER TABLE repos DROP COLUMN tracker; ALTER TABLE repos DROP COLUMN actions;
		ALTER TABLE bridges DROP COLUMN trackers; ALTER TABLE bridges DROP COLUMN harness; PRAGMA user_version = 3`)
	must(t, err)
	must(t, s.Close())

	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	if got := status(t, s); got != SetupInUse {
		t.Fatalf("upgraded hub: %q", got)
	}
}

func TestNameAndTimeZone(t *testing.T) {
	s, _ := open(t)
	must(t, s.SetUserName("  Alex  "))
	must(t, s.SetTimeZone("Europe/Paris"))
	h, err := s.Settings()
	must(t, err)
	if h.UserName != "Alex" || h.TimeZone != "Europe/Paris" {
		t.Fatalf("settings: %+v", h)
	}
	for _, name := range []string{strings.Repeat("a", maxUserName+1), "Al\nex"} {
		if err := s.SetUserName(name); !errors.Is(err, ErrInvalid) {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	for _, tz := range []string{"", "Local", "Mars/Olympus"} {
		if err := s.SetTimeZone(tz); !errors.Is(err, ErrInvalid) {
			t.Fatalf("time zone %q: %v", tz, err)
		}
	}
	must(t, s.SetUserName(""))
	h, _ = s.Settings()
	if h.UserName != "" || h.TimeZone != "Europe/Paris" {
		t.Fatalf("after clearing the name: %+v", h)
	}
}

func TestPasswordHash(t *testing.T) {
	s, _ := open(t)
	if err := s.SetPasswordHash("hunter2"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("plain password: %v", err)
	}
	phc := cheapHash("correct horse battery staple")
	must(t, s.SetPasswordHash(phc))
	h, _ := s.Settings()
	if h.PasswordHash != phc {
		t.Fatalf("settings: %+v", h)
	}
	must(t, s.SetPasswordHash(""))
	h, _ = s.Settings()
	if h.PasswordHash != "" {
		t.Fatalf("after clearing: %+v", h)
	}
}

func TestSetupCodeClaimsOnce(t *testing.T) {
	s, c := open(t)
	phc := cheapHash("correct horse battery staple")
	if err := s.ClaimSetupCode("ABCD-EFGH-JKLM", phc); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("no code made: %v", err)
	}

	code, err := s.NewSetupCode(time.Hour)
	must(t, err)
	if len(code) != 14 || code[4] != '-' || code[9] != '-' || strings.ContainsAny(code, "01IO") {
		t.Fatalf("code %q", code)
	}
	h, _ := s.Settings()
	if h.SetupCodeExpires == nil || !h.SetupCodeExpires.Equal(c.t.Add(time.Hour)) || h.SetupCodeUsed != nil {
		t.Fatalf("settings: %+v", h)
	}
	if err := s.ClaimSetupCode("ABCD-EFGH-JKLM", phc); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong code: %v", err)
	}
	if err := s.ClaimSetupCode(code, "not a hash"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad hash: %v", err)
	}

	// Typed in lower case with spaces, it still matches.
	typed := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	must(t, s.ClaimSetupCode(typed, phc))
	h, _ = s.Settings()
	if h.PasswordHash != phc || h.SetupCodeUsed == nil {
		t.Fatalf("after claiming: %+v", h)
	}
	if err := s.ClaimSetupCode(code, phc); !errors.Is(err, ErrConflict) {
		t.Fatalf("second claim with a password set: %v", err)
	}
	must(t, s.SetPasswordHash(""))
	if err := s.ClaimSetupCode(code, phc); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("used code: %v", err)
	}
}

func TestSetupCodeExpires(t *testing.T) {
	s, c := open(t)
	code, err := s.NewSetupCode(time.Hour)
	must(t, err)
	c.t = c.t.Add(time.Hour)
	if err := s.ClaimSetupCode(code, cheapHash("pw")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired code: %v", err)
	}
	// A new code replaces the old one.
	code2, err := s.NewSetupCode(time.Hour)
	must(t, err)
	if err := s.ClaimSetupCode(code, cheapHash("pw")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("replaced code: %v", err)
	}
	must(t, s.ClaimSetupCode(code2, cheapHash("pw")))
}

func TestUpdateProfileIsAllOrNothing(t *testing.T) {
	s, _ := open(t)
	name, tz, lead := "Sam", "Europe/Paris", 30
	must(t, s.UpdateProfile(&name, &tz, &lead))
	bad, other, otherLead := "Mars/Olympus", "Casey", 60
	if err := s.UpdateProfile(&other, &bad, &otherLead); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad zone: %v", err)
	}
	for _, l := range []int{-1, api.MaxLeadMinutes + 1} {
		if err := s.UpdateProfile(&other, nil, &l); !errors.Is(err, ErrInvalid) {
			t.Fatalf("lead %d: %v", l, err)
		}
	}
	h, err := s.Settings()
	must(t, err)
	if h.UserName != "Sam" || h.TimeZone != "Europe/Paris" || h.ReminderLead != 30 {
		t.Fatalf("after a refused update: %+v", h)
	}
	must(t, s.UpdateProfile(nil, nil, nil))
	must(t, s.UpdateProfile(&other, nil, nil))
	h, _ = s.Settings()
	if h.UserName != "Casey" || h.TimeZone != "Europe/Paris" || h.ReminderLead != 30 {
		t.Fatalf("name only: %+v", h)
	}
	for _, l := range []int{0, api.MaxLeadMinutes} {
		must(t, s.UpdateProfile(nil, nil, &l))
		if h, _ = s.Settings(); h.ReminderLead != l {
			t.Fatalf("lead %d: %+v", l, h)
		}
	}
}

func TestNameAndTimeZoneSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	must(t, s.SetUserName("Sam"))
	must(t, s.SetTimeZone("America/New_York"))
	s.Close()
	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	h, err := s.Settings()
	must(t, err)
	if h.UserName != "Sam" || h.TimeZone != "America/New_York" {
		t.Fatalf("after reopening: %+v", h)
	}
}
