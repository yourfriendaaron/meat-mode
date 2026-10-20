package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Items of the hub's lists once carried a checklist, which sub-tasks
// replaced. Schema 14 converts the checklists a hub still holds, and the
// upgrade of an export from version 2 converts those in the file, both by
// convertChecklist.

// legacyCheck is one entry of an item's checklist as schema 13 and export
// version 2 kept it.
type legacyCheck struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// convertChecklist turns parent's checklist into sub-tasks, one per entry
// in order, each in the parent's list, done entries closed as done, all
// made at at. A parent that cannot hold sub-tasks, a sub-task itself (they
// go one level deep) or a closed item (a closed parent holds no open
// sub-task), gets the entries appended to its description as a Markdown
// list instead, done ones struck through. taken reports whether an id is
// already used in the list; newID is marked taken by the caller. It returns
// the parent as changed and the new sub-tasks.
func convertChecklist(parent api.Item, checks []legacyCheck, at time.Time, taken func(id string) (bool, error)) (api.Item, []api.Item, error) {
	var texts []legacyCheck
	for _, c := range checks {
		c.Text = strings.TrimSpace(c.Text)
		if c.Text != "" {
			texts = append(texts, c)
		}
	}
	if len(texts) == 0 {
		return parent, nil, nil
	}
	if parent.Parent != "" || parent.Status == "closed" {
		lines := make([]string, len(texts))
		for i, c := range texts {
			text := strings.ReplaceAll(c.Text, "\n", " ")
			if c.Done {
				text = "~~" + text + "~~"
			}
			lines[i] = "- " + text
		}
		d := strings.TrimRight(parent.Description, " \t\n")
		if d != "" {
			d += "\n\n"
		}
		parent.Description = truncRunes(d+strings.Join(lines, "\n"), maxDescriptionLen)
		parent.UpdatedAt = laterOf(parent.UpdatedAt, at)
		return parent, nil, nil
	}
	subs := make([]api.Item, 0, len(texts))
	// The app lists sub-tasks by id, so the numbers are padded to one width.
	width := len(fmt.Sprint(len(texts)))
	for i, c := range texts {
		id := ""
		for n := 1; ; n++ {
			id = fmt.Sprintf("%s.%0*d", parent.ID, width, i+1)
			if n > 1 {
				id = fmt.Sprintf("%s-%d", id, n)
			}
			used, err := taken(id)
			if err != nil {
				return parent, nil, err
			}
			if !used {
				break
			}
		}
		sub := api.Item{
			ID:        id,
			Title:     truncRunes(c.Text, MaxTitleLen),
			Status:    "open",
			Priority:  2,
			CreatedBy: parent.CreatedBy,
			Comments:  []api.Comment{},
			Parent:    parent.ID,
			CreatedAt: at,
			UpdatedAt: at,
		}
		if c.Done {
			sub.Status, sub.CloseReason, sub.ClosedAt = "closed", ReasonDone, ptr(at)
		}
		subs = append(subs, sub)
	}
	parent.UpdatedAt = laterOf(parent.UpdatedAt, at)
	return parent, subs, nil
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// migrateChecklists is schema 14: every item of the hub's lists that holds
// a checklist gets it as sub-tasks by convertChecklist, and its checklist
// is cleared. An item with none is left as it is, so it runs again safely.
func (s *Store) migrateChecklists(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT repo, id, json_extract(data, '$.checklist') FROM items
		WHERE bridge = ? AND json_extract(data, '$.checklist') IS NOT NULL ORDER BY repo, first_seen, id`, HubBridge)
	if err != nil {
		return err
	}
	type held struct {
		repo, id string
		checks   []legacyCheck
	}
	var all []held
	for rows.Next() {
		var h held
		var raw string
		if err := rows.Scan(&h.repo, &h.id, &raw); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(raw), &h.checks); err != nil {
			rows.Close()
			return fmt.Errorf("checklist of %s: %w", ItemKey(HubBridge, h.repo, h.id), err)
		}
		all = append(all, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	at := s.now()
	for _, h := range all {
		it, err := hubItem(tx, h.repo, h.id)
		if err != nil {
			return err
		}
		repo := h.repo
		taken := func(id string) (bool, error) {
			var n int
			err := tx.QueryRow(`SELECT COUNT(*) FROM items WHERE bridge = ? AND repo = ? AND id = ?`, HubBridge, repo, id).Scan(&n)
			return n > 0, err
		}
		parent, subs, err := convertChecklist(it, h.checks, at, taken)
		if err != nil {
			return err
		}
		// putHubItem writes the item without its checklist, which api.Item
		// no longer holds.
		if err := putHubItem(tx, h.repo, parent, parent.FirstSeen, parent.SeenAt); err != nil {
			return err
		}
		for _, sub := range subs {
			if err := putHubItem(tx, h.repo, sub, at, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// upgradeExportV2 turns version 2 into version 3, which has no
// checklists: each item's checklist becomes sub-tasks by convertChecklist,
// made at the export's time, and edits in the action log lose their
// checklist field.
func upgradeExportV2(raw json.RawMessage) (json.RawMessage, error) {
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	file["version"] = json.RawMessage("3")
	var at time.Time
	if r, ok := file["exportedAt"]; ok {
		if err := json.Unmarshal(r, &at); err != nil {
			return nil, err
		}
	}
	var items []map[string]json.RawMessage
	if r, ok := file["items"]; ok && string(r) != "null" {
		if err := json.Unmarshal(r, &items); err != nil {
			return nil, err
		}
	}
	ids := map[string]bool{}
	for _, it := range items {
		var key struct {
			Repo string `json:"repo"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(mustMarshal(it), &key) == nil {
			ids[key.Repo+"/"+key.ID] = true
		}
	}
	out := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		rawChecks, has := it["checklist"]
		delete(it, "checklist")
		var checks []legacyCheck
		if has {
			if err := json.Unmarshal(rawChecks, &checks); err != nil {
				return nil, fmt.Errorf("a checklist could not be read: %w", err)
			}
		}
		if len(checks) == 0 {
			out = append(out, mustMarshal(it))
			continue
		}
		var parent api.ExportItem
		if err := json.Unmarshal(mustMarshal(it), &parent); err != nil {
			return nil, err
		}
		taken := func(id string) (bool, error) { return ids[parent.Repo+"/"+id], nil }
		item, subs, err := convertChecklist(parent.Item, checks, at, taken)
		if err != nil {
			return nil, err
		}
		// Only the fields the conversion changes are written back, so the
		// rest of the item reaches the version 3 check as the file held it.
		for k, v := range map[string]any{"description": item.Description, "updatedAt": item.UpdatedAt} {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			it[k] = b
		}
		out = append(out, mustMarshal(it))
		for _, sub := range subs {
			ids[parent.Repo+"/"+sub.ID] = true
			sub.Bridge, sub.Repo = HubBridge, parent.Repo
			sub.Key = ItemKey(HubBridge, parent.Repo, sub.ID)
			sub.FirstSeen = at
			b, err := json.Marshal(api.ExportItem{Item: sub})
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	file["items"] = b

	var actions []map[string]json.RawMessage
	if r, ok := file["actions"]; ok && string(r) != "null" {
		if err := json.Unmarshal(r, &actions); err != nil {
			return nil, err
		}
		for _, a := range actions {
			var payload map[string]json.RawMessage
			if json.Unmarshal(a["payload"], &payload) != nil {
				continue
			}
			for _, field := range []string{"set", "was"} {
				var e map[string]json.RawMessage
				if json.Unmarshal(payload[field], &e) != nil || e == nil {
					continue
				}
				if _, ok := e["checklist"]; !ok {
					continue
				}
				delete(e, "checklist")
				payload[field] = mustMarshal(e)
			}
			a["payload"] = mustMarshal(payload)
		}
		file["actions"] = mustMarshal(actions)
	}
	return json.Marshal(file)
}

// mustMarshal marshals JSON values decoded a moment before, which cannot
// fail.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
