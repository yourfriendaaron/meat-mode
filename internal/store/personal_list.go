package store

import (
	"database/sql"
	"slices"
	"sort"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Statuses a PersonalFilter takes.
const (
	FilterOpen   = "open"
	FilterClosed = "closed"
	FilterAll    = "all"
)

// ListPersonal returns the items of the personal list, or of the project
// when it is not empty, that the app shows and f matches, oldest first,
// for any paired machine's agents to read.
func (s *Store) ListPersonal(project string, f api.PersonalFilter) ([]api.Item, error) {
	repo, err := ProjectRepo(project)
	if err != nil {
		return nil, err
	}
	switch f.Status {
	case "":
		f.Status = FilterOpen
	case FilterOpen, FilterClosed, FilterAll:
	default:
		return nil, invalidf("status must be open, closed or all")
	}
	query := strings.ToLower(strings.TrimSpace(f.Query))
	now := s.now()
	out := []api.Item{}
	if err := s.eachRow(`SELECT data, first_seen, seen_at FROM items WHERE bridge = ? AND repo = ?`,
		[]any{HubBridge, repo}, func(rows *sql.Rows) error {
			var data, first string
			var seen sql.NullString
			if err := rows.Scan(&data, &first, &seen); err != nil {
				return err
			}
			it, err := decodeItem(HubBridge, repo, data, first, seen)
			if err != nil {
				return err
			}
			if hiddenClosed(it, now) {
				return nil
			}
			closed := it.Status == "closed"
			if (f.Status == FilterOpen && closed) || (f.Status == FilterClosed && !closed) {
				return nil
			}
			if query != "" && !strings.Contains(strings.ToLower(it.Title), query) {
				return nil
			}
			for _, tag := range f.Tags {
				if !slices.ContainsFunc(it.Labels, func(l string) bool { return strings.EqualFold(l, strings.TrimSpace(tag)) }) {
					return nil
				}
			}
			out = append(out, it)
			return nil
		}); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].FirstSeen.Equal(out[j].FirstSeen) {
			return out[i].FirstSeen.Before(out[j].FirstSeen)
		}
		return out[i].ID < out[j].ID
	})
	return out, joinAttachments(s.db, out)
}

// Projects returns the projects the app shows, those with an item open or
// closed in the last 7 days, by name.
func (s *Store) Projects() ([]api.Project, error) {
	now := s.now()
	open := map[string]int{}
	if err := s.eachRow(`SELECT repo, data, first_seen, seen_at FROM items WHERE bridge = ? AND repo != ?`,
		[]any{HubBridge, PersonalRepo}, func(rows *sql.Rows) error {
			var repo, data, first string
			var seen sql.NullString
			if err := rows.Scan(&repo, &data, &first, &seen); err != nil {
				return err
			}
			it, err := decodeItem(HubBridge, repo, data, first, seen)
			if err != nil {
				return err
			}
			if hiddenClosed(it, now) {
				return nil
			}
			if it.Status != "closed" {
				open[repo]++
			} else if _, ok := open[repo]; !ok {
				open[repo] = 0
			}
			return nil
		}); err != nil {
		return nil, err
	}
	out := []api.Project{}
	for name, n := range open {
		out = append(out, api.Project{Name: name, Open: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
