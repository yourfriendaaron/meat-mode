package view

// The keys page: what ? shows in place of the list, the keys in groups and
// the marks the rows carry, drawn as the rows draw them.

// helpGroup is one titled group of keys.
type helpGroup struct {
	title string
	keys  []hint
}

var helpGroups = []helpGroup{
	{"Move", []hint{
		{"j k", "down, up; the arrows too"},
		{"g G", "top, bottom"},
		{"Space", "a page down; PgUp PgDn too"},
		{"^d ^u", "half a page down, up"},
		{"Tab", "to the epics, or back to the to-dos"},
		{"1 2", "to the to-dos, to the epics"},
		{"/", "filter by title or id"},
		{"p", "this project, or every list"},
	}},
	{"Open", []hint{
		{"Enter", "open full screen"},
		{"l", "unfold, step in, or open"},
		{"h Esc", "back; h on an epic folds it"},
		{"o", "show the item's app link"},
	}},
	{"Act", []hint{
		{"x", "done, or reopen"},
		{"d", "won't do, or delete a to-do"},
		{"s", "snooze, then pick a time"},
		{"u", "undo the last change"},
		{"y", "copy id and title"},
		{"Y", "copy with details"},
	}},
	{"App", []hint{
		{"r", "refresh"},
		{"?", "these keys"},
		{"q", "quit"},
	}},
}

// helpMark is one mark and what it means.
type helpMark struct {
	mark line
	does string
}

func helpMarks() []helpMark {
	return []helpMark{
		{line{sg(badgeText(gClosed), style{fg: cOnAcc, bg: cPurple, bold: true})}, "your personal list"},
		{line{sg(badgeText("ABC"), style{fg: cOnAcc, bg: repoColours[0], bold: true})}, "a project, in its colour"},
		{line{sg(gHuman, sOrange)}, "a task an agent left for you"},
		{line{statusGlyph("open")}, "Open: ready to start"},
		{line{statusGlyph("in_progress")}, "In progress"},
		{line{statusGlyph("blocked")}, "Blocked"},
		{line{statusGlyph("waiting")}, "Waiting on another bead"},
		{line{statusGlyph("deferred")}, "Deferred, to a date when it has one"},
		{line{statusGlyph("closed")}, "Done, or Closed"},
		{line{sg(gSnoozed, sAccent)}, "snoozed"},
		{line{sg(gRepeat, sCyan)}, "repeats"},
		{line{sg(gBarFull+gBarFull, sGreen), sg(gBarEmpty+gBarEmpty, sDim)}, "an epic's closed tasks over all of them"},
	}
}

// helpKeyWidth and helpMarkWidth are the cells of the key and mark columns.
const (
	helpKeyWidth  = 11
	helpMarkWidth = 5
)

// helpCols is how many columns the keys page sets at width w.
func helpCols(w int) int {
	switch {
	case w >= 120:
		return 3
	case w >= 80:
		return 2
	}
	return 1
}

// helpBlocks are the page's groups in order, each w cells wide: the key
// groups, then the marks.
func helpBlocks(w int) [][]line {
	var out [][]line
	for _, g := range helpGroups {
		b := []line{{sg(" "+g.title, sHead)}}
		for _, k := range g.keys {
			b = append(b, hangLines(line{sg("  "+padRight(k.key, helpKeyWidth), style{fg: cPurple, bold: true})}, k.does, w, sPlain)...)
		}
		out = append(out, b)
	}
	marks := []line{{sg(" Marks", sHead)}}
	for _, mk := range helpMarks() {
		lead := append(line{sg("  ", sPlain)}, mk.mark...)
		lead = append(lead, sg(padRight("", helpMarkWidth-mk.mark.width()), sPlain))
		marks = append(marks, hangLines(lead, mk.does, w, sSoft)...)
	}
	return append(out, marks)
}

// hangLines are lead and text wrapped to w cells, the wrapped lines
// indented under the text's first line.
func hangLines(lead line, text string, w int, st style) []line {
	ind := lead.width()
	ws := wrap(text, max(8, w-ind))
	out := []line{append(lead, sg(ws[0], st))}
	for _, l := range ws[1:] {
		out = append(out, line{sg(padRight("", ind)+l, st)})
	}
	return out
}

// helpLines are the whole keys page, w cells wide: its groups in order,
// split into helpCols columns so that the tallest is shortest.
func helpLines(w int) []line {
	n := helpCols(w)
	cw := w / n
	blocks := helpBlocks(cw - 1)
	var best [][]line
	for _, cuts := range splits(len(blocks), n) {
		var cols [][]line
		from := 0
		for _, to := range append(cuts, len(blocks)) {
			var col []line
			for i, b := range blocks[from:to] {
				if i > 0 {
					col = append(col, nil)
				}
				col = append(col, b...)
			}
			cols, from = append(cols, col), to
		}
		if best == nil || tallest(cols) < tallest(best) {
			best = cols
		}
	}
	out := []line{{sg(" Keys and marks", sBold)}, nil}
	for i := range tallest(best) {
		var row line
		for c, col := range best {
			var l line
			if i < len(col) {
				l = col[i]
			}
			if c < len(best)-1 {
				l = fit(l, cw, sPlain)
			}
			row = append(row, l...)
		}
		out = append(out, row)
	}
	return out
}

// splits are the ways to cut n blocks, in order, into k runs that are not
// empty, each as the indexes where the runs after the first start.
func splits(n, k int) [][]int {
	if k <= 1 || n <= 1 {
		return [][]int{nil}
	}
	var out [][]int
	for first := 1; first <= n-(k-1); first++ {
		for _, rest := range splits(n-first, k-1) {
			cut := []int{first}
			for _, r := range rest {
				cut = append(cut, first+r)
			}
			out = append(out, cut)
		}
	}
	return out
}

// tallest is the most lines in any of cols.
func tallest(cols [][]line) int {
	h := 0
	for _, c := range cols {
		h = max(h, len(c))
	}
	return h
}

// helpMax is the furthest the keys page scrolls.
func (m *Model) helpMax() int {
	return max(0, len(helpLines(m.width))-m.bodyHeight())
}

// helpBody is the keys page from its scroll position, h lines, and its
// position for the status line when it scrolls.
func (m *Model) helpBody(w, h int) ([]line, string) {
	all := helpLines(w)
	top := min(m.helpScroll, m.helpMax())
	end := min(len(all), top+h)
	more := ""
	if len(all) > h {
		more = itoa(top+1) + "-" + itoa(end) + "/" + itoa(len(all))
	}
	return all[top:end], more
}
