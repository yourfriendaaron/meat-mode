package tracker

import (
	"context"
	"slices"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Epics is what a tracker that can show a repo's epics also implements
// (docs/trackers.md, Epics). The bridge checks for it with a type
// assertion and reports the repo with api.Repo.Epics.
type Epics interface {
	// EpicRows returns every task in the repo, closed ones included
	// however old, as rows: ID, Title, Type, Status, Priority, Assignee,
	// Parent, Human (the task carries HumanLabel), Description, UpdatedAt
	// and ClosedAt filled as the tracker has them. It leaves Epic unset.
	// Like List, it returns the whole set or an error, never part of it.
	// The bridge passes the result through Outline before sending it.
	EpicRows(ctx context.Context, dir string) ([]api.EpicRow, error)
}

// Outline selects what the hub gets from every row of a repo, the same way
// for every tracker:
//
//   - a card (Epic true) for each bead that is not closed and whose type is
//     "epic" (any case) or that has a child, open or closed;
//   - every direct child of a card, whatever its status; a child that is
//     itself a card is both;
//   - every bead that is not closed, is not a card and whose parent is not
//     a card (none, one the repo does not hold, or a closed one): the
//     repo's top-level tasks.
//
// Parent is kept only when it names a card, so a row with a Parent is
// always a direct child of a card in the set. Only cards keep their
// Description, cut to api.ItemDescriptionLimit runes; past
// api.MaxEpicDescriptions bytes over the set, later cards (by ID) lose
// theirs. Rows come back sorted by ID; past api.MaxEpicRows the set is cut
// at that many, dropping closed rows first. Rows whose id is not a bead id
// are left out.
func Outline(rows []api.EpicRow) []api.EpicRow {
	rows = slices.DeleteFunc(slices.Clone(rows), func(r api.EpicRow) bool { return !api.ValidBeadID(r.ID) })
	byID := make(map[string]bool, len(rows))
	for _, r := range rows {
		byID[r.ID] = true
	}
	hasChild := map[string]bool{}
	for _, r := range rows {
		if r.Parent != "" && byID[r.Parent] && r.Parent != r.ID {
			hasChild[r.Parent] = true
		}
	}
	card := map[string]bool{}
	for _, r := range rows {
		if r.Status != StatusClosed && (strings.EqualFold(r.Type, "epic") || hasChild[r.ID]) {
			card[r.ID] = true
		}
	}
	out := make([]api.EpicRow, 0)
	for _, r := range rows {
		parent := r.Parent
		if parent == r.ID || !card[parent] {
			parent = ""
		}
		keep := card[r.ID] || parent != "" || r.Status != StatusClosed
		if !keep {
			continue
		}
		r.Parent = parent
		r.Epic = card[r.ID]
		if r.Epic {
			r.Description = api.TruncRunes(r.Description, api.ItemDescriptionLimit)
		} else {
			r.Description = ""
		}
		out = append(out, r)
	}
	byIDOrder := func(a, b api.EpicRow) int { return strings.Compare(a.ID, b.ID) }
	slices.SortFunc(out, byIDOrder)
	if len(out) > api.MaxEpicRows {
		open := slices.DeleteFunc(slices.Clone(out), func(r api.EpicRow) bool { return r.Status == StatusClosed })
		if len(open) >= api.MaxEpicRows {
			out = open[:api.MaxEpicRows]
		} else {
			closed := slices.DeleteFunc(out, func(r api.EpicRow) bool { return r.Status != StatusClosed })
			out = append(open, closed[:api.MaxEpicRows-len(open)]...)
			slices.SortFunc(out, byIDOrder)
		}
		// A child whose card was cut is top-level now.
		kept := map[string]bool{}
		for _, r := range out {
			kept[r.ID] = r.Epic
		}
		for i := range out {
			if !kept[out[i].Parent] {
				out[i].Parent = ""
			}
		}
	}
	total := 0
	for i := range out {
		total += len(out[i].Description)
		if total > api.MaxEpicDescriptions {
			total -= len(out[i].Description)
			out[i].Description = ""
		}
	}
	return out
}
