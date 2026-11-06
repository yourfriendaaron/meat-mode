package view

import (
	"slices"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Epic groups: what can be done about a card, in the order the lists show
// them. A card nested in another goes where its top-level card goes.
const (
	grpBlocked = iota
	grpMoving
	grpReady
	grpWaiting
	grpDeferred
	grpDone
)

var epicGroups = []struct {
	glyph, label string
	st           style
}{
	grpBlocked:  {gBlocked, "Blocked", style{fg: cRed, bold: true}},
	grpMoving:   {gProgress, "Moving", style{fg: cCyan, bold: true}},
	grpReady:    {gOpen, "Ready to start", style{fg: cFg, bold: true}},
	grpWaiting:  {gBlocked, "Waiting on another epic", style{fg: cOrange, bold: true}},
	grpDeferred: {gDeferred, "Deferred", style{fg: cDim, bold: true}},
	grpDone:     {gClosed, "Done", style{fg: cGreen, bold: true}},
}

// cardGroup is the group a card shows in: its own state, and for an open
// card Moving when a child is in progress, else Ready to start.
func (d *data) cardGroup(re *api.RepoEpics, r *api.EpicRow) int {
	switch d.rowState(re, r) {
	case "blocked":
		return grpBlocked
	case "in_progress":
		return grpMoving
	case "waiting":
		return grpWaiting
	case "deferred":
		return grpDeferred
	case "closed":
		return grpDone
	}
	for _, k := range d.children(re, r.ID) {
		if d.rowState(re, k) == "in_progress" {
			return grpMoving
		}
	}
	return grpReady
}

// groupHead is a group's heading, "◒ Moving 1", indented by pad.
func groupHead(g, n int, pad string) row {
	eg := epicGroups[g]
	return headRow(sg(pad+eg.glyph+" "+eg.label, eg.st), sg(" "+itoa(n), sDim))
}

// cardBlock is a top-level card and the cards nested in it, as cardTree
// lists them, with the group the top-level card is in.
type cardBlock struct {
	repo  *api.RepoEpics
	cards []cardAt
	group int
	tier  int
}

// cardBlocks are a repo's cards as blocks, in the lists' order.
func (d *data) cardBlocks(q string, re *api.RepoEpics) []cardBlock {
	var out []cardBlock
	for _, c := range d.cardTree(q, re) {
		if c.depth == 0 || len(out) == 0 {
			out = append(out, cardBlock{repo: re, group: d.cardGroup(re, c.row), tier: d.tier(re, c.row.ID)})
		}
		b := &out[len(out)-1]
		b.cards = append(b.cards, c)
	}
	return out
}

// groupBlocks orders blocks by group, keeping their order within a group,
// except Waiting, which goes in the order the cards can start.
func groupBlocks(bs []cardBlock) [][]cardBlock {
	groups := make([][]cardBlock, len(epicGroups))
	for _, b := range bs {
		groups[b.group] = append(groups[b.group], b)
	}
	slices.SortStableFunc(groups[grpWaiting], func(a, b cardBlock) int { return a.tier - b.tier })
	return groups
}

// blockerMap is what each row of a repo waits on, by id, built once per
// repo for the data's life.
func (d *data) blockerMap(re *api.RepoEpics) map[string][]string {
	if m, ok := d.blockers[re]; ok {
		return m
	}
	m := make(map[string][]string, len(re.Rows))
	for k := range re.Rows {
		if len(re.Rows[k].BlockedBy) > 0 {
			m[re.Rows[k].ID] = re.Rows[k].BlockedBy
		}
	}
	if d.blockers == nil {
		d.blockers = map[*api.RepoEpics]map[string][]string{}
	}
	d.blockers[re] = m
	return m
}

// directBlockers are the beads a row waits on that are not also waited on
// through another of them: gd-4 waits on gd-1, gd-2 and gd-3, but gd-3
// waits on the other two, so gd-4 says "after gd-3". A bead outside
// the set waits on nothing the viewer knows of.
func (d *data) directBlockers(re *api.RepoEpics, r *api.EpicRow) []string {
	if len(r.BlockedBy) < 2 {
		return r.BlockedBy
	}
	by := d.blockerMap(re)
	reach := func(from string) map[string]bool {
		seen := map[string]bool{}
		stack := slices.Clone(by[from])
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[id] {
				continue
			}
			seen[id] = true
			stack = append(stack, by[id]...)
		}
		return seen
	}
	var out []string
	for _, b := range r.BlockedBy {
		implied := false
		for _, o := range r.BlockedBy {
			if o != b && reach(o)[b] {
				implied = true
				break
			}
		}
		if !implied {
			out = append(out, b)
		}
	}
	if len(out) == 0 { // a cycle implies every one; name them all
		return r.BlockedBy
	}
	return out
}

// tier is a row's step in the order of work: 1 when it waits on nothing,
// else one more than the latest step it waits on. A cycle stops counting.
func (d *data) tier(re *api.RepoEpics, id string) int {
	by := d.blockerMap(re)
	memo := map[string]int{}
	var walk func(id string, depth int) int
	walk = func(id string, depth int) int {
		if t, ok := memo[id]; ok {
			return t
		}
		memo[id] = 1 // seen: a cycle back here counts as a first step
		t := 1
		if depth < len(re.Rows) {
			for _, b := range by[id] {
				t = max(t, walk(b, depth+1)+1)
			}
		}
		memo[id] = t
		return t
	}
	return walk(id, 0)
}

// unlocks are the cards that wait directly on id.
func (d *data) unlocks(re *api.RepoEpics, id string) []string {
	var out []string
	for k := range re.Rows {
		r := &re.Rows[k]
		if r.ID == id || !d.epicTarget(re, r).card || d.rowState(re, r) == "closed" {
			continue
		}
		if slices.Contains(d.directBlockers(re, r), id) {
			out = append(out, r.ID)
		}
	}
	return out
}

// childStates are the order and colours of a card's children by state.
var childStates = []struct {
	state, words string
	st           style
}{
	{"in_progress", "in progress", sCyan},
	{"blocked", "blocked", sRed},
	{"waiting", "waiting", sOrange},
	{"open", "open", style{fg: cFg}},
	{"deferred", "deferred", sDim},
	{"closed", "done", sGreen},
}

// childCounts are a card's direct children by state.
func (d *data) childCounts(re *api.RepoEpics, id string) map[string]int {
	n := map[string]int{}
	for _, k := range d.children(re, id) {
		n[d.rowState(re, k)]++
	}
	return n
}

// chips are a card's children by state, "◒2 ⊘1 ✓4", or with words, "◒2 in
// progress ⊘1 waiting ✓4 done".
func (d *data) chips(re *api.RepoEpics, id string, words bool) line {
	n := d.childCounts(re, id)
	var out line
	for _, c := range childStates {
		if n[c.state] == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, sg(" ", sPlain))
		}
		t := glyphFor(c.state) + itoa(n[c.state])
		if words {
			t += " " + c.words
		}
		out = append(out, sg(t, c.st))
	}
	return out
}

func glyphFor(state string) string {
	switch state {
	case "in_progress":
		return gProgress
	case "blocked", "waiting":
		return gBlocked
	case "closed":
		return gClosed
	case "deferred":
		return gDeferred
	}
	return gOpen
}

// stateBar is a card's progress bar w cells wide, a run of cells for each
// state of its children: done filled in green, then in progress, blocked,
// waiting, open and deferred as empty cells in their colours.
func (d *data) stateBar(re *api.RepoEpics, id string, w int) line {
	n := d.childCounts(re, id)
	total := 0
	for _, v := range n {
		total += v
	}
	if total == 0 {
		return line{sg(strings.Repeat(gBarEmpty, w), sDim)}
	}
	order := []string{"closed", "in_progress", "blocked", "waiting", "open", "deferred"}
	cells := make([]int, len(order))
	rest := make([]int, len(order))
	used := 0
	for i, s := range order {
		cells[i] = n[s] * w / total
		rest[i] = n[s] * w % total
		used += cells[i]
	}
	// Largest remainder; a state with any child keeps at least one cell
	// when there is room.
	for used < w {
		best := -1
		for i := range order {
			if n[order[i]] > 0 && (best < 0 || rest[i] > rest[best] || (rest[i] == rest[best] && cells[i] < cells[best])) {
				best = i
			}
		}
		cells[best]++
		rest[best] = -1
		used++
	}
	var out line
	for i, s := range order {
		if cells[i] == 0 {
			continue
		}
		g := gBarEmpty
		if s == "closed" {
			g = gBarFull
		}
		st := sDim
		for _, c := range childStates {
			if c.state == s {
				st = c.st
			}
		}
		out = append(out, sg(strings.Repeat(g, cells[i]), st))
	}
	return out
}

// oneRepo reports whether the epics in view come from one repo, so their
// rows need no list mark.
func (d *data) oneRepo() bool { return len(d.epics.Repos) <= 1 }

// working are the assignees of a card's children in progress.
func (d *data) working(re *api.RepoEpics, id string) []string {
	var out []string
	for _, k := range d.children(re, id) {
		if d.rowState(re, k) == "in_progress" && k.Assignee != "" && !slices.Contains(out, k.Assignee) {
			out = append(out, k.Assignee)
		}
	}
	return out
}

// cardFacts are the lines that say what a card holds, each starting at
// indent: its counts in words, what it starts after and unlocks, and who
// is on it. Layout B's right pane shows them under the card; layouts A and
// C under the selected row.
func (d *data) cardFacts(re *api.RepoEpics, r *api.EpicRow, indent string, w int) []line {
	var out []line
	done, total := d.progress(re, r.ID)
	l := line{sg(indent, sPlain)}
	l = append(l, d.stateBar(re, r.ID, 12)...)
	l = append(l, sg(" "+itoa(done)+"/"+itoa(total), sSoft))
	if total > 0 {
		l = append(append(l, sg("  ", sPlain)), d.chips(re, r.ID, w >= 70)...)
	} else {
		l = append(l, sg("  no tasks yet", sDim))
	}
	out = append(out, l)
	var flow line
	if d.rowState(re, r) == "waiting" {
		direct := d.directBlockers(re, r)
		flow = append(flow, sg("starts after "+clean(strings.Join(direct, ", ")), sOrange))
		if len(r.BlockedBy) > len(direct) {
			flow = append(flow, sg("  "+gDot+" waits on "+clean(strings.Join(r.BlockedBy, ", ")), sDim))
		}
	}
	if u := d.unlocks(re, r.ID); len(u) > 0 {
		if len(flow) > 0 {
			flow = append(flow, sg("  ", sPlain))
		}
		flow = append(flow, sg("unlocks "+clean(strings.Join(u, ", ")), sGreen))
	}
	if len(flow) > 0 {
		out = append(out, append(line{sg(indent, sPlain)}, flow...))
	}
	var misc line
	for _, a := range d.working(re, r.ID) {
		misc = append(misc, sg("@"+clean(a)+" ", sCyan))
	}
	if r.Human {
		misc = append(misc, sg(gHuman+" you ", sOrange))
	}
	if r.Priority <= 1 {
		misc = append(misc, sg(strings.TrimSpace(priority(r.Priority).text)+" ", priority(r.Priority).st))
	}
	misc = append(misc, commentsTag(r)...)
	misc = append(misc, notesTag(r)...)
	if len(misc) > 0 {
		out = append(out, append(line{sg(indent, sPlain)}, misc...))
	}
	return out
}

// openLines are the selected row of layout A opened in place: the
// rest of its title, wrapped, then a card's facts. first is the row's own
// line: its id and the title's first line.
func (m *Model) openLines(re *api.RepoEpics, r *api.EpicRow, lead line, w int) (first line, more []line) {
	d := m.d
	indent := lead.width()
	ts := sBold
	if d.rowStatus(re, r) == "closed" {
		ts = sDim
	}
	tl := wrap(clean(d.rowTitle(re, r)), max(10, w-indent-1))
	first = append(slices.Clone(lead), sg(tl[0], ts))
	pad := strings.Repeat(" ", indent)
	for _, t := range tl[1:] {
		more = append(more, line{sg(pad+t, ts)})
	}
	if d.epicTarget(re, r).card {
		return first, append(more, d.cardFacts(re, r, pad, w)...)
	}
	facts := line{sg(pad, sPlain)}
	facts = append(facts, statusTag(d.rowState(re, r), r, false, d.now)...)
	if r.Assignee != "" {
		facts = append(facts, sg("  @"+clean(r.Assignee), sCyan))
	}
	if r.Human {
		facts = append(facts, sg("  "+gHuman+" you", sOrange))
	}
	facts = append(facts, commentsTag(r)...)
	facts = append(facts, notesTag(r)...)
	return first, append(more, facts)
}
