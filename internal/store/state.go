package store

import (
	"database/sql"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Close reasons the hub shows before the bridge reports the real bead.
const (
	ReasonDone      = "Done in Meat Mode"
	ReasonDismissed = "Dismissed"
)

// provisionalTTL bounds how long an applied add is shown as a provisional
// item while waiting for the bridge to report the real bead.
const provisionalTTL = 24 * time.Hour

// State returns every item, repo and bridge, with pending actions folded
// into the items (marked Pending). User is left for the caller to fill.
func (s *Store) State() (api.State, error) {
	st := api.State{ServerTime: s.now()}
	var err error
	if st.Bridges, err = s.Bridges(); err != nil {
		return st, err
	}
	if st.Repos, err = s.Repos(); err != nil {
		return st, err
	}
	repoSynced := map[string]*time.Time{}
	if err := s.eachRow(`SELECT r.bridge, r.name, r.synced_at FROM repos r
		JOIN bridges b ON b.name = r.bridge AND b.revoked_at IS NULL
		WHERE r.personal = 0 OR r.bridge = ?`, []any{HubBridge}, func(rows *sql.Rows) error {
		var b, n string
		var synced sql.NullString
		if err := rows.Scan(&b, &n, &synced); err != nil {
			return err
		}
		repoSynced[b+"/"+n] = parseTimePtr(synced)
		return nil
	}); err != nil {
		return st, err
	}

	st.Items = []api.Item{}
	index := map[string]int{}
	if err := s.eachRow(`SELECT i.bridge, i.repo, i.data, i.first_seen, i.seen_at FROM items i
		JOIN bridges b ON b.name = i.bridge AND b.revoked_at IS NULL
		ORDER BY i.bridge, i.repo, i.id`, nil, func(rows *sql.Rows) error {
		var b, r, data, first string
		var seenAt sql.NullString
		if err := rows.Scan(&b, &r, &data, &first, &seenAt); err != nil {
			return err
		}
		it, err := decodeItem(b, r, data, first, seenAt)
		if err != nil {
			return err
		}
		if b == HubBridge && hiddenClosed(it, st.ServerTime) {
			return nil
		}
		index[it.Key] = len(st.Items)
		st.Items = append(st.Items, it)
		return nil
	}); err != nil {
		return st, err
	}

	if err := s.joinRuns(st.Items, index, st.Bridges, st.ServerTime); err != nil {
		return st, err
	}

	// The latest settled-or-pending action on each item decides its error.
	if err := s.eachRow(`SELECT a.bridge, a.repo, a.bead_id, a.error FROM actions a
		JOIN (SELECT MAX(seq) AS m FROM actions WHERE status != ? AND bead_id != ''
		      GROUP BY bridge, repo, bead_id) l ON a.seq = l.m
		WHERE a.status = ?`, []any{api.StatusCancelled, api.StatusFailed}, func(rows *sql.Rows) error {
		var b, r, id, msg string
		if err := rows.Scan(&b, &r, &id, &msg); err != nil {
			return err
		}
		if i, ok := index[ItemKey(b, r, id)]; ok {
			st.Items[i].Error = msg
		}
		return nil
	}); err != nil {
		return st, err
	}

	pending, err := s.queryActions(`SELECT `+actionCols+` FROM actions a
		WHERE status = ? AND EXISTS (SELECT 1 FROM bridges b WHERE b.name = a.bridge AND b.revoked_at IS NULL)
		ORDER BY seq`, api.StatusPending)
	if err != nil {
		return st, err
	}
	st.Unapplied = len(pending)
	// The latest pending sync or agents switch on a repo shows where it was
	// switched to.
	repoAt := map[string]int{}
	for i, r := range st.Repos {
		repoAt[r.Bridge+"/"+r.Name] = i
	}
	for _, a := range pending {
		i, ok := repoAt[a.Bridge+"/"+a.Repo]
		if !ok || a.Payload.On == nil {
			continue
		}
		switch a.Kind {
		case api.KindSync:
			st.Repos[i].OptedIn, st.Repos[i].Pending = *a.Payload.On, true
		case api.KindAgents:
			st.Repos[i].AgentsAllowed, st.Repos[i].AgentsPending = *a.Payload.On, true
		}
	}

	since := fmtTime(st.ServerTime.Add(-provisionalTTL))
	added, err := s.queryActions(`SELECT `+actionCols+` FROM actions
		WHERE kind = ? AND status = ? AND applied_at >= ? ORDER BY seq`, api.KindAdd, api.StatusApplied, since)
	if err != nil {
		return st, err
	}
	// An applied add stays provisional until the real bead appears or the
	// bridge sends that repo's set again.
	var shown []api.Action
	for _, a := range added {
		synced, ok := repoSynced[a.Bridge+"/"+a.Repo]
		if !ok {
			continue
		}
		if _, real := index[ItemKey(a.Bridge, a.Repo, a.BeadID)]; real {
			continue
		}
		if synced != nil && a.AppliedAt != nil && !synced.Before(*a.AppliedAt) {
			continue
		}
		shown = append(shown, a)
	}
	shown = append(shown, pending...)

	// An applied note, or ask with a note, keeps showing its comment until
	// the bridge sends that repo's set again, as an add keeps its item.
	noted, err := s.queryActions(`SELECT `+actionCols+` FROM actions
		WHERE kind IN (?, ?) AND status = ? AND applied_at >= ? AND bridge != ? ORDER BY seq`,
		api.KindNote, api.KindAsk, api.StatusApplied, since, HubBridge)
	if err != nil {
		return st, err
	}
	for _, a := range noted {
		synced := repoSynced[a.Bridge+"/"+a.Repo]
		if synced != nil && a.AppliedAt != nil && !synced.Before(*a.AppliedAt) {
			continue
		}
		if i, ok := index[ItemKey(a.Bridge, a.Repo, a.BeadID)]; ok {
			apply(&st.Items[i], a, provisionalPrefix+a.ID)
		}
	}

	for _, a := range shown {
		if a.Kind == api.KindAdd {
			if _, ok := repoSynced[a.Bridge+"/"+a.Repo]; !ok {
				continue
			}
			st.Items = append(st.Items, provisional(a))
			continue
		}
		i, ok := index[ItemKey(a.Bridge, a.Repo, a.BeadID)]
		if !ok {
			continue
		}
		fold(&st.Items[i], a, s.isApplying(a))
	}
	return st, nil
}

// provisional is the item an add shows until the bridge reports the bead.
func provisional(a api.Action) api.Item {
	id := provisionalPrefix + a.ID
	return api.Item{
		Key:       ItemKey(a.Bridge, a.Repo, id),
		Bridge:    a.Bridge,
		Repo:      a.Repo,
		ID:        id,
		Title:     a.Payload.Title,
		Status:    "open",
		Priority:  2,
		Labels:    []string{"human"},
		CreatedBy: a.Author,
		DueAt:     a.Payload.Due,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.CreatedAt,
		Comments:  []api.Comment{},
		Pending:   true,
		FirstSeen: a.CreatedAt,
		Run:       provisionalRun(a),
	}
}

// provisionalRun is the run an add with ask shows until the bridge reports
// the bead and its run.
func provisionalRun(a api.Action) *api.Run {
	if !a.Payload.Ask {
		return nil
	}
	return &api.Run{
		ID:        provisionalPrefix + a.ID,
		State:     api.RunWorking,
		Agent:     "agent",
		Activity:  startingActivity,
		StartedAt: a.CreatedAt,
		Count:     1,
	}
}

// fold applies one pending action to the hub's copy of an item. An action
// the bridge's run list already holds as applied is in the run it reported,
// so only its note is folded.
func fold(it *api.Item, a api.Action, applying bool) {
	it.Pending = true
	apply(it, a, provisionalPrefix+a.ID)
	if !applying {
		foldRun(it, a)
	}
}

// isApplying reports whether a's bridge says in its last run list that it
// has applied a.
func (s *Store) isApplying(a api.Action) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applying[a.Bridge][a.ID]
}

func (s *Store) eachRow(query string, args []any, fn func(*sql.Rows) error) error {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
