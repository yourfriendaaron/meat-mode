package view

import (
	"strings"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// typed is the text a key types into the filter or the password prompt,
// or "" for a key that types nothing.
func typed(k string) string {
	if k == "space" {
		return " "
	}
	if utf8.RuneCountInString(k) == 1 {
		if r, _ := utf8.DecodeRuneInString(k); r >= ' ' && r != 0x7f {
			return k
		}
	}
	return ""
}

func dropLast(s string) string {
	if s == "" {
		return s
	}
	_, n := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-n]
}

func (m Model) passwordKey(k string) (Model, []Effect) {
	switch k {
	case "esc":
		return m, []Effect{Quit{}}
	case "enter":
		pw := m.pwText
		m.pw, m.pwText = false, ""
		return m, []Effect{Login{Password: pw}}
	case "backspace":
		m.pwText = dropLast(m.pwText)
	default:
		m.pwText += typed(k)
	}
	return m, nil
}

// key applies one key outside the password prompt.
func (m *Model) key(k string) []Effect {
	if m.filtering {
		switch k {
		case "esc":
			m.filter, m.filtering = "", false
			m.resetSel()
		case "enter":
			m.filtering = false
		case "backspace":
			m.filter = dropLast(m.filter)
			m.resetSel()
		default:
			if s := typed(k); s != "" {
				m.filter += s
				m.resetSel()
			}
		}
		return nil
	}
	if m.help {
		m.help = false
		return nil
	}
	if m.snoozeKey != "" {
		return m.snoozeMenuKey(k)
	}
	if m.d == nil {
		switch k {
		case "q":
			return []Effect{Quit{}}
		case "r":
			return []Effect{Refresh{}}
		case "?":
			m.help = true
		}
		return nil
	}
	t := m.target()
	lay := m.layout()
	switch k {
	case "q":
		return []Effect{Quit{}}
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "pgdown", "space":
		m.move(m.page())
	case "pgup":
		m.move(-m.page())
	case "g", "home":
		m.move(-1 << 30)
	case "G", "end":
		m.move(1 << 30)
	case "tab":
		m.tabTo((m.tab + 1) % 3)
	case "shift+tab":
		m.tabTo((m.tab + 2) % 3)
	case "1", "2", "3":
		m.tabTo(Tab(k[0] - '1'))
	case "l", "right":
		switch {
		case m.open:
		case lay == "b" && m.tab == TabEpics && !m.right && m.rightKids() > 0:
			m.right, m.rightIdx = true, 0
		case lay == "a" && m.tab == TabEpics && t != nil && t.card && !strings.Contains(m.selKey, ">"):
			m.setExpanded(m.selKey, true)
		}
	case "h", "left":
		switch {
		case m.open:
		case m.right:
			m.right = false
		case lay == "a" && m.tab == TabEpics && t != nil:
			if parent, _, ok := strings.Cut(m.selKey, ">"); ok {
				m.selKey = parent
				m.setExpanded(parent, false)
			} else if t.card {
				m.setExpanded(m.selKey, false)
			}
		}
	case "enter":
		switch {
		case t == nil || m.open:
		case lay == "b":
			if m.tab == TabEpics && !m.right && m.rightKids() > 0 {
				m.right, m.rightIdx = true, 0
			}
		case lay == "c" && m.drill == "" && t.card && t.row != nil:
			m.drill = t.key
			m.resetSel()
		default:
			m.openTarget(t)
		}
	case "esc", "backspace":
		switch {
		case m.open:
			m.open = false
		case m.right:
			m.right = false
		case m.drill != "":
			m.selKey, m.drill = m.drill, ""
		case m.filter != "":
			m.filter = ""
			m.resetSel()
		}
	case "/":
		if !m.open {
			m.filtering = true
		}
	case "?":
		m.help = true
	case "y":
		if t != nil {
			return []Effect{Copy{Text: m.d.refLine(t), Ref: t.id()}}
		}
	case "Y":
		if t != nil {
			return []Effect{Copy{Text: m.d.refFull(t), Ref: t.id()}}
		}
	case "x":
		return m.done(t)
	case "s":
		m.openSnooze(t)
	case "u":
		return m.undo(t)
	case "o":
		switch {
		case t == nil:
		case t.readOnly():
			m.say(clean(t.id())+" has no page in the app.", NoticeWarn)
		default:
			return []Effect{OpenURL{URL: strings.TrimRight(m.hub, "/") + "/?item=" + encodeURIComponent(t.item.Key)}}
		}
	case "r":
		return []Effect{Refresh{}}
	}
	return nil
}

// page is the rows a page key moves.
func (m *Model) page() int { return max(1, m.listHeight()-1) }

// move moves the selection, the right pane's selection, or an open
// detail's scroll by delta.
func (m *Model) move(delta int) {
	switch {
	case m.open:
		m.detailScroll = max(0, min(m.detailScroll+delta, m.detailMax()))
	case m.right:
		m.rightIdx = min(max(m.rightIdx+delta, 0), max(0, m.rightKids()-1))
	default:
		sel := selectable(m.rows())
		if len(sel) == 0 {
			return
		}
		m.setSel(min(max(m.selIdx+delta, 0), len(sel)-1))
	}
}

func (m *Model) tabTo(t Tab) {
	if t == m.tab && !m.open && m.drill == "" {
		return
	}
	m.tab, m.open, m.drill = t, false, ""
	m.resetSel()
}

// readOnlyNote says an epic row cannot change here, naming its repo.
func (m *Model) readOnlyNote(t *target) {
	m.say("Epic rows are read-only here. Change "+clean(t.id())+" in "+clean(t.repo.Repo)+".", NoticeWarn)
}

// notOffered says a list's tracker cannot apply what was asked.
func (m *Model) notOffered(it *api.Item, what string) {
	m.say(m.d.listName(it.Bridge, it.Repo)+" cannot "+what+" from here.", NoticeWarn)
}

// done marks an open to-do done, or reopens a closed one.
func (m *Model) done(t *target) []Effect {
	if t == nil {
		return nil
	}
	if t.readOnly() {
		m.readOnlyNote(t)
		return nil
	}
	it := t.item
	repo := m.d.repoOf(it.Bridge, it.Repo)
	if !isOpen(it) {
		if !repoCan(repo, api.KindReopen) {
			m.notOffered(it, "reopen")
			return nil
		}
		return []Effect{Act{Action: api.NewAction{Kind: api.KindReopen, Key: it.Key}, Done: "Reopened: " + clean(it.Title)}}
	}
	if !repoCan(repo, api.KindDone) {
		m.notOffered(it, "mark it done")
		return nil
	}
	if n := len(openSubtasks(it, m.d.state.Items)); n > 0 {
		if n == 1 {
			m.say("Finish or remove its open sub-task first.", NoticeWarn)
		} else {
			m.say("Finish or remove its "+itoa(n)+" open sub-tasks first.", NoticeWarn)
		}
		return nil
	}
	return []Effect{Act{Action: api.NewAction{Kind: api.KindDone, Key: it.Key}, Done: "Done: " + clean(it.Title)}}
}

// openSnooze opens the snooze menu for an open to-do.
func (m *Model) openSnooze(t *target) {
	switch {
	case t == nil:
	case t.readOnly():
		m.readOnlyNote(t)
	case !isOpen(t.item):
		m.say("Only open to-dos snooze.", NoticeWarn)
	case !repoCan(m.d.repoOf(t.item.Bridge, t.item.Repo), api.KindSnooze):
		m.notOffered(t.item, "snooze")
	default:
		m.snoozeKey = t.item.Key
	}
}

func (m *Model) snoozeMenuKey(k string) []Effect {
	switch k {
	case "esc", "backspace", "s", "q":
		m.snoozeKey = ""
		return nil
	}
	if len(k) != 1 || k[0] < '1' || k[0] > '6' {
		return nil
	}
	opts := snoozeOptions(m.now)
	n := int(k[0] - '1')
	it := m.d.items[m.snoozeKey]
	if n >= len(opts) || it == nil {
		return nil
	}
	m.snoozeKey = ""
	until := opts[n].until
	p := api.Payload{Until: &until}
	if it.DeferUntil != nil {
		prev := *it.DeferUntil
		p.Previous = &prev
	}
	return []Effect{Act{Action: api.NewAction{Kind: api.KindSnooze, Key: it.Key, Payload: p}, Done: "Snoozed to " + fmtWhen(until, m.now)}}
}

// undo reverses the last done or snooze: a done by a reopen, a snooze by a
// snooze back. A done that moved a repeating to-do on is left to the app,
// whose undo restores its date and sub-tasks.
func (m *Model) undo(t *target) []Effect {
	u := m.last
	switch {
	case u == nil && t != nil && t.readOnly():
		m.readOnlyNote(t)
		return nil
	case u == nil:
		m.say("Nothing to undo.", NoticeInfo)
		return nil
	case u.movedOn:
		m.say(clean(u.id)+" moved on to its next date. Undo it in the app.", NoticeWarn)
		return nil
	}
	m.last = nil
	title := u.id
	if it := m.d.items[u.key]; it != nil {
		title = clean(it.Title)
	}
	if u.kind == api.KindDone {
		return []Effect{Act{Action: api.NewAction{Kind: api.KindReopen, Key: u.key}, Done: "Reopened: " + title}}
	}
	done := "Snooze cleared"
	if u.previous != nil {
		done = "Snoozed to " + fmtWhen(u.previous.In(m.now.Location()), m.now)
	}
	return []Effect{Act{Action: api.NewAction{Kind: api.KindSnooze, Key: u.key, Payload: api.Payload{Until: u.previous, Previous: u.until}}, Done: done}}
}

// encodeURIComponent escapes s as JavaScript's encodeURIComponent does, as
// the app's links are built.
func encodeURIComponent(s string) string {
	const keep = "-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%" + strings.ToUpper(string("0123456789abcdef"[c>>4])+string("0123456789abcdef"[c&15])))
	}
	return b.String()
}
