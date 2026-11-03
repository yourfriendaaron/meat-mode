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
		case "up", "ctrl+p":
			m.move(-1)
		case "down", "ctrl+n":
			m.move(1)
		case "ctrl+u":
			m.filter = ""
			m.resetSel()
		case "ctrl+w":
			m.filter = dropWord(m.filter)
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
		switch k {
		case "j", "down":
			m.helpScroll = min(m.helpScroll+1, m.helpMax())
		case "k", "up":
			m.helpScroll = max(0, m.helpScroll-1)
		case "pgdown", "space", "ctrl+f":
			m.helpScroll = min(m.helpScroll+max(1, m.bodyHeight()-1), m.helpMax())
		case "pgup", "ctrl+b":
			m.helpScroll = max(0, m.helpScroll-max(1, m.bodyHeight()-1))
		default:
			m.help, m.helpScroll = false, 0
		}
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
	case "pgdown", "space", "ctrl+f":
		m.move(m.page())
	case "pgup", "ctrl+b":
		m.move(-m.page())
	case "ctrl+d":
		m.move(max(1, m.page()/2))
	case "ctrl+u":
		m.move(-max(1, m.page()/2))
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
		// Forward: into what holds children, else as Enter.
		switch {
		case m.open && !m.openKids && len(m.openKidRows()) > 0:
			m.openKids, m.openKid = true, 0
			m.fixOpenKid()
		case lay == "b" && m.tab == TabEpics && !m.open && !m.right && m.rightKids() > 0:
			m.right, m.rightIdx = true, 0
		case lay == "a" && m.tab == TabEpics && !m.open && t != nil && t.card && !strings.Contains(m.selKey, ">"):
			switch {
			case m.nextIsChild():
				m.move(1)
			case !m.expanded[m.selKey]:
				m.setExpanded(m.selKey, true)
			default:
				m.enter(t, lay)
			}
		default:
			m.enter(t, lay)
		}
	case "h", "left":
		switch {
		case m.open && m.openKids:
			m.openKids = false
		case m.open:
			m.closeOpen()
		case m.right:
			m.right = false
		case m.drill != "":
			m.selKey, m.drill = m.drill, ""
		case lay == "a" && m.tab == TabEpics && t != nil:
			// A child goes up to its card and leaves it unfolded; the
			// card's own h folds it.
			if parent, _, ok := strings.Cut(m.selKey, ">"); ok {
				m.selKey = parent
			} else if t.card {
				m.setExpanded(m.selKey, false)
			}
		}
	case "enter":
		m.enter(t, lay)
	case "esc", "backspace":
		switch {
		case m.open && m.openKids:
			m.openKids = false
		case m.open:
			m.closeOpen()
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
		fx := m.done(t)
		if len(fx) > 0 && t.item != nil && isOpen(t.item) {
			m.stepPast(t.key)
		}
		return fx
	case "d":
		fx := m.dismiss(t)
		if len(fx) > 0 {
			m.stepPast(t.key)
		}
		return fx
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
	case "p":
		m.toggleScope()
	case "r":
		return []Effect{Refresh{}}
	}
	return nil
}

// enter opens the target: a child selected in an open card's detail, a
// card on the All page as a drill, anything else as its detail.
func (m *Model) enter(t *target, lay Layout) {
	switch {
	case t != nil && m.open && m.openKids:
		m.openKidChild(t)
	case t == nil || m.open:
	case lay == "c" && m.drill == "" && t.card && t.row != nil:
		m.drill = t.key
		m.resetSel()
	default:
		m.openTarget(t)
	}
}

// nextIsChild says the row after the selection is a child of the selected
// card in layout A's tree.
func (m *Model) nextIsChild() bool {
	rows := m.rows()
	sel := selectable(rows)
	return m.selIdx+1 < len(sel) && strings.HasPrefix(rows[sel[m.selIdx+1]].id, m.selKey+">")
}

// toggleScope switches between the project the viewer was started in and
// every list.
func (m *Model) toggleScope() {
	if len(m.here) == 0 {
		m.say("This folder is in no project the hub knows.", NoticeWarn)
		return
	}
	m.every = !m.every
	m.reindex(m.now)
	m.open, m.drill, m.right = false, "", false
	m.resetSel()
	if m.every {
		m.say("Showing every list.", NoticeInfo)
	} else {
		m.say("Showing "+clean(m.scopeName())+" only.", NoticeInfo)
	}
}

// stepPast moves the list's selection off key, to the next row or else
// the one before, so a done to-do that moves to "Done today" does not take
// the selection with it.
func (m *Model) stepPast(key string) {
	if m.open || m.right {
		return
	}
	rows := m.rows()
	sel := selectable(rows)
	if len(sel) < 2 || m.selIdx >= len(sel) || rows[sel[m.selIdx]].id != key {
		return
	}
	if m.selIdx+1 < len(sel) {
		m.setSel(m.selIdx + 1)
	} else {
		m.setSel(m.selIdx - 1)
	}
}

// dropWord removes the last word of s and the spaces after it.
func dropWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndex(s, " "); i >= 0 {
		return s[:i+1]
	}
	return ""
}

// page is the rows a page key moves.
func (m *Model) page() int { return max(1, m.listHeight()-1) }

// move moves the selection, the right pane's selection, or an open
// detail's scroll by delta.
func (m *Model) move(delta int) {
	switch {
	case m.open && m.openKids:
		m.openKid += delta
		m.fixOpenKid()
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
	m.started = true
	if t == m.tab && !m.open && m.drill == "" {
		return
	}
	m.tabSel[m.tab] = m.selKey
	m.tab, m.open, m.drill = t, false, ""
	m.resetSel()
	m.selKey = m.tabSel[t] // fix finds it again, or the nearest row
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

// dismiss is the app's Won't do on an agent's task and Delete on a personal
// to-do. Its open sub-tasks are dismissed first, as the app does.
func (m *Model) dismiss(t *target) []Effect {
	if t == nil {
		return nil
	}
	if t.readOnly() {
		m.readOnlyNote(t)
		return nil
	}
	it := t.item
	verb := "Won't do"
	if isPersonal(it.Bridge) {
		verb = "Deleted"
	}
	switch {
	case !isOpen(it):
		m.say("Only open to-dos can be dismissed. x reopens a closed one.", NoticeWarn)
		return nil
	case !repoCan(m.d.repoOf(it.Bridge, it.Repo), api.KindDismiss):
		if isPersonal(it.Bridge) {
			m.notOffered(it, "delete it")
		} else {
			m.notOffered(it, "mark it won't do")
		}
		return nil
	}
	subs := openSubtasks(it, m.d.state.Items)
	var before []api.NewAction
	for _, s := range subs {
		before = append(before, api.NewAction{Kind: api.KindDismiss, Key: s.Key})
	}
	done := verb + ": " + clean(it.Title)
	switch len(subs) {
	case 0:
	case 1:
		done += " and its sub-task"
	default:
		done += " and " + itoa(len(subs)) + " sub-tasks"
	}
	return []Effect{Act{Before: before, Action: api.NewAction{Kind: api.KindDismiss, Key: it.Key}, Done: done}}
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
		m.snoozeKey, m.snoozeIdx = t.item.Key, 0
	}
}

func (m *Model) snoozeMenuKey(k string) []Effect {
	switch k {
	case "esc", "backspace", "s", "q":
		m.snoozeKey = ""
		return nil
	}
	opts := snoozeOptions(m.now)
	switch k {
	case "up", "k":
		m.snoozeIdx = max(0, m.snoozeIdx-1)
		return nil
	case "down", "j":
		m.snoozeIdx = min(len(opts)-1, m.snoozeIdx+1)
		return nil
	case "enter":
		k = string(rune('1' + min(max(m.snoozeIdx, 0), len(opts)-1)))
	}
	it := m.d.items[m.snoozeKey]
	if k == "0" && it != nil && isSnoozed(it, m.now) {
		m.snoozeKey = ""
		prev := *it.DeferUntil
		return []Effect{Act{Action: api.NewAction{Kind: api.KindSnooze, Key: it.Key, Payload: api.Payload{Previous: &prev}}, Done: "Snooze cleared"}}
	}
	if len(k) != 1 || k[0] < '1' || k[0] > '6' {
		return nil
	}
	n := int(k[0] - '1')
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

// undo reverses the last done, dismiss or snooze: a done or a dismiss by a
// reopen (a dismiss's sub-tasks too), a snooze by a snooze back. A done that moved a repeating to-do on is left to the app,
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
	switch u.kind {
	case api.KindDone:
		return []Effect{Act{Action: api.NewAction{Kind: api.KindReopen, Key: u.key}, Done: "Reopened: " + title}}
	case api.KindDismiss:
		var after []api.NewAction
		for _, k := range u.also {
			after = append(after, api.NewAction{Kind: api.KindReopen, Key: k})
		}
		verb := "Reopened: "
		if bridge, _, _ := strings.Cut(u.key, "/"); isPersonal(bridge) {
			verb = "Restored: "
		}
		return []Effect{Act{Action: api.NewAction{Kind: api.KindReopen, Key: u.key}, After: after, Done: verb + title}}
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
