package view

import (
	"maps"
	"strings"
	"time"
)

// undoable is the last done or snooze this session made.
type undoable struct {
	kind, key, id   string
	until, previous *time.Time // snooze
	movedOn         bool       // a done that moved a repeating to-do to its next date
}

func (m *Model) layout() Layout { return LayoutFor(m.tab, m.width) }

// say sets the status line's message.
func (m *Model) say(text string, kind NoticeKind) {
	m.seq++
	m.notice, m.noticeKind, m.noticeSeq = text, kind, m.seq
}

// topLines is the lines above the body: the tabs, and the filter while one
// is typed or kept.
func (m *Model) topLines() int {
	if m.filtering || m.filter != "" {
		return 2
	}
	return 1
}

// bodyHeight is the lines between the top lines and the status line.
func (m *Model) bodyHeight() int { return max(0, m.height-m.topLines()-1) }

// listHeight is the lines the list's rows get: the body, less the crumb of
// a drilled card.
func (m *Model) listHeight() int {
	if m.layout() == "c" && m.drill != "" {
		return max(0, m.bodyHeight()-1)
	}
	return m.bodyHeight()
}

// rows are the current list's rows.
func (m *Model) rows() []row {
	if m.d == nil {
		return nil
	}
	switch m.tab {
	case TabTodos:
		return m.d.todoRows(m.filter)
	case TabEpics:
		if m.layout() == "a" {
			return m.d.epicTreeRows(m.filter, m.expanded)
		}
		return m.d.epicListRows(m.filter)
	}
	if t := m.drillTarget(); t != nil {
		return m.d.drillRows(m.filter, t, m.width)
	}
	rows, _ := m.d.allRows(m.filter)
	return rows
}

func (m *Model) drillTarget() *target {
	if m.drill == "" || m.d == nil {
		return nil
	}
	return m.d.resolve(m.drill, true)
}

// selectable are the indexes of the rows that can hold the selection.
func selectable(rows []row) []int {
	var out []int
	for i, r := range rows {
		if r.selectable() {
			out = append(out, i)
		}
	}
	return out
}

// listTarget is the selected row's target in the list, or nil.
func (m *Model) listTarget() *target {
	rows := m.rows()
	sel := selectable(rows)
	if len(sel) == 0 {
		return nil
	}
	return rows[sel[min(m.selIdx, len(sel)-1)]].t
}

// target is what the keys act on: the open detail, the child selected in
// layout B's right pane, or the selected row.
func (m *Model) target() *target {
	if m.d == nil {
		return nil
	}
	if m.open {
		return m.d.resolve(m.openKey, m.openRow)
	}
	t := m.listTarget()
	if t != nil && m.right && t.row != nil {
		kids := m.d.children(t.repo, t.row.ID)
		if m.rightIdx < len(kids) {
			return m.d.epicTarget(t.repo, kids[m.rightIdx])
		}
	}
	return t
}

// rightKids are the children of the card selected in layout B, or nil.
func (m *Model) rightKids() int {
	if m.tab != TabEpics || m.layout() != "b" {
		return 0
	}
	t := m.listTarget()
	if t == nil || t.row == nil || !t.card {
		return 0
	}
	return len(m.d.children(t.repo, t.row.ID))
}

func (m *Model) openTarget(t *target) {
	m.open, m.openKey, m.openRow, m.detailScroll = true, t.key, t.row != nil, 0
}

// toSplit carries a selection from layout A's tree into layout B: a child
// of a card becomes that card with its child selected on the right.
func (m *Model) toSplit() {
	if m.tab != TabEpics || m.d == nil {
		return
	}
	parent, child, ok := strings.Cut(m.selKey, ">")
	if !ok {
		return
	}
	if i := strings.LastIndex(child, ">"); i >= 0 {
		child = child[i+1:]
	}
	m.selKey = parent
	if re, r := m.d.findRow(parent); r != nil {
		for n, k := range m.d.children(re, r.ID) {
			if rowKey(re, k) == child {
				m.right, m.rightIdx = true, n
			}
		}
	}
}

// resetSel puts the selection on the first row.
func (m *Model) resetSel() {
	m.selKey, m.selIdx, m.scroll, m.right, m.rightIdx = "", 0, 0, false, 0
}

// setSel selects the n-th selectable row.
func (m *Model) setSel(n int) {
	rows := m.rows()
	sel := selectable(rows)
	if len(sel) == 0 {
		return
	}
	n = min(max(n, 0), len(sel)-1)
	m.selIdx, m.selKey = n, rows[sel[n]].id
	m.right, m.rightIdx = false, 0
}

// fix makes the model consistent after a change: the selection on its
// key, or the nearest row when the key has gone; the selected row in
// view; and an open detail, drill or menu closed when its target has gone.
func (m *Model) fix() {
	if m.d == nil {
		return
	}
	if m.open && m.d.resolve(m.openKey, m.openRow) == nil {
		m.open = false
	}
	if m.drill != "" && m.drillTarget() == nil {
		m.drill = ""
		m.resetSel()
	}
	if m.snoozeKey != "" {
		if it := m.d.items[m.snoozeKey]; it == nil || !isOpen(it) {
			m.snoozeKey = ""
		}
	}
	rows := m.rows()
	sel := selectable(rows)
	if len(sel) == 0 {
		m.selIdx, m.scroll, m.right = 0, 0, false
		return
	}
	found := false
	if m.selKey != "" {
		for n, i := range sel {
			if rows[i].id == m.selKey {
				m.selIdx, found = n, true
				break
			}
		}
	}
	if !found {
		m.selIdx = min(max(m.selIdx, 0), len(sel)-1)
		m.selKey = rows[sel[m.selIdx]].id
	}
	if kids := m.rightKids(); m.right && kids == 0 {
		m.right = false
	} else if m.right {
		m.rightIdx = min(max(m.rightIdx, 0), kids-1)
	}
	h := m.listHeight()
	cur := sel[m.selIdx]
	if m.selIdx == 0 {
		m.scroll = 0
	}
	if cur < m.scroll {
		m.scroll = cur
	}
	if h > 0 && cur >= m.scroll+h {
		m.scroll = cur - h + 1
	}
	m.scroll = max(0, min(m.scroll, len(rows)-h))
}

// setExpanded folds or unfolds a card of layout A's tree.
func (m *Model) setExpanded(id string, on bool) {
	e := maps.Clone(m.expanded)
	if e == nil {
		e = map[string]bool{}
	}
	if on {
		e[id] = true
	} else {
		delete(e, id)
	}
	m.expanded = e
}
