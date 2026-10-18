package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// reminderTables are the reminder scheduler's records of reminders sent,
// keyed by item_key. The scheduler creates them, so a store used without
// one may lack them.
var reminderTables = []string{"reminder_fired", "reminder_early"}

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
func (s *Store) ReminderKey(ctx context.Context, it api.Item) (string, time.Time, error) {
	// The personal list has no provisional copies: an add is the item.
	if it.Bridge == "" || it.Bridge == HubBridge || strings.HasPrefix(it.ID, provisionalPrefix) {
		return it.Key, it.FirstSeen, nil
	}
	var id, created string
	err := s.db.QueryRowContext(ctx, `SELECT id, created_at FROM actions
		WHERE bridge = ? AND repo = ? AND bead_id = ? AND kind = ? AND status = ?
		ORDER BY seq LIMIT 1`, it.Bridge, it.Repo, it.ID, api.KindAdd, api.StatusApplied).Scan(&id, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return it.Key, it.FirstSeen, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	first := it.FirstSeen
	if c := parseTime(created); !c.IsZero() && (first.IsZero() || c.Before(first)) {
		first = c
	}
	return ItemKey(it.Bridge, it.Repo, provisionalPrefix+id), first, nil
}
