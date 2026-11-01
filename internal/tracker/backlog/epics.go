package backlog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// EpicRows implements tracker.Epics. `backlog task list` leaves out the
// tasks Backlog.md has moved off the board, so the rows are three reads:
//
//   - the listed tasks;
//   - the files in the store's completed/ folder, which the list does not
//     print and which are done tasks, so a card's progress counts them;
//   - the files in archive/tasks/ whose status is the closed one. An
//     archived task in any other status was put away unfinished, not done,
//     and is left out.
//
// The list leaves out descriptions; the view of each card, a task that is
// not closed and is of type epic or has a child, supplies its own.
func (b *Tracker) EpicRows(ctx context.Context, dir string) ([]api.EpicRow, error) {
	statuses, err := b.statuses(ctx, dir)
	if err != nil {
		return nil, err
	}
	tasks, err := b.listAll(ctx, dir)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		seen[idKey(t.ID)] = true
	}
	root := filepath.Join(dir, backlogDir(dir))
	for _, sub := range []struct {
		path       string
		closedOnly bool
	}{{"completed", false}, {filepath.Join("archive", "tasks"), true}} {
		files, err := readTaskFiles(filepath.Join(root, sub.path))
		if err != nil {
			return nil, err
		}
		for _, t := range files {
			if seen[idKey(t.ID)] || (sub.closedOnly && mapStatus(t.Status, statuses, true) != tracker.StatusClosed) {
				continue
			}
			seen[idKey(t.ID)] = true
			tasks = append(tasks, t)
		}
	}
	hasChild := map[string]bool{}
	for _, t := range tasks {
		if p := deref(t.ParentID); p != "" {
			hasChild[idKey(p)] = true
		}
	}
	rows := make([]api.EpicRow, 0, len(tasks))
	for _, t := range tasks {
		is := toIssue(t, statuses)
		if is.Status != tracker.StatusClosed && (strings.EqualFold(deref(t.Type), "epic") || hasChild[idKey(t.ID)]) {
			v, err := b.view(ctx, dir, t.ID)
			if err != nil {
				return nil, err
			}
			is.Description = deref(v.Description)
		}
		r := api.EpicRow{
			ID:          is.ID,
			Title:       is.Title,
			Type:        deref(t.Type),
			Status:      is.Status,
			Priority:    is.Priority,
			Parent:      is.Parent,
			Human:       hasLabel(t.Labels, tracker.HumanLabel),
			Description: is.Description,
			UpdatedAt:   is.UpdatedAt,
			ClosedAt:    is.ClosedAt,
		}
		if len(t.Assignees) > 0 {
			r.Assignee = t.Assignees[0]
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// readTaskFiles reads the tasks in a folder of task files, as much of each
// as EpicRows needs. A missing folder holds none. A file that does not read
// is an error, so the set is never partial.
func readTaskFiles(folder string) ([]taskJSON, error) {
	entries, err := os.ReadDir(folder)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []taskJSON
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(folder, e.Name()))
		if err != nil {
			return nil, err
		}
		if t, ok := parseTaskFile(string(data)); ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// parseTaskFile reads the front matter of a task file: the scalar keys and
// the lists Backlog.md writes as "key:" followed by "  - item" lines. A file
// without an id or a front matter is not a task.
func parseTaskFile(data string) (taskJSON, bool) {
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		return taskJSON{}, false
	}
	scalars := map[string]string{}
	lists := map[string][]string{}
	key := ""
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		if item, ok := strings.CutPrefix(strings.TrimLeft(l, " "), "- "); ok && key != "" && strings.HasPrefix(l, " ") {
			lists[key] = append(lists[key], unquote(item))
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok || strings.HasPrefix(l, " ") {
			continue
		}
		key = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if inner, ok := strings.CutPrefix(v, "["); ok {
			for part := range strings.SplitSeq(strings.TrimSuffix(inner, "]"), ",") {
				if p := unquote(part); p != "" {
					lists[key] = append(lists[key], p)
				}
			}
			continue
		}
		scalars[key] = unquote(v)
	}
	if scalars["id"] == "" {
		return taskJSON{}, false
	}
	opt := func(k string) *string {
		if v := scalars[k]; v != "" && v != "null" {
			return &v
		}
		return nil
	}
	return taskJSON{
		ID:        scalars["id"],
		Title:     scalars["title"],
		Status:    scalars["status"],
		Priority:  opt("priority"),
		Type:      opt("type"),
		Assignees: lists["assignee"],
		Labels:    lists["labels"],
		ParentID:  opt("parent_task_id"),
		CreatedAt: opt("created_date"),
		UpdatedAt: opt("updated_date"),
	}, true
}

// unquote strips one pair of the quotes YAML puts round a scalar. Backlog.md
// writes single quotes, doubling any inside.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		switch {
		case s[0] == '\'' && s[len(s)-1] == '\'':
			return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
		case s[0] == '"' && s[len(s)-1] == '"':
			return s[1 : len(s)-1]
		}
	}
	return s
}
