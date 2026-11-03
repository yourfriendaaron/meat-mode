package view

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
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
// width cells, styled and plain, and no control, bidi or other invisible
// character left after the styles go.
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
		for _, r := range p {
			if badRune(r) {
				t.Errorf("%s: line %d keeps %q (U+%04X), which must not be drawn", name, i, r, r)
			}
		}
		if w := lipgloss.Width(p); w != m.width {
			t.Errorf("%s: plain line %d is %d cells, want %d", name, i, w, m.width)
		}
	}
}

// TestListCodes checks the badge codes: three letters or digits, leading
// punctuation skipped, and lists that share their first three told apart
// by a later word's initial or character.
func TestListCodes(t *testing.T) {
	cases := map[string]string{
		"projects/garden": "GAR",
		".agents":         "AGE",
		"ui":              "UI",
		"x":               "X",
		"🌱":               "?",
		"éclair":          "ÉCL",
	}
	for name, want := range cases {
		if got := codeCandidates(name)[0]; got != want {
			t.Errorf("codeCandidates(%q)[0] = %q, want %q", name, got, want)
		}
	}
	repos := []api.Repo{
		{Bridge: "laptop", Name: "work/garden.plan", OptedIn: true},
		{Bridge: "laptop", Name: "work/gardenparty", OptedIn: true},
		{Bridge: "laptop", Name: "work/garden-shed", OptedIn: true},
	}
	got := listCodes(repos)
	want := map[string]string{"work/garden-shed": "GAR", "work/garden.plan": "GAP", "work/gardenparty": "GAD"}
	for name, code := range want {
		if c := got[listKey("laptop", name)]; c != code {
			t.Errorf("code of %s = %q, want %q", name, c, code)
		}
	}
	for _, c := range []string{"X", "UI", "AGE"} {
		if w := lipgloss.Width(badgeText(c)); w != codeWidth {
			t.Errorf("badgeText(%q) is %d cells", c, w)
		}
	}
}

// TestHereScope checks that the viewer opens on the project it was started
// in, that p switches to every list and back, and that a folder in no
// project leaves every list showing.
func TestHereScope(t *testing.T) {
	s, e := load(t)
	open := func(m Model) map[string]bool {
		out := map[string]bool{}
		for _, it := range m.d.state.Items {
			out[it.Repo] = true
		}
		for _, re := range m.d.epics.Repos {
			out[re.Repo] = true
		}
		return out
	}
	// A root outside home matches by the last segments of the path.
	m := New(hub).SetHere(Here{Home: "/srv/sam", Dir: "/srv/acme/storefront/web"}).SetData(s, e, now)
	if got := open(m); len(got) != 1 || !got["acme/storefront"] {
		t.Fatalf("scoped lists = %v, want acme/storefront alone", got)
	}
	m, _ = m.Key("p")
	if got := open(m); !got["personal"] || !got["acme/api"] {
		t.Fatalf("after p, lists = %v, want every list", got)
	}
	m, _ = m.Key("p")
	if got := open(m); len(got) != 1 {
		t.Fatalf("after p p, lists = %v, want the project again", got)
	}
	// A folder that names no list shows everything, and p says so.
	m = New(hub).SetHere(Here{Home: "/srv/sam", Dir: "/srv/sam/notes"}).SetData(s, e, now)
	if got := open(m); !got["personal"] {
		t.Fatalf("unmatched folder lists = %v, want every list", got)
	}
	m, _ = m.Key("p")
	if m.notice == "" || m.noticeKind != NoticeWarn {
		t.Fatalf("p outside a project gave notice %q", m.notice)
	}
}

// TestOpensOnEpicsInAProject checks that the viewer started in a project
// with epic rows opens on Epics, that a tab key then sticks through new
// data, and that it opens on To-dos anywhere else.
func TestOpensOnEpicsInAProject(t *testing.T) {
	s, e := load(t)
	here := Here{Home: "/srv/sam", Dir: "/srv/sam/acme/api/internal"}
	m := New(hub).SetHere(here).Resize(100, 30).SetData(s, e, now)
	if m.tab != TabEpics {
		t.Fatalf("in a project with epics: tab %v, want Epics", m.tab)
	}
	m, _ = m.Key("1")
	if m = m.SetData(s, e, now); m.tab != TabTodos {
		t.Errorf("after 1 and new data: tab %v, want To-dos", m.tab)
	}
	if m := New(hub).Resize(100, 30).SetData(s, e, now); m.tab != TabTodos {
		t.Errorf("outside a project: tab %v, want To-dos", m.tab)
	}
	late := New(hub).SetHere(here).Resize(100, 30).SetData(s, api.Epics{}, now)
	if late.tab != TabTodos {
		t.Fatalf("before epics arrive: tab %v, want To-dos", late.tab)
	}
	if late = late.SetData(s, e, now); late.tab != TabEpics {
		t.Errorf("when epics arrive: tab %v, want Epics", late.tab)
	}
}
