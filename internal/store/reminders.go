package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// reminderTables are the reminder scheduler's records of reminders and
// agent pushes sent, keyed by item_key. The scheduler creates them, so a
// store used without one may lack them.
var reminderTables = []string{"reminder_fired", "reminder_early", "run_pushed"}

// reminderTablesIn lists the reminder tables that exist.
func reminderTablesIn(tx *sql.Tx) ([]string, error) {
	var out []string
	for _, t := range reminderTables {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, t).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

// ReminderKey names the item a reminder is recorded under, and when the hub
// first knew of the task. A bead created by an add from the app answers to
// the provisional item the app showed until the bridge reported it, so a
// reminder that fired for one does not fire again for the other, and the
// task counts as known from the add rather than from the bridge's report.
// A task of a linked list answers to the same key from every repo of it.
func (s *Store) ReminderKey(ctx context.Context, it api.Item) (string, time.Time, error) {
	// The personal list has no provisional copies: an add is the item.
	if it.Bridge == "" || it.Bridge == HubBridge || strings.HasPrefix(it.ID, provisionalPrefix) {
		return it.Key, it.FirstSeen, nil
	}
	// A task of a linked list answers to one key whichever repo shows it,
	// the list's primary's, and counts as known from when the hub first had
	// any copy, so moving to another source fires nothing again.
	key, first := it.Key, it.FirstSeen
	members := [][2]string{{it.Bridge, it.Repo}}
	l, err := linkedLists(s.db, s.now(), s.onlineWindow())
	if err != nil {
		return "", time.Time{}, err
	}
	if g, ok := l.group(it.Bridge + "/" + it.Repo); ok {
		key = ItemKey(g.members[0].bridge, g.members[0].name, it.ID)
		members = members[:0]
		for _, m := range g.members {
			members = append(members, [2]string{m.bridge, m.name})
			var seen string
			err := s.db.QueryRowContext(ctx, `SELECT first_seen FROM items WHERE bridge = ? AND repo = ? AND id = ?`,
				m.bridge, m.name, it.ID).Scan(&seen)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return "", time.Time{}, err
			}
			if t := parseTime(seen); !t.IsZero() && (first.IsZero() || t.Before(first)) {
				first = t
			}
		}
	}
	var id, created, bridge, repo string
	for _, m := range members {
		err := s.db.QueryRowContext(ctx, `SELECT id, created_at FROM actions
			WHERE bridge = ? AND repo = ? AND bead_id = ? AND kind = ? AND status = ?
			ORDER BY seq LIMIT 1`, m[0], m[1], it.ID, api.KindAdd, api.StatusApplied).Scan(&id, &created)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", time.Time{}, err
		}
		bridge, repo = m[0], m[1]
		break
	}
	if bridge == "" {
		return key, first, nil
	}
	if c := parseTime(created); !c.IsZero() && (first.IsZero() || c.Before(first)) {
		first = c
	}
	return ItemKey(bridge, repo, provisionalPrefix+id), first, nil
}
