package store

import (
	"database/sql"
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// maxOccurrencesPerItem bounds one item's dates in a window, so a rule that
// somehow never moves on cannot hold the loop. A daily rule in the longest
// window the hub allows needs 63.
const maxOccurrencesPerItem = 1000

// Occurrences lists the later dates of every open repeating item's series
// that fall in [from, to): after the item's current due time, up to the
// rule's until, ordered by time then key, in UTC. An item with a fromDone
// rule has none until it is done. The list is never nil.
func (s *Store) Occurrences(from, to time.Time) ([]api.Occurrence, error) {
	out := []api.Occurrence{}
	err := s.eachRow(`SELECT i.bridge, i.repo, i.data, i.first_seen, i.seen_at FROM items i
		WHERE i.bridge = ? ORDER BY i.repo, i.id`, []any{HubBridge}, func(rows *sql.Rows) error {
		var b, r, data, first string
		var seenAt sql.NullString
		if err := rows.Scan(&b, &r, &data, &first, &seenAt); err != nil {
			return err
		}
		// One item that will not load cleanly leaves its own dates out, not
		// the whole month.
		it, err := decodeItem(b, r, data, first, seenAt)
		if err != nil || it.Status == "closed" || it.DueAt == nil || it.Repeat == nil || it.Repeat.Start == nil || it.Repeat.FromDone {
			return nil
		}
		rule, err := checkRepeat(*it.Repeat)
		if err != nil {
			return nil
		}
		rule.Start = it.Repeat.Start
		// The first date to look at is the first after the due time, and
		// not before from.
		after := *it.DueAt
		if f := from.Add(-time.Nanosecond); f.After(after) {
			after = f
		}
		for n := 0; n < maxOccurrencesPerItem; n++ {
			at := nextAfter(rule, after)
			if !at.Before(to) || afterUntil(rule, at) {
				break
			}
			out = append(out, api.Occurrence{Key: it.Key, At: at.UTC()})
			after = at
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b api.Occurrence) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out, nil
}
