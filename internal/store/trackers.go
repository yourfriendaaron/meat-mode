package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Limits on what a bridge says about its trackers. Longer text is cut and
// extra entries dropped, as with item text: they must not stop a sync.
const (
	maxTrackerLen = 64
	maxActionLen  = 32
	maxActions    = 32
	maxTrackers   = 32
	maxRaiseLen   = 1000
)

// cleanActions trims a repo's action kinds, drops empty and repeated ones,
// and returns nil for none, which means every kind. The wire cannot tell an
// empty list from an absent one, so the store does not either.
func cleanActions(in []string) []string {
	var out []string
	for _, k := range in {
		k = truncRunes(strings.TrimSpace(k), maxActionLen)
		if k == "" || slices.Contains(out, k) {
			continue
		}
		out = append(out, k)
		if len(out) == maxActions {
			break
		}
	}
	return out
}

// encodeActions is the actions column for a list: NULL for every kind.
func encodeActions(actions []string) (any, error) {
	actions = cleanActions(actions)
	if actions == nil {
		return nil, nil
	}
	b, err := json.Marshal(actions)
	return string(b), err
}

func decodeActions(s sql.NullString) ([]string, error) {
	if !s.Valid {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s.String), &out); err != nil {
		return nil, err
	}
	return cleanActions(out), nil
}

// encodeTrackers is the trackers column for a hello's list, ” for none.
func encodeTrackers(in []api.TrackerVersion) (string, error) {
	var out []api.TrackerVersion
	for _, t := range in {
		t.Name = truncRunes(strings.TrimSpace(t.Name), maxTrackerLen)
		t.Title = truncRunes(strings.TrimSpace(t.Title), maxTrackerLen)
		t.Version = truncRunes(strings.TrimSpace(t.Version), maxVersionLen)
		t.Raise = truncRunes(strings.TrimSpace(t.Raise), maxRaiseLen)
		if t.Name == "" {
			continue
		}
		out = append(out, t)
		if len(out) == maxTrackers {
			break
		}
	}
	if out == nil {
		return "", nil
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func decodeTrackers(s string) ([]api.TrackerVersion, error) {
	if s == "" {
		return nil, nil
	}
	var out []api.TrackerVersion
	err := json.Unmarshal([]byte(s), &out)
	return out, err
}

// capabilityOf is the capability an action needs from its repo's tracker,
// or "" when it needs none: sync, agents and stop work on every repo, and
// the edits are refused on a tracker's items before this.
func capabilityOf(kind string, p api.Payload) string {
	switch kind {
	case api.KindDone, api.KindReopen, api.KindSnooze, api.KindDismiss, api.KindNote, api.KindAsk:
		return kind
	case api.KindAdd:
		if p.Parent != "" {
			return tracker.CapSubtask
		}
		return api.KindAdd
	}
	return ""
}

// repoTracker reads a repo's tracker name and actions; nil actions means
// every kind. A repo the store does not hold allows every kind, so the
// caller's own not-found check speaks first.
func repoTracker(q interface {
	QueryRow(string, ...any) *sql.Row
}, bridge, repo string) (string, []string, error) {
	var name string
	var actions sql.NullString
	err := q.QueryRow(`SELECT tracker, actions FROM repos WHERE bridge = ? AND name = ?`, bridge, repo).Scan(&name, &actions)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	list, err := decodeActions(actions)
	return name, list, err
}

// checkCapability refuses a capability the repo's tracker does not list.
func checkCapability(tx *sql.Tx, bridge, repo, capability string) error {
	if capability == "" {
		return nil
	}
	name, actions, err := repoTracker(tx, bridge, repo)
	if err != nil || actions == nil || slices.Contains(actions, capability) {
		return err
	}
	if name == "" {
		name = "this list's tracker"
	}
	return invalidf("%s", tracker.Refusal(name, capability))
}

// Allows reports whether the repo of the item with key can apply kind, for
// a notification's buttons. An item the store does not hold allows every
// kind; the action is checked again when it is posted.
func (s *Store) Allows(_ context.Context, key, kind string) (bool, error) {
	b, r, _, ok := SplitKey(key)
	if !ok {
		return true, nil
	}
	_, actions, err := repoTracker(s.db, b, r)
	if err != nil {
		return true, err
	}
	return actions == nil || slices.Contains(actions, kind), nil
}
