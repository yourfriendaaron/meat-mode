package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// ReminderKey names the item a reminder is recorded under. A bead created by
// an add from the app answers to the provisional item the app showed until
// the bridge reported it, so a reminder that fired for one does not fire
// again for the other.
func (s *Store) ReminderKey(ctx context.Context, it api.Item) (string, error) {
	if it.Bridge == "" || strings.HasPrefix(it.ID, provisionalPrefix) {
		return it.Key, nil
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM actions
		WHERE bridge = ? AND repo = ? AND bead_id = ? AND kind = ? AND status = ?
		ORDER BY seq LIMIT 1`, it.Bridge, it.Repo, it.ID, api.KindAdd, api.StatusApplied).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return it.Key, nil
	}
	if err != nil {
		return "", err
	}
	return ItemKey(it.Bridge, it.Repo, provisionalPrefix+id), nil
}
