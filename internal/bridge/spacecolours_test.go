package bridge

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSpaceSlot(t *testing.T) {
	for _, tc := range []struct {
		in   string
		slot int
		ok   bool
	}{
		{"ansi5", 0, true}, {"ansi6", 1, true}, {"ansi2", 2, true},
		{"ansi3", 3, true}, {"ansi4", 4, true}, {"ansi1", 5, true},
		{"ANSI6", 0, false}, {"ansi0", 0, false}, {"ansi7", 0, false}, {"", 0, false},
		{"#8b7cf6", 0, true}, {"#22a99a", 1, true}, {"#3fae6a", 2, true},
		{"#e3814f", 3, true}, {"#5b8def", 4, true}, {"#d9649a", 5, true},
		{"#8B7CF6", 0, true}, {"#D9649A", 5, true},
		{"#000000", 2, true}, // nearest by squared distance in RGB
		{"#ffffff", 0, true},
		{"#ff0000", 3, true},
		{"#fff", 0, false}, {"#ffffffff", 0, false}, {"ffffff", 0, false},
		{"#gggggg", 0, false}, {"#12345", 0, false}, {"#+12345", 0, false},
	} {
		slot, ok := spaceSlot(tc.in)
		if ok != tc.ok || (ok && slot != tc.slot) {
			t.Errorf("spaceSlot(%q) = %d, %v; want %d, %v", tc.in, slot, ok, tc.slot, tc.ok)
		}
	}
}

// A colour equally near two references takes the lower slot. The test finds
// such colours on a coarse grid and checks each against the rule.
func TestSpaceSlotTieGoesToLowerSlot(t *testing.T) {
	ties := 0
	for r := 0; r < 256; r += 5 {
		for g := 0; g < 256; g += 5 {
			for b := 0; b < 256; b += 5 {
				var ds [6]int
				best := 1 << 30
				for s, ref := range spaceRefs {
					ds[s] = (r-ref[0])*(r-ref[0]) + (g-ref[1])*(g-ref[1]) + (b-ref[2])*(b-ref[2])
					best = min(best, ds[s])
				}
				first, n := -1, 0
				for s, d := range ds {
					if d == best {
						n++
						if first < 0 {
							first = s
						}
					}
				}
				if n < 2 {
					continue
				}
				ties++
				in := "#" + hex2(r) + hex2(g) + hex2(b)
				if slot, _ := spaceSlot(in); slot != first {
					t.Errorf("spaceSlot(%s) = %d, want %d (the lowest of a tie)", in, slot, first)
				}
			}
		}
	}
	if ties == 0 {
		t.Fatal("found no tie to test")
	}
}

func hex2(n int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[n>>4], digits[n&15]})
}

func TestParseSpaceColours(t *testing.T) {
	good := func(p, c string) SpaceColour {
		s, _ := spaceSlot(c)
		return SpaceColour{Path: p, Slot: s}
	}
	for _, tc := range []struct {
		name string
		in   string
		want []SpaceColour
		bad  bool
	}{
		{"one entry", `{"version":1,"spaces":[{"path":"/srv/projects/garden","colour":"ansi6"}]}`,
			[]SpaceColour{good("/srv/projects/garden", "ansi6")}, false},
		{"version 1.0", `{"version":1.0,"spaces":[]}`, nil, false},
		{"spaces missing", `{"version":1}`, nil, false},
		{"spaces null", `{"version":1,"spaces":null}`, nil, false},
		{"spaces empty", `{"version":1,"spaces":[]}`, nil, false},
		{"unknown keys kept out", `{"version":1,"extra":true,"spaces":[{"path":"/srv/a","colour":"ansi1","name":"x"}]}`,
			[]SpaceColour{good("/srv/a", "ansi1")}, false},
		{"path is cleaned", `{"version":1,"spaces":[{"path":"/srv//a/./b/../c/","colour":"ansi2"}]}`,
			[]SpaceColour{good("/srv/a/c", "ansi2")}, false},
		{"hex in any case", `{"version":1,"spaces":[{"path":"/srv/a","colour":"#D9649A"},{"path":"/srv/b","colour":"#d9649a"}]}`,
			[]SpaceColour{good("/srv/a", "ansi1"), good("/srv/b", "ansi1")}, false},
		{"one bad entry among good", `{"version":1,"spaces":[
			{"path":"/srv/a","colour":"ansi6"},
			"nope", 7, null, [], {},
			{"path":"/srv/b"}, {"colour":"ansi6"},
			{"path":7,"colour":"ansi6"}, {"path":"/srv/c","colour":7},
			{"path":"/srv/d","colour":"#fff"}, {"path":"/srv/e","colour":"ANSI6"},
			{"path":"relative/f","colour":"ansi6"}, {"path":"","colour":"ansi6"},
			{"path":"/srv/g","colour":"ansi3"}]}`,
			[]SpaceColour{good("/srv/a", "ansi6"), good("/srv/g", "ansi3")}, false},
		{"version string", `{"version":"1","spaces":[{"path":"/srv/a","colour":"ansi6"}]}`, nil, true},
		{"version 2", `{"version":2,"spaces":[]}`, nil, true},
		{"version missing", `{"spaces":[]}`, nil, true},
		{"version null", `{"version":null,"spaces":[]}`, nil, true},
		{"array", `[{"path":"/srv/a","colour":"ansi6"}]`, nil, true},
		{"null", `null`, nil, true},
		{"string", `"x"`, nil, true},
		{"not JSON", `{"version":1,`, nil, true},
		{"empty", ``, nil, true},
		{"spaces not a list", `{"version":1,"spaces":{}}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSpaceColours("f.json", []byte(tc.in))
			if (err != nil) != tc.bad {
				t.Fatalf("err = %v, want error %v", err, tc.bad)
			}
			if tc.bad && got != nil {
				t.Errorf("a bad file gave entries %+v", got)
			}
			if !tc.bad && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseSpaceColoursResolvesSymlinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real", "garden")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	in := `{"version":1,"spaces":[{"path":"` + filepath.Join(root, "link", "garden") + `","colour":"ansi3"},` +
		`{"path":"` + filepath.Join(root, "link", "gone") + `","colour":"ansi4"}]}`
	got, err := parseSpaceColours("f.json", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	// A path that does not resolve stays as cleaned.
	want := []SpaceColour{{real, 3}, {filepath.Join(root, "link", "gone"), 4}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReadSpaceColoursFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("missing", func(t *testing.T) {
		got, err := ReadSpaceColours(filepath.Join(dir, "none.json"))
		if got != nil || err != nil {
			t.Errorf("got %+v, %v; want nothing, no error", got, err)
		}
	})
	t.Run("a folder cannot be read", func(t *testing.T) {
		if got, err := ReadSpaceColours(dir); got != nil || err == nil {
			t.Errorf("got %+v, %v; want an error", got, err)
		}
	})
	pad := func(n int) string { return strings.Repeat(" ", n) }
	const head = `{"version":1,"spaces":[{"path":"/srv/a","colour":"ansi6"}]}`
	t.Run("exactly 1 MiB is read", func(t *testing.T) {
		got, err := ReadSpaceColours(write("max.json", head+pad(1<<20-len(head))))
		if err != nil || len(got) != 1 {
			t.Errorf("got %+v, %v; want one entry", got, err)
		}
	})
	t.Run("one byte over is refused", func(t *testing.T) {
		got, err := ReadSpaceColours(write("over.json", head+pad(1<<20-len(head)+1)))
		if err == nil || got != nil || !strings.Contains(err.Error(), "1 MiB") {
			t.Errorf("got %+v, %v; want a size error", got, err)
		}
	})
}

func TestSpaceColoursPath(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "MEAT_MODE_SPACE_COLOURS" {
				return v
			}
			return ""
		}
	}
	for _, tc := range []struct {
		name, env, state, want string
		bad                    bool
	}{
		{"default", "", "/state/meat-mode", "/state/meat-mode/space-colours.json", false},
		{"variable wins", "/srv/colours.json", "/state/meat-mode", "/srv/colours.json", false},
		{"relative variable", "colours.json", "/state/meat-mode", "", true},
		{"dot-relative variable", "./colours.json", "/state/meat-mode", "", true},
		{"no home, no variable", "", "", "", true},
		{"no home, variable set", "/srv/colours.json", "", "/srv/colours.json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SpaceColoursPath(env(tc.env), tc.state)
			if (err != nil) != tc.bad || got != tc.want {
				t.Errorf("got %q, %v; want %q, error %v", got, err, tc.want, tc.bad)
			}
		})
	}
	// The loader turns the same errors into no colours.
	if got, err := LoadSpaceColours(env("colours.json"), "/state")(); got != nil || err == nil {
		t.Errorf("relative variable: got %+v, %v; want an error", got, err)
	}
}

func TestAssignSpaceColours(t *testing.T) {
	p := func(n int) *int { return &n }
	for _, tc := range []struct {
		name    string
		entries []SpaceColour
		dirs    []string
		want    []*int
	}{
		{"exact folder", []SpaceColour{{"/srv/garden", 1}}, []string{"/srv/garden"}, []*int{p(1)}},
		{"inside the repo", []SpaceColour{{"/srv/garden/web", 2}}, []string{"/srv/garden"}, []*int{p(2)}},
		{"under no repo", []SpaceColour{{"/srv/other", 2}}, []string{"/srv/garden"}, []*int{nil}},
		{"a repo inside the path is not reached", []SpaceColour{{"/srv", 2}}, []string{"/srv/garden"}, []*int{nil}},
		{"whole segments only", []SpaceColour{{"/srv/gardenx", 2}}, []string{"/srv/garden"}, []*int{nil}},
		{"whole segments, the other way", []SpaceColour{{"/srv/garden", 2}}, []string{"/srv/gardenx"}, []*int{nil}},
		{"byte for byte", []SpaceColour{{"/srv/Garden", 2}}, []string{"/srv/garden"}, []*int{nil}},
		{"nested repos: the deepest gets it", []SpaceColour{{"/srv/garden/web/src", 3}},
			[]string{"/srv/garden", "/srv/garden/web"}, []*int{nil, p(3)}},
		{"nested repos: the outer keeps its own", []SpaceColour{{"/srv/garden", 3}, {"/srv/garden/web", 4}},
			[]string{"/srv/garden", "/srv/garden/web"}, []*int{p(3), p(4)}},
		{"nested repos, listed deepest first", []SpaceColour{{"/srv/garden/web", 4}},
			[]string{"/srv/garden/web", "/srv/garden"}, []*int{p(4), nil}},
		{"exact beats inside", []SpaceColour{{"/srv/garden/web", 1}, {"/srv/garden", 2}},
			[]string{"/srv/garden"}, []*int{p(2)}},
		{"first among exact", []SpaceColour{{"/srv/garden", 1}, {"/srv/garden", 2}},
			[]string{"/srv/garden"}, []*int{p(1)}},
		{"first among inside", []SpaceColour{{"/srv/garden/a", 1}, {"/srv/garden/b", 2}},
			[]string{"/srv/garden"}, []*int{p(1)}},
		{"exact after inside, then another exact", []SpaceColour{{"/srv/garden/a", 1}, {"/srv/garden", 2}, {"/srv/garden", 3}},
			[]string{"/srv/garden"}, []*int{p(2)}},
		{"slot 0 is a colour", []SpaceColour{{"/srv/garden", 0}}, []string{"/srv/garden"}, []*int{p(0)}},
		{"a root repo holds everything", []SpaceColour{{"/srv/a", 2}}, []string{"/"}, []*int{p(2)}},
		{"no entries", nil, []string{"/srv/garden"}, []*int{nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := assignSpaceColours(tc.entries, tc.dirs); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %s, want %s", showSlots(got), showSlots(tc.want))
			}
		})
	}
}

func showSlots(s []*int) string {
	var parts []string
	for _, p := range s {
		if p == nil {
			parts = append(parts, "nil")
		} else {
			parts = append(parts, string(rune('0'+*p)))
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// A changed file changes the repo report the hub gets; a bad file takes the
// colours away rather than keeping the last read.
func TestDiscoverReportsSpaceColours(t *testing.T) {
	w := newWorld(t, false)
	garden := w.repo("garden", true)
	w.repo("shed", true)
	file := filepath.Join(t.TempDir(), "space-colours.json")
	var logs bytes.Buffer
	b := w.start()
	b.Log = slog.New(slog.NewTextHandler(&logs, nil))
	b.LoadSpaceColours = LoadSpaceColours(func(k string) string {
		if k == "MEAT_MODE_SPACE_COLOURS" {
			return file
		}
		return ""
	}, "")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		w.tick(b)
	}
	colours := func() map[string]*int {
		out := map[string]*int{}
		for _, r := range w.hub.Repos(us) {
			out[r.Name] = r.SpaceColour
		}
		return out
	}
	want := func(garden, shed *int) {
		t.Helper()
		if got := colours(); !reflect.DeepEqual(got, map[string]*int{"garden": garden, "shed": shed}) {
			t.Errorf("colours = %v, want garden %v shed %v", showMap(got), garden, shed)
		}
	}
	one, four := 1, 4

	w.tick(b) // no file yet
	want(nil, nil)
	write(`{"version":1,"spaces":[{"path":"` + filepath.Join(garden, "web") + `","colour":"ansi6"}]}`)
	want(&one, nil)
	write(`{"version":1,"spaces":[{"path":"` + garden + `","colour":"#5b8def"}]}`)
	want(&four, nil)
	if strings.Contains(logs.String(), "WARN") {
		t.Errorf("logged for a clean file: %s", logs.String())
	}
	write(`{"version":2,"spaces":[]}`)
	want(nil, nil)
	write(`{"version":2,"spaces":[]}`)
	write(`{"version":"1"}`)
	write(`not json`)
	if n := strings.Count(logs.String(), "no workspace colours"); n != 2 {
		t.Errorf("warnings = %d, want 2 (one per change of message):\n%s", n, logs.String())
	}
	write(`{"version":1,"spaces":[{"path":"` + garden + `","colour":"ansi6"}]}`)
	want(&one, nil)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	w.tick(b)
	want(nil, nil)
	if n := strings.Count(logs.String(), "no workspace colours"); n != 2 {
		t.Errorf("a clean or missing file logged: %s", logs.String())
	}
	// A problem that returns after a clean read is warned of again.
	write(`not json`)
	if n := strings.Count(logs.String(), "no workspace colours"); n != 3 {
		t.Errorf("warnings = %d, want 3", n)
	}
}

func showMap(m map[string]*int) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if v != nil {
			out[k] = *v
		} else {
			out[k] = nil
		}
	}
	return out
}
