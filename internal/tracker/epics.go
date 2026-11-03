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
// always a direct child of a card in the set. Every row keeps its
// Description and Notes, each cut to api.ItemDescriptionLimit runes. Past
// api.MaxEpicDescriptions bytes over the set, descriptions and then notes
// are kept in this order until the budget runs out, and the rest are
// dropped: cards, then rows that are not closed, then closed rows, each by
// ID. Rows come back sorted by ID; past api.MaxEpicRows the set is cut
// at that many, dropping closed rows first. Rows whose id is not a bead id
// are left out, and so is any row after the first with the same id.
func Outline(rows []api.EpicRow) []api.EpicRow {
	seen := make(map[string]bool, len(rows))
	rows = slices.DeleteFunc(slices.Clone(rows), func(r api.EpicRow) bool {
		drop := !api.ValidBeadID(r.ID) || seen[r.ID]
		seen[r.ID] = true
		return drop
	})
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
		r.Description = api.TruncRunes(r.Description, api.ItemDescriptionLimit)
		r.Notes = api.TruncRunes(r.Notes, api.ItemDescriptionLimit)
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
	// Descriptions spend the budget first, then notes, each in the same
	// order: cards, rows that are not closed, closed rows.
	total := 0
	passes := []func(api.EpicRow) bool{
		func(r api.EpicRow) bool { return r.Epic },
		func(r api.EpicRow) bool { return !r.Epic && r.Status != StatusClosed },
		func(r api.EpicRow) bool { return !r.Epic && r.Status == StatusClosed },
	}
	for _, field := range []func(*api.EpicRow) *string{
		func(r *api.EpicRow) *string { return &r.Description },
		func(r *api.EpicRow) *string { return &r.Notes },
	} {
		for _, pass := range passes {
			for i := range out {
				if !pass(out[i]) {
					continue
				}
				f := field(&out[i])
				total += len(*f)
				if total > api.MaxEpicDescriptions {
					total -= len(*f)
					*f = ""
				}
			}
		}
	}
	return out
}
