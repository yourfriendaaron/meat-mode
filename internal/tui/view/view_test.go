package view

import (
	"encoding/json"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	if got := m.unmatched(); got != "notes" {
		t.Fatalf("unmatched = %q, want notes", got)
	}
	wide := m.Resize(100, 30)
	if top := wide.topBar(100).plain(); !strings.Contains(top, "notes is not on the hub") {
		t.Fatalf("top line %q does not say the folder is not on the hub", top)
	}
	m, _ = m.Key("p")
	if m.notice == "" || m.noticeKind != NoticeWarn {
		t.Fatalf("p outside a project gave notice %q", m.notice)
	}
	// Home and a matched project say nothing.
	home := New(hub).SetHere(Here{Home: "/srv/sam", Dir: "/srv/sam"}).SetData(s, e, now)
	if got := home.unmatched(); got != "" {
		t.Fatalf("unmatched in home = %q, want none", got)
	}
	proj := New(hub).SetHere(Here{Home: "/srv/sam", Dir: "/srv/sam/acme/api"}).SetData(s, e, now)
	if got := proj.unmatched(); got != "" {
		t.Fatalf("unmatched in a project = %q, want none", got)
	}
}

// TestStartsOnTheToDos checks that the viewer started in a project with
// epic rows still starts on the first to-do, with the project's epics
// below.
func TestStartsOnTheToDos(t *testing.T) {
	s, e := load(t)
	here := Here{Home: "/srv/sam", Dir: "/srv/sam/acme/api/internal"}
	m := New(hub).SetHere(here).Resize(100, 30).SetData(s, e, now)
	if inEpics(m.selKey) || m.selIdx != 0 {
		t.Fatalf("selection %q at %d, want the first to-do", m.selKey, m.selIdx)
	}
	m, _ = m.Key("2")
	if !inEpics(m.selKey) {
		t.Fatalf("2 selected %q, want an epic of the project", m.selKey)
	}
}

// TestHereTakesLinkedCopies checks that a folder matching one copy of a
// linked list scopes to every copy, as the hub shows the list from one copy
// at a time.
func TestHereTakesLinkedCopies(t *testing.T) {
	repos := []api.Repo{
		{Bridge: "desk", Name: "projects/garden", Group: "desk/projects/garden"},
		{Bridge: "laptop", Name: "Developer/garden", Group: "desk/projects/garden", Standby: true},
		{Bridge: "desk", Name: "projects/kitchen"},
	}
	got := hereLists(repos, Here{Home: "/srv/sam", Dir: "/srv/sam/Developer/garden/src"})
	want := map[string]bool{listKey("desk", "projects/garden"): true, listKey("laptop", "Developer/garden"): true}
	if !maps.Equal(got, want) {
		t.Errorf("hereLists = %v, want %v", got, want)
	}
	// The list counts once, from the copy the hub shows.
	repos[0].OptedIn, repos[1].OptedIn = true, true
	if v := visibleRepos(repos); len(v) != 1 || v[0].Bridge != "desk" {
		t.Errorf("visibleRepos = %v, want desk's copy alone", v)
	}
}

// TestHereSaysWhenOff checks that a project in scope whose sync switch is
// off, or whose epics are off, says so with the way to switch it on, and
// does not read as an empty list ("Nothing due today.", "No epics yet.")
// while another repo has its epics on.
func TestHereSaysWhenOff(t *testing.T) {
	here := Here{Home: "/srv/sam", Dir: "/srv/sam/acme/api"}
	// off is the fixtures with acme/api changed by set, its items and
	// epics gone as the hub drops them.
	off := func(set func(r *api.Repo)) Model {
		s, e := load(t)
		for k := range s.Repos {
			if s.Repos[k].Name == "acme/api" {
				set(&s.Repos[k])
			}
		}
		s.Items = slices.DeleteFunc(s.Items, func(it api.Item) bool { return it.Repo == "acme/api" })
		e.Repos = slices.DeleteFunc(e.Repos, func(re api.RepoEpics) bool { return re.Repo == "acme/api" })
		return New(hub).SetHere(here).Resize(100, 30).SetConn(ConnLive).SetData(s, e, now)
	}
	page := func(m Model) string { return strings.Join(plain(m), "\n") }
	for _, tc := range []struct {
		name      string
		set       func(r *api.Repo)
		want, not []string
	}{
		{"unsynced", func(r *api.Repo) { r.OptedIn = false },
			[]string{"api isn't synced: switch it on in Settings.", "Epics show once api is synced."},
			[]string{"Nothing due today.", "No epics yet."}},
		{"epics off", func(r *api.Repo) { r.EpicsOn = false },
			[]string{"Nothing due today.", "api's epics are off: switch them on in Settings."},
			[]string{"No epics yet.", "isn't synced"}},
	} {
		m := off(tc.set)
		got := page(m)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: page does not say %q:\n%s", tc.name, w, got)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: page says %q:\n%s", tc.name, n, got)
			}
		}
		// Every list shown, the other repos fill the page and nothing is off.
		m, _ = m.Key("p")
		if got := page(m); strings.Contains(got, "switch it on") || strings.Contains(got, "switch them on") {
			t.Errorf("%s: every list shown, page still says the project is off:\n%s", tc.name, got)
		}
	}
}
