package store

import (
	"database/sql"
	"slices"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Export reads what an export holds (docs/api.md, Export and restore) in
// one read transaction, so the file is one moment's state. It reads no
// secret column: no token or password hash, session, setup code, calendar
// feed hash or push subscription.
func (s *Store) Export() (api.Export, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return api.Export{}, err
	}
	defer tx.Rollback()
	e := api.Export{Format: api.ExportFormat, Version: api.ExportVersion, ExportedAt: s.now()}
	if e.Settings, err = exportSettings(tx); err != nil {
		return e, err
	}
	if e.Items, err = exportItems(tx); err != nil {
		return e, err
	}
	if e.Actions, err = queryActionsTx(tx, `SELECT `+actionCols+` FROM actions ORDER BY seq`); err != nil {
		return e, err
	}
	if e.Actions == nil {
		e.Actions = []api.Action{}
	}
	if e.Reminders, err = exportReminders(tx, e.Items); err != nil {
		return e, err
	}
	if e.Origins, err = exportOrigins(tx); err != nil {
		return e, err
	}
	if e.Machines, err = queryStrings(tx, `SELECT name FROM bridges
		WHERE revoked_at IS NULL AND name != ? ORDER BY name`, HubBridge); err != nil {
		return e, err
	}
	if e.Machines == nil {
		e.Machines = []string{}
	}
	return e, nil
}

// exportSettings reads the settings kept in the app; of the calendar feed,
// only whether it is on.
func exportSettings(tx *sql.Tx) (api.ExportSettings, error) {
	var st api.ExportSettings
	err := tx.QueryRow(`SELECT user_name, time_zone, reminder_lead, calendar_hash IS NOT NULL
		FROM hub_settings WHERE id = 1`).Scan(&st.Name, &st.TimeZone, &st.ReminderLead, &st.CalendarFeed)
	return st, err
}

// exportItems reads every item of the lists the hub keeps, closed ones
// included, with its owner.
func exportItems(tx *sql.Tx) ([]api.ExportItem, error) {
	rows, err := tx.Query(`SELECT i.repo, i.data, i.first_seen, i.seen_at, COALESCE(o.bridge, '')
		FROM items i LEFT JOIN personal_owners o ON o.repo = i.repo AND o.item_id = i.id
		WHERE i.bridge = ? ORDER BY i.repo, i.first_seen, i.id`, HubBridge)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.ExportItem{}
	for rows.Next() {
		var repo, data, first, owner string
		var seen sql.NullString
		if err := rows.Scan(&repo, &data, &first, &seen, &owner); err != nil {
			return nil, err
		}
		it, err := decodeItem(HubBridge, repo, data, first, seen)
		if err != nil {
			return nil, err
		}
		it.Run, it.Pending, it.Error = nil, false, ""
		out = append(out, api.ExportItem{Item: it, Owner: owner})
	}
	return out, rows.Err()
}

// exportReminders reads the reminders already sent for the exported items,
// from those of the scheduler's tables that exist. A mark for a key the
// export does not hold is left out, so every key names an item in the file.
func exportReminders(tx *sql.Tx, items []api.ExportItem) ([]api.ExportReminder, error) {
	tables, err := reminderTablesIn(tx)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(items))
	for _, it := range items {
		keys[it.Key] = true
	}
	kinds := []struct{ table, col, kind string }{
		{"reminder_fired", "fire_at", api.ReminderDue},
		{"reminder_early", "due_at", api.ReminderEarly},
	}
	out := []api.ExportReminder{}
	for _, k := range kinds {
		if !slices.Contains(tables, k.table) {
			continue
		}
		rows, err := tx.Query(`SELECT item_key, `+k.col+` FROM `+k.table+`
			WHERE item_key LIKE ? ORDER BY item_key, `+k.col, HubBridge+"/%")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key string
			var ms int64
			if err := rows.Scan(&key, &ms); err != nil {
				rows.Close()
				return nil, err
			}
			if keys[key] {
				out = append(out, api.ExportReminder{Key: key, Kind: k.kind, At: time.UnixMilli(ms).UTC()})
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// exportOrigins reads which machine's personal repo bead each moved item
// came from.
func exportOrigins(tx *sql.Tx) ([]api.ExportOrigin, error) {
	rows, err := tx.Query(`SELECT bridge, bead_id, item_id FROM personal_origins ORDER BY bridge, bead_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.ExportOrigin{}
	for rows.Next() {
		var o api.ExportOrigin
		if err := rows.Scan(&o.Machine, &o.BeadID, &o.ItemID); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
