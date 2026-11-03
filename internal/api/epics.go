package api

import (
	"slices"
	"time"
)

// EpicRow is one bead of a repo's epics projection: an epic card, a direct
// child of one, or an open top-level task (docs/api.md, Epics). It is a
// read-only view of the tracker, never an Item: the hub keeps rows in their
// own table, outside State, the export and every path that serves the
// person's own to-dos.
type EpicRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type,omitempty"` // the tracker's own type, such as "epic", "task", "bug"; may be empty
	Status   string `json:"status"`         // open, in_progress, blocked, deferred, closed
	Priority int    `json:"priority"`       // 0 (highest) to 4
	Assignee string `json:"assignee,omitempty"`
	// Parent is the id of the card this row is a direct child of, in the
	// same set; empty for a top-level bead. tracker.Outline clears it when
	// the parent is not a card in the set.
	Parent string `json:"parent,omitempty"`
	// Epic marks a card: a bead whose type is "epic" or that has a child,
	// open or closed, in the repo. tracker.Outline sets it.
	Epic bool `json:"epic,omitempty"`
	// Human marks a bead that is also a task for the person (HumanLabel).
	// The app draws such a row from the State item with the same key when
	// it has one, and opens that item's sheet.
	Human bool `json:"human,omitempty"`
	// Description is the bead's own, for Details, on every row: at most
	// ItemDescriptionLimit runes, and within MaxEpicDescriptions bytes over
	// the whole set (tracker.Outline spends that on cards first, then open
	// rows, then closed ones).
	Description string     `json:"description,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
	// BlockedBy are the ids of the open beads this one waits on, as the
	// tracker reports them; empty when nothing holds it. A closed row
	// waits on nothing.
	BlockedBy []string `json:"blockedBy,omitempty"`
	// DeferUntil is when a deferred bead wakes, when it has a date.
	DeferUntil *time.Time `json:"deferUntil,omitempty"`
	// CommentCount is how many comments the bead has.
	CommentCount int `json:"commentCount,omitempty"`
}

// RepoEpics is one repo's projection in GET /api/epics.
type RepoEpics struct {
	Bridge   string    `json:"bridge"`
	Repo     string    `json:"repo"`
	Tracker  string    `json:"tracker,omitempty"` // as api.Repo.Tracker
	SyncedAt time.Time `json:"syncedAt"`          // when the hub last took a set for the repo
	Rows     []EpicRow `json:"rows"`              // never nil
}

// Epics is GET /api/epics: every repo whose epics are on and whose bridge
// has sent a set, ordered by bridge then repo; never nil.
type Epics struct {
	Repos []RepoEpics `json:"repos"`
}

// EpicsEvent is the server-sent event name the hub sends when it takes or
// drops a repo's epic rows. It is not "change", so an epics sync never
// re-fetches State. Its data is {}: events coalesce, so the app re-fetches
// every repo's rows.
const EpicsEvent = "epics"

// Limits on a repo's epic set (docs/api.md, Epics).
const (
	MaxEpicRows         = 2000
	MaxEpicDescriptions = 4 << 20 // bytes of description over the whole set
	MaxEpicBlockedBy    = 20      // blocker ids kept on one row
)

// CheckEpicRows checks and cleans a repo's epic set in place, as the hub
// and the fake hub take it. Like CheckItems it truncates rather than
// refuses what one bead could get wrong: a title past ItemTitleLimit, a
// description past ItemDescriptionLimit, a
// priority outside 0 to 4 (clamped), an unknown status (read as open), and
// a parent that names no card in the set or leads back to the row
// (cleared). It refuses, with an
// error matching ErrInvalid, only a set past MaxEpicRows, a row whose id is
// not a bead id or appears twice, or descriptions past MaxEpicDescriptions.
func CheckEpicRows(rows []EpicRow) error {
	if len(rows) > MaxEpicRows {
		return Invalidf("an epic set holds at most %d rows, not %d", MaxEpicRows, len(rows))
	}
	cards := make(map[string]bool, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if !ValidBeadID(r.ID) {
			return Invalidf("epic row id %q is not a bead id", r.ID)
		}
		if seen[r.ID] {
			return Invalidf("epic row %q appears twice", r.ID)
		}
		seen[r.ID] = true
		if r.Epic {
			cards[r.ID] = true
		}
	}
	total := 0
	for i := range rows {
		r := &rows[i]
		r.Title = TruncRunes(r.Title, ItemTitleLimit)
		r.Type = TruncRunes(r.Type, LabelLimit)
		r.Assignee = TruncRunes(r.Assignee, AgentNameLimit)
		r.Priority = min(max(r.Priority, 0), 4)
		switch r.Status {
		case "open", "in_progress", "blocked", "deferred", "closed":
		default:
			r.Status = "open"
		}
		if r.Parent == r.ID || !cards[r.Parent] {
			r.Parent = ""
		}
		r.Description = TruncRunes(r.Description, ItemDescriptionLimit)
		total += len(r.Description)
		r.BlockedBy = cleanBlockedBy(r)
		r.CommentCount = max(r.CommentCount, 0)
		if r.Status != "deferred" {
			r.DeferUntil = nil
		}
	}
	// A card may sit under another card, but a chain of parents must end:
	// a row whose chain comes back to it loses its parent, which breaks the
	// loop for every row on it.
	parent := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.Parent != "" {
			parent[r.ID] = r.Parent
		}
	}
	for i := range rows {
		id := rows[i].ID
		for p, n := parent[id], 0; p != "" && n <= len(rows); p, n = parent[p], n+1 {
			if p == id {
				rows[i].Parent = ""
				delete(parent, id)
				break
			}
		}
	}
	if total > MaxEpicDescriptions {
		return Invalidf("an epic set holds at most %d bytes of description, not %d", MaxEpicDescriptions, total)
	}
	return nil
}

// cleanBlockedBy is a row's blocker ids as CheckEpicRows keeps them.
func cleanBlockedBy(r *EpicRow) []string {
	if r.Status == "closed" {
		return nil
	}
	var out []string
	for _, id := range r.BlockedBy {
		if len(out) == MaxEpicBlockedBy {
			break
		}
		if ValidBeadID(id) && id != r.ID && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
