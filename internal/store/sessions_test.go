package store

import (
	"slices"
	"testing"
	"time"
)

func TestEndOtherLogins(t *testing.T) {
	s, c := open(t)
	old, _, err := s.CreateSession("old", c.now(), time.Hour)
	must(t, err)
	cur, _, err := s.CreateSession("new", c.now(), time.Hour)
	must(t, err)

	ended, err := s.EndOtherLogins("new")
	must(t, err)
	if want := []string{SessionID(old)}; !slices.Equal(ended, want) {
		t.Errorf("ended %v, want %v", ended, want)
	}
	if sess, err := s.CheckSession(cur, "new", c.now(), time.Hour); err != nil || sess == nil {
		t.Errorf("session under the current tag: %v, %v; want it kept", sess, err)
	}

	// Login off ends every session.
	ended, err = s.EndOtherLogins("")
	must(t, err)
	if want := []string{SessionID(cur)}; !slices.Equal(ended, want) {
		t.Errorf("login off ended %v, want %v", ended, want)
	}
}
