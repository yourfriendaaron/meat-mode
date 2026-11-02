package view

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// The To-dos groups, ported from web/src/model.ts: the app's Today view,
// with its labels, its evening hour and its nesting of sub-tasks.

// groupID is one of the app's groups.
type groupID string

const (
	gPastDue  groupID = "pastdue"
	gToday    groupID = "today"
	gEvening  groupID = "evening"
	gTomorrow groupID = "tomorrow"
	gWeek     groupID = "week"
	gLater    groupID = "later"
	gNoDate   groupID = "nodate"
	gDone     groupID = "done"
)

// groupLabels are GROUP_LABELS, with the Today view's "Any time" for no date.
var groupLabels = map[groupID]string{
	gPastDue:  "Past due",
	gToday:    "Today",
	gEvening:  "This evening",
	gTomorrow: "Tomorrow",
	gWeek:     "This week",
	gLater:    "Later",
	gNoDate:   "Any time",
	gDone:     "Done today",
}

// todayGroups is VIEW_GROUPS.today: undated to-dos come before far-off ones.
var todayGroups = []groupID{gPastDue, gToday, gEvening, gTomorrow, gWeek, gNoDate, gLater, gDone}

// allGroups is VIEW_GROUPS.all, the order the All tab reads groups in.
var allGroups = []groupID{gPastDue, gToday, gEvening, gTomorrow, gWeek, gLater, gNoDate, gDone}

// agenda is the groups the Today view takes from every list.
var agenda = []groupID{gPastDue, gToday, gEvening, gDone}

// eveningHour is the hour from which a task counts as this evening.
const eveningHour = 17

// hubBridge is the bridge name under which the hub keeps its own lists.
const hubBridge = "hub"

// dismissed is the close reason of a deleted personal item.
const dismissed = "Dismissed"

func isOpen(i *api.Item) bool { return i.Status != "closed" }

// isPersonal: on a list the hub keeps itself, the personal list or a project.
func isPersonal(bridge string) bool { return bridge == hubBridge }

func isDeleted(i *api.Item) bool {
	return !isOpen(i) && i.CloseReason == dismissed && isPersonal(i.Bridge)
}

func isWontDo(i *api.Item) bool {
	return !isOpen(i) && i.CloseReason == dismissed && !isPersonal(i.Bridge)
}

func isSnoozed(i *api.Item, now time.Time) bool {
	return i.DeferUntil != nil && i.DeferUntil.After(now)
}

type whenKind int

const (
	whenNone whenKind = iota
	whenSnoozed
	whenDue
	whenResurfaced
)

// whenOf is the moment that places an item: a future snooze wins, then the
// due date, then an expired snooze (the item has resurfaced).
func whenOf(i *api.Item, now time.Time) (time.Time, whenKind) {
	loc := now.Location()
	if i.DeferUntil != nil && i.DeferUntil.After(now) {
		return i.DeferUntil.In(loc), whenSnoozed
	}
	if i.DueAt != nil {
		return i.DueAt.In(loc), whenDue
	}
	if i.DeferUntil != nil {
		return i.DeferUntil.In(loc), whenResurfaced
	}
	return time.Time{}, whenNone
}

// groupOf is the group an item falls in, or "" for none (closed before today).
func groupOf(i *api.Item, now time.Time) groupID {
	if !isOpen(i) {
		if i.ClosedAt != nil && sameDay(i.ClosedAt.In(now.Location()), now) {
			return gDone
		}
		return ""
	}
	d, kind := whenOf(i, now)
	switch {
	case kind == whenNone:
		return gNoDate
	case kind == whenResurfaced:
		if d.Hour() >= eveningHour && sameDay(d, now) {
			return gEvening
		}
		return gToday
	case d.Before(now):
		return gPastDue
	case sameDay(d, now):
		if d.Hour() >= eveningHour {
			return gEvening
		}
		return gToday
	case sameDay(d, addDays(now, 1)):
		return gTomorrow
	case d.Before(nextMonday(now)):
		return gWeek
	}
	return gLater
}

func sortKey(i *api.Item, now time.Time) int64 {
	if d, kind := whenOf(i, now); kind != whenNone {
		return d.UnixMilli()
	}
	return math.MaxInt64
}

// compareItems orders by placing moment, then priority, then title.
func compareItems(now time.Time) func(a, b *api.Item) int {
	return func(a, b *api.Item) int {
		return cmp.Or(
			cmp.Compare(sortKey(a, now), sortKey(b, now)),
			cmp.Compare(a.Priority, b.Priority),
			cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
			cmp.Compare(a.Title, b.Title),
		)
	}
}

// listKey is "<bridge>/<repo>", the key of an item's list.
func listKey(bridge, repo string) string { return bridge + "/" + repo }

// subtasksOf are the item's sub-tasks among items, deleted and won't-do
// ones left out.
func subtasksOf(it *api.Item, items []api.Item) []*api.Item {
	var out []*api.Item
	for k := range items {
		s := &items[k]
		if s.Parent == it.ID && s.Bridge == it.Bridge && s.Repo == it.Repo && !isDeleted(s) && !isWontDo(s) {
			out = append(out, s)
		}
	}
	return out
}

func openSubtasks(it *api.Item, items []api.Item) []*api.Item {
	var out []*api.Item
	for _, s := range subtasksOf(it, items) {
		if isOpen(s) {
			out = append(out, s)
		}
	}
	return out
}

// subtaskChip is "1/3": done sub-tasks of all, or "" with none.
func subtaskChip(it *api.Item, items []api.Item) string {
	subs := subtasksOf(it, items)
	if len(subs) == 0 {
		return ""
	}
	done := 0
	for _, s := range subs {
		if !isOpen(s) {
			done++
		}
	}
	return strconv.Itoa(done) + "/" + strconv.Itoa(len(subs))
}

// nestsUnder is the parent a sub-task shows under: open, top-level and not
// deleted, with the sub-task open too.
func nestsUnder(i *api.Item, byKey map[string]*api.Item) *api.Item {
	if i.Parent == "" || !isOpen(i) {
		return nil
	}
	p := byKey[listKey(i.Bridge, i.Repo)+"/"+i.Parent]
	if p != nil && isOpen(p) && p.Parent == "" {
		return p
	}
	return nil
}

// placedBy is the copy a parent is placed by: its own future snooze wins,
// else the earliest moment among it and its open sub-tasks.
func placedBy(it *api.Item, subs []*api.Item, now time.Time) *api.Item {
	ownAt, own := whenOf(it, now)
	if own == whenSnoozed || len(subs) == 0 {
		return it
	}
	var earliest *time.Time
	for _, s := range subs {
		if d, k := whenOf(s, now); k != whenNone && (earliest == nil || d.Before(*earliest)) {
			earliest = &d
		}
	}
	if earliest == nil || (own != whenNone && !ownAt.After(*earliest)) {
		return it
	}
	c := *it
	c.DueAt = earliest
	c.DeferUntil = nil
	return &c
}

// group is one group of the To-dos list, in display order.
type group struct {
	id    groupID
	items []*api.Item
	// nested marks the keys shown as sub-tasks under the item before them.
	nested map[string]bool
}

// buildGroups is buildView: the Today view's groups when today is set (the
// agenda of every list, then the rest of the hub's own lists), else every
// group of every list as the All view orders them.
func buildGroups(items []api.Item, now time.Time, today bool) []group {
	var live []*api.Item
	byKey := map[string]*api.Item{}
	for k := range items {
		i := &items[k]
		if isDeleted(i) {
			continue
		}
		live = append(live, i)
		byKey[i.Key] = i
	}
	subs := map[string][]*api.Item{}
	for _, i := range live {
		if p := nestsUnder(i, byKey); p != nil {
			subs[p.Key] = append(subs[p.Key], i)
		}
	}
	placed := map[string]*api.Item{}
	for _, i := range live {
		if s, ok := subs[i.Key]; ok {
			placed[i.Key] = placedBy(i, s, now)
		}
	}
	place := func(i *api.Item) *api.Item {
		if p, ok := placed[i.Key]; ok {
			return p
		}
		return i
	}
	order := allGroups
	if today {
		order = todayGroups
	}
	buckets := map[groupID][]*api.Item{}
	for _, i := range live {
		if nestsUnder(i, byKey) != nil {
			continue
		}
		g := groupOf(place(i), now)
		if today && !isPersonal(i.Bridge) && !slices.Contains(agenda, cmp.Or(g, gNoDate)) {
			continue
		}
		if g == "" || !slices.Contains(order, g) {
			continue
		}
		buckets[g] = append(buckets[g], i)
	}
	byPlace := compareItems(now)
	var out []group
	for _, id := range order {
		list := buckets[id]
		if len(list) == 0 {
			continue
		}
		if id == gDone {
			slices.SortStableFunc(list, func(a, b *api.Item) int { return cmp.Compare(closedMilli(b), closedMilli(a)) })
		} else {
			slices.SortStableFunc(list, func(a, b *api.Item) int { return byPlace(place(a), place(b)) })
		}
		g := group{id: id}
		for _, i := range list {
			g.items = append(g.items, i)
			s := slices.Clone(subs[i.Key])
			slices.SortStableFunc(s, byPlace)
			for _, c := range s {
				g.items = append(g.items, c)
				if g.nested == nil {
					g.nested = map[string]bool{}
				}
				g.nested[c.Key] = true
			}
		}
		out = append(out, g)
	}
	return out
}

func closedMilli(i *api.Item) int64 {
	if i.ClosedAt == nil {
		return 0
	}
	return i.ClosedAt.UnixMilli()
}

// openToday counts the open tasks on today's agenda: past due, today and
// this evening.
func openToday(items []api.Item, now time.Time) int {
	n := 0
	for k := range items {
		switch groupOf(&items[k], now) {
		case gPastDue, gToday, gEvening:
			n++
		}
	}
	return n
}

// chipTone colours a date chip.
type chipTone int

const (
	toneSoon chipTone = iota
	toneDate
	toneOverdue
	toneSnoozed
)

// dateChip is the app's date chip for an item: its text and tone, or ok
// false for none (closed, undated, or a snooze that has passed).
func dateChip(i *api.Item, now time.Time) (text string, tone chipTone, ok bool) {
	if !isOpen(i) {
		return "", 0, false
	}
	d, kind := whenOf(i, now)
	switch {
	case kind == whenNone || kind == whenResurfaced:
		return "", 0, false
	case kind == whenSnoozed:
		return fmtWhen(d, now), toneSnoozed, true
	case d.Before(now):
		return fmtOverdue(d, now), toneOverdue, true
	case sameDay(d, now):
		return fmtWhen(d, now), toneSoon, true
	}
	return fmtWhen(d, now), toneDate, true
}

// repoCan reports whether a list's tracker can apply kind; a list without
// actions takes every kind.
func repoCan(r *api.Repo, kind string) bool {
	return r == nil || r.Actions == nil || slices.Contains(r.Actions, kind)
}

// repoLabel is a repo's display name: the last segment of its path.
func repoLabel(name string) string { return name[strings.LastIndex(name, "/")+1:] }

// codeWidth is the cells a list's badge takes: three, so the tile reads as
// a square in a terminal, whose cells are twice as tall as they are wide.
const codeWidth = 3

// codeRune reports whether r can stand in a list's code: a letter or digit
// that draws one cell wide in every terminal.
func codeRune(r rune) bool {
	return (unicode.IsLetter(r) || unicode.IsDigit(r)) && r < 0x1100 && lipgloss.Width(string(r)) == 1
}

// codeCandidates are the codes a list's label can take, best first: its
// first three letters or digits, then the first two with the initial of
// each later word, then the first two with each later character. Leading
// punctuation is skipped, so ".agents" is AGE.
func codeCandidates(name string) []string {
	var runes []rune
	var initials []rune // of the words after the first
	word := false
	for _, r := range strings.ToUpper(repoLabel(name)) {
		if !codeRune(r) {
			word = false
			continue
		}
		if !word && len(runes) > 0 {
			initials = append(initials, r)
		}
		word = true
		runes = append(runes, r)
	}
	if len(runes) <= codeWidth {
		if len(runes) == 0 {
			return []string{"?"}
		}
		return []string{string(runes)}
	}
	out := []string{string(runes[:codeWidth])}
	for _, r := range initials {
		out = append(out, string(runes[:2])+string(r))
	}
	for _, r := range runes[codeWidth:] {
		out = append(out, string(runes[:2])+string(r))
	}
	return out
}

// listCodes gives each visible list other than the personal list its
// code, in the order the app lists them: the first candidate no earlier
// list holds, else its first. Keyed by listKey.
func listCodes(repos []api.Repo) map[string]string {
	out := map[string]string{}
	held := map[string]bool{}
	for _, r := range visibleRepos(repos) {
		if isPersonalList(r) {
			continue
		}
		cs := codeCandidates(r.Name)
		code := cs[0]
		for _, c := range cs {
			if !held[c] {
				code = c
				break
			}
		}
		held[code] = true
		out[listKey(r.Bridge, r.Name)] = code
	}
	return out
}

// badgeText is a code centred in the badge's cells.
func badgeText(code string) string {
	n := lipgloss.Width(code)
	if n >= codeWidth {
		return code
	}
	left := (codeWidth - n) / 2
	return strings.Repeat(" ", left) + code + strings.Repeat(" ", codeWidth-n-left)
}

// visibleRepos are the repos shown as lists: the hub's own and opted-in
// ones, the personal list first, then by label, name and bridge.
func visibleRepos(repos []api.Repo) []*api.Repo {
	var out []*api.Repo
	for k := range repos {
		if r := &repos[k]; isPersonal(r.Bridge) || r.OptedIn {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b *api.Repo) int {
		pa, pb := isPersonalList(a), isPersonalList(b)
		if pa != pb {
			if pa {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(repoLabel(a.Name), repoLabel(b.Name)), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Bridge, b.Bridge))
	})
	return out
}

// isPersonalList reports whether a repo is the personal list itself.
func isPersonalList(r *api.Repo) bool { return isPersonal(r.Bridge) && r.Personal }

// listColours is the number of list colour slots.
const listColours = 6

// listSlot is a list's colour slot: the hub's colour, or for an older hub
// the slot by the order the app lists the repos.
func listSlot(r *api.Repo, repos []api.Repo) int {
	if r.Colour != nil {
		return ((*r.Colour % listColours) + listColours) % listColours
	}
	for n, v := range visibleRepos(repos) {
		if v.Bridge == r.Bridge && v.Name == r.Name {
			return n % listColours
		}
	}
	return 0
}

// listDetail is the secondary words for a repo whose label repeats among
// the visible ones: its path when that tells it apart, and its machine
// when the same path is on several (repoDetail in model.ts).
func listDetail(r *api.Repo, repos []api.Repo) string {
	label := repoLabel(r.Name)
	var same []*api.Repo
	for _, x := range visibleRepos(repos) {
		if repoLabel(x.Name) == label {
			same = append(same, x)
		}
	}
	if len(same) < 2 {
		return ""
	}
	var parts []string
	if r.Name != label {
		parts = append(parts, r.Name)
	}
	for _, x := range same {
		if x.Name == r.Name && x.Bridge != r.Bridge {
			if isPersonal(r.Bridge) && !r.Personal {
				parts = append(parts, "project")
			} else {
				parts = append(parts, "on "+r.Bridge)
			}
			break
		}
	}
	return strings.Join(parts, " ")
}
