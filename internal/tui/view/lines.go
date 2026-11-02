package view

import (
	"strconv"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Glyphs. Each is one cell wide and East Asian Width neutral, so it stays
// one cell in a terminal set to draw ambiguous-width characters wide.
const (
	gOpen     = "◦" // U+25E6
	gProgress = "◒" // U+25D2
	gBlocked  = "⊘" // U+2298
	gClosed   = "✓" // U+2713
	gDeferred = "☾" // U+263E, also the mark of a snoozed date
	gHuman    = "✦" // U+2726, a task for the person
	gRepeat   = "↻" // U+21BB
	gFolded   = "▸" // U+25B8
	gUnfolded = "▾" // U+25BE
	gBarFull  = "▰" // U+25B0
	gBarEmpty = "▱" // U+25B1
	gSplit    = "╎" // U+254E, between the panes of layout B
	gEdge     = "╌" // U+254C, the top and bottom of an overlay
	gBullet   = "‣" // U+2023
	gDot      = "∙" // U+2219
	gBack     = "‹" // U+2039
	gCrumb    = "›" // U+203A
	gLive     = "◉" // U+25C9
)

func itoa(n int) string { return strconv.Itoa(n) }

// statusGlyph is a status's glyph and colour.
func statusGlyph(status string) seg {
	switch status {
	case "in_progress":
		return sg(gProgress, sYellow)
	case "blocked":
		return sg(gBlocked, sRed)
	case "closed":
		return sg(gClosed, sGreen)
	case "deferred":
		return sg(gDeferred, sDim)
	}
	return sg(gOpen, sSoft)
}

// statusWords is a status in words.
func statusWords(status string) string {
	if status == "in_progress" {
		return "in progress"
	}
	return status
}

// mark is a list's badge, codeWidth cells: the personal list's check,
// otherwise its code (listCodes) on its colour slot (web/src/identity.ts).
// The web shows one letter on a square; a terminal cell is half as wide,
// so the terminal shows up to three, which also tells apart lists that
// share an initial.
func (d *data) mark(bridge, name string) seg {
	r := d.repoOf(bridge, name)
	if r == nil {
		r = &api.Repo{Bridge: bridge, Name: name}
	}
	if isPersonalList(r) {
		return sg(badgeText(gClosed), style{fg: cOnAcc, bg: cPurple, bold: true})
	}
	code, ok := d.codes[listKey(bridge, name)]
	if !ok {
		code = codeCandidates(name)[0]
	}
	return sg(badgeText(code), style{fg: cOnAcc, bg: repoColours[listSlot(r, d.state.Repos)], bold: true})
}

// listName is the name the app shows for a list, with its path or machine
// where names repeat.
func (d *data) listName(bridge, name string) string {
	r := d.repoOf(bridge, name)
	if r == nil {
		return clean(repoLabel(name))
	}
	if det := listDetail(r, d.state.Repos); det != "" {
		return clean(repoLabel(name) + " " + gDot + " " + det)
	}
	return clean(repoLabel(name))
}

// bar is a progress bar w cells wide.
func bar(done, total, w int) line {
	if total == 0 {
		return line{sg(strings.Repeat(gBarEmpty, w), sDim)}
	}
	f := (done*w*2 + total) / (total * 2)
	st := sAccent
	if done == total {
		st = sGreen
	}
	return line{sg(strings.Repeat(gBarFull, f), st), sg(strings.Repeat(gBarEmpty, w-f), sDim)}
}

// tags are an item's labels as the app shows them, without HumanLabel.
func tags(it *api.Item) []string {
	var out []string
	for _, l := range it.Labels {
		if l != api.HumanLabel {
			out = append(out, "#"+clean(l))
		}
	}
	return out
}

// itemMark is the glyph at the start of an item's row.
func itemMark(it *api.Item) seg {
	switch {
	case !isOpen(it):
		return sg(gClosed, sGreen)
	case !isPersonal(it.Bridge):
		return sg(gHuman, sOrange)
	}
	return sg(gOpen, sSoft)
}

func chipStyle(t chipTone) style {
	switch t {
	case toneOverdue:
		return sRed
	case toneSoon:
		return sGreen
	case toneSnoozed:
		return sOrange
	}
	return sAccent
}

// chipText is the date chip with the snoozed mark.
func chipText(text string, t chipTone) string {
	if t == toneSnoozed {
		return gDeferred + " " + text
	}
	return text
}

// itemLine is one to-do row.
func (m *Model) itemLine(it *api.Item, w int, nested bool) line {
	d := m.d
	narrow := w < 70
	var right line
	if ts := tags(it); !narrow && len(ts) > 0 {
		right = append(right, sg(" "+strings.Join(ts, " "), sDim))
	}
	if c := subtaskChip(it, d.state.Items); c != "" {
		right = append(right, sg(" "+c, sDim))
	}
	if it.Repeat != nil && isPersonal(it.Bridge) {
		right = append(right, sg(" "+gRepeat, sCyan))
	}
	if text, tone, ok := dateChip(it, d.now); ok {
		text = chipText(text, tone)
		if !narrow {
			text = padLeft(text, 11)
		}
		right = append(right, sg(" "+text, chipStyle(tone)))
	}
	right = append(right, sg(" ", sPlain))
	indent := "  "
	if nested {
		indent = "    "
	}
	left := line{sg(indent, sPlain), itemMark(it), sg(" ", sPlain), d.mark(it.Bridge, it.Repo), sg(" ", sPlain)}
	if !narrow {
		left = append(left, sg(padRight(clean(it.ID), 9)+" ", sDim))
	}
	ts := sPlain
	if !isOpen(it) {
		ts = style{fg: cDim, strike: true}
	}
	left = append(left, sg(clean(it.Title), ts))
	return lr(left, right, w)
}

// epicLine is one row of the epics tree, or of a drilled card.
func (m *Model) epicLine(re *api.RepoEpics, r *api.EpicRow, depth int, card, foldable, open bool, w int) line {
	d := m.d
	status := d.rowStatus(re, r)
	left := line{sg("  "+strings.Repeat("  ", depth), sPlain)}
	switch {
	case foldable && open:
		left = append(left, sg(gUnfolded, sAccent))
	case foldable:
		left = append(left, sg(gFolded, sAccent))
	default:
		left = append(left, statusGlyph(status))
	}
	idw := 9
	if depth > 0 {
		idw = 12
	}
	left = append(left, sg(" ", sPlain), sg(padRight(clean(r.ID), idw)+" ", sDim))
	ts := sPlain
	switch {
	case status == "closed":
		ts = sDim
	case card:
		ts = sBold
	}
	left = append(left, sg(clean(d.rowTitle(re, r)), ts))
	var right line
	if r.Human {
		right = append(right, sg(" "+gHuman+" you", sOrange))
	}
	if r.Assignee != "" {
		right = append(right, sg(" @"+clean(r.Assignee), sCyan))
	}
	if card {
		done, total := d.progress(re, r.ID)
		bw := 10
		if w < 70 {
			bw = 6
		}
		right = append(right, sg(" ", sPlain))
		right = append(right, bar(done, total, bw)...)
		right = append(right, sg(padRight(" "+itoa(done)+"/"+itoa(total), 6), sSoft))
	}
	right = append(right, sg(" ", sPlain))
	return lr(left, right, w)
}

// cardLine is one row of layout B's left list and of All's epics: the
// list mark, id and title, and a card's bar or a task's status.
func (m *Model) cardLine(re *api.RepoEpics, r *api.EpicRow, w int) line {
	d := m.d
	t := d.epicTarget(re, r)
	ts := sPlain
	if t.card {
		ts = sBold
	}
	left := line{sg(" ", sPlain), d.mark(re.Bridge, re.Repo), sg(" ", sPlain), sg(padRight(clean(r.ID), 9)+" ", sDim), sg(clean(d.rowTitle(re, r)), ts)}
	if !t.card {
		right := line{}
		if r.Human {
			right = append(right, sg(" "+gHuman, sOrange))
		}
		right = append(right, sg(" ", sPlain), statusGlyph(d.rowStatus(re, r)), sg(" ", sPlain))
		return lr(left, right, w)
	}
	done, total := d.progress(re, r.ID)
	bw := 8
	if w < 50 {
		bw = 5
	}
	right := append(line{sg(" ", sPlain)}, bar(done, total, bw)...)
	right = append(right, sg(padLeft(" "+itoa(done)+"/"+itoa(total)+" ", 7), sSoft))
	return lr(left, right, w)
}

// ---- detail

// detailLines are the detail of a target, w cells wide.
func (m *Model) detailLines(t *target, w int) []line {
	if t.item != nil {
		return m.itemDetail(t, w)
	}
	return m.rowDetail(t, w)
}

func titleLines(title string, w int) []line {
	var out []line
	for _, l := range wrap(title, w-2) {
		out = append(out, line{sg(" "+l, sBold)})
	}
	return out
}

func textLines(text string, w int, st style) []line {
	var out []line
	for _, l := range wrap(text, w-3) {
		out = append(out, line{sg("  "+l, st)})
	}
	return out
}

func (m *Model) itemDetail(t *target, w int) []line {
	d, it := m.d, t.item
	out := titleLines(it.Title, w)
	idl := line{sg(" "+clean(it.ID), sDim), sg("  ", sPlain), d.mark(it.Bridge, it.Repo), sg(" "+d.listName(it.Bridge, it.Repo), sSoft)}
	if !isPersonal(it.Bridge) {
		idl = append(idl, sg("  "+gHuman+" for you", sOrange))
	}
	if it.Status != "open" && it.Status != "closed" {
		idl = append(idl, sg("  ", sPlain), statusGlyph(it.Status), sg(" "+clean(statusWords(it.Status)), sSoft))
	}
	out = append(out, idl)
	var meta line
	add := func(s seg) {
		if len(meta) > 0 {
			s.text = "  " + s.text
		}
		meta = append(meta, s)
	}
	if !isOpen(it) {
		if it.ClosedAt != nil {
			add(sg("done "+fmtWhenAt(it.ClosedAt.In(d.now.Location()), d.now), sGreen))
		} else {
			add(sg("done", sGreen))
		}
	} else if isSnoozed(it, d.now) {
		add(sg("snoozed until "+fmtWhenAt(it.DeferUntil.In(d.now.Location()), d.now), sOrange))
		if it.DueAt != nil {
			add(sg("due "+fmtWhenAt(it.DueAt.In(d.now.Location()), d.now), sSoft))
		}
	} else if text, tone, ok := dateChip(it, d.now); ok {
		if tone != toneOverdue {
			text = "due " + fmtWhenAt(it.DueAt.In(d.now.Location()), d.now)
		}
		add(sg(text, chipStyle(tone)))
	}
	if it.Repeat != nil && isPersonal(it.Bridge) {
		add(sg(gRepeat+" "+repeatLabel(it.Repeat, d.now), sCyan))
	}
	if ts := tags(it); len(ts) > 0 {
		add(sg(strings.Join(ts, " "), sDim))
	}
	if len(meta) > 0 {
		out = append(out, append(line{sg(" ", sPlain)}, meta...))
	}
	if it.Parent != "" {
		pt := it.ParentTitle
		if p := d.items[listKey(it.Bridge, it.Repo)+"/"+it.Parent]; p != nil {
			pt = p.Title
		}
		if pt != "" {
			out = append(out, line{sg(" Sub-task of "+clean(pt), sDim)})
		}
	}
	out = append(out, nil)
	if strings.TrimSpace(it.Description) != "" {
		out = append(out, textLines(it.Description, w, sPlain)...)
	} else {
		out = append(out, line{sg("  No description", sDim)})
	}
	if subs := subtasksOf(it, d.state.Items); len(subs) > 0 {
		out = append(out, nil, line{sg(" Sub-tasks ", sHead), sg(subtaskChip(it, d.state.Items), sDim)})
		for _, s := range subs {
			ts := sPlain
			if !isOpen(s) {
				ts = sDim
			}
			out = append(out, line{sg("  ", sPlain), itemMark(s), sg(" "+clean(s.ID)+" ", sDim), sg(clean(s.Title), ts)})
		}
	}
	if len(it.Comments) > 0 {
		out = append(out, nil, line{sg(" Notes ", sHead), sg(itoa(len(it.Comments)), sDim)})
		for _, c := range it.Comments {
			head := clean(c.Author) + " " + gDot + " " + fmtAgo(c.CreatedAt, d.now)
			out = append(out, line{sg("  "+gBullet+" ", sAccent), sg(head, sDim)})
			for _, l := range wrap(c.Text, w-5) {
				out = append(out, line{sg("    "+l, sSoft)})
			}
		}
	}
	return out
}

func (m *Model) rowDetail(t *target, w int) []line {
	d, re, r := m.d, t.repo, t.row
	out := titleLines(r.Title, w)
	idl := line{sg(" "+clean(r.ID), sDim), sg("  ", sPlain), d.mark(re.Bridge, re.Repo), sg(" "+clean(re.Repo), sSoft),
		sg("  ", sPlain), statusGlyph(r.Status), sg(" "+clean(statusWords(r.Status)), sSoft), priority(r.Priority)}
	if r.Human {
		idl = append(idl, sg("  "+gHuman+" needs you", sOrange))
	}
	out = append(out, idl)
	var facts []string
	if r.Type != "" {
		facts = append(facts, clean(r.Type))
	}
	if r.Assignee != "" {
		facts = append(facts, "@"+clean(r.Assignee))
	}
	if !r.UpdatedAt.IsZero() {
		facts = append(facts, "updated "+fmtAgo(r.UpdatedAt, d.now))
	}
	if r.ClosedAt != nil {
		facts = append(facts, "closed "+fmtAgo(*r.ClosedAt, d.now))
	}
	if len(facts) > 0 {
		out = append(out, line{sg(" "+strings.Join(facts, " "+gDot+" "), sDim)})
	}
	if r.Parent != "" {
		for k := range re.Rows {
			if re.Rows[k].ID == r.Parent {
				out = append(out, line{sg(" Epic: ", sDim), sg(clean(re.Rows[k].Title), sSoft)})
			}
		}
	}
	out = append(out, nil)
	if strings.TrimSpace(r.Description) != "" {
		out = append(out, textLines(r.Description, w, sPlain)...)
	} else {
		out = append(out, line{sg("  No description", sDim)})
	}
	if kids := d.children(re, r.ID); len(kids) > 0 {
		done, total := d.progress(re, r.ID)
		head := line{sg(" Children ", sHead)}
		head = append(head, bar(done, total, 10)...)
		out = append(out, nil, append(head, sg(" "+itoa(done)+"/"+itoa(total), sSoft)))
		for _, k := range kids {
			out = append(out, m.childLine(re, k, w))
		}
	}
	return out
}

// childLine is a card's child in a detail or layout B's right pane.
func (m *Model) childLine(re *api.RepoEpics, k *api.EpicRow, w int) line {
	d := m.d
	status := d.rowStatus(re, k)
	ts := sPlain
	if status == "closed" {
		ts = sDim
	}
	left := line{sg("  ", sPlain), statusGlyph(status), sg(" "+padRight(clean(k.ID), 12)+" ", sDim), sg(clean(d.rowTitle(re, k)), ts)}
	var right line
	switch {
	case k.Human:
		right = line{sg(" "+gHuman+" you ", sOrange)}
	case k.Assignee != "":
		right = line{sg(" @"+clean(k.Assignee)+" ", sCyan)}
	}
	return lr(left, right, w)
}

// priority is "P0" to "P4"; Critical in red and High in yellow, as the app.
func priority(p int) seg {
	st := sSoft
	switch p {
	case 0:
		st = sRed
	case 1:
		st = sYellow
	}
	return sg("  P"+itoa(p), st)
}
