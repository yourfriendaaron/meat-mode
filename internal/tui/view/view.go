// Package view is the terminal viewer's screen, as a pure package: the
// model, what each key does and the frame it draws. It does no I/O and
// imports no terminal library beyond Lip Gloss for styles, so every layout
// is tested as text. The design is the epic's (layouts A, B and C, the
// 90-column switch, keys, copy text); the prototype it ports is tag
// prototype/tui-viewer.
package view

import (
	"time"

	"charm.land/lipgloss/v2"

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
//     web/src/styles.css) and first letter. Dark Dracula palette only.
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
// "?", "y", "Y", "x", "u", "s", "o", "r", "q", "ctrl+c", "1" to "6". The
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
	// Lane 2 adds the rest: selection by item key or epic row, scroll,
	// open detail, drill, B focus, filter, help, snooze menu, password
	// prompt, notice, the last undoable action.
}

// New is the model before any data, for the hub at hubURL (used for the
// app links that o shows).
func New(hubURL string) Model { return Model{hub: hubURL} }

// SetData replaces the state and epics and the time the groups are worked
// out from (its Location is the user's). The selection stays on the same
// item key or epic row when it still exists, else on the nearest row.
func (m Model) SetData(s api.State, e api.Epics, now time.Time) Model { return m }

// SetNow moves the clock without new data (due labels, groups).
func (m Model) SetNow(now time.Time) Model { return m }

// Resize sets the pane size. Crossing below SplitAt with a row selected in
// layout B opens that row's detail in layout A.
func (m Model) Resize(width, height int) Model { return m }

// SetConn sets the link state.
func (m Model) SetConn(c Conn) Model { return m }

// Notice shows a message in the status line until the next key or 3 s,
// whichever comes first. It returns the notice's number; the program's 3 s
// tick calls ClearNotice with it, which clears only that notice.
func (m Model) Notice(text string, kind NoticeKind) (Model, int) { return m, 0 }

// ClearNotice removes the status line's message if it is still notice seq.
func (m Model) ClearNotice(seq int) Model { return m }

// Acted records the stored action a successful Act returned, for undo.
func (m Model) Acted(a api.Action) Model { return m }

// AskPassword opens the in-place password prompt with a reason line
// ("The session ended. Enter the hub's password."). Typed keys go to it,
// masked; Enter gives a Login effect, Esc gives Quit. The program calls it
// again with "Wrong password." or the wait of a 429.
func (m Model) AskPassword(reason string) Model { return m }

// Key applies one key, named as Bubble Tea's KeyPressMsg.String() names it
// ("j", "enter", "esc", "tab", "shift+tab", "pgdown", "ctrl+c").
func (m Model) Key(k string) (Model, []Effect) { return m, nil }

// Frame draws the model: exactly height lines, each exactly width cells
// wide (ansi.StringWidth), styled with Lip Gloss in the app's palette.
func Frame(m Model) []string { return nil }

// keep Lip Gloss in the module graph until lane 2 styles the frame
var _ = lipgloss.NewStyle
