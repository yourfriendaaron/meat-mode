package store

import (
	"archive/zip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	if err := s.checkExportFiles(e.Items); err != nil {
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
	var items []api.Item
	var owners []string
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
		if it.Labels != nil {
			it.Labels = keptLabels(it.Labels)
		}
		items = append(items, it)
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := joinAttachments(tx, items); err != nil {
		return nil, err
	}
	out := make([]api.ExportItem, len(items))
	for i, it := range items {
		out[i] = api.ExportItem{Item: it, Owner: owners[i]}
	}
	return out, nil
}

// checkExportFiles checks that the file of every attachment an export
// lists is on disk with its size, so an export that cannot hold a file
// stops before it starts rather than leave the file out.
func (s *Store) checkExportFiles(items []api.ExportItem) error {
	for _, it := range items {
		for _, a := range it.Attachments {
			fi, err := os.Stat(s.attachmentPath(a.ID))
			if err != nil || fi.Size() != a.Size {
				return &userError{ErrFileMissing, fmt.Sprintf("The file of %s (attachment %s) on item %s is missing or changed in the hub's data directory, "+
					"so the export stops rather than leave it out.", a.Name, a.ID, it.Key)}
			}
		}
	}
	return nil
}

// WriteExportZip writes e as the export's zip (docs/api.md, Export and
// restore): export.json, then the bytes of each attachment it lists,
// stored without compression, as it goes. A file that cannot be read whole,
// or whose bytes no longer match its size and SHA-256, fails the export:
// what was written so far is not a whole export.
func (s *Store) WriteExportZip(w io.Writer, e api.Export) error {
	zw := zip.NewWriter(w)
	jw, err := zw.CreateHeader(&zip.FileHeader{Name: api.ExportJSONName, Method: zip.Deflate, Modified: e.ExportedAt})
	if err != nil {
		return err
	}
	if err := json.NewEncoder(jw).Encode(e); err != nil {
		return err
	}
	for _, it := range e.Items {
		for _, a := range it.Attachments {
			if err := s.writeExportFile(zw, it.Key, a); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

func (s *Store) writeExportFile(zw *zip.Writer, key string, a api.Attachment) error {
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: api.ExportAttachmentDir + a.ID, Method: zip.Store, Modified: a.CreatedAt})
	if err != nil {
		return err
	}
	f, err := os.Open(s.attachmentPath(a.ID))
	if err != nil {
		return fmt.Errorf("export: the file of %s (attachment %s) on item %s: %w", a.Name, a.ID, key, err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(fw, h), io.LimitReader(f, a.Size+1))
	if err != nil {
		return fmt.Errorf("export: the file of %s (attachment %s) on item %s: %w", a.Name, a.ID, key, err)
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return fmt.Errorf("export: the file of %s (attachment %s) on item %s no longer matches its size and SHA-256", a.Name, a.ID, key)
	}
	return nil
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
