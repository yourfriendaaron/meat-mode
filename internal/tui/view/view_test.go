package view

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

var update = flag.Bool("update", false, "rewrite the golden frames in testdata")

// now is the fixtures' moment: a Monday morning.
var now = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

const hub = "https://meat.example.com"

func load(t *testing.T) (api.State, api.Epics) {
	t.Helper()
	var s api.State
	var e api.Epics
	for name, v := range map[string]any{"state.json": &s, "epics.json": &e} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	return s, e
}

// model is the fixtures at width by height, live.
func model(t *testing.T, w, h int) Model {
	t.Helper()
	s, e := load(t)
	return New(hub).Resize(w, h).SetConn(ConnLive).SetData(s, e, now)
}

// keys presses each key in turn, keeping the effects of the last.
func keys(t *testing.T, m Model, ks ...string) (Model, []Effect) {
	t.Helper()
	var fx []Effect
	for _, k := range ks {
		m, fx = m.Key(k)
	}
	return m, fx
}

var sgr = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// plain is the frame without styles.
func plain(m Model) []string {
	var out []string
	for _, l := range Frame(m) {
		out = append(out, sgr.ReplaceAllString(l, ""))
	}
	return out
}

// checkFrame checks the frame's size: exactly height lines, each exactly
// width cells, styled and plain, and no escape left after the styles go.
func checkFrame(t *testing.T, name string, m Model) {
	t.Helper()
	styled := Frame(m)
	if len(styled) != m.height {
		t.Fatalf("%s: %d lines, want %d", name, len(styled), m.height)
	}
	for i, l := range styled {
		if w := lipgloss.Width(l); w != m.width {
			t.Errorf("%s: line %d is %d cells, want %d: %q", name, i, w, m.width, sgr.ReplaceAllString(l, ""))
		}
		p := sgr.ReplaceAllString(l, "")
		if strings.ContainsRune(p, 0x1b) {
			t.Errorf("%s: line %d keeps an escape: %q", name, i, p)
		}
		if w := lipgloss.Width(p); w != m.width {
			t.Errorf("%s: plain line %d is %d cells, want %d", name, i, w, m.width)
		}
	}
}
