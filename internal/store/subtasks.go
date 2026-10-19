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
