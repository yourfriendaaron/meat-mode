package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// OfflineAfter is how long a bridge may go unheard before its working runs
// show as stopped with reason offline.
const OfflineAfter = 60 * time.Second

// OfflineDetail is the detail of a run the hub shows as offline.
const OfflineDetail = "The machine running the agent stopped reporting, so the hub cannot tell whether the agent is still at work."

// startingActivity is the activity of a run the hub shows before the bridge
// has started it.
const startingActivity = "Starting"

// ReplaceRuns replaces a bridge's run list. Runs on beads the hub does not
// hold are kept, so they show once the bead arrives, but State leaves them
// out. It emits a change only when the list changed.
func (s *Store) ReplaceRuns(bridge string, reports []api.RunReport) error {
	type target struct{ repo, bead string }
	want := map[target]string{}
	applying := map[string]bool{}
	reports, err := api.CheckRuns(reports)
	if err != nil {
		return err
	}
	for _, r := range reports {
		if r.Applying != "" {
			applying[r.Applying] = true
		}
		data, err := json.Marshal(r.Run)
		if err != nil {
			return err
		}
		want[target{r.Repo, r.BeadID}] = string(data)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	have := map[target]string{}
	rows, err := tx.Query(`SELECT repo, bead_id, data FROM runs WHERE bridge = ?`, bridge)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t target
		var data string
		if err := rows.Scan(&t.repo, &t.bead, &data); err != nil {
			rows.Close()
			return err
		}
		have[t] = data
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	changed := false
	for t := range have {
		if _, ok := want[t]; !ok {
			if _, err := tx.Exec(`DELETE FROM runs WHERE bridge = ? AND repo = ? AND bead_id = ?`, bridge, t.repo, t.bead); err != nil {
				return err
			}
			changed = true
		}
	}
	for t, data := range want {
		if have[t] == data {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO runs (bridge, repo, bead_id, data) VALUES (?, ?, ?, ?)
			ON CONFLICT (bridge, repo, bead_id) DO UPDATE SET data = excluded.data`,
			bridge, t.repo, t.bead, data); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	// The applied actions live only in memory: they matter for the moment
	// between this list and the actions' results.
	s.mu.Lock()
	if !maps.Equal(s.applying[bridge], applying) {
		if s.applying == nil {
			s.applying = map[string]map[string]bool{}
		}
		s.applying[bridge] = applying
		changed = true
	}
	s.mu.Unlock()
	if !changed {
		return nil
	}
	s.emit(Change{Kind: "runs", Bridge: bridge})
	return nil
}

// joinRuns sets each bridge item's Run from the stored run lists. A working
// run whose bridge has not been heard from for OfflineAfter shows as stopped
// with reason offline, ended when the bridge was last seen. The personal list
// has no runs.
//
// No write marks a run offline, so the first State that shows one offline,
// or shows one back after the bridge was heard from again, emits a runs
// change: open apps fetch the state again and the push check runs.
func (s *Store) joinRuns(items []api.Item, index map[string]int, bridges []api.Bridge, now time.Time) error {
	lastSeen := map[string]*time.Time{}
	for _, b := range bridges {
		lastSeen[b.Name] = b.LastSeen
	}
	offline := map[string]bool{}
	err := s.eachRow(`SELECT bridge, repo, bead_id, data FROM runs WHERE bridge != ?`, []any{HubBridge}, func(rows *sql.Rows) error {
		var b, r, id, data string
		if err := rows.Scan(&b, &r, &id, &data); err != nil {
			return err
		}
		i, ok := index[ItemKey(b, r, id)]
		if !ok {
			return nil
		}
		var run api.Run
		if err := json.Unmarshal([]byte(data), &run); err != nil {
			return err
		}
		if run.State == api.RunWorking {
			seen := lastSeen[b]
			if seen == nil || now.Sub(*seen) > OfflineAfter {
				run.State, run.Reason, run.Detail, run.Queued = api.RunStopped, api.ReasonOffline, OfflineDetail, 0
				if seen != nil {
					t := *seen
					run.EndedAt = &t
				}
				offline[items[i].Key+"/"+run.ID] = true
			}
		}
		items[i].Run = &run
		return nil
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	fresh := false
	for k := range offline {
		if !s.offline[k] {
			fresh = true
		}
	}
	for k := range s.offline {
		if !offline[k] {
			fresh = true
		}
	}
	s.offline = offline
	s.mu.Unlock()
	if fresh {
		s.emit(Change{Kind: "runs"})
	}
	return nil
}

// foldRun shows a pending ask or stop on an item's run. An ask on an item
// with no working run starts a provisional one; on a working run a note
// joins it as queued. A stop ends a working run.
func foldRun(it *api.Item, a api.Action) {
	switch a.Kind {
	case api.KindAsk:
		old := it.Run
		if old != nil && old.State == api.RunWorking {
			if a.Payload.Text != "" {
				r := *old
				r.Queued++
				it.Run = &r
			}
			return
		}
		r := api.Run{
			ID:        provisionalPrefix + a.ID,
			State:     api.RunWorking,
			Agent:     "agent",
			Activity:  startingActivity,
			StartedAt: a.CreatedAt,
			Count:     1,
		}
		if old != nil && old.Agent != "" {
			r.Agent = old.Agent
		}
		if old != nil {
			r.Count = old.Count + 1
		}
		it.Run = &r
	case api.KindStop:
		if it.Run == nil || it.Run.State != api.RunWorking {
			return
		}
		r := *it.Run
		t := a.CreatedAt
		r.State, r.Reason, r.Detail, r.EndedAt, r.Queued = api.RunStopped, api.ReasonStopped, "", &t, 0
		it.Run = &r
	}
}

// agentsAllowedTx reports whether agents may work in a repo: its agents
// switch as the bridge last reported it, or where the latest pending agents
// action switched it.
func agentsAllowedTx(tx *sql.Tx, bridge, repo string) (bool, error) {
	var allowed bool
	err := tx.QueryRow(`SELECT agents_allowed FROM repos WHERE bridge = ? AND name = ?`, bridge, repo).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var payload string
	err = tx.QueryRow(`SELECT payload FROM actions WHERE bridge = ? AND repo = ? AND kind = ? AND status = ?
		ORDER BY seq DESC LIMIT 1`, bridge, repo, api.KindAgents, api.StatusPending).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return allowed, nil
	}
	if err != nil {
		return false, err
	}
	var p api.Payload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return false, err
	}
	if p.On != nil {
		allowed = *p.On
	}
	return allowed, nil
}

// ClosedInApp reports whether the user closed the item from the app at or
// after since: the latest done, dismiss or reopen on it from then on, pending
// or applied, is a done or a dismiss. Every action on a bridge's item comes
// from the app; an agent closes a bead in beads, not through the hub.
func (s *Store) ClosedInApp(ctx context.Context, key string, since time.Time) (bool, error) {
	b, r, id, ok := SplitKey(key)
	if !ok {
		return false, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, created_at FROM actions
		WHERE bridge = ? AND repo = ? AND bead_id = ? AND kind IN (?, ?, ?) AND status IN (?, ?)
		ORDER BY seq`, b, r, id, api.KindDone, api.KindDismiss, api.KindReopen, api.StatusPending, api.StatusApplied)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	closed := false
	for rows.Next() {
		var kind, created string
		if err := rows.Scan(&kind, &created); err != nil {
			return false, err
		}
		// Times compare parsed: RFC 3339 text with trimmed fractions does
		// not sort as text.
		if parseTime(created).Before(since) {
			continue
		}
		closed = kind != api.KindReopen
	}
	return closed, rows.Err()
}
