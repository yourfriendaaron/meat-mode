package view

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// target is what a row stands for: an item, an epic row, or a human epic
// row that acts as its item (item and row both set).
type target struct {
	key  string // the item key, or the epic row's "<bridge>/<repo>/<id>"
	item *api.Item
	repo *api.RepoEpics // epic rows only
	row  *api.EpicRow
	card bool // an epic card: a row with children
}

// readOnly is an epic row that is not also an item.
func (t *target) readOnly() bool { return t.item == nil }

// id is the bead id shown for the target.
func (t *target) id() string {
	if t.item != nil {
		return t.item.ID
	}
	return t.row.ID
}

func (t *target) title() string {
	if t.item != nil {
		return t.item.Title
	}
	return t.row.Title
}

// row is one list row: a heading, or a selectable target.
type row struct {
	id   string // the selection's key: the target's key, or a path for a child in the tree
	head line   // a heading's segments; nil for a target
	t    *target
	draw func(m *Model, w int) line
	// lead is an epic row's line up to its title, for opening it in place
	// (Model.openSelected); nil for a row that does not open.
	lead func(w int) line
	// facts marks a line of the selected row opened in place.
	facts bool
}

func headRow(segs ...seg) row { return row{head: line(segs)} }

// selectable reports whether a row can hold the selection.
func (r row) selectable() bool { return r.t != nil }

// data is the state and epics, indexed for the row builders.
type data struct {
	state api.State
	epics api.Epics
	now   time.Time
	items map[string]*api.Item
	repos map[string]*api.Repo // by "<bridge>/<repo>"
	codes map[string]string    // each visible list's badge code, by "<bridge>/<repo>"
	// blockers caches what each epic row waits on, per repo (blockerMap).
	blockers map[*api.RepoEpics]map[string][]string
}

func index(s api.State, e api.Epics, now time.Time) *data {
	d := &data{state: s, epics: e, now: now, items: map[string]*api.Item{}, repos: map[string]*api.Repo{}}
	for k := range d.state.Items {
		d.items[d.state.Items[k].Key] = &d.state.Items[k]
	}
	for k := range d.state.Repos {
		r := &d.state.Repos[k]
		d.repos[listKey(r.Bridge, r.Name)] = r
	}
	d.codes = listCodes(d.state.Repos)
	return d
}

// rowKey is an epic row's key, the same form as an item's.
func rowKey(re *api.RepoEpics, r *api.EpicRow) string { return re.Bridge + "/" + re.Repo + "/" + r.ID }

// epicTarget is the target for an epic row: the item it acts as, when it
// is human and the state holds that item.
func (d *data) epicTarget(re *api.RepoEpics, r *api.EpicRow) *target {
	t := &target{key: rowKey(re, r), repo: re, row: r, card: r.Epic || len(d.children(re, r.ID)) > 0}
	if r.Human {
		t.item = d.items[t.key]
	}
	return t
}

// children are a card's direct children, sorted as the app sorts them,
// closed ones last.
func (d *data) children(re *api.RepoEpics, id string) []*api.EpicRow {
	var out []*api.EpicRow
	for k := range re.Rows {
		if re.Rows[k].Parent == id {
			out = append(out, &re.Rows[k])
		}
	}
	d.sortRows(re, out)
	return out
}

// statusRank orders row states by what needs a hand first, as the web's
// NEED_RANK: blocked, in progress, open, waiting, deferred, then closed.
func statusRank(s string) int {
	switch s {
	case "blocked":
		return 0
	case "in_progress":
		return 1
	case "open":
		return 2
	case "waiting":
		return 3
	case "deferred":
		return 4
	}
	return 5
}

// rowState is the state a row shows: its status, or "waiting" for an open
// bead that waits on another.
func (d *data) rowState(re *api.RepoEpics, r *api.EpicRow) string {
	s := d.rowStatus(re, r)
	if s == "open" && len(r.BlockedBy) > 0 {
		return "waiting"
	}
	return s
}

// rowStatus is the status a row shows: its item's when it acts as one.
func (d *data) rowStatus(re *api.RepoEpics, r *api.EpicRow) string {
	if r.Human {
		if it := d.items[rowKey(re, r)]; it != nil {
			return it.Status
		}
	}
	return r.Status
}

func (d *data) rowTitle(re *api.RepoEpics, r *api.EpicRow) string {
	if r.Human {
		if it := d.items[rowKey(re, r)]; it != nil {
			return it.Title
		}
	}
	return r.Title
}

// sortRows sorts as the app's Epics view: closed last, then priority, then
// status, then the most recently updated.
func (d *data) sortRows(re *api.RepoEpics, rows []*api.EpicRow) {
	slices.SortStableFunc(rows, func(a, b *api.EpicRow) int {
		sa, sb := d.rowState(re, a), d.rowState(re, b)
		return cmp.Or(
			cmp.Compare(boolInt(sa == "closed"), boolInt(sb == "closed")),
			cmp.Compare(a.Priority, b.Priority),
			cmp.Compare(statusRank(sa), statusRank(sb)),
			b.UpdatedAt.Compare(a.UpdatedAt),
			cmp.Compare(a.ID, b.ID),
		)
	})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// progress is a card's closed direct children over all of them.
func (d *data) progress(re *api.RepoEpics, id string) (done, total int) {
	for _, k := range d.children(re, id) {
		total++
		if d.rowStatus(re, k) == "closed" {
			done++
		}
	}
	return done, total
}

// cards are a repo's epic cards, and tasks its top-level rows that are no
// card, each sorted.
func (d *data) cardsAndTasks(re *api.RepoEpics) (cards, tasks []*api.EpicRow) {
	for k := range re.Rows {
		r := &re.Rows[k]
		switch {
		case r.Epic || len(d.children(re, r.ID)) > 0:
			cards = append(cards, r)
		case r.Parent == "":
			tasks = append(tasks, r)
		}
	}
	d.sortRows(re, cards)
	d.sortRows(re, tasks)
	return cards, tasks
}

// cardAt is a card in a list, and how deep it sits under other cards.
type cardAt struct {
	row   *api.EpicRow
	depth int
}

// cardTree is a repo's cards as the lists show them: each top-level card,
// then the cards nested in it one level deeper, each level sorted. With a
// filter, a card shows when it or one of its children matches, or when a
// card nested in it shows.
func (d *data) cardTree(q string, re *api.RepoEpics) []cardAt {
	cards, _ := d.cardsAndTasks(re)
	isCard := make(map[string]bool, len(cards))
	for _, c := range cards {
		isCard[c.ID] = true
	}
	var out []cardAt
	var walk func(c *api.EpicRow, depth int)
	walk = func(c *api.EpicRow, depth int) {
		at := len(out)
		out = append(out, cardAt{c, depth})
		if depth < len(cards) {
			for _, k := range cards {
				if k.Parent == c.ID && k.ID != c.ID {
					walk(k, depth+1)
				}
			}
		}
		if len(out) == at+1 && !d.rowMatches(q, re, c) {
			out = out[:at]
		}
	}
	for _, c := range cards {
		if !isCard[c.Parent] || c.Parent == c.ID {
			walk(c, 0)
		}
	}
	return out
}

// cardCount is the number of epic cards across every repo, for the tab.
func (d *data) cardCount() int {
	n := 0
	for k := range d.epics.Repos {
		c, _ := d.cardsAndTasks(&d.epics.Repos[k])
		n += len(c)
	}
	return n
}

// epicsSwitchedOn reports whether any repo has its epics on.
func (d *data) epicsSwitchedOn() bool {
	for _, r := range d.state.Repos {
		if r.EpicsOn {
			return true
		}
	}
	return len(d.epics.Repos) > 0
}

// repoOf is the State repo of a list, or nil.
func (d *data) repoOf(bridge, name string) *api.Repo { return d.repos[listKey(bridge, name)] }

// matches is the filter: title or id, without case.
func matches(q, title, id string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	return strings.Contains(strings.ToLower(title), q) || strings.Contains(strings.ToLower(id), q)
}

// rowMatches is the filter for an epic row: it, or for a card any child.
func (d *data) rowMatches(q string, re *api.RepoEpics, r *api.EpicRow) bool {
	if matches(q, d.rowTitle(re, r), r.ID) {
		return true
	}
	for _, k := range d.children(re, r.ID) {
		if matches(q, d.rowTitle(re, k), k.ID) {
			return true
		}
	}
	return false
}

// ---- To-dos

// todoRows are the Today view's groups, as rows.
func (d *data) todoRows(q string) []row {
	var rows []row
	for _, g := range buildGroups(d.state.Items, d.now, true) {
		var its []*api.Item
		for _, it := range g.items {
			if matches(q, it.Title, it.ID) {
				its = append(its, it)
			}
		}
		if len(its) == 0 {
			continue
		}
		hs := sHead
		if g.id == gPastDue {
			hs = style{fg: cRed, bold: true}
		}
		rows = append(rows, headRow(sg(" "+groupLabels[g.id]+" ", hs), sg(itoa(len(its)), sDim)))
		for _, it := range its {
			rows = append(rows, d.itemRow(it, g.nested[it.Key]))
		}
	}
	return rows
}

func (d *data) itemRow(it *api.Item, nested bool) row {
	t := &target{key: it.Key, item: it}
	return row{id: it.Key, t: t, draw: func(m *Model, w int) line { return m.itemLine(it, w, nested) }}
}

// ---- Epics, layout A: a tree under each repo

func (d *data) epicTreeRows(q string, expanded map[string]bool) []row {
	var rows []row
	for k := range d.epics.Repos {
		re := &d.epics.Repos[k]
		_, tasks := d.cardsAndTasks(re)
		if q != "" && !d.repoMatches(q, re) {
			continue
		}
		rows = append(rows, d.repoHead(re))
		n := 0
		var add func(parent string, r *api.EpicRow, depth int)
		add = func(parent string, r *api.EpicRow, depth int) {
			t := d.epicTarget(re, r)
			id := t.key
			if parent != "" {
				id = parent + ">" + t.key
			}
			// A card nested in another shows once, as a card of its own
			// under its parent's tasks, one level deeper.
			foldable := t.card && parent == ""
			open := foldable && (expanded[id] || q != "")
			rows = append(rows, row{id: id, t: t, draw: func(m *Model, w int) line {
				return m.epicLine(re, r, depth, t.card, foldable, open, w)
			}, lead: func(int) line { return d.epicLead(re, r, depth, foldable, open) }})
			n++
			if open {
				for _, c := range d.children(re, r.ID) {
					if d.epicTarget(re, c).card {
						continue
					}
					if q == "" || matches(q, d.rowTitle(re, c), c.ID) {
						add(id, c, depth+1)
					}
				}
			}
		}
		for g, bs := range groupBlocks(d.cardBlocks(q, re)) {
			if len(bs) == 0 {
				continue
			}
			rows = append(rows, groupHead(g, blockCards(bs), "   "))
			for _, b := range bs {
				for _, c := range b.cards {
					add("", c.row, c.depth)
				}
			}
		}
		shown := n
		var top []*api.EpicRow
		for _, t := range tasks {
			if matches(q, d.rowTitle(re, t), t.ID) {
				top = append(top, t)
			}
		}
		if len(top) > 0 {
			rows = append(rows, headRow(sg("   No epic", sDim)))
			for _, t := range top {
				add("", t, 0)
			}
		}
		if shown == 0 && len(top) == 0 {
			rows = append(rows, headRow(sg("   Nothing open.", sDim)))
		}
	}
	return rows
}

func (d *data) repoHead(re *api.RepoEpics) row {
	return headRow(sg(" ", sPlain), d.mark(re.Bridge, re.Repo), sg(" "+clean(re.Repo), sBold), sg("  synced "+fmtAgo(re.SyncedAt, d.now), sDim))
}

// ---- Epics, layout B: cards on the left, then the top-level tasks

func (d *data) epicListRows(q string) []row {
	var tops []row
	var blocks []cardBlock
	for k := range d.epics.Repos {
		re := &d.epics.Repos[k]
		_, tasks := d.cardsAndTasks(re)
		blocks = append(blocks, d.cardBlocks(q, re)...)
		for _, t := range tasks {
			if matches(q, d.rowTitle(re, t), t.ID) {
				tops = append(tops, d.cardRow(re, t, 0))
			}
		}
	}
	rows := d.groupedRows(blocks, " ")
	if len(tops) > 0 {
		rows = append(rows, headRow(sg(" No epic ", sHead), sg(itoa(len(tops)), sDim)))
		rows = append(rows, tops...)
	}
	return rows
}

func (d *data) cardRow(re *api.RepoEpics, r *api.EpicRow, depth int) row {
	t := d.epicTarget(re, r)
	return row{id: t.key, t: t, draw: func(m *Model, w int) line { return m.cardLine(re, r, depth, w) },
		lead: func(int) line { return d.cardLead(re, r, depth) }}
}

// groupedRows are cards in blocks under their group headings, each heading
// indented by pad.
func (d *data) groupedRows(blocks []cardBlock, pad string) []row {
	var rows []row
	for g, bs := range groupBlocks(blocks) {
		if len(bs) == 0 {
			continue
		}
		rows = append(rows, groupHead(g, blockCards(bs), pad))
		for _, b := range bs {
			for _, c := range b.cards {
				rows = append(rows, d.cardRow(b.repo, c.row, c.depth))
			}
		}
	}
	return rows
}

// blockCards is the number of cards in blocks.
func blockCards(bs []cardBlock) int {
	n := 0
	for _, b := range bs {
		n += len(b.cards)
	}
	return n
}

// ---- All, layout C

// allRows are Waiting on you, Later and the epics; hidden counts the
// snoozed and done items left out.
func (d *data) allRows(q string) (rows []row, hidden int) {
	var waiting, later []row
	nw, nl := 0, 0
	for _, g := range buildGroups(d.state.Items, d.now, false) {
		// A nested sub-task goes where its parent went.
		dest := ""
		for _, it := range g.items {
			nested := g.nested[it.Key]
			if !nested {
				switch {
				case g.id == gDone || isSnoozed(it, d.now):
					dest = "hidden"
				case g.id == gPastDue || g.id == gToday || g.id == gEvening || (g.id == gNoDate && !isPersonal(it.Bridge)):
					dest = "waiting"
				default:
					dest = "later"
				}
			}
			if dest == "hidden" {
				hidden++
				continue
			}
			if !matches(q, it.Title, it.ID) {
				continue
			}
			r := d.itemRow(it, nested)
			if dest == "waiting" {
				waiting = append(waiting, r)
				if !nested {
					nw++
				}
			} else {
				later = append(later, r)
				if !nested {
					nl++
				}
			}
		}
	}
	rows = append(rows, headRow(sg(" Waiting on you ", style{fg: cOrange, bold: true}), sg(itoa(nw), sDim)))
	if len(waiting) == 0 {
		rows = append(rows, headRow(sg("   Nothing waiting on you.", sDim)))
	}
	rows = append(rows, waiting...)
	rows = append(rows, headRow(), headRow(sg(" Later ", sHead), sg(itoa(nl), sDim)))
	rows = append(rows, later...)
	var blocks []cardBlock
	for k := range d.epics.Repos {
		blocks = append(blocks, d.cardBlocks(q, &d.epics.Repos[k])...)
	}
	if len(blocks) > 0 || d.epicsSwitchedOn() {
		rows = append(rows, headRow(), headRow(sg(" Epics ", sHead), sg(itoa(blockCards(blocks)), sDim)))
		rows = append(rows, d.groupedRows(blocks, "  ")...)
	}
	return rows, hidden
}

// drillStatuses are the groups of a drilled card's children.
var drillStatuses = []struct{ status, label string }{
	{"blocked", "Blocked"}, {"in_progress", "In progress"}, {"open", "Open"}, {"waiting", "Waiting"}, {"deferred", "Deferred"}, {"closed", "Closed"},
}

// drillRows are a card's children grouped by status, under its bar and
// description.
func (d *data) drillRows(q string, t *target, w int) []row {
	re, card := t.repo, t.row
	done, total := d.progress(re, card.ID)
	rows := []row{headRow(append(append(line{sg(" ", sPlain)}, d.stateBar(re, card.ID, 16)...),
		sg(" "+itoa(done)+"/"+itoa(total)+" done", sSoft), sg("  "+clean(card.ID)+" ∙ "+clean(re.Repo), sDim))...)}
	if card.Description != "" {
		for _, l := range wrap(card.Description, w-3) {
			rows = append(rows, headRow(sg("  "+l, sSoft)))
		}
	}
	for _, l := range notesLines(card, w, sSoft) {
		rows = append(rows, headRow(l...))
	}
	rows = append(rows, headRow())
	kids := d.children(re, card.ID)
	for _, st := range drillStatuses {
		var g []*api.EpicRow
		for _, k := range kids {
			if d.rowState(re, k) == st.status && matches(q, d.rowTitle(re, k), k.ID) {
				g = append(g, k)
			}
		}
		if len(g) == 0 {
			continue
		}
		rows = append(rows, headRow(sg(" "+st.label+" ", sHead), sg(itoa(len(g)), sDim)))
		for _, k := range g {
			kt := d.epicTarget(re, k)
			rows = append(rows, row{id: kt.key, t: kt, draw: func(m *Model, w int) line {
				return m.epicLine(re, k, 0, kt.card, false, false, w)
			}, lead: func(int) line { return d.epicLead(re, k, 0, false, false) }})
		}
	}
	if len(kids) == 0 {
		rows = append(rows, headRow(sg("  No children yet.", sDim)))
	}
	return rows
}

// findRow is the epic row with key, and its repo.
func (d *data) findRow(key string) (*api.RepoEpics, *api.EpicRow) {
	for k := range d.epics.Repos {
		re := &d.epics.Repos[k]
		if !strings.HasPrefix(key, re.Bridge+"/"+re.Repo+"/") {
			continue
		}
		id := key[len(re.Bridge)+len(re.Repo)+2:]
		for j := range re.Rows {
			if re.Rows[j].ID == id {
				return re, &re.Rows[j]
			}
		}
	}
	return nil, nil
}

// resolve finds a target again by key in fresh data: an epic row when
// isRow, else an item.
func (d *data) resolve(key string, isRow bool) *target {
	if isRow {
		if re, r := d.findRow(key); r != nil {
			return d.epicTarget(re, r)
		}
		return nil
	}
	if it := d.items[key]; it != nil {
		return &target{key: key, item: it}
	}
	return nil
}

// repoMatches reports whether any row of a repo matches the filter.
func (d *data) repoMatches(q string, re *api.RepoEpics) bool {
	for k := range re.Rows {
		if matches(q, d.rowTitle(re, &re.Rows[k]), re.Rows[k].ID) {
			return true
		}
	}
	return false
}
