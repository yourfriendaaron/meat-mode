package store

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// MaxChecklist is how many entries a personal item's checklist holds.
const MaxChecklist = 100

// personalOnly reports whether an action kind changes personal items only.
func personalOnly(kind string) bool {
	switch kind {
	case api.KindEdit, api.KindNoteEdit, api.KindNoteDelete, api.KindNoteRestore:
		return true
	}
	return false
}

// checkEdit validates an edit's fields and returns them cleaned up: the
// title trimmed, checklist texts trimmed and every entry given an id, a
// repeat rule without the start the hub sets, the parent and sub-task ids
// trimmed, the tags cleaned by cleanLabels. The rules that need the list are checkEditList's.
func checkEdit(e api.Edit) (api.Edit, error) {
	if e.Title == nil && e.Description == nil && e.Due == nil && !e.ClearDue && e.Priority == nil && e.Checklist == nil &&
		e.Repeat == nil && !e.ClearRepeat && e.RemindBefore == nil && !e.ClearRemindBefore &&
		e.Parent == nil && len(e.SubtasksDone) == 0 && e.Labels == nil {
		return e, invalidf("edit needs at least one field in payload.set")
	}
	if e.Title != nil {
		title, err := api.CheckTitle(*e.Title)
		if err != nil {
			return e, err
		}
		e.Title = &title
	}
	if e.Description != nil {
		d := *e.Description
		if strings.TrimSpace(d) == "" {
			d = ""
		}
		if utf8.RuneCountInString(d) > MaxNoteLen {
			return e, invalidf("description is longer than %d characters", MaxNoteLen)
		}
		e.Description = &d
	}
	if e.Due != nil && e.ClearDue {
		return e, invalidf("send either due or clearDue, not both")
	}
	if e.Priority != nil {
		if err := api.CheckPriority(*e.Priority); err != nil {
			return e, err
		}
	}
	if e.Checklist != nil {
		in := *e.Checklist
		if len(in) > MaxChecklist {
			return e, invalidf("a checklist holds at most %d entries", MaxChecklist)
		}
		out := make([]api.Check, 0, len(in))
		ids := map[string]bool{}
		for _, c := range in {
			c.ID = strings.TrimSpace(c.ID)
			c.Text = strings.TrimSpace(c.Text)
			if c.Text == "" {
				return e, invalidf("a checklist entry cannot be empty")
			}
			if utf8.RuneCountInString(c.Text) > MaxTitleLen {
				return e, invalidf("a checklist entry is longer than %d characters", MaxTitleLen)
			}
			if c.ID == "" {
				c.ID = uuid.NewString()
			}
			if ids[c.ID] {
				return e, invalidf("checklist entry id %q is listed twice", c.ID)
			}
			ids[c.ID] = true
			out = append(out, c)
		}
		e.Checklist = &out
	}
	if e.Repeat != nil && e.ClearRepeat {
		return e, invalidf("send either repeat or clearRepeat, not both")
	}
	if e.Repeat != nil {
		if e.ClearDue {
			return e, invalidf("a repeat needs a due date")
		}
		r, err := checkRepeat(*e.Repeat)
		if err != nil {
			return e, err
		}
		e.Repeat = &r
	}
	if e.RemindBefore != nil && e.ClearRemindBefore {
		return e, invalidf("send either remindBefore or clearRemindBefore, not both")
	}
	if e.RemindBefore != nil && (*e.RemindBefore < 0 || *e.RemindBefore > api.MaxLeadMinutes) {
		return e, invalidf("remindBefore must be 0 to %d minutes", api.MaxLeadMinutes)
	}
	if e.Parent != nil {
		parent := strings.TrimSpace(*e.Parent)
		if parent != "" && !validSegment(parent) {
			return e, invalidf("parent is not a valid item id")
		}
		e.Parent = &parent
	}
	if e.SubtasksDone != nil {
		ids := make([]string, 0, len(e.SubtasksDone))
		for _, id := range e.SubtasksDone {
			id = strings.TrimSpace(id)
			if !validSegment(id) {
				return e, invalidf("subtasksDone holds %q, which is not a valid item id", id)
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		e.SubtasksDone = ids
	}
	if e.Labels != nil {
		labels, err := cleanLabels(*e.Labels)
		if err != nil {
			return e, err
		}
		e.Labels = &labels
	}
	return e, nil
}

// checkRestore validates the comment a note-restore puts back. One with no
// time gets now.
func checkRestore(c *api.Comment, now time.Time) (api.Comment, error) {
	if c == nil || strings.TrimSpace(c.ID) == "" {
		return api.Comment{}, invalidf("note-restore needs payload.comment with its id")
	}
	r := *c
	if strings.TrimSpace(r.Text) == "" {
		return r, invalidf("note-restore needs the comment's text")
	}
	if utf8.RuneCountInString(r.Text) > maxCommentLen {
		return r, invalidf("note text is longer than %d characters", maxCommentLen)
	}
	if utf8.RuneCountInString(r.Author) > maxCommentAuthorLen {
		return r, invalidf("author is longer than %d characters", maxCommentAuthorLen)
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return r, nil
}

// applyPersonal applies the kinds that change personal items only, and
// done on a repeating item, and records in a's payload what they replaced:
// an edit's old values and a repeating done's due time and checklist as
// Was, a note-edit's or note-delete's comment as it was. The sub-tasks they
// change are applyHubSubtasks'.
func applyPersonal(it *api.Item, a *api.Action) error {
	p := &a.Payload
	switch a.Kind {
	case api.KindDone:
		if it.Repeat == nil || it.DueAt == nil || it.Status == "closed" {
			return nil
		}
		next := nextDue(*it.Repeat, *it.DueAt, a.CreatedAt)
		if afterUntil(*it.Repeat, next) {
			// The series is over: with no Was, apply closes it, and it
			// keeps its due time and repeat for a reopen.
			return nil
		}
		// It stays open and moves on to its next date; apply sees Was and
		// leaves it open.
		p.Was = &api.Edit{Due: ptr(*it.DueAt), Checklist: ptr(cloneChecklist(it.Checklist))}
		it.DueAt = ptr(next)
		it.DeferUntil = nil
		if len(it.Checklist) > 0 {
			cs := slices.Clone(it.Checklist)
			for i := range cs {
				cs[i].Done = false
			}
			it.Checklist = cs
		}
	case api.KindEdit:
		set, was := p.Set, &api.Edit{}
		if set.Title != nil {
			was.Title = ptr(it.Title)
			it.Title = *set.Title
		}
		if set.Description != nil {
			was.Description = ptr(it.Description)
			it.Description = *set.Description
		}
		if set.Due != nil || set.ClearDue {
			if it.DueAt != nil {
				was.Due = ptr(*it.DueAt)
			} else {
				was.ClearDue = true
			}
			it.DueAt = nil
			if set.Due != nil {
				it.DueAt = ptr(*set.Due)
			}
		}
		switch {
		case set.Repeat != nil || set.ClearRepeat || (set.ClearDue && it.Repeat != nil):
			// Clearing the due date ends the repeat too.
			old := it.Repeat
			if old != nil {
				was.Repeat = ptr(*old)
			} else {
				was.ClearRepeat = true
			}
			it.Repeat = nil
			if set.Repeat != nil {
				if it.DueAt == nil {
					return invalidf("a repeat needs a due date")
				}
				r := *set.Repeat
				r.Start = ptr(*it.DueAt)
				if old != nil && onlyUntilDiffers(*old, r) && isOccurrence(*old, *it.DueAt) {
					// A new end date never moves the series.
					r.Start = old.Start
				}
				it.Repeat = &r
			}
		case set.Due != nil && it.Repeat != nil && !isOccurrence(*it.Repeat, *it.DueAt):
			// Moving the due date moves the series. A move to one of its own
			// dates, such as the Undo of a done, keeps the start, so a
			// monthly rule from the 31st still returns to it.
			r := *it.Repeat
			r.Start = ptr(*it.DueAt)
			it.Repeat = &r
		}
		if (set.Repeat != nil || set.Due != nil) && it.Repeat != nil {
			if err := checkUntil(*it.Repeat, *it.DueAt); err != nil {
				return err
			}
		}
		if set.Priority != nil {
			was.Priority = ptr(it.Priority)
			it.Priority = *set.Priority
		}
		if set.Checklist != nil {
			was.Checklist = ptr(cloneChecklist(it.Checklist))
			it.Checklist = nil
			if len(*set.Checklist) > 0 {
				it.Checklist = slices.Clone(*set.Checklist)
			}
		}
		if set.Parent != nil {
			// checkEditList has checked the move against the list.
			was.Parent = ptr(it.Parent)
			it.Parent = *set.Parent
			it.ParentTitle = ""
		}
		if set.RemindBefore != nil || set.ClearRemindBefore {
			if it.RemindBefore != nil {
				was.RemindBefore = ptr(*it.RemindBefore)
			} else {
				was.ClearRemindBefore = true
			}
			it.RemindBefore = nil
			if set.RemindBefore != nil {
				it.RemindBefore = ptr(*set.RemindBefore)
			}
		}
		if set.Labels != nil {
			was.Labels = ptr(append([]string{}, it.Labels...))
			it.Labels = slices.Clone(*set.Labels)
			if len(it.Labels) == 0 {
				it.Labels = nil
			}
		}
		p.Was = was
	case api.KindNoteEdit, api.KindNoteDelete:
		i := slices.IndexFunc(it.Comments, func(c api.Comment) bool { return c.ID == p.CommentID })
		if i < 0 {
			return notFoundf("no note %q on this item", p.CommentID)
		}
		old := it.Comments[i]
		p.Comment = &old
		cs := slices.Clone(it.Comments)
		if a.Kind == api.KindNoteEdit {
			cs[i].Text = p.Text
		} else {
			cs = slices.Delete(cs, i, i+1)
		}
		it.Comments = cs
	case api.KindNoteRestore:
		c := *p.Comment
		if slices.ContainsFunc(it.Comments, func(o api.Comment) bool { return o.ID == c.ID }) {
			return conflictf("note %q is already on this item", c.ID)
		}
		i := slices.IndexFunc(it.Comments, func(o api.Comment) bool { return o.CreatedAt.After(c.CreatedAt) })
		if i < 0 {
			i = len(it.Comments)
		}
		it.Comments = slices.Insert(slices.Clone(it.Comments), i, c)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// cloneChecklist copies a checklist for Was, where none is an empty list.
func cloneChecklist(cs []api.Check) []api.Check {
	if cs == nil {
		return []api.Check{}
	}
	return slices.Clone(cs)
}

// agentAuthor is the author an agent's post carries: its own name, or
// fallback (the machine's) when it gives none.
func agentAuthor(name, fallback string) (string, error) {
	author := truncRunes(strings.TrimSpace(name), maxCommentAuthorLen)
	if author == "" {
		author = fallback
	}
	if strings.ContainsFunc(author, unicode.IsControl) {
		return "", invalidf("author must be plain text on one line")
	}
	return author, nil
}

// recordOwner records that bridge's token added item id to the hub's list
// repo. A second record for the same item changes nothing.
func recordOwner(tx *sql.Tx, repo, id, bridge string) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO personal_owners (repo, item_id, bridge) VALUES (?, ?, ?)`, repo, id, bridge)
	return err
}

// ProjectRepo returns the hub list an agent's post names with project: the
// personal list when it is empty, else the project of that name. A
// project's name is a repo name, and PersonalRepo is kept for the personal
// list.
func ProjectRepo(project string) (string, error) {
	switch {
	case project == "":
		return PersonalRepo, nil
	case strings.EqualFold(project, PersonalRepo):
		return "", invalidf("the project name %q is kept for the personal list; leave the project out to use it", project)
	case !ValidRepoName(project):
		return "", invalidf("project must be a repo name, such as projects/garden")
	}
	return project, nil
}

// ownedPersonal returns item id of the hub's list repo when bridge's token
// added it and the app still shows it. Every other case is the same
// not-found error, so an agent cannot learn which ids exist.
func (s *Store) ownedPersonal(bridge, repo, id string) (api.Item, error) {
	gone := notFoundf("no to-do %q added from this machine", id)
	if !validSegment(id) {
		return api.Item{}, gone
	}
	tx, err := s.db.Begin()
	if err != nil {
		return api.Item{}, err
	}
	defer tx.Rollback()
	var owner string
	err = tx.QueryRow(`SELECT bridge FROM personal_owners WHERE repo = ? AND item_id = ?`, repo, id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != bridge) {
		return api.Item{}, gone
	}
	if err != nil {
		return api.Item{}, err
	}
	it, err := hubItem(tx, repo, id)
	if errors.Is(err, ErrNotFound) || (err == nil && hiddenClosed(it, s.now())) {
		return api.Item{}, gone
	}
	return it, err
}

// PersonalItem returns a to-do that bridge's token added to the personal
// list, or to the project when it is not empty, for its agents to read.
func (s *Store) PersonalItem(bridge, project, id string) (api.Item, error) {
	repo, err := ProjectRepo(project)
	if err != nil {
		return api.Item{}, err
	}
	return s.ownedPersonal(bridge, repo, id)
}

// ChangePersonal applies an agent's change to a to-do its machine's token
// added to the personal list, or to the project when it is not empty. The
// author is c.Author, or the machine's name when it is empty. A repeated
// c.ID returns the action it made and changes nothing.
func (s *Store) ChangePersonal(bridge, project, id string, c api.PersonalChange) (api.Action, bool, error) {
	repo, err := ProjectRepo(project)
	if err != nil {
		return api.Action{}, false, err
	}
	switch c.Kind {
	case api.KindEdit, api.KindDone, api.KindReopen, api.KindDismiss, api.KindNote:
	case "":
		return api.Action{}, false, invalidf("a change needs a kind")
	default:
		return api.Action{}, false, invalidf("an agent cannot post %q; use edit, done, reopen, dismiss or note", c.Kind)
	}
	author, err := agentAuthor(c.Author, bridge)
	if err != nil {
		return api.Action{}, false, err
	}
	if _, err := s.ownedPersonal(bridge, repo, id); err != nil {
		return api.Action{}, false, err
	}
	if set := c.Payload.Set; c.Kind == api.KindEdit && set != nil {
		// The items an edit moves under or closes are the machine's own
		// too, so an agent cannot learn which other ids exist.
		others := slices.Clone(set.SubtasksDone)
		if set.Parent != nil && strings.TrimSpace(*set.Parent) != "" {
			others = append(others, strings.TrimSpace(*set.Parent))
		}
		for _, other := range others {
			if _, err := s.ownedPersonal(bridge, repo, strings.TrimSpace(other)); err != nil {
				return api.Action{}, false, err
			}
		}
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	a, created, err := s.AppendAction(api.NewAction{ID: c.ID, Kind: c.Kind,
		Key: ItemKey(HubBridge, repo, id), Payload: c.Payload}, author)
	if err != nil {
		return a, false, err
	}
	if !created && (a.Kind != c.Kind || !onHub(a.Bridge) || a.Repo != repo || a.BeadID != id) {
		return api.Action{}, false, conflictf("action %q is not a %s of %q", c.ID, c.Kind, id)
	}
	return a, created, nil
}
