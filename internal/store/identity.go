package store

import (
	"database/sql"
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// A list's identity (docs/api.md, List identity): the colour slot the hub
// gives each repo row once and keeps with it, and the icon its bridge
// sends. Both go with the row: a dropped repo loses them, and a repo moved
// to a new folder name keeps its colour (renameRepo) while its bridge
// sends the icon again. The one exception to "once" is a workspace colour
// its bridge reports for the repo (SpaceColour), which sets the slot again
// (docs/api.md, Workspace colours).

// migrateIdentity adds repos.colour, NULL until the repo has a slot, and
// repo_icons, each repo's icon as its bridge last sent it, kept apart from
// items and dropped with the repo. The repos already held get their slots
// in the order the app lists them: the personal list first, then by the
// last segment of the name without regard to case, then by name and
// bridge. It is Go so that the tests of older migrations, which step
// user_version back, can run it again.
func (s *Store) migrateIdentity(tx *sql.Tx) error {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('repos') WHERE name = 'colour'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := tx.Exec(`ALTER TABLE repos ADD COLUMN colour INTEGER`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS repo_icons (
	bridge TEXT NOT NULL,
	repo   TEXT NOT NULL,
	hash   TEXT NOT NULL,
	type   TEXT NOT NULL,
	data   BLOB NOT NULL,
	PRIMARY KEY (bridge, repo),
	FOREIGN KEY (bridge, repo) REFERENCES repos(bridge, name) ON DELETE CASCADE
)`); err != nil {
		return err
	}
	_, err := fillColours(tx, listOrder)
	return err
}

// colourRow is a repo row waiting for its slot.
type colourRow struct{ bridge, name string }

// listOrder is the order the app lists repos in, close enough for slots:
// the personal list first, then by the last segment of the name without
// regard to case, then by name and bridge.
func listOrder(a, b colourRow) bool {
	if pa, pb := a.bridge == HubBridge && a.name == PersonalRepo, b.bridge == HubBridge && b.name == PersonalRepo; pa != pb {
		return pa
	}
	if la, lb := strings.ToLower(path.Base(a.name)), strings.ToLower(path.Base(b.name)); la != lb {
		return la < lb
	}
	if a.name != b.name {
		return a.name < b.name
	}
	return a.bridge < b.bridge
}

// nameOrder gives the new repos of one report their slots in name order.
func nameOrder(a, b colourRow) bool {
	if a.name != b.name {
		return a.name < b.name
	}
	return a.bridge < b.bridge
}

// fillColours gives each repo row the app can show that has no slot yet
// the slot held by the fewest repos that count, ties to the lowest, one
// after another in the given order. A repo counts when it is not hidden
// and its bridge is not revoked. A bridge's own personal repo, only a
// source for the personal list, neither counts nor gets a slot. It
// reports whether any row got one.
func fillColours(tx *sql.Tx, less func(a, b colourRow) bool) (bool, error) {
	var held [api.ListColours]int
	rows, err := tx.Query(`SELECT r.colour, COUNT(*) FROM repos r
		JOIN bridges b ON b.name = r.bridge AND b.revoked_at IS NULL
		WHERE r.colour IS NOT NULL AND r.hidden = 0 AND (r.personal = 0 OR r.bridge = ?)
		GROUP BY r.colour`, HubBridge)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var c, n int
		if err := rows.Scan(&c, &n); err != nil {
			rows.Close()
			return false, err
		}
		if c >= 0 && c < api.ListColours {
			held[c] = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}

	type waiting struct {
		colourRow
		counts bool
	}
	var todo []waiting
	rows, err = tx.Query(`SELECT r.bridge, r.name, r.hidden = 0 AND b.revoked_at IS NULL FROM repos r
		JOIN bridges b ON b.name = r.bridge
		WHERE r.colour IS NULL AND (r.personal = 0 OR r.bridge = ?)`, HubBridge)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var w waiting
		if err := rows.Scan(&w.bridge, &w.name, &w.counts); err != nil {
			rows.Close()
			return false, err
		}
		todo = append(todo, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	sort.Slice(todo, func(i, j int) bool { return less(todo[i].colourRow, todo[j].colourRow) })
	for _, w := range todo {
		slot := 0
		for c := 1; c < api.ListColours; c++ {
			if held[c] < held[slot] {
				slot = c
			}
		}
		if _, err := tx.Exec(`UPDATE repos SET colour = ? WHERE bridge = ? AND name = ?`, slot, w.bridge, w.name); err != nil {
			return false, err
		}
		if w.counts {
			held[slot]++
		}
	}
	return len(todo) > 0, nil
}

// SetIcon sets or, with no bytes, clears the icon of one of a bridge's
// repos (PUT /bridge/v1/icons). The repo must be in the bridge's latest
// report. Bytes with the hash the repo already has change nothing; any
// other change changes State.
func (s *Store) SetIcon(bridge, repo string, data []byte) error {
	var typ, hash string
	if len(data) > 0 {
		var ok bool
		if typ, ok = api.SniffIcon(data); !ok {
			return invalidf("the icon is not an SVG, PNG or ICO of at most %d bytes", api.MaxIconBytes)
		}
		hash = api.IconHash(data)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM repos WHERE bridge = ? AND name = ?`, bridge, repo).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return conflictf("repo %q is not in this bridge's latest repo report; send PUT /bridge/v1/repos first", repo)
	}
	var had string
	err = tx.QueryRow(`SELECT hash FROM repo_icons WHERE bridge = ? AND repo = ?`, bridge, repo).Scan(&had)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if had == hash {
		return nil
	}
	if hash == "" {
		_, err = tx.Exec(`DELETE FROM repo_icons WHERE bridge = ? AND repo = ?`, bridge, repo)
	} else {
		_, err = tx.Exec(`INSERT INTO repo_icons (bridge, repo, hash, type, data) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (bridge, repo) DO UPDATE SET hash = excluded.hash, type = excluded.type, data = excluded.data`,
			bridge, repo, hash, typ, data)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.emit(Change{Kind: "repos", Bridge: bridge, Repo: repo})
	return nil
}

// Icon returns the bytes and type of the icon with the given hash, from
// any repo holding it (GET /api/icons/{hash}).
func (s *Store) Icon(hash string) (typ string, data []byte, err error) {
	err = s.db.QueryRow(`SELECT type, data FROM repo_icons WHERE hash = ? LIMIT 1`, hash).Scan(&typ, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, notFoundf("no repo has the icon %q", hash)
	}
	return typ, data, err
}

// addProject makes the row of a project the hub keeps, with its colour
// slot, unless it has one.
func (s *Store) addProject(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO repos (bridge, name, personal, opted_in) VALUES (?, ?, 0, 1)`,
		HubBridge, name); err != nil {
		return err
	}
	if _, err := fillColours(tx, listOrder); err != nil {
		return err
	}
	return tx.Commit()
}
