package view

import (
	"strings"
)

// hint is one key and what it does, for the status line.
type hint struct{ key, does string }

// frame is the whole screen as lines, each exactly width cells.
func (m *Model) frame() []line {
	w, h := m.width, m.height
	if h <= 0 {
		return nil
	}
	top := []line{m.topBar(w)}
	if m.filtering || m.filter != "" {
		top = append(top, m.filterLine(w))
	}
	body, more := m.body(w, m.bodyHeight())
	out := append(top, body...)
	for len(out) < h-1 {
		out = append(out, fit(nil, w, sPlain))
	}
	out = append(out[:min(len(out), h-1)], m.statusLine(w, more))
	if len(out) > h {
		out = out[:h]
	}
	for i := range out {
		out[i] = fit(out[i], w, sPlain)
	}
	switch {
	case m.pw:
		m.overlay(out, m.passwordPanel(w))
	case m.snoozeKey != "":
		m.overlay(out, m.snoozePanel(w))
	}
	return out
}

// topBar is the counts of both sections, the section the selection is in
// lit, then the project and the link on the right.
func (m *Model) topBar(w int) line {
	open, cards := 0, 0
	epics := false
	if m.d != nil {
		open, cards = openToday(m.d.state.Items, m.d.now), m.d.cardCount()
		epics = m.d.hasEpics()
	}
	part := func(on bool, label string) seg {
		if on {
			return sg(label, sTab)
		}
		return sg(label, sSoft)
	}
	here := inEpics(m.selKey)
	left := line{sg(" ", sPlain), part(!here, " To-dos "+itoa(open)+" ")}
	if epics {
		left = append(left, sg(" ", sPlain), part(here, " Epics "+itoa(cards)+" "))
	}
	var right line
	if m.inScope() {
		right = line{sg(clean(m.scopeName())+"  ", sAccent)}
	}
	switch m.conn {
	case ConnLive:
		right = append(right, sg(gLive+" live ", sGreen))
	case ConnDown:
		right = append(right, sg(gLive+" retrying ", sRed))
	default:
		right = append(right, sg(gLive+" connecting ", sYellow))
	}
	return lr(left, right, w)
}

func (m *Model) filterLine(w int) line {
	l := line{sg(" / ", sHead), sg(clean(m.filter), sPlain)}
	if m.filtering {
		l = append(l, sg(" ", style{bg: cPurple}), sg("  Enter keep "+gDot+" Esc clear", sDim))
	} else {
		l = append(l, sg("   Esc clear", sDim))
	}
	return fit(l, w, sPlain)
}

func (m *Model) statusLine(w int, more string) line {
	if m.notice != "" {
		st := style{fg: cOnAcc, bg: cGreen}
		switch m.noticeKind {
		case NoticeWarn:
			st.bg = cOrange
		case NoticeError:
			st.bg = cRed
		}
		return fit(line{sg(" "+clean(m.notice), st)}, w, st)
	}
	var left line
	for i, h := range m.hints() {
		sep := " "
		if i > 0 {
			sep = "  "
		}
		left = append(left, sg(sep+h.key, style{fg: cPurple, bg: cBg2, bold: true}), sg(" "+h.does, style{fg: cSoft, bg: cBg2}))
	}
	var right line
	if more != "" {
		right = line{sg(" "+more+" ", style{fg: cDim, bg: cBg2})}
	}
	return withBg(lr(left, right, w), cBg2)
}

// hints are the status line's keys for what is on screen.
func (m *Model) hints() []hint {
	t := m.target()
	lay := m.layout()
	acts := []hint{{"x", "done"}, {"s", "snooze"}, {"d", "won't do"}}
	switch {
	case t != nil && t.readOnly():
		acts = nil
	case t != nil && isPersonal(t.item.Bridge):
		acts[2].does = "delete"
	}
	switch {
	case m.pw:
		return []hint{{"Enter", "sign in"}, {"Esc", "quit"}}
	case m.help && m.helpMax() > 0:
		return []hint{{"j k", "scroll"}, {"any other key", "close"}}
	case m.help:
		return []hint{{"any key", "close"}}
	case m.snoozeKey != "":
		return []hint{{"Enter", "snooze"}, {"1-" + itoa(len(snoozeOptions(m.now))), "pick"}, {"Esc", "cancel"}}
	case m.filtering:
		return []hint{{"Enter", "keep"}, {"Esc", "clear"}}
	case m.d == nil:
		return []hint{{"r", "refresh"}, {"?", "keys"}, {"q", "quit"}}
	case m.open && m.openKids:
		return append(append([]hint{{"Enter", "open"}, {"y", "copy"}}, acts...), hint{"h", "back"}, hint{"?", "keys"})
	case m.open && len(m.openKidRows()) > 0:
		return []hint{{"l", "into epic"}, {"y", "copy"}, {"Y", "copy all"}, {"Esc", "back"}, {"?", "keys"}}
	case m.open:
		return append(append([]hint{{"y", "copy"}, {"Y", "copy all"}}, acts...), hint{"Esc", "back"}, hint{"?", "keys"})
	case m.right:
		return []hint{{"Enter", "open"}, {"h", "back"}, {"y", "copy"}, {"Y", "copy all"}, {"?", "keys"}}
	case inEpics(m.selKey) && lay == "b":
		return []hint{{"Enter", "open"}, {"l", "into epic"}, {"y", "copy"}, {"Tab", "to-dos"}, {"/", "filter"}, {"?", "keys"}}
	case inEpics(m.selKey):
		return []hint{{"Enter", "open"}, {"l h", "fold"}, {"y", "copy"}, {"Tab", "to-dos"}, {"/", "filter"}, {"?", "keys"}}
	}
	hs := append([]hint{{"Enter", "open"}, {"y", "copy"}}, acts...)
	if m.hasEpicRows() {
		hs = append(hs, hint{"Tab", "epics"})
	}
	return append(hs, hint{"/", "filter"}, hint{"?", "keys"})
}

// hasEpicRows reports whether the epics section has a row Tab can go to.
func (m *Model) hasEpicRows() bool {
	for _, r := range m.rows() {
		if r.selectable() && inEpics(r.id) {
			return true
		}
	}
	return false
}

// body is the lines between the top and the status line, and the scroll
// position for the status line.
func (m *Model) body(w, h int) ([]line, string) {
	if h <= 0 {
		return nil, ""
	}
	if m.help {
		return m.helpBody(w, h)
	}
	if m.d == nil {
		return []line{fit(line{sg("  Loading", sDim)}, w, sPlain)}, ""
	}
	if m.open {
		if t := m.shown(); t != nil {
			return m.detailBody(t, w, h), ""
		}
	}
	if m.layout() == "b" {
		return m.splitBody(w, h)
	}
	return m.viewport(m.rows(), h, w, true)
}

// viewport draws rows from the scroll offset into h lines, the selected
// row highlighted when focus is set.
func (m *Model) viewport(rows []row, h, w int, focus bool) ([]line, string) {
	if h <= 0 {
		return nil, ""
	}
	sel := selectable(rows)
	cur := -1
	if len(sel) > 0 {
		cur = sel[min(m.selIdx, len(sel)-1)]
	}
	scroll := max(0, min(m.scroll, len(rows)-h))
	var out []line
	for i := scroll; i < scroll+h && i < len(rows); i++ {
		r := rows[i]
		if !r.selectable() {
			out = append(out, fit(r.head, w, sPlain))
			continue
		}
		l := fit(r.draw(m, w), w, sPlain)
		if i == cur {
			l = selected(l, focus)
		}
		out = append(out, l)
	}
	more := ""
	if len(rows) > h {
		more = itoa(scroll+1) + "-" + itoa(min(len(rows), scroll+h)) + "/" + itoa(len(rows))
	}
	return out, more
}

// detailBody is a full-screen detail under its crumb.
func (m *Model) detailBody(t *target, w, h int) []line {
	from := "To-dos"
	if inEpics(m.selKey) {
		from = "Epics"
	}
	out := []line{fit(line{sg(" "+gBack+" Esc ", sAccent), sg(from, sDim)}, w, sPlain)}
	lines := m.detailLines(t, w)
	start := min(m.detailScroll, max(0, len(lines)-(h-1)))
	for i := start; i < len(lines) && len(out) < h; i++ {
		out = append(out, fit(lines[i], w, sPlain))
	}
	return out
}

// detailMax is the furthest an open detail scrolls.
func (m *Model) detailMax() int {
	t := m.shown()
	if t == nil {
		return 0
	}
	return max(0, len(m.detailLines(t, m.width))-(m.bodyHeight()-1))
}

// splitWidths are layout B's left and right pane widths.
func splitWidths(w int) (int, int) {
	lw := w * 55 / 100
	return lw, w - lw - 1
}

// splitBody is layout B: the list on the left, the selected row's detail,
// or a card's children, on the right.
func (m *Model) splitBody(w, h int) ([]line, string) {
	lw, rw := splitWidths(w)
	rows := m.rows()
	left, more := m.viewport(rows, h, lw, !m.right)
	var right []line
	if t := m.listTarget(); t != nil {
		if inEpics(m.selKey) && t.card && t.row != nil {
			right = m.cardPane(t, rw, h)
		} else {
			right = m.detailLines(t, rw)
		}
	}
	out := make([]line, h)
	for i := range out {
		var l, r line
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = append(append(fit(l, lw, sPlain), sg(gSplit, sDim)), fit(r, rw, sPlain)...)
	}
	return out, more
}

// cardPane is a card on layout B's right: its title, bar and description,
// then its children, which l walks into.
func (m *Model) cardPane(t *target, w, h int) []line {
	d, re, r := m.d, t.repo, t.row
	out := titleLines(d.rowTitle(re, r), w)
	out = append(out, line{sg(" "+clean(r.ID), sDim), sg("  ", sPlain), d.mark(re.Bridge, re.Repo), sg(" "+clean(re.Repo), sSoft)})
	out = append(out, d.cardFacts(re, r, " ", w)...)
	kids := d.children(re, r.ID)
	var desc []line
	if strings.TrimSpace(r.Description) != "" {
		desc = textLines(r.Description, w, sSoft)
	}
	if desc = append(desc, notesLines(r, w, sSoft)...); len(desc) > 0 {
		// A long description and notes keep room for the children below
		// them, at least half the pane; Enter on the epic shows the rest.
		keep := min(len(kids), max(3, h/2))
		if most := max(2, h-len(out)-1-keep); len(desc) > most {
			desc = append(desc[:most-1], line{sg(" "+ellipsis+" Enter shows the rest", sDim)})
		}
		out = append(out, desc...)
	}
	out = append(out, nil)
	if len(kids) == 0 {
		return append(out, line{sg("  No children yet.", sDim)})
	}
	// Keep the selected child in view below the card's own lines.
	room := max(1, h-len(out))
	start := 0
	if m.right && m.rightIdx >= room {
		start = m.rightIdx - room + 1
	}
	for i := start; i < len(kids); i++ {
		l := fit(m.childLine(re, kids[i], w), w, sPlain)
		if m.right && i == m.rightIdx {
			l = selected(l, true)
		}
		out = append(out, l)
	}
	return out
}

// ---- overlays

// panel is an overlay's lines, already bw cells wide.
type panel []line

// box frames content as a panel bw cells wide, titled on its top edge.
func box(title string, content []line, bw int) panel {
	st := style{bg: cRaise}
	edge := style{fg: cPurple, bg: cRaise}
	head := line{sg(gEdge+" ", edge), sg(title, style{fg: cFg, bg: cRaise, bold: true}), sg(" ", st)}
	topRest := bw - head.width()
	if topRest > 0 {
		head = append(head, sg(strings.Repeat(gEdge, topRest), edge))
	}
	p := panel{withBg(fit(head, bw, st), cRaise)}
	for _, c := range content {
		p = append(p, withBg(fit(append(line{sg(" ", st)}, c...), bw, st), cRaise))
	}
	return append(p, line{sg(strings.Repeat(gEdge, bw), edge)})
}

// overlay draws a panel over the middle of the lines, above the status line.
func (m *Model) overlay(lines []line, p panel) {
	w := m.width
	if len(p) == 0 || w <= 0 {
		return
	}
	bw := p[0].width()
	x0 := max(0, (w-bw)/2)
	y0 := max(1, (len(lines)-1-len(p))/2)
	for i, l := range p {
		y := y0 + i
		if y >= len(lines)-1 {
			break
		}
		row := append(fit(nil, x0, sPlain), l...)
		lines[y] = fit(row, w, sPlain)
	}
}

func panelWidth(w, want int) int { return max(1, min(want, w-4)) }

func (m *Model) snoozePanel(w int) panel {
	bw := panelWidth(w, 40)
	it := m.d.items[m.snoozeKey]
	title := "Snooze"
	if it != nil {
		title += " " + clean(it.ID)
	}
	var c []line
	for n, o := range snoozeOptions(m.now) {
		bg := cRaise
		if n == m.snoozeIdx {
			bg = cLine
		}
		c = append(c, withBg(lr(line{sg(itoa(n+1)+"  ", style{fg: cPurple, bg: bg, bold: true}), sg(o.label, style{fg: cFg, bg: bg})},
			line{sg(o.hint+" ", style{fg: cSoft, bg: bg})}, bw-1), bg))
	}
	c = append(c, nil, line{sg("Esc", style{fg: cPurple, bg: cRaise, bold: true}), sg("  cancel", style{fg: cSoft, bg: cRaise})})
	return box(title, c, bw)
}

func (m *Model) passwordPanel(w int) panel {
	bw := panelWidth(w, 52)
	var c []line
	for _, l := range wrap(m.pwReason, bw-2) {
		c = append(c, line{sg(l, style{fg: cFg, bg: cRaise})})
	}
	c = append(c, nil, line{sg("Password ", style{fg: cSoft, bg: cRaise}), sg(strings.Repeat("*", utf8Len(m.pwText)), style{fg: cFg, bg: cRaise}), sg(" ", style{bg: cPurple})},
		nil, line{sg("Enter", style{fg: cPurple, bg: cRaise, bold: true}), sg(" sign in  ", style{fg: cSoft, bg: cRaise}),
			sg("Esc", style{fg: cPurple, bg: cRaise, bold: true}), sg(" quit", style{fg: cSoft, bg: cRaise})})
	return box("Sign in", c, bw)
}

func utf8Len(s string) int { return len([]rune(s)) }

// gSelected marks the selected row in its first cell, so the selection
// shows without colour too.
const gSelected = "›"

// selected is a row drawn as the selection: its background, and the mark
// in its first cell when that is a space. Without focus (layout B's list
// while the keys are on the right) it is dimmer.
func selected(l line, focus bool) line {
	bg := cLine
	if !focus {
		bg = cRaise
	}
	if len(l) > 0 && strings.HasPrefix(l[0].text, " ") {
		rest := l[0]
		rest.text = rest.text[1:]
		l = append(line{sg(gSelected, style{fg: cPurple, bold: true}), rest}, l[1:]...)
	}
	return withBg(l, bg)
}
