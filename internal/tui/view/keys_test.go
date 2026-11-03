package view

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// contractKeys are the keys the contract names.
var contractKeys = []string{"j", "k", "up", "down", "left", "right", "h", "l", "g", "G", "home", "end", "pgup",
	"pgdown", "enter", "esc", "backspace", "tab", "shift+tab", "space", "/", "?", "y", "Y", "x", "d", "u", "s", "o",
	"r", "q", "ctrl+c", "0", "1", "2", "3", "4", "5", "6", "p", "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b", "ctrl+n",
	"ctrl+p", "ctrl+w"}

func ptime(t time.Time) *time.Time { return &t }

func act(fx []Effect) (Act, bool) {
	if len(fx) == 1 {
		a, ok := fx[0].(Act)
		return a, ok
	}
	return Act{}, false
}

// TestEveryKeyEverywhere presses each contract key in each tab and layout,
// from the list and from an open detail, and checks the frame stays exact
// and the effects are the ones that key can give.
func TestEveryKeyEverywhere(t *testing.T) {
	allowed := map[string][]string{
		"q": {"view.Quit"}, "ctrl+c": {"view.Quit"}, "r": {"view.Refresh"},
		"y": {"view.Copy"}, "Y": {"view.Copy"}, "x": {"view.Act"}, "d": {"view.Act"}, "o": {"view.OpenURL"},
	}
	for _, w := range []int{56, 140} {
		for _, tab := range []string{"1", "2", "3"} {
			for _, before := range [][]string{nil, {"enter"}, {"j", "j", "l"}} {
				for _, k := range contractKeys {
					m, _ := keys(t, model(t, w, 24), append([]string{tab}, before...)...)
					m2, fx := m.Key(k)
					checkFrame(t, "after "+k, m2)
					for _, e := range fx {
						name := reflect.TypeOf(e).String()
						ok := false
						for _, a := range allowed[k] {
							ok = ok || a == name
						}
						if !ok {
							t.Errorf("w %d tab %s after %v: %q gave %s", w, tab, before, k, name)
						}
					}
				}
			}
		}
	}
}

func TestKeyTable(t *testing.T) {
	cases := []struct {
		name  string
		w     int
		setup []string
		sel   string // a row to select before the keys
		keys  []string
		want  []Effect
		note  string // a part of the notice the keys leave
		check func(t *testing.T, m Model)
	}{
		{name: "y copies id and title", w: 56, keys: []string{"y"},
			want: []Effect{Copy{Text: "mm-28 Book the boiler service", Ref: "mm-28"}}},
		{name: "y on a project item names the project", w: 140, sel: "hub/projects/garden/mm-4", keys: []string{"y"},
			want: []Effect{Copy{Text: "mm-4 (projects/garden) Order bark mulch", Ref: "mm-4"}}},
		{name: "j then y", w: 89, keys: []string{"j", "y"},
			want: []Effect{Copy{Text: "api-9fq Rotate the staging API key in the vault", Ref: "api-9fq"}}},
		{name: "Y copies the block", w: 140, sel: "laptop/acme/api/api-9fq", keys: []string{"Y"},
			want: []Effect{Copy{Ref: "api-9fq", Text: "api-9fq Rotate the staging API key in the vault\n" +
				"api · due Sun, Nov 1 17:00\n\n" +
				"The agent cannot reach the vault UI.\n\n1. Open the staging vault.\n2. Rotate `STAGING_API_KEY`.\n3. Run `bd close api-9fq` or tick it here.\n\n" +
				"Notes:\n- agent: The deploy is blocked on this; everything else is ready."}}},
		{name: "Y on a to-do with sub-tasks", w: 56, sel: "hub/personal/mm-46", keys: []string{"Y"},
			want: []Effect{Copy{Ref: "mm-46", Text: "mm-46 Buy birthday card for Alex\npersonal · due Thu, Nov 5 9:00 · #errands\n\n" +
				"Sub-tasks:\n- [ ] mm-47 Choose a card\n- [x] mm-48 Write the card"}}},
		{name: "Y on an epic lists its children", w: 140, setup: []string{"2"}, sel: "laptop/acme/api/api-z9p", keys: []string{"Y"},
			want: []Effect{Copy{Ref: "api-z9p", Text: "api-z9p Remove legacy auth endpoints\nacme/api · Open · P2\n\nChildren:\n" +
				"- [x] api-z9p.1 Log callers of /v1/login\n- [x] api-z9p.2 Email the two remaining callers\n- [x] api-z9p.3 Delete /v1/login\n- [x] api-z9p.4 Delete /v1/token"}}},
		{name: "x marks done", w: 56, keys: []string{"x"},
			want: []Effect{Act{Action: api.NewAction{Kind: "done", Key: "hub/personal/mm-28"}, Done: "Done: Book the boiler service"}}},
		{name: "x on a done to-do reopens", w: 140, sel: "hub/personal/mm-30", keys: []string{"x"},
			want: []Effect{Act{Action: api.NewAction{Kind: "reopen", Key: "hub/personal/mm-30"}, Done: "Reopened: Take out recycling"}}},
		{name: "x with an open sub-task says so", w: 56, sel: "hub/personal/mm-46", keys: []string{"x"},
			note: "Finish or remove its open sub-task first."},
		{name: "d on an agent's task is won't do", w: 140, sel: "laptop/acme/api/api-9fq", keys: []string{"d"},
			want: []Effect{Act{Action: api.NewAction{Kind: "dismiss", Key: "laptop/acme/api/api-9fq"}, Done: "Won't do: Rotate the staging API key in the vault"}}},
		{name: "d deletes a to-do and its open sub-task first", w: 56, sel: "hub/personal/mm-46", keys: []string{"d"},
			want: []Effect{Act{Before: []api.NewAction{{Kind: "dismiss", Key: "hub/personal/mm-47"}},
				Action: api.NewAction{Kind: "dismiss", Key: "hub/personal/mm-46"}, Done: "Deleted: Buy birthday card for Alex and its sub-task"}},
			check: func(t *testing.T, m Model) {
				if m.selKey == "hub/personal/mm-46" {
					t.Fatal("selection stayed on the deleted to-do")
				}
			}},
		{name: "d on a done to-do", w: 140, sel: "hub/personal/mm-30", keys: []string{"d"}, note: "Only open to-dos can be dismissed."},
		{name: "epic row is read-only for d", w: 56, setup: []string{"2"}, keys: []string{"d"}, note: "in acme/api"},
		{name: "s 1 snoozes an hour", w: 56, sel: "hub/personal/mm-44", keys: []string{"s", "1"},
			want: []Effect{Act{Action: api.NewAction{Kind: "snooze", Key: "hub/personal/mm-44",
				Payload: api.Payload{Until: ptime(now.Add(time.Hour))}}, Done: "Snoozed to 11:00"}}},
		{name: "s 3 on a snoozed to-do sends the snooze it replaces", w: 140, sel: "hub/personal/mm-50", keys: []string{"s", "3"},
			want: []Effect{Act{Action: api.NewAction{Kind: "snooze", Key: "hub/personal/mm-50",
				Payload: api.Payload{Until: ptime(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)), Previous: ptime(time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC))}},
				Done: "Snoozed to Tomorrow 9:00"}}},
		{name: "s then esc closes the menu", w: 56, keys: []string{"s", "esc", "y"},
			want: []Effect{Copy{Text: "mm-28 Book the boiler service", Ref: "mm-28"}}},
		{name: "s on a done to-do", w: 56, sel: "hub/personal/mm-30", keys: []string{"s"}, note: "Only open to-dos snooze."},
		{name: "o shows the app link", w: 56, keys: []string{"o"},
			want: []Effect{OpenURL{URL: "https://meat.example.com/?item=hub%2Fpersonal%2Fmm-28"}}},
		{name: "q quits", w: 56, keys: []string{"q"}, want: []Effect{Quit{}}},
		{name: "ctrl+c quits from the filter", w: 56, keys: []string{"/", "ctrl+c"}, want: []Effect{Quit{}}},
		{name: "r refreshes", w: 140, setup: []string{"3"}, keys: []string{"r"}, want: []Effect{Refresh{}}},
		{name: "q types into the filter", w: 56, keys: []string{"/", "q"}, check: func(t *testing.T, m Model) {
			if m.filter != "q" || !m.filtering {
				t.Fatalf("filter %q typing %v", m.filter, m.filtering)
			}
		}},
		{name: "filter keeps on enter and clears on esc", w: 56, keys: []string{"/", "m", "u", "l", "c", "h", "enter"}, check: func(t *testing.T, m Model) {
			if m.filter != "mulch" || m.filtering {
				t.Fatalf("filter %q typing %v", m.filter, m.filtering)
			}
			if tg := m.target(); tg == nil || tg.item.ID != "mm-4" {
				t.Fatalf("target %v", tg)
			}
			m, _ = m.Key("esc")
			if m.filter != "" {
				t.Fatalf("esc left filter %q", m.filter)
			}
		}},
		{name: "tab and shift+tab", w: 56, keys: []string{"tab", "tab", "tab", "shift+tab"}, check: func(t *testing.T, m Model) {
			if m.tab != TabAll {
				t.Fatalf("tab %d", m.tab)
			}
		}},
		{name: "help closes on any key", w: 56, keys: []string{"?", "x"}, check: func(t *testing.T, m Model) {
			if m.help {
				t.Fatal("help still open")
			}
		}},
		{name: "epic row is read-only for x", w: 140, setup: []string{"2"}, keys: []string{"x"},
			note: "Epic rows are read-only here. Change api-h3k in acme/api."},
		{name: "epic row is read-only for s", w: 56, setup: []string{"2"}, keys: []string{"s"}, note: "in acme/api"},
		{name: "epic row is read-only for u", w: 56, setup: []string{"2"}, keys: []string{"u"}, note: "in acme/api"},
		{name: "epic row has no link", w: 56, setup: []string{"2"}, keys: []string{"o"}, note: "api-h3k has no page in the app."},
		{name: "a child row in B is read-only too", w: 140, setup: []string{"2"}, sel: "laptop/acme/storefront/sf-c1x", keys: []string{"l", "j", "j", "x"},
			note: "Change sf-c1x.4 in acme/storefront."},
		{name: "a human epic row acts as its item", w: 140, setup: []string{"3"}, sel: "laptop/acme/storefront/sf-c1x", keys: []string{"enter", "x"},
			want: []Effect{Act{Action: api.NewAction{Kind: "done", Key: "laptop/acme/storefront/sf-c1x.5"}, Done: "Done: Set up the wallet-pay sandbox account"}}},
		{name: "a human epic row links to its item", w: 140, setup: []string{"2"}, sel: "laptop/acme/storefront/sf-c1x", keys: []string{"l", "o"},
			want: []Effect{OpenURL{URL: "https://meat.example.com/?item=laptop%2Facme%2Fstorefront%2Fsf-c1x.5"}}},
		{name: "enter in A opens and esc closes", w: 56, keys: []string{"enter"}, check: func(t *testing.T, m Model) {
			if !m.open {
				t.Fatal("no detail")
			}
			m, _ = m.Key("esc")
			if m.open {
				t.Fatal("esc left the detail open")
			}
		}},
		{name: "l and h fold the tree in A", w: 56, setup: []string{"2"}, keys: []string{"l", "j"}, check: func(t *testing.T, m Model) {
			if !strings.Contains(m.selKey, ">") {
				t.Fatalf("selection %q is not a child", m.selKey)
			}
			m, _ = m.Key("h")
			if m.selKey != "laptop/acme/api/api-h3k" || !m.expanded[m.selKey] {
				t.Fatalf("h on a child: %q expanded %v, want its card, still unfolded", m.selKey, m.expanded)
			}
			m, _ = m.Key("h")
			if m.expanded[m.selKey] {
				t.Fatalf("h on the card left it unfolded")
			}
		}},
		{name: "right walks the tree in A: unfold, first child, open", w: 56, setup: []string{"2"}, keys: []string{"right"}, check: func(t *testing.T, m Model) {
			if !m.expanded[m.selKey] {
				t.Fatalf("right did not unfold %q", m.selKey)
			}
			m, _ = m.Key("right")
			if !strings.HasPrefix(m.selKey, "laptop/acme/api/api-h3k>") {
				t.Fatalf("second right selected %q, want the card's first child", m.selKey)
			}
			m, _ = m.Key("right")
			if !m.open {
				t.Fatal("right on a child did not open it")
			}
			m, _ = m.Key("left")
			if m.open || !strings.HasPrefix(m.selKey, "laptop/acme/api/api-h3k>") {
				t.Fatalf("left: open %v selection %q", m.open, m.selKey)
			}
		}},
		{name: "right opens a to-do and left comes back", w: 140, keys: []string{"right"}, check: func(t *testing.T, m Model) {
			if !m.open || m.openKey != "hub/personal/mm-28" {
				t.Fatalf("right: open %v key %q", m.open, m.openKey)
			}
			m, _ = m.Key("left")
			if m.open || m.selKey != "hub/personal/mm-28" {
				t.Fatalf("left: open %v selection %q", m.open, m.selKey)
			}
		}},
		{name: "right drills into a card in All", w: 56, setup: []string{"3"}, sel: "laptop/acme/api/api-h3k", keys: []string{"right"}, check: func(t *testing.T, m Model) {
			if m.drill != "laptop/acme/api/api-h3k" {
				t.Fatalf("drill %q", m.drill)
			}
		}},
		{name: "right on a child in B opens it", w: 140, setup: []string{"2"}, sel: "laptop/acme/storefront/sf-c1x", keys: []string{"right", "j", "right"}, check: func(t *testing.T, m Model) {
			if !m.open || !m.right || m.rightIdx != 1 {
				t.Fatalf("open %v right %v idx %d", m.open, m.right, m.rightIdx)
			}
		}},
		{name: "a tab keeps its selection while another shows", w: 140, keys: []string{"j", "j", "2", "j", "1"}, check: func(t *testing.T, m Model) {
			if m.selIdx != 2 {
				t.Fatalf("back on To-dos at %d %q, want the third row", m.selIdx, m.selKey)
			}
			m, _ = m.Key("2")
			if m.selIdx != 1 {
				t.Fatalf("back on Epics at %d %q, want the second row", m.selIdx, m.selKey)
			}
		}},
		{name: "snooze menu: arrows pick, enter snoozes", w: 56, keys: []string{"s", "down", "down", "up", "enter"}, check: func(t *testing.T, m Model) {
			if m.snoozeKey != "" {
				t.Fatal("enter left the menu open")
			}
		}},
		{name: "drill and back in All", w: 56, setup: []string{"3"}, sel: "laptop/acme/api/api-h3k", keys: []string{"enter", "esc"}, check: func(t *testing.T, m Model) {
			if m.drill != "" || m.selKey != "laptop/acme/api/api-h3k" {
				t.Fatalf("drill %q selection %q", m.drill, m.selKey)
			}
		}},
		{name: "G and g", w: 56, keys: []string{"G"}, check: func(t *testing.T, m Model) {
			if m.selKey != "hub/personal/mm-30" {
				t.Fatalf("G selected %q", m.selKey)
			}
			m, _ = m.Key("g")
			if m.selKey != "hub/personal/mm-28" || m.scroll != 0 {
				t.Fatalf("g selected %q scroll %d", m.selKey, m.scroll)
			}
		}},
		{name: "u with nothing to undo", w: 56, keys: []string{"u"}, note: "Nothing to undo."},
		{name: "enter on a to-do in B opens it full screen", w: 140, keys: []string{"enter"}, check: func(t *testing.T, m Model) {
			if !m.open || m.openKey != "hub/personal/mm-28" {
				t.Fatalf("open %v key %q", m.open, m.openKey)
			}
			m, _ = m.Key("h")
			if m.open {
				t.Fatal("h left the detail open")
			}
		}},
		{name: "enter on a child in B opens the child, esc comes back to it", w: 140, setup: []string{"2"}, sel: "laptop/acme/storefront/sf-c1x",
			keys: []string{"l", "j", "enter"}, check: func(t *testing.T, m Model) {
				if !m.open || m.openKey != "laptop/acme/storefront/sf-c1x.3" {
					t.Fatalf("open %v key %q", m.open, m.openKey)
				}
				m, _ = m.Key("esc")
				if m.open || !m.right || m.rightIdx != 1 {
					t.Fatalf("esc: open %v right %v idx %d", m.open, m.right, m.rightIdx)
				}
			}},
		{name: "h leaves a drill in All", w: 56, setup: []string{"3"}, sel: "laptop/acme/api/api-h3k", keys: []string{"enter", "h"}, check: func(t *testing.T, m Model) {
			if m.drill != "" || m.selKey != "laptop/acme/api/api-h3k" {
				t.Fatalf("drill %q selection %q", m.drill, m.selKey)
			}
		}},
		{name: "x moves the selection off the done to-do", w: 56, keys: []string{"x"},
			want: []Effect{Act{Action: api.NewAction{Kind: "done", Key: "hub/personal/mm-28"}, Done: "Done: Book the boiler service"}},
			check: func(t *testing.T, m Model) {
				if m.selKey == "hub/personal/mm-28" {
					t.Fatal("selection stayed on the done to-do")
				}
			}},
		{name: "s 0 clears a snooze", w: 140, sel: "hub/personal/mm-50", keys: []string{"s", "0"},
			want: []Effect{Act{Action: api.NewAction{Kind: "snooze", Key: "hub/personal/mm-50",
				Payload: api.Payload{Previous: ptime(time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC))}}, Done: "Snooze cleared"}}},
		{name: "s 0 on a to-do with no snooze does nothing", w: 56, keys: []string{"s", "0"}, check: func(t *testing.T, m Model) {
			if m.snoozeKey == "" {
				t.Fatal("0 closed the menu without a snooze to clear")
			}
		}},
		{name: "ctrl+d moves half a page", w: 56, keys: []string{"ctrl+d"}, check: func(t *testing.T, m Model) {
			if m.selIdx == 0 {
				t.Fatal("ctrl+d did not move")
			}
			m, _ = m.Key("ctrl+u")
			if m.selIdx != 0 {
				t.Fatalf("ctrl+u left %d", m.selIdx)
			}
		}},
		{name: "arrows move while filtering; ctrl+w and ctrl+u edit", w: 56, keys: []string{"/", "o", "r", " ", "b", "down"}, check: func(t *testing.T, m Model) {
			if m.filter != "or b" || !m.filtering {
				t.Fatalf("filter %q typing %v", m.filter, m.filtering)
			}
			m, _ = m.Key("ctrl+w")
			if m.filter != "or " {
				t.Fatalf("ctrl+w left %q", m.filter)
			}
			m, _ = m.Key("ctrl+u")
			if m.filter != "" || !m.filtering {
				t.Fatalf("ctrl+u left %q typing %v", m.filter, m.filtering)
			}
		}},
		{name: "help scrolls on a short screen", w: 56, keys: []string{"?", "j", "j"}, check: func(t *testing.T, m Model) {
			if !m.help || m.helpScroll != 2 {
				t.Fatalf("help %v scroll %d", m.help, m.helpScroll)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := keys(t, model(t, c.w, 24), c.setup...)
			if c.sel != "" {
				m = selectKey(t, m, c.sel)
			}
			m, fx := keys(t, m, c.keys...)
			checkFrame(t, c.name, m)
			if c.want != nil || c.note != "" || c.check == nil {
				if !reflect.DeepEqual(fx, c.want) {
					t.Errorf("effects\n got %#v\nwant %#v", fx, c.want)
				}
			}
			if c.note != "" && !strings.Contains(m.notice, c.note) {
				t.Errorf("notice %q, want %q", m.notice, c.note)
			}
			if c.check != nil {
				c.check(t, m)
			}
		})
	}
}

func TestPasswordPrompt(t *testing.T) {
	m := model(t, 56, 20).AskPassword("Wrong password.")
	m, fx := keys(t, m, "q", "space", "x", "backspace", "1", "enter")
	if !reflect.DeepEqual(fx, []Effect{Login{Password: "q 1"}}) {
		t.Fatalf("effects %#v", fx)
	}
	if m.pw {
		t.Fatal("prompt still open after enter")
	}
	m, fx = m.AskPassword("Wrong password.").Key("esc")
	if !reflect.DeepEqual(fx, []Effect{Quit{}}) {
		t.Fatalf("esc gave %#v", fx)
	}
	if !strings.Contains(strings.Join(plain(m), "\n"), "Wrong password.") {
		t.Fatal("reason not shown")
	}
}

func TestUndo(t *testing.T) {
	m := selectKey(t, model(t, 56, 20), "hub/personal/mm-50")
	m, fx := keys(t, m, "s", "3")
	a, ok := act(fx)
	if !ok {
		t.Fatalf("no act: %#v", fx)
	}
	stored := api.Action{ID: "a1", Kind: "snooze", Bridge: "hub", Repo: "personal", BeadID: "mm-50", Payload: a.Action.Payload, Status: "applied"}
	m, fx = m.Acted(stored).Key("u")
	want := []Effect{Act{Action: api.NewAction{Kind: "snooze", Key: "hub/personal/mm-50",
		Payload: api.Payload{Until: a.Action.Payload.Previous, Previous: a.Action.Payload.Until}}, Done: "Snoozed to Wed, Nov 4"}}
	if !reflect.DeepEqual(fx, want) {
		t.Fatalf("undo of a snooze\n got %#v\nwant %#v", fx, want)
	}
	if _, fx = m.Key("u"); len(fx) != 0 {
		t.Fatalf("a second u gave %#v", fx)
	}

	// A snooze of an unsnoozed to-do undoes to no snooze.
	m = m.Acted(api.Action{Kind: "snooze", Bridge: "hub", Repo: "personal", BeadID: "mm-44", Payload: api.Payload{Until: ptime(now.Add(time.Hour))}})
	_, fx = m.Key("u")
	want = []Effect{Act{Action: api.NewAction{Kind: "snooze", Key: "hub/personal/mm-44", Payload: api.Payload{Previous: ptime(now.Add(time.Hour))}}, Done: "Snooze cleared"}}
	if !reflect.DeepEqual(fx, want) {
		t.Fatalf("undo of a first snooze\n got %#v\nwant %#v", fx, want)
	}

	m = m.Acted(api.Action{Kind: "done", Bridge: "hub", Repo: "personal", BeadID: "mm-28"})
	_, fx = m.Key("u")
	want = []Effect{Act{Action: api.NewAction{Kind: "reopen", Key: "hub/personal/mm-28"}, Done: "Reopened: Book the boiler service"}}
	if !reflect.DeepEqual(fx, want) {
		t.Fatalf("undo of a done\n got %#v\nwant %#v", fx, want)
	}

	due := time.Date(2026, 11, 2, 18, 0, 0, 0, time.UTC)
	m = m.Acted(api.Action{Kind: "done", Bridge: "hub", Repo: "personal", BeadID: "mm-31", Payload: api.Payload{Was: &api.Edit{Due: &due}}})
	m, fx = m.Key("u")
	if len(fx) != 0 || !strings.Contains(m.notice, "Undo it in the app") {
		t.Fatalf("undo of a repeating done gave %#v, notice %q", fx, m.notice)
	}

	// A dismiss reopens the to-do, then the sub-tasks it took.
	m = m.Acted(api.Action{Kind: "dismiss", Bridge: "hub", Repo: "personal", BeadID: "mm-46"},
		api.Action{Kind: "dismiss", Bridge: "hub", Repo: "personal", BeadID: "mm-47"})
	_, fx = m.Key("u")
	want = []Effect{Act{Action: api.NewAction{Kind: "reopen", Key: "hub/personal/mm-46"},
		After: []api.NewAction{{Kind: "reopen", Key: "hub/personal/mm-47"}}, Done: "Restored: Buy birthday card for Alex"}}
	if !reflect.DeepEqual(fx, want) {
		t.Fatalf("undo of a delete\n got %#v\nwant %#v", fx, want)
	}
	m = m.Acted(api.Action{Kind: "dismiss", Bridge: "laptop", Repo: "acme/api", BeadID: "api-9fq"})
	_, fx = m.Key("u")
	want = []Effect{Act{Action: api.NewAction{Kind: "reopen", Key: "laptop/acme/api/api-9fq"}, Done: "Reopened: Rotate the staging API key in the vault"}}
	if !reflect.DeepEqual(fx, want) {
		t.Fatalf("undo of a won't do\n got %#v\nwant %#v", fx, want)
	}

	// Other kinds leave the last done or snooze undoable.
	m = m.Acted(api.Action{Kind: "done", Bridge: "hub", Repo: "personal", BeadID: "mm-12"}).Acted(api.Action{Kind: "reopen", Bridge: "hub", Repo: "personal", BeadID: "mm-30"})
	if _, fx = m.Key("u"); len(fx) != 1 || fx[0].(Act).Action.Key != "hub/personal/mm-12" {
		t.Fatalf("undo after a reopen gave %#v", fx)
	}
}

func TestSetDataKeepsSelection(t *testing.T) {
	s, e := load(t)
	m := selectKey(t, model(t, 56, 20), "hub/personal/mm-44")
	idx := m.selIdx

	// The selected item moves down a row and stays selected.
	s.Items = append(s.Items, api.Item{Key: "hub/personal/mm-60", Bridge: "hub", Repo: "personal", ID: "mm-60", Title: "Feed the cat", Status: "open", DueAt: ptime(now.Add(-time.Hour))})
	m = m.SetData(s, e, now)
	if tg := m.target(); tg == nil || tg.key != "hub/personal/mm-44" || m.selIdx != idx+1 {
		t.Fatalf("selection moved to %+v at %d", tg, m.selIdx)
	}

	// With the item gone, the selection lands on the row in its place.
	var kept []api.Item
	for _, i := range s.Items {
		if i.ID != "mm-44" {
			kept = append(kept, i)
		}
	}
	s.Items = kept
	m = m.SetData(s, e, now)
	if tg := m.target(); tg == nil || tg.key != "hub/personal/mm-31" {
		t.Fatalf("selection after removal %+v", tg)
	}

	// An epic row keeps its place across a new epics set.
	m, _ = m.Key("2")
	m = selectKey(t, m, "laptop/acme/storefront/sf-q2e")
	e.Repos = e.Repos[1:]
	m = m.SetData(s, e, now)
	if m.selKey != "laptop/acme/storefront/sf-q2e" {
		t.Fatalf("epic selection %q", m.selKey)
	}
}

func TestResizeOpensDetail(t *testing.T) {
	m := selectKey(t, model(t, 100, 30), "laptop/acme/api/api-9fq")
	if m.layout() != "b" {
		t.Fatalf("layout %s at 100", m.layout())
	}
	m = m.Resize(80, 30)
	if !m.open || m.openKey != "laptop/acme/api/api-9fq" {
		t.Fatalf("no detail after the resize: open %v %q", m.open, m.openKey)
	}
	f := strings.Join(plain(m), "\n")
	if !strings.Contains(f, gBack+" Esc") || !strings.Contains(f, "The agent cannot reach the vault UI.") {
		t.Fatalf("frame is not the detail:\n%s", f)
	}
	// Back up to B the detail goes and the selection holds.
	m = m.Resize(100, 30)
	if m.open || m.selKey != "laptop/acme/api/api-9fq" {
		t.Fatalf("after widening: open %v selection %q", m.open, m.selKey)
	}

	// A child walked into on the right opens as the detail.
	m, _ = keys(t, m, "2")
	m = selectKey(t, m, "laptop/acme/storefront/sf-c1x")
	m, _ = keys(t, m, "l", "j")
	child := m.target().key
	m = m.Resize(80, 30)
	if !m.open || m.openKey != child {
		t.Fatalf("child %q not open: %v %q", child, m.open, m.openKey)
	}
	m, _ = m.Key("esc")
	if m.selKey != "laptop/acme/storefront/sf-c1x" {
		t.Fatalf("back in A the selection is %q", m.selKey)
	}

	// A selection inside A's tree becomes the card and its child in B.
	m, _ = keys(t, m, "l", "j", "j")
	m = m.Resize(120, 30)
	if !m.right || m.target().key != "laptop/acme/storefront/sf-c1x.3" {
		t.Fatalf("B right %v target %q", m.right, m.target().key)
	}
}

func TestNotices(t *testing.T) {
	m := model(t, 56, 20)
	m, first := m.Notice("Copied mm-28", NoticeInfo)
	m, second := m.Notice("Could not reach the hub", NoticeError)
	if m = m.ClearNotice(first); m.notice == "" {
		t.Fatal("an older seq cleared the newer notice")
	}
	if m = m.ClearNotice(second); m.notice != "" {
		t.Fatal("its own seq did not clear it")
	}
	m, _ = m.Notice("Copied mm-28", NoticeInfo)
	if m, _ = m.Key("j"); m.notice != "" {
		t.Fatal("a key did not clear the notice")
	}
}

func TestGroups(t *testing.T) {
	s, _ := load(t)
	var got []string
	for _, g := range buildGroups(s.Items, now, true) {
		var ids []string
		for _, i := range g.items {
			ids = append(ids, i.ID)
		}
		got = append(got, string(g.id)+": "+strings.Join(ids, " "))
	}
	want := []string{
		"pastdue: mm-28 api-9fq",
		"today: mm-44",
		"evening: mm-31",
		"week: mm-50 mm-46 mm-47 mm-6",
		"nodate: mm-12 mm-52 mm-4 mm-45",
		"later: mm-40",
		"done: mm-30",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups\n got %q\nwant %q", got, want)
	}
}

func TestSnoozeOptions(t *testing.T) {
	label := func(o []snoozeOption) []string {
		var out []string
		for _, x := range o {
			out = append(out, x.label+" "+x.until.Format("Mon Jan 2 15:04"))
		}
		return out
	}
	if got, want := label(snoozeOptions(now)), []string{"In 1 hour Mon Nov 2 11:00", "This evening Mon Nov 2 18:00", "Tomorrow Tue Nov 3 09:00",
		"This weekend Sat Nov 7 10:00", "Next week Mon Nov 9 09:00", "In a month Wed Dec 2 09:00"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("morning\n got %q\nwant %q", got, want)
	}
	late := time.Date(2026, 11, 7, 19, 30, 20, 0, time.UTC) // a Saturday evening
	if got, want := label(snoozeOptions(late)), []string{"In 1 hour Sat Nov 7 20:30", "Tomorrow Sun Nov 8 09:00",
		"This weekend Sat Nov 14 10:00", "Next week Mon Nov 9 09:00", "In a month Mon Dec 7 09:00"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("evening\n got %q\nwant %q", got, want)
	}
	// After 18:00 the menu has five, so 6 picks nothing.
	m := selectKey(t, model(t, 56, 20).SetNow(late), "hub/personal/mm-12")
	if _, fx := keys(t, m, "s", "6"); len(fx) != 0 {
		t.Fatalf("6 after 18:00 gave %#v", fx)
	}
}

// A paste goes into the password prompt or the filter, never runs as keys.
func TestPaste(t *testing.T) {
	m := model(t, 56, 20).AskPassword("The session ended.").Paste("correct horse\n")
	if _, fx := m.Key("enter"); !reflect.DeepEqual(fx, []Effect{Login{Password: "correct horse"}}) {
		t.Fatalf("paste into prompt gave %#v", fx)
	}
	m = model(t, 56, 20)
	before := strings.Join(plain(m), "\n")
	m = m.Paste("xxsu")
	if got := strings.Join(plain(m), "\n"); got != before {
		t.Fatal("a paste outside an input changed the screen")
	}
	m, _ = m.Key("/")
	m = m.Paste("plants")
	if m.filter != "plants" {
		t.Fatalf("filter = %q", m.filter)
	}
}

// TestOpenCardChildren walks into an open epic's tasks in a narrow pane,
// opens one, acts on it and backs out, as layout B's right pane does.
func TestOpenCardChildren(t *testing.T) {
	m, _ := keys(t, model(t, 56, 24), "2")
	m = selectKey(t, m, "laptop/acme/storefront/sf-c1x")
	m, _ = keys(t, m, "enter", "l")
	checkFrame(t, "into children", m)
	kids := m.openKidRows()
	if !m.openKids || len(kids) < 2 || m.target().id() != kids[0].ID {
		t.Fatalf("l: in children %v, target %v", m.openKids, m.target().id())
	}
	if m, _ = m.Key("j"); m.target().id() != kids[1].ID {
		t.Fatalf("j: target %v, want %v", m.target().id(), kids[1].ID)
	}
	for i := 0; i < 10 && m.target().id() != "sf-c1x.5"; i++ {
		m, _ = m.Key("k")
	}
	if m.target().id() != "sf-c1x.5" {
		t.Fatalf("j never reached sf-c1x.5: %s", m.target().id())
	}
	kid := m.openKid
	if !strings.Contains(strings.Join(plain(m), "\n"), "Enter open") {
		t.Fatal("hints do not offer Enter")
	}
	_, fx := m.Key("x")
	if a, ok := act(fx); !ok || a.Action.Key != "laptop/acme/storefront/sf-c1x.5" {
		t.Fatalf("x on the selected task gave %#v", fx)
	}
	m, _ = m.Key("enter")
	checkFrame(t, "child open", m)
	if !m.open || m.openKey != "laptop/acme/storefront/sf-c1x.5" || m.openKids {
		t.Fatalf("enter: open %v key %q kids %v", m.open, m.openKey, m.openKids)
	}
	m, _ = m.Key("esc")
	if !m.open || m.openKey != "laptop/acme/storefront/sf-c1x" || !m.openKids || m.openKid != kid {
		t.Fatalf("esc from the task: key %q kids %v kid %d", m.openKey, m.openKids, m.openKid)
	}
	m, _ = m.Key("h")
	if !m.open || m.openKids {
		t.Fatalf("h: open %v kids %v", m.open, m.openKids)
	}
	m, _ = m.Key("esc")
	if m.open {
		t.Fatal("esc left the detail open")
	}
}

func TestSnoozeArrowsMatchDigits(t *testing.T) {
	_, byArrows := keys(t, model(t, 56, 24), "s", "down", "down", "up", "enter")
	_, byDigit := keys(t, model(t, 56, 24), "s", "2")
	if len(byDigit) != 1 || !reflect.DeepEqual(byArrows, byDigit) {
		t.Fatalf("arrows gave %#v, 2 gave %#v", byArrows, byDigit)
	}
}
