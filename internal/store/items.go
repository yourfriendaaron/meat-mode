package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// ValidRepoName reports whether name can be a repo name: 1 to 128
// characters, no slash or control character, not "." or "..", and no
// leading "-" (which a command line would read as a flag).
func ValidRepoName(name string) bool { return validSegment(name) }

func validSegment(s string) bool {
	if s == "" || len(s) > 128 || s == "." || s == ".." || s[0] == '-' || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '/' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ItemKey joins bridge, repo and bead id into an item key.
func ItemKey(bridge, repo, id string) string { return bridge + "/" + repo + "/" + id }

// SplitKey splits an item key into bridge, repo and bead id.
func SplitKey(key string) (bridge, repo, id string, ok bool) {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 || !ValidBridgeName(parts[0]) || !validSegment(parts[1]) || !validSegment(parts[2]) {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// ReplaceRepos replaces a bridge's repo list. Repos missing from it are
// dropped along with their items, and a repo that is no longer opted in
// loses its items.
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
	for _, r := range repos {
		if _, err := tx.Exec(`INSERT INTO repos (bridge, name, personal, opted_in) VALUES (?, ?, ?, ?)
			ON CONFLICT (bridge, name) DO UPDATE SET personal = excluded.personal, opted_in = excluded.opted_in`,
			bridge, r.Name, r.Personal, r.OptedIn); err != nil {
			return err
		}
	}
	existing, err := queryStrings(tx, `SELECT name FROM repos WHERE bridge = ?`, bridge)
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

// Repos lists the repos of active bridges.
func (s *Store) Repos() ([]api.Repo, error) {
	rows, err := s.db.Query(`SELECT r.bridge, r.name, r.personal, r.opted_in FROM repos r
		JOIN bridges b ON b.name = r.bridge AND b.revoked_at IS NULL ORDER BY r.bridge, r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.Repo{}
	for rows.Next() {
		var r api.Repo
		if err := rows.Scan(&r.Bridge, &r.Name, &r.Personal, &r.OptedIn); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReplaceItems replaces the complete item set of one repo. Items missing
// from the set are dropped; items still there keep their seen and
// first-seen times.
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
		it.Pending, it.SeenAt, it.Error, it.FirstSeen = false, nil, "", time.Time{}
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
