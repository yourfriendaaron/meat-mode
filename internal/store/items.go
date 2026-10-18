package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// ValidRepoName reports whether name can be a repo name; see
// api.ValidRepoName.
func ValidRepoName(name string) bool { return api.ValidRepoName(name) }

func validSegment(s string) bool { return api.ValidSegment(s) }

// ItemKey joins bridge, repo and bead id into an item key.
func ItemKey(bridge, repo, id string) string { return bridge + "/" + repo + "/" + id }

// SplitKey splits an item key into bridge, repo and bead id. The bridge name
// and bead id hold no slash, so the repo is everything between the first
// and the last.
func SplitKey(key string) (bridge, repo, id string, ok bool) {
	first, last := strings.IndexByte(key, '/'), strings.LastIndexByte(key, '/')
	if first < 0 || last == first {
		return "", "", "", false
	}
	bridge, repo, id = key[:first], key[first+1:last], key[last+1:]
	if !ValidBridgeName(bridge) || !ValidRepoName(repo) || !validSegment(id) {
		return "", "", "", false
	}
	return bridge, repo, id, true
}

// ReplaceRepos replaces a bridge's repo list. Repos missing from it are
// dropped along with their items, and a repo that is no longer opted in
// loses its items. A repo stored under an older bridge's folder-name key
// (garden) moves to the one new path key ending in that name
// (projects/garden), with its items, actions and reminders; see
// renameRepo.
func (s *Store) ReplaceRepos(bridge string, repos []api.Repo) error {
	seen := map[string]bool{}
	for _, r := range repos {
		if !ValidRepoName(r.Name) {
			return invalidf("repo name %q is not valid", r.Name)
		}
		if seen[r.Name] {
			return invalidf("repo %q is listed twice", r.Name)
		}
		seen[r.Name] = true
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, err := queryStrings(tx, `SELECT name FROM repos WHERE bridge = ?`, bridge)
	if err != nil {
		return err
	}
	if err := migrateFolderNames(tx, bridge, existing, repos); err != nil {
		return err
	}
	for _, r := range repos {
		if _, err := tx.Exec(`INSERT INTO repos (bridge, name, personal, opted_in, agents_allowed) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (bridge, name) DO UPDATE SET personal = excluded.personal, opted_in = excluded.opted_in,
				agents_allowed = excluded.agents_allowed`,
			bridge, r.Name, r.Personal && r.Name == PersonalRepo, r.OptedIn, r.AgentsAllowed); err != nil {
			return err
		}
	}
	existing, err = queryStrings(tx, `SELECT name FROM repos WHERE bridge = ?`, bridge)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM items WHERE bridge = ? AND repo IN
		(SELECT name FROM repos WHERE bridge = ? AND personal = 0 AND opted_in = 0)`, bridge, bridge); err != nil {
		return err
	}
	for _, name := range existing {
		if !seen[name] {
			if _, err := tx.Exec(`DELETE FROM repos WHERE bridge = ? AND name = ?`, bridge, name); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.emit(Change{Kind: "repos", Bridge: bridge})
	return nil
}

// migrateFolderNames renames each stored single-segment repo that the new
// list lacks to the one new path key whose last segment it is. A name that
// two new keys end in is ambiguous and is left to be dropped.
func migrateFolderNames(tx *sql.Tx, bridge string, existing []string, repos []api.Repo) error {
	stored := map[string]bool{}
	for _, n := range existing {
		stored[n] = true
	}
	byBase := map[string][]string{}
	for _, r := range repos {
		if strings.Contains(r.Name, "/") && !stored[r.Name] {
			base := path.Base(r.Name)
			byBase[base] = append(byBase[base], r.Name)
		}
	}
	listed := map[string]bool{}
	for _, r := range repos {
		listed[r.Name] = true
	}
	for _, old := range existing {
		if strings.Contains(old, "/") || listed[old] || len(byBase[old]) != 1 {
			continue
		}
		if err := renameRepo(tx, bridge, old, byBase[old][0]); err != nil {
			return err
		}
	}
	return nil
}

// renameRepo moves a repo and everything keyed by it to a new name.
func renameRepo(tx *sql.Tx, bridge, from, to string) error {
	oldPrefix, newPrefix := bridge+"/"+from+"/", bridge+"/"+to+"/"
	tables, err := reminderTablesIn(tx)
	if err != nil {
		return err
	}
	stmts := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO repos (bridge, name, personal, opted_in, agents_allowed, synced_at)
			SELECT bridge, ?, personal, opted_in, agents_allowed, synced_at FROM repos WHERE bridge = ? AND name = ?`, []any{to, bridge, from}},
		{`UPDATE items SET repo = ? WHERE bridge = ? AND repo = ?`, []any{to, bridge, from}},
		{`UPDATE runs SET repo = ? WHERE bridge = ? AND repo = ?`, []any{to, bridge, from}},
		{`UPDATE actions SET repo = ? WHERE bridge = ? AND repo = ?`, []any{to, bridge, from}},
		{`DELETE FROM repos WHERE bridge = ? AND name = ?`, []any{bridge, from}},
	}
	for _, table := range tables {
		keys, err := queryStrings(tx, `SELECT DISTINCT item_key FROM `+table)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if rest, ok := strings.CutPrefix(k, oldPrefix); ok {
				stmts = append(stmts, struct {
					q    string
					args []any
				}{`UPDATE ` + table + ` SET item_key = ? WHERE item_key = ?`, []any{newPrefix + rest, k}})
			}
		}
	}
	for _, st := range stmts {
		if _, err := tx.Exec(st.q, st.args...); err != nil {
			return fmt.Errorf("rename repo %q to %q: %w", from, to, err)
		}
	}
	return nil
}

// Repos lists the repos of active bridges.
func (s *Store) Repos() ([]api.Repo, error) {
	// A bridge's own personal repo is only a source for the personal list.
	rows, err := s.db.Query(`SELECT r.bridge, r.name, r.personal, r.opted_in, r.agents_allowed FROM repos r
		JOIN bridges b ON b.name = r.bridge AND b.revoked_at IS NULL
		WHERE r.personal = 0 OR r.bridge = ? ORDER BY r.bridge, r.name`, HubBridge)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.Repo{}
	for rows.Next() {
		var r api.Repo
		if err := rows.Scan(&r.Bridge, &r.Name, &r.Personal, &r.OptedIn, &r.AgentsAllowed); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReplaceItems replaces the complete item set of one repo. Items missing
// from the set are dropped; items still there keep their seen and
// first-seen times. The set of a personal repo, which only an older bridge
// sends, goes to the personal list instead; see TransferPersonal.
func (s *Store) ReplaceItems(bridge, repo string, items []api.Item) error {
	seen := map[string]bool{}
	for i := range items {
		it := &items[i]
		if !validSegment(it.ID) {
			return invalidf("item id %q is not valid", it.ID)
		}
		if strings.HasPrefix(it.ID, provisionalPrefix) {
			return invalidf("item id %q uses the reserved prefix %q", it.ID, provisionalPrefix)
		}
		if seen[it.ID] {
			return invalidf("item %q is listed twice", it.ID)
		}
		seen[it.ID] = true
		// Hub-only fields never come from a bridge.
		it.Key, it.Bridge, it.Repo = "", "", ""
		it.Pending, it.SeenAt, it.Error, it.FirstSeen, it.Run = false, nil, "", time.Time{}, nil
		it.Title = truncRunes(it.Title, maxTitleLen)
		it.Description = truncRunes(it.Description, maxDescriptionLen)
		for j := range it.Comments {
			it.Comments[j].Text = truncRunes(it.Comments[j].Text, maxCommentLen)
			it.Comments[j].Author = truncRunes(it.Comments[j].Author, maxCommentAuthorLen)
		}
		if it.Comments == nil {
			it.Comments = []api.Comment{}
		}
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
		return notFoundf("repo %q is not in this bridge's repo list; send PUT /bridge/v1/repos first", repo)
	}
	if legacy, err := isLegacyPersonal(tx, bridge, repo); err != nil {
		return err
	} else if legacy {
		return s.importFromBridge(tx, bridge, repo, items)
	}
	existing, err := queryStrings(tx, `SELECT id FROM items WHERE bridge = ? AND repo = ?`, bridge, repo)
	if err != nil {
		return err
	}
	had := map[string]bool{}
	for _, id := range existing {
		had[id] = true
		if !seen[id] {
			if _, err := tx.Exec(`DELETE FROM items WHERE bridge = ? AND repo = ? AND id = ?`, bridge, repo, id); err != nil {
				return err
			}
		}
	}
	now := fmtTime(s.now())
	var fresh []string
	for _, it := range items {
		data, err := json.Marshal(it)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO items (bridge, repo, id, data, first_seen) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (bridge, repo, id) DO UPDATE SET data = excluded.data`,
			bridge, repo, it.ID, string(data), now); err != nil {
			return err
		}
		if !had[it.ID] {
			fresh = append(fresh, ItemKey(bridge, repo, it.ID))
		}
	}
	if _, err := tx.Exec(`UPDATE repos SET synced_at = ? WHERE bridge = ? AND name = ?`, now, bridge, repo); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.emit(Change{Kind: "items", Bridge: bridge, Repo: repo, NewItems: fresh})
	return nil
}

// decodeItem rebuilds one stored item with its hub-local times.
func decodeItem(bridge, repo, data, first string, seenAt sql.NullString) (api.Item, error) {
	var it api.Item
	if err := json.Unmarshal([]byte(data), &it); err != nil {
		return it, err
	}
	it.Bridge, it.Repo = bridge, repo
	it.Key = ItemKey(bridge, repo, it.ID)
	it.FirstSeen = parseTime(first)
	it.SeenAt = parseTimePtr(seenAt)
	if it.Comments == nil {
		it.Comments = []api.Comment{}
	}
	return it, nil
}

// MarkSeen records that the user opened the item now.
func (s *Store) MarkSeen(key string) error {
	b, r, id, ok := SplitKey(key)
	if !ok {
		return invalidf("item key %q is not valid", key)
	}
	res, err := s.db.Exec(`UPDATE items SET seen_at = ? WHERE bridge = ? AND repo = ? AND id = ?`,
		fmtTime(s.now()), b, r, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFoundf("no item %q", key)
	}
	s.emit(Change{Kind: "seen", Bridge: b, Repo: r})
	return nil
}

type rowsQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func queryStrings(q rowsQuerier, query string, args ...any) ([]string, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
