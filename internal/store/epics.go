package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Epic rows are a read-only projection of a repo's tracker for the Epics
// view (docs/api.md, Epics). They live in epic_rows, never in items, so no
// path that serves the person's own to-dos (State, reminders, push, the
// calendar feed, the export) reads them. A change to them emits only a
// Change of Kind "epics", which the hub sends as `event: epics`.

// ReplaceEpics replaces the complete epic set of one repo, as
// api.CheckEpicRows cleans it, and sets when the hub last took one. It
// answers a conflict, never a not-found, when the bridge's latest repo
// report does not hold the repo or shows its epics off, so the bridge
// reports its repos again first. An unchanged set still emits a change.
func (s *Store) ReplaceEpics(bridge, repo string, rows []api.EpicRow) error {
	if err := api.CheckEpicRows(rows); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var on bool
	err = tx.QueryRow(`SELECT epics_on FROM repos WHERE bridge = ? AND name = ? AND personal = 0`, bridge, repo).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return conflictf("repo %q is not in this bridge's repo list; send PUT /bridge/v1/repos first", repo)
	}
	if err != nil {
		return err
	}
	if !on {
		return conflictf("repo %q is not reported with epicsOn; send PUT /bridge/v1/repos first", repo)
	}
	if _, err := tx.Exec(`DELETE FROM epic_rows WHERE bridge = ? AND repo = ?`, bridge, repo); err != nil {
		return err
	}
	for _, r := range rows {
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO epic_rows (bridge, repo, id, data) VALUES (?, ?, ?, ?)`,
			bridge, repo, r.ID, string(data)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE repos SET epics_synced_at = ? WHERE bridge = ? AND name = ?`,
		fmtTime(s.now()), bridge, repo); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.emit(Change{Kind: "epics", Bridge: bridge, Repo: repo})
	return nil
}

// Epics returns GET /api/epics: the epic rows of every repo of an active
// bridge whose epics are on and that has sent a set, ordered by bridge then
// repo, rows by id. A repo whose epics switch-off is pending is left out.
func (s *Store) Epics() (api.Epics, error) {
	out := api.Epics{Repos: []api.RepoEpics{}}
	at := map[string]int{}
	if err := s.eachRow(`SELECT r.bridge, r.name, r.tracker, r.epics_synced_at FROM repos r
		JOIN bridges b ON b.name = r.bridge AND b.revoked_at IS NULL
		WHERE r.personal = 0 AND r.epics_on = 1 AND r.epics_synced_at IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM actions a WHERE a.bridge = r.bridge AND a.repo = r.name
				AND a.kind = ? AND a.status = ?)
		ORDER BY r.bridge, r.name`, []any{api.KindEpics, api.StatusPending}, func(rows *sql.Rows) error {
		var re api.RepoEpics
		var synced string
		if err := rows.Scan(&re.Bridge, &re.Repo, &re.Tracker, &synced); err != nil {
			return err
		}
		re.SyncedAt, re.Rows = parseTime(synced), []api.EpicRow{}
		at[re.Bridge+"/"+re.Repo] = len(out.Repos)
		out.Repos = append(out.Repos, re)
		return nil
	}); err != nil {
		return out, err
	}
	err := s.eachRow(`SELECT bridge, repo, data FROM epic_rows ORDER BY bridge, repo, id`, nil, func(rows *sql.Rows) error {
		var b, r, data string
		if err := rows.Scan(&b, &r, &data); err != nil {
			return err
		}
		i, ok := at[b+"/"+r]
		if !ok {
			return nil
		}
		var row api.EpicRow
		if err := json.Unmarshal([]byte(data), &row); err != nil {
			return err
		}
		out.Repos[i].Rows = append(out.Repos[i].Rows, row)
		return nil
	})
	if err != nil {
		return out, err
	}
	linked, err := linkedLists(s.db, s.now(), s.onlineWindow())
	if err != nil {
		return out, err
	}
	out.Repos = oneEpicSet(out.Repos, linked)
	return out, nil
}

// oneEpicSet keeps one epic set of each linked list, so its epics show
// once: the first repo's, in the list's order, whose bridge is online,
// else the first repo's that has a set.
func oneEpicSet(repos []api.RepoEpics, l links) []api.RepoEpics {
	has := map[string]bool{}
	for _, re := range repos {
		has[re.Bridge+"/"+re.Repo] = true
	}
	keep := map[string]bool{}
	for _, g := range l.groups {
		pick := ""
		for _, m := range g.members {
			if has[m.key()] && m.online {
				pick = m.key()
				break
			}
		}
		if pick == "" {
			for _, m := range g.members {
				if has[m.key()] {
					pick = m.key()
					break
				}
			}
		}
		keep[pick] = true
	}
	out := repos[:0]
	for _, re := range repos {
		key := re.Bridge + "/" + re.Repo
		if _, linked := l.group(key); linked && !keep[key] {
			continue
		}
		out = append(out, re)
	}
	return out
}

// epicsHeld sums up what a bridge holds for the Epics view, the rows and
// the repos with a set, so a repo report can tell whether it dropped any.
func epicsHeld(tx *sql.Tx, bridge string) (int, error) {
	var n int
	err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM epic_rows WHERE bridge = ?) +
		(SELECT COUNT(*) FROM repos WHERE bridge = ? AND epics_synced_at IS NOT NULL)`, bridge, bridge).Scan(&n)
	return n, err
}
