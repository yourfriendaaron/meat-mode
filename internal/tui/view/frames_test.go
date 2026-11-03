package view

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

var (
	widths  = []int{56, 89, 90, 140}
	heights = []int{20, 30}
)

// selectKey puts the selection on the row with id, as j and k would.
func selectKey(t *testing.T, m Model, id string) Model {
	t.Helper()
	rows := m.rows()
	for n, i := range selectable(rows) {
		if rows[i].id == id {
			m.setSel(n)
			m.fix()
			return m
		}
	}
	t.Fatalf("no row %q", id)
	return m
}

// scenario builds the model to draw at w by h.
type scenario struct {
	name  string
	build func(t *testing.T, w, h int) Model
}

func pressing(ks ...string) func(t *testing.T, w, h int) Model {
	return func(t *testing.T, w, h int) Model {
		m, _ := keys(t, model(t, w, h), ks...)
		return m
	}
}

// only keeps the items and repos keep picks, and epics when with is set.
func only(t *testing.T, w, h int, keep func(bridge, repo string) bool, withEpics bool, tab string) Model {
	t.Helper()
	s, e := load(t)
	var items []api.Item
	for _, i := range s.Items {
		if keep(i.Bridge, i.Repo) {
			items = append(items, i)
		}
	}
	var repos []api.Repo
	for _, r := range s.Repos {
		if keep(r.Bridge, r.Name) {
			repos = append(repos, r)
		}
	}
	s.Items, s.Repos = items, repos
	if !withEpics {
		e = api.Epics{Repos: []api.RepoEpics{}}
		s.Bridges = nil
	}
	m := New(hub).Resize(w, h).SetConn(ConnLive).SetData(s, e, now)
	m, _ = m.Key(tab)
	return m
}

var scenarios = []scenario{
	{"todos", pressing()},
	{"epics", pressing("2", "l")},
	{"all", pressing("3")},
	{"detail", pressing("j", "enter")},
	{"drill", func(t *testing.T, w, h int) Model {
		m, _ := keys(t, selectKey(t, pressing("3")(t, w, h), "laptop/acme/storefront/sf-c1x"), "enter")
		return m
	}},
	{"epic-detail", func(t *testing.T, w, h int) Model {
		m := pressing("2")(t, w, h)
		m = selectKey(t, m, "laptop/acme/api/api-h3k")
		m, _ = keys(t, m, "l", "enter")
		return m
	}},
	{"waiting-detail", func(t *testing.T, w, h int) Model {
		m, _ := keys(t, selectKey(t, pressing("3")(t, w, h), "laptop/acme/storefront/sf-c1x"), "enter")
		m = selectKey(t, m, "laptop/acme/storefront/sf-c1x.6")
		m, _ = keys(t, m, "enter")
		return m
	}},
	{"help", pressing("?")},
	{"snooze", func(t *testing.T, w, h int) Model {
		m, _ := keys(t, selectKey(t, model(t, w, h), "hub/personal/mm-44"), "s")
		return m
	}},
	{"filter", pressing("/", "c", "a", "r", "d")},
	{"filter-epics", pressing("2", "/", "l", "o", "g", "enter")},
	{"password", func(t *testing.T, w, h int) Model {
		m, _ := keys(t, model(t, w, h).AskPassword("The session ended. Enter the hub's password."), "s", "e", "c")
		return m
	}},
	{"notice", func(t *testing.T, w, h int) Model {
		m, _ := model(t, w, h).Notice("Sent mm-28 to the terminal's clipboard", NoticeInfo)
		return m
	}},
	{"no-epics-on", func(t *testing.T, w, h int) Model {
		s, _ := load(t)
		for i := range s.Repos {
			s.Repos[i].EpicsOn = false
		}
		m := New(hub).Resize(w, h).SetConn(ConnLive).SetData(s, api.Epics{Repos: []api.RepoEpics{}}, now)
		m, _ = m.Key("2")
		return m
	}},
	{"nothing-open-todos", func(t *testing.T, w, h int) Model {
		return only(t, w, h, func(string, string) bool { return false }, false, "1")
	}},
	{"nothing-open-epics", func(t *testing.T, w, h int) Model {
		s, e := load(t)
		for i := range e.Repos {
			e.Repos[i].Rows = []api.EpicRow{}
		}
		m := New(hub).Resize(w, h).SetConn(ConnConnecting).SetData(s, e, now)
		m, _ = m.Key("2")
		return m
	}},
	{"personal-only", func(t *testing.T, w, h int) Model {
		return only(t, w, h, func(b, r string) bool { return b == "hub" && r == "personal" }, false, "3")
	}},
	{"here", func(t *testing.T, w, h int) Model {
		s, e := load(t)
		m := New(hub).SetHere(Here{Home: "/srv/sam", Dir: "/srv/sam/acme/api/internal"}).Resize(w, h).SetConn(ConnLive).SetData(s, e, now)
		m, _ = m.Key("3")
		return m
	}},
	{"loading", func(t *testing.T, w, h int) Model { return New(hub).Resize(w, h) }},
}

// TestFrames draws every scenario at every width and height and compares
// it with its golden file, one file per scenario.
func TestFrames(t *testing.T) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			var b strings.Builder
			for _, h := range heights {
				for _, w := range widths {
					m := sc.build(t, w, h)
					checkFrame(t, fmt.Sprintf("%s %dx%d", sc.name, w, h), m)
					fmt.Fprintf(&b, "== %d x %d, layout %s\n", w, h, m.layout())
					for _, l := range plain(m) {
						b.WriteString(l + "|\n")
					}
					b.WriteString("\n")
				}
			}
			path := filepath.Join("testdata", "golden", sc.name+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run go test -update to write it)", err)
			}
			if string(want) != b.String() {
				t.Errorf("frames differ from %s; run go test ./internal/tui/view -update and read the diff", path)
			}
		})
	}
}

// TestTinySizes draws every scenario in panes too small for it: the frame
// keeps its exact size and nothing panics.
func TestTinySizes(t *testing.T) {
	for _, sc := range scenarios {
		for _, w := range []int{0, 1, 5, 12, 30} {
			for _, h := range []int{0, 1, 2, 3, 6} {
				m := sc.build(t, 56, 20).Resize(w, h)
				checkFrame(t, fmt.Sprintf("%s %dx%d", sc.name, w, h), m)
			}
		}
	}
}

// TestFramesHostileText draws an item and an epic card whose id, title,
// status and description carry ESC, U+009B, U+202E, U+2028 and BEL: none
// of them may reach the frame, in All, in the drill and in the item
// detail.
func TestFramesHostileText(t *testing.T) {
	s, e := load(t)
	for i := range s.Items {
		if s.Items[i].ID != "mm-28" {
			continue
		}
		it := &s.Items[i]
		it.ID = "mm-28\x1b\u009b\u202e\u2028"
		it.Title = "Book the \x1b[2Jboiler\u009bser\u202evice\u2028"
		it.Status = "in_progress\x1b]0;x\a"
		it.Description = "Boiler \x1b]0;x\aser\u202evice\u2028\n\u009b service"
	}
	const cardID = "sf-c1x\u009b"
	for k := range e.Repos {
		if e.Repos[k].Repo != "acme/storefront" {
			continue
		}
		for j := range e.Repos[k].Rows {
			r := &e.Repos[k].Rows[j]
			if r.ID == "sf-c1x" {
				r.ID = cardID
			}
			if r.Parent == "sf-c1x" {
				r.Parent = cardID
			}
		}
	}
	m := New(hub).Resize(90, 30).SetConn(ConnLive).SetData(s, e, now)
	m, _ = m.Key("3")
	checkFrame(t, "all hostile", m)
	m = selectKey(t, m, "laptop/acme/storefront/"+cardID)
	m, _ = m.Key("enter")
	checkFrame(t, "drill hostile", m)
	d := New(hub).Resize(90, 30).SetConn(ConnLive).SetData(s, e, now)
	d = selectKey(t, d, "hub/personal/mm-28")
	d, _ = d.Key("enter")
	checkFrame(t, "item detail hostile", d)
}
