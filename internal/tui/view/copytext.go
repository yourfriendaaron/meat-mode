package view

import (
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// What y and Y put on the clipboard: text for pasting into an agent's
// session, so it names dates in full rather than relative to now.

// refLine is "<id> <title>"; an item of a project adds its project:
// "mm-4 (projects/garden) Order bark mulch".
func (d *data) refLine(t *target) string {
	if it := t.item; it != nil {
		proj := ""
		if isPersonal(it.Bridge) {
			if r := d.repoOf(it.Bridge, it.Repo); (r == nil && it.Repo != "personal") || (r != nil && !r.Personal) {
				proj = " (" + clean(it.Repo) + ")"
			}
		}
		return clean(it.ID) + proj + " " + clean(it.Title)
	}
	return clean(t.row.ID) + " " + clean(t.row.Title)
}

// refFull is refLine, then the list, due date and tags (or the repo,
// status and priority), then the description, the notes, and the
// sub-tasks or an epic's children as a checklist.
func (d *data) refFull(t *target) string {
	var b strings.Builder
	b.WriteString(d.refLine(t))
	if it := t.item; it != nil {
		meta := []string{d.listName(it.Bridge, it.Repo)}
		if it.Status != "open" {
			meta = append(meta, statusWords(it.Status))
		}
		if it.DueAt != nil {
			meta = append(meta, "due "+fmtFull(it.DueAt.In(d.now.Location())))
		}
		if isOpen(it) && isSnoozed(it, d.now) {
			meta = append(meta, "snoozed until "+fmtFull(it.DeferUntil.In(d.now.Location())))
		}
		if it.Repeat != nil && isPersonal(it.Bridge) {
			meta = append(meta, strings.ToLower(repeatLabel(it.Repeat, d.now)[:1])+repeatLabel(it.Repeat, d.now)[1:])
		}
		if ts := tags(it); len(ts) > 0 {
			meta = append(meta, strings.Join(ts, " "))
		}
		b.WriteString("\n" + strings.Join(meta, " · "))
		if desc := strings.TrimSpace(it.Description); desc != "" {
			b.WriteString("\n\n" + desc)
		}
		if subs := subtasksOf(it, d.state.Items); len(subs) > 0 {
			b.WriteString("\n\nSub-tasks:")
			for _, s := range subs {
				b.WriteString("\n- [" + check(!isOpen(s)) + "] " + clean(s.ID) + " " + clean(s.Title))
			}
		}
		if len(it.Comments) > 0 {
			b.WriteString("\n\nNotes:")
			for _, c := range it.Comments {
				b.WriteString("\n- " + clean(c.Author) + ": " + strings.Join(paragraphs(c.Text), "\n  "))
			}
		}
		return b.String()
	}
	re, r := t.repo, t.row
	b.WriteString("\n" + clean(re.Repo) + " · " + statusWords(r.Status) + " · P" + itoa(r.Priority))
	if desc := strings.TrimSpace(r.Description); desc != "" {
		b.WriteString("\n\n" + desc)
	}
	if kids := d.children(re, r.ID); len(kids) > 0 {
		b.WriteString("\n\nChildren:")
		for _, k := range kids {
			b.WriteString("\n- [" + check(d.rowStatus(re, k) == "closed") + "] " + clean(k.ID) + " " + clean(d.rowTitle(re, k)))
		}
	}
	return b.String()
}

func check(done bool) string {
	if done {
		return "x"
	}
	return " "
}

// repeatLabel is a repeat in words, as web/src/repeat.ts repeatLabel:
// "Every day", "Every 2 weeks on Mon, Thu", "Every weekday", "Every month
// after done", "Every week on Sun until Nov 26".
func repeatLabel(r *api.Repeat, now time.Time) string {
	n := max(r.Interval, 1)
	var days []int
	if r.Every == api.EveryWeek && !r.FromDone {
		days = slices.Clone(r.Weekdays)
		slices.SortFunc(days, func(a, b int) int { return (a+6)%7 - (b+6)%7 })
	}
	weekOf := map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", -1: "last"}
	var out string
	switch {
	case weekOf[r.WeekOfMonth] != "" && len(r.Weekdays) == 1 && r.Weekdays[0] >= 0 && r.Weekdays[0] <= 6:
		if n == 1 {
			out = "Every month"
		} else {
			out = "Every " + itoa(n) + " months"
		}
		out += " on the " + weekOf[r.WeekOfMonth] + " " + dayNames[r.Weekdays[0]]
	case n == 1 && slices.Equal(days, []int{1, 2, 3, 4, 5}):
		out = "Every weekday"
	default:
		if n == 1 {
			out = "Every " + clean(r.Every)
		} else {
			out = "Every " + itoa(n) + " " + clean(r.Every) + "s"
		}
		if len(days) > 0 {
			var names []string
			for _, d := range days {
				if d >= 0 && d <= 6 {
					names = append(names, dayNames[d])
				}
			}
			out += " on " + strings.Join(names, ", ")
		}
	}
	if r.FromDone {
		out += " after done"
	}
	if u, err := time.ParseInLocation("2006-01-02", r.Until, now.Location()); err == nil {
		date := monthNames[u.Month()-1] + " " + itoa(u.Day())
		if u.Year() != now.Year() {
			date += ", " + itoa(u.Year())
		}
		out += " until " + date
	}
	return out
}
