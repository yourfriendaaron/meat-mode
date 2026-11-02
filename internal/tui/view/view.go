// Package view is the terminal viewer's screen, as a pure package: the
// model, what each key does and the frame it draws. It does no I/O and
// imports no terminal library beyond Lip Gloss for styles, so every layout
// is tested as text. The design is the epic's (layouts A, B and C, the
// 90-column switch, keys, copy text); the prototype it ports is tag
// prototype/tui-viewer.
package view

import (
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// SplitAt is the width from which To-dos and Epics draw as layout B.
const SplitAt = 90

// Tab is one of the viewer's three tabs.
type Tab int

const (
	TabTodos Tab = iota // 1 To-dos
	TabEpics            // 2 Epics
	TabAll              // 3 All
)

// Layout is how a tab draws: "a" one list with a full-screen detail, "b" a
// list with the detail beside it, "c" the All page.
type Layout string

// LayoutFor is the layout a tab draws in at width.
func LayoutFor(t Tab, width int) Layout {
	switch {
	case t == TabAll:
		return "c"
	case width < SplitAt:
		return "a"
	default:
		return "b"
	}
}

// Conn is the state of the link to the hub, shown at the top right.
type Conn int

const (
	ConnConnecting Conn = iota
	ConnLive
	ConnDown // retrying; the Notice carries why
)

// NoticeKind colours the status line's message.
type NoticeKind int

const (
	NoticeInfo  NoticeKind = iota // copied, sent, done, snoozed
	NoticeWarn                    // read-only row, too long to send
	NoticeError                   // a failed action or fetch
)

// Effect is something the program must do after a key.
type Effect interface{ effect() }

// Copy asks for Text to go to the clipboard. Ref is the id shown in the
// message afterwards ("Copied mm-12", "Sent mm-12 to the terminal's clipboard").
type Copy struct{ Text, Ref string }

// Act asks for one app action (POST /api/actions); ID is left empty for the
// client to fill. Done is the message to show once it succeeds; the program
// then hands the stored action back with Acted and refetches the state (and
// the epics when the target came from an epic row).
type Act struct {
	Action api.NewAction
	Done   string
}

// OpenURL asks for the item's page in the app to be shown (printed in the
// message line; a terminal may not have a browser).
type OpenURL struct{ URL string }

// Login carries the password typed into the in-place password prompt.
type Login struct{ Password string }

// Refresh asks for the state and epics again.
type Refresh struct{}

// Quit ends the program.
type Quit struct{}

func (Copy) effect()    {}
func (Act) effect()     {}
func (OpenURL) effect() {}
func (Login) effect()   {}
func (Refresh) effect() {}
func (Quit) effect()    {}

// Identity, colour, links and time:
//   - An item is known by api.Item.Key. An epic row by "<bridge>/<repo>/<id>",
//     the same form. A row with human set whose key is an item in the state
//     acts as that item; otherwise it is read-only (x, s and u say so and
//     name its repo).
//   - A list's mark: the personal list (api.Repo.Personal) its own mark;
//     others the repo's Colour slot (0 to 5, --role-repo-0..5 of
//     web/src/styles.css) and a code of up to three letters (listCodes).
//     Dark Dracula palette only.
//   - o links to <hub>/?item=<url-escaped key>, as web/src/sw.ts does; a
//     read-only epic row has no link.
//   - Groups use now's Location; the program passes time.Now() (local time,
//     as the app on the same machine) and calls SetNow each minute.
//
// Undo (u) works on the last done or snooze this session made: a snooze is
// undone by a snooze with until and previous swapped (web/src/changes.ts);
// a done by a reopen, except a done that moved a repeating to-do to its next
// date, which says "Undo it in the app".
//
// Keys are Bubble Tea v2's KeyPressMsg.String() names: "j", "k", "up",
// "down", "left", "right", "h", "l", "g", "G", "home", "end", "pgup",
// "pgdown", "enter", "esc", "backspace", "tab", "shift+tab", "space", "/",
// "?", "y", "Y", "x", "u", "s", "o", "p", "r", "q", "ctrl+c", "1" to "6". The
// snooze menu takes "1" to "6" in snoozeOptions order (one is missing after
// 18:00, as in the app).
//
// Model is the whole screen state. Methods return a changed copy.
type Model struct {
	hub    string
	state  api.State
	epics  api.Epics
	now    time.Time
	width  int
	height int
	tab    Tab
	conn   Conn

	hereAt Here            // where the viewer was started
	here   map[string]bool // the lists hereAt is in, by listKey; nil for none
	every  bool            // p: show every list, not just here's

	d      *data // state and epics, indexed; nil before SetData
	selKey string
	selIdx int // among the selectable rows
	scroll int // first row drawn

	open         bool // a full-screen detail (layouts A and C)
	openKey      string
	openRow      bool // openKey is an epic row's
	detailScroll int

	drill    string          // All: the card drilled into
	expanded map[string]bool // Epics in layout A: unfolded cards, by row id; shared between copies, so copied before a change
	right    bool            // Epics in layout B: the keys move in the selected card's children
	rightIdx int

	filter    string
	filtering bool
	help      bool
	snoozeKey string // the item the snooze menu is open for

	pw       bool // the password prompt
	pwReason string
	pwText   string

	notice     string
	noticeKind NoticeKind
	noticeSeq  int
	seq        int

	last *undoable
}

// New is the model before any data, for the hub at hubURL (used for the
// app links that o shows).
func New(hubURL string) Model { return Model{hub: hubURL} }

// SetHere sets where the viewer was started. While that folder is in a
// project the hub knows, the viewer shows that project's lists alone
// until p switches to every list.
func (m Model) SetHere(h Here) Model {
	m.hereAt = h
	if m.d != nil {
		m.reindex(m.now)
		m.fix()
	}
	return m
}

// reindex indexes the state and epics, cut to here's lists unless every
// list is shown.
func (m *Model) reindex(now time.Time) {
	m.here = hereLists(m.state.Repos, m.hereAt)
	s, e := m.state, m.epics
	if m.inScope() {
		s, e = scoped(s, e, m.here)
	}
	m.d = index(s, e, now)
}

// inScope reports whether the viewer shows here's lists alone.
func (m *Model) inScope() bool { return len(m.here) > 0 && !m.every }

// SetData replaces the state and epics and the time the groups are worked
// out from (its Location is the user's). The selection stays on the same
// item key or epic row when it still exists, else on the nearest row.
func (m Model) SetData(s api.State, e api.Epics, now time.Time) Model {
	m.state, m.epics, m.now = s, e, now
	m.reindex(now)
	m.fix()
	return m
}

// SetNow moves the clock without new data (due labels, groups).
func (m Model) SetNow(now time.Time) Model {
	m.now = now
	if m.d != nil {
		m.reindex(now)
	}
	m.fix()
	return m
}

// Resize sets the pane size. Crossing below SplitAt with a row selected in
// layout B opens that row's detail in layout A.
func (m Model) Resize(width, height int) Model {
	was := LayoutFor(m.tab, m.width)
	var t *target
	if was == "b" && m.width > 0 {
		t = m.target()
	}
	m.width, m.height = max(width, 0), max(height, 0)
	now := LayoutFor(m.tab, m.width)
	switch {
	case was == "b" && now == "a":
		if t != nil {
			m.openTarget(t)
		}
		m.right = false
	case was == "a" && now == "b":
		m.open = false
		m.toSplit()
	}
	m.fix()
	return m
}

// SetConn sets the link state.
func (m Model) SetConn(c Conn) Model {
	m.conn = c
	return m
}

// Notice shows a message in the status line until the next key or 3 s,
// whichever comes first. It returns the notice's number; the program's 3 s
// tick calls ClearNotice with it, which clears only that notice.
func (m Model) Notice(text string, kind NoticeKind) (Model, int) {
	m.say(text, kind)
	return m, m.noticeSeq
}

// ClearNotice removes the status line's message if it is still notice seq.
func (m Model) ClearNotice(seq int) Model {
	if m.noticeSeq == seq {
		m.notice = ""
	}
	return m
}

// Acted records the stored action a successful Act returned, for undo.
func (m Model) Acted(a api.Action) Model {
	key := a.Bridge + "/" + a.Repo + "/" + a.BeadID
	switch a.Kind {
	case api.KindDone:
		m.last = &undoable{kind: api.KindDone, key: key, id: a.BeadID, movedOn: a.Payload.Was != nil}
	case api.KindSnooze:
		m.last = &undoable{kind: api.KindSnooze, key: key, id: a.BeadID, until: a.Payload.Until, previous: a.Payload.Previous}
	}
	return m
}

// AskPassword opens the in-place password prompt with a reason line
// ("The session ended. Enter the hub's password."). Typed keys go to it,
// masked; Enter gives a Login effect, Esc gives Quit. The program calls it
// again with "Wrong password." or the wait of a 429.
func (m Model) AskPassword(reason string) Model {
	m.pw, m.pwReason, m.pwText = true, reason, ""
	m.help, m.snoozeKey, m.filtering = false, "", false
	return m
}

// Paste puts pasted text into the password prompt or the filter, whichever
// is open, without line breaks. Anywhere else it does nothing, so pasted
// text never runs as keys.
func (m Model) Paste(text string) Model {
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r < ' ' {
			return -1
		}
		return r
	}, text)
	switch {
	case m.pw:
		m.pwText += text
	case m.filtering:
		m.filter += text
		m.resetSel()
		m.fix()
	}
	return m
}

// Key applies one key, named as Bubble Tea's KeyPressMsg.String() names it
// ("j", "enter", "esc", "tab", "shift+tab", "pgdown", "ctrl+c").
func (m Model) Key(k string) (Model, []Effect) {
	if k == "ctrl+c" {
		return m, []Effect{Quit{}}
	}
	if m.pw {
		return m.passwordKey(k)
	}
	m.notice = ""
	fx := m.key(k)
	m.fix()
	return m, fx
}

// Frame draws the model: exactly height lines, each exactly width cells
// wide (ansi.StringWidth), styled with Lip Gloss in the app's palette.
func Frame(m Model) []string {
	lines := m.frame()
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.render()
	}
	return out
}
