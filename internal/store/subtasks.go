package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Sub-tasks are items whose Parent names another item in the same list. The
// rules here hold for every list, the personal list and a tracker's alike:
// a sub-task's parent is a top-level open item, and a parent closes (done or
// dismiss) only once none of its sub-tasks is open.

// checkParent refuses a new sub-task whose parent is not an open top-level
// item in the list.
func checkParent(tx *sql.Tx, bridge, repo, parent string) error {
	if strings.HasPrefix(parent, provisionalPrefix) {
		return conflictf("the parent is still being created; try again in a moment")
	}
	var data string
	err := tx.QueryRow(`SELECT data FROM items WHERE bridge = ? AND repo = ? AND id = ?`, bridge, repo, parent).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundf("no item %q in this list to add a sub-task to", parent)
	}
	if err != nil {
		return err
	}
	var it api.Item
	if err := json.Unmarshal([]byte(data), &it); err != nil {
		return err
	}
	if it.Status == "closed" {
		return invalidf("%q is closed; reopen it to add a sub-task", it.Title)
	}
	if it.Parent != "" {
		return invalidf("%q is a sub-task itself; sub-tasks go one level deep", it.Title)
	}
	return nil
}

// openSubtasks counts the open sub-tasks of an item: stored ones, folded
// with their pending done, dismiss and reopen actions (the latest wins),
// and ones still being created.
func (s *Store) openSubtasks(tx *sql.Tx, bridge, repo, id string) (int, error) {
	rows, err := tx.Query(`SELECT id, json_extract(data, '$.status') FROM items
		WHERE bridge = ? AND repo = ? AND json_extract(data, '$.parent') = ?`, bridge, repo, id)
	if err != nil {
		return 0, err
	}
	type child struct {
		id   string
		open bool
	}
	var children []child
	for rows.Next() {
		var c child
		var status sql.NullString
		if err := rows.Scan(&c.id, &status); err != nil {
			rows.Close()
			return 0, err
		}
		c.open = status.String != "closed"
		children = append(children, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, c := range children {
		var kind string
		err := tx.QueryRow(`SELECT kind FROM actions WHERE bridge = ? AND repo = ? AND bead_id = ? AND status = ?
			AND kind IN (?, ?, ?) ORDER BY seq DESC LIMIT 1`,
			bridge, repo, c.id, api.StatusPending, api.KindDone, api.KindDismiss, api.KindReopen).Scan(&kind)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return 0, err
		default:
			c.open = kind == api.KindReopen
		}
		if c.open {
			n++
		}
	}
	// Still being created: a pending add, or an applied one whose bead the
	// bridge has not sent yet, by the rule State shows provisional items by.
	// A bridge closes a parent without looking at children it has not seen.
	var adding int
	since := fmtTime(s.now().Add(-provisionalTTL))
	if err := tx.QueryRow(`SELECT COUNT(*) FROM actions a WHERE a.bridge = ? AND a.repo = ? AND a.kind = ?
		AND json_extract(a.payload, '$.parent') = ? AND (a.status = ? OR (a.status = ? AND a.applied_at >= ?
			AND NOT EXISTS (SELECT 1 FROM items i WHERE i.bridge = a.bridge AND i.repo = a.repo AND i.id = a.bead_id)
			AND NOT EXISTS (SELECT 1 FROM repos r WHERE r.bridge = a.bridge AND r.name = a.repo AND r.synced_at >= a.applied_at)))`,
		bridge, repo, api.KindAdd, id, api.StatusPending, api.StatusApplied, since).Scan(&adding); err != nil {
		return 0, err
	}
	return n + adding, nil
}

// checkCloses refuses done or dismiss on an item with open sub-tasks.
func (s *Store) checkCloses(tx *sql.Tx, bridge, repo, id string) error {
	n, err := s.openSubtasks(tx, bridge, repo, id)
	if err != nil || n == 0 {
		return err
	}
	return conflictf("%s", OpenSubtasksError(n))
}

// OpenSubtasksError is the refusal of a close while n sub-tasks are open.
func OpenSubtasksError(n int) string {
	if n == 1 {
		return "finish or remove its open sub-task first"
	}
	return fmt.Sprintf("finish or remove its %d open sub-tasks first", n)
}

// checkEditList checks an edit's parent and subtasksDone against the list.
// A move to a parent follows checkParent, and an item with sub-tasks of its
// own, open, closed or still being added, cannot become a sub-task. Every
// id in subtasksDone must be one of the item's sub-tasks.
func checkEditList(tx *sql.Tx, bridge, repo, id string, set api.Edit) error {
	if set.Parent != nil && *set.Parent != "" {
		if *set.Parent == id {
			return invalidf("an item cannot be its own sub-task")
		}
		var n int
		if err := tx.QueryRow(`SELECT
			(SELECT COUNT(*) FROM items WHERE bridge = ? AND repo = ? AND json_extract(data, '$.parent') = ?) +
			(SELECT COUNT(*) FROM actions WHERE bridge = ? AND repo = ? AND kind = ? AND status = ?
				AND json_extract(payload, '$.parent') = ?)`,
			bridge, repo, id, bridge, repo, api.KindAdd, api.StatusPending, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return invalidf("it has sub-tasks of its own; sub-tasks go one level deep")
		}
		if err := checkParent(tx, bridge, repo, *set.Parent); err != nil {
			return err
		}
	}
	for _, sub := range set.SubtasksDone {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM items WHERE bridge = ? AND repo = ? AND id = ?
			AND json_extract(data, '$.parent') = ?`, bridge, repo, sub, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return invalidf("%q is not a sub-task of this item", sub)
		}
	}
	return nil
}

// applyHubSubtasks changes the sub-tasks an applied action on a list the
// hub keeps names. An edit's SubtasksDone closes them as done. A done that
// moved a repeating parent on to its next date reopens its sub-tasks closed
// as done, even ones closed long enough ago to have left the app, and
// records them in Was, so its Undo closes them again. Dismissed ones stay
// closed.
func applyHubSubtasks(tx *sql.Tx, a *api.Action) error {
	switch {
	case a.Kind == api.KindEdit && a.Payload.Set != nil:
		for _, id := range a.Payload.Set.SubtasksDone {
			it, err := hubItem(tx, a.Repo, id)
			if err != nil {
				return err
			}
			if it.Status == "closed" {
				continue
			}
			apply(&it, api.Action{Kind: api.KindDone, CreatedAt: a.CreatedAt}, "")
			if err := putHubItem(tx, a.Repo, it, it.FirstSeen, it.SeenAt); err != nil {
				return err
			}
		}
	case a.Kind == api.KindDone && a.Payload.Was != nil:
		rows, err := tx.Query(`SELECT id FROM items WHERE bridge = ? AND repo = ? AND json_extract(data, '$.parent') = ?
			AND json_extract(data, '$.status') = 'closed' AND json_extract(data, '$.closeReason') = ? ORDER BY id`,
			HubBridge, a.Repo, a.BeadID, ReasonDone)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range ids {
			it, err := hubItem(tx, a.Repo, id)
			if err != nil {
				return err
			}
			apply(&it, api.Action{Kind: api.KindReopen, CreatedAt: a.CreatedAt}, "")
			if err := putHubItem(tx, a.Repo, it, it.FirstSeen, it.SeenAt); err != nil {
				return err
			}
		}
		a.Payload.Was.SubtasksDone = ids
	}
	return nil
}
