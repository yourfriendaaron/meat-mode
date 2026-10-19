package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// ErrHubInUse refuses a restore into a hub that already holds data: any
// item, any action, or any machine other than the hub, revoked ones
// included. It matches ErrConflict, so the hub answers 409.
var ErrHubInUse error = &userError{ErrConflict, "This hub already has data, so it cannot be restored into. " +
	"Restore goes only into a fresh hub, so nothing a hub holds is lost; start a new hub with an empty data directory and restore there."}

// CancelledByRestore is the error a restore gives each action that was
// still waiting for a machine, so a machine paired again never replays it.
const CancelledByRestore = "Not sent: the hub was restored from an export."

// exportUpgrades turns an export of version i+1 into one of version i+2,
// one step each, so a file of any older version reaches api.ExportVersion
// one step at a time. When ExportVersion goes up, add the step from the
// version before; TestExportUpgradesReachTheCurrentVersion holds the count.
var exportUpgrades = []func(json.RawMessage) (json.RawMessage, error){
	upgradeExportV1,
}

// upgradeExportV1 turns version 1 into version 2, which added attachments
// and the tags of hub list items. A version 1 file has neither, so only its
// version changes.
func upgradeExportV1(raw json.RawMessage) (json.RawMessage, error) {
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	file["version"] = json.RawMessage("2")
	return json.Marshal(file)
}

// restoreKnownStatuses are the item statuses a restored item may have.
var restoreKnownStatuses = []string{"open", "in_progress", "blocked", "deferred", "closed"}

// restoreActionStatuses are the action statuses a restored action may have.
var restoreActionStatuses = []string{api.StatusPending, api.StatusApplied, api.StatusFailed, api.StatusCancelled}

// Restore loads an export (docs/api.md, Export and restore) into a hub that
// holds no data yet. It reads the file, upgrades an older version step by
// step, checks all of it, and only then writes it in one transaction, so a
// file that fails a check changes nothing. A hub with data answers
// ErrHubInUse and is left as it was. The settings already set are
// replaced by the export's; the setup wizard is not recorded as done.
func (s *Store) Restore(file []byte) (api.RestoreResult, error) {
	var res api.RestoreResult
	ex, err := readExport(file)
	if err != nil {
		return res, err
	}
	if err := checkExport(&ex); err != nil {
		return res, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	var used bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM items) OR EXISTS (SELECT 1 FROM actions)
		OR EXISTS (SELECT 1 FROM bridges WHERE name != ?)`, HubBridge).Scan(&used); err != nil {
		return res, err
	}
	if used {
		return res, ErrHubInUse
	}

	if _, err := tx.Exec(`UPDATE hub_settings SET user_name = ?, time_zone = ?, reminder_lead = ?, calendar_hash = NULL WHERE id = 1`,
		ex.Settings.Name, ex.Settings.TimeZone, ex.Settings.ReminderLead); err != nil {
		return res, err
	}

	for _, it := range ex.Items {
		if it.Repo != PersonalRepo {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO repos (bridge, name, personal, opted_in) VALUES (?, ?, 0, 1)`,
				HubBridge, it.Repo); err != nil {
				return res, err
			}
		}
		first, seen := it.FirstSeen, it.SeenAt
		item := it.Item
		item.Run = nil
		if err := putHubItem(tx, it.Repo, item, first, seen); err != nil {
			return res, err
		}
		if it.Owner != "" {
			if err := recordOwner(tx, it.Repo, it.ID, it.Owner); err != nil {
				return res, err
			}
		}
	}

	for _, a := range ex.Actions {
		if a.Status == api.StatusPending && !onHub(a.Bridge) {
			a.Status, a.Error = api.StatusCancelled, CancelledByRestore
			res.Cancelled++
		}
		payload, err := json.Marshal(a.Payload)
		if err != nil {
			return res, err
		}
		if _, err := tx.Exec(`INSERT INTO actions (seq, id, kind, bridge, repo, bead_id, payload, author, status, error, created_at, applied_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.Seq, a.ID, a.Kind, a.Bridge, a.Repo, a.BeadID, string(payload), a.Author, a.Status, a.Error,
			fmtTime(a.CreatedAt), fmtTimePtr(a.AppliedAt)); err != nil {
			return res, err
		}
	}

	if len(ex.Reminders) > 0 {
		if err := createReminderTables(tx); err != nil {
			return res, err
		}
	}
	for _, r := range ex.Reminders {
		table := `reminder_fired (item_key, fire_at)`
		if r.Kind == api.ReminderEarly {
			table = `reminder_early (item_key, due_at)`
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO `+table+` VALUES (?, ?)`, r.Key, r.At.UnixMilli()); err != nil {
			return res, err
		}
	}

	for _, o := range ex.Origins {
		if _, err := tx.Exec(`INSERT INTO personal_origins (bridge, bead_id, item_id) VALUES (?, ?, ?)`,
			o.Machine, o.BeadID, o.ItemID); err != nil {
			return res, err
		}
	}

	if err := tx.Commit(); err != nil {
		return res, err
	}
	// No NewItems: what a restore loads is not news, so nothing is pushed
	// for it.
	s.emit(Change{Kind: "items", Bridge: HubBridge})
	s.emit(Change{Kind: "actions"})

	res.Items, res.Actions, res.Reminders = len(ex.Items), len(ex.Actions), len(ex.Reminders)
	res.Machines = ex.Machines
	if res.Machines == nil {
		res.Machines = []string{}
	}
	res.CalendarFeed = ex.Settings.CalendarFeed
	return res, nil
}

// createReminderTables makes the reminder scheduler's tables of reminders
// sent, as internal/reminders creates them, when the scheduler has not yet.
func createReminderTables(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS reminder_fired (
		item_key TEXT NOT NULL,
		fire_at  INTEGER NOT NULL, -- unix milliseconds
		PRIMARY KEY (item_key, fire_at)
	);
	CREATE TABLE IF NOT EXISTS reminder_early (
		item_key TEXT NOT NULL,
		due_at   INTEGER NOT NULL, -- unix milliseconds
		PRIMARY KEY (item_key, due_at)
	)`)
	return err
}

// notAnExport is the error for a file that is not a Meat Mode export.
func notAnExport() error {
	return invalidf("This file is not a Meat Mode export.")
}

// readExport reads an export file: its format and version first, then,
// once an older version is upgraded to the current one, the whole of it.
// A field this hub does not know is refused, since each version names
// every field it holds.
func readExport(file []byte) (api.Export, error) {
	var ex api.Export
	var head struct {
		Format  string          `json:"format"`
		Version json.RawMessage `json:"version"`
	}
	if err := json.Unmarshal(file, &head); err != nil || head.Format != api.ExportFormat {
		return ex, notAnExport()
	}
	var version int
	if err := json.Unmarshal(head.Version, &version); err != nil || version < 1 {
		return ex, notAnExport()
	}
	if version > api.ExportVersion {
		return ex, invalidf("This export is from a newer hub: the file is version %d, and this hub reads version %d and older. Update the hub, then restore again.",
			version, api.ExportVersion)
	}
	raw := json.RawMessage(file)
	for v := version; v < api.ExportVersion; v++ {
		var err error
		if raw, err = exportUpgrades[v-1](raw); err != nil {
			return ex, invalidf("This export could not be upgraded from version %d: %v", v, err)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ex); err != nil {
		return ex, invalidf("This export could not be read: %v", err)
	}
	ex.Version = api.ExportVersion
	return ex, nil
}

// checkExport checks everything an export holds before any of it is
// written, and cleans what it may: items lose the fields the hub sets
// itself. Each error names what failed.
func checkExport(ex *api.Export) error {
	set := ex.Settings
	name, err := cleanUserName(set.Name)
	if err != nil {
		return invalidf("settings: %v", err)
	}
	ex.Settings.Name = name
	// A hub that never chose a time zone exports none.
	if set.TimeZone != "" {
		if err := checkTimeZone(set.TimeZone); err != nil {
			return invalidf("settings: %v", err)
		}
	}
	if err := checkReminderLead(set.ReminderLead); err != nil {
		return invalidf("settings: %v", err)
	}

	keys := map[string]bool{}
	for i := range ex.Items {
		it := &ex.Items[i]
		if err := checkExportItem(it); err != nil {
			return err
		}
		if keys[it.Key] {
			return invalidf("item %s is listed twice", it.Key)
		}
		keys[it.Key] = true
	}

	ids := map[string]bool{}
	var last int64
	for _, a := range ex.Actions {
		if !ValidActionID(a.ID) {
			return invalidf("action %q: an action id must be a UUID", a.ID)
		}
		if ids[a.ID] {
			return invalidf("action %s is listed twice", a.ID)
		}
		ids[a.ID] = true
		if a.Seq <= last {
			return invalidf("action %s is out of order: actions are listed oldest first, each with a higher seq", a.ID)
		}
		last = a.Seq
		if a.Kind == "" {
			return invalidf("action %s has no kind", a.ID)
		}
		if !slices.Contains(restoreActionStatuses, a.Status) {
			return invalidf("action %s has an unknown status %q", a.ID, a.Status)
		}
		if !onHub(a.Bridge) && !ValidBridgeName(a.Bridge) {
			return invalidf("action %s names a machine %q that is not a valid machine name", a.ID, a.Bridge)
		}
		if a.CreatedAt.IsZero() {
			return invalidf("action %s has no time", a.ID)
		}
	}

	for _, r := range ex.Reminders {
		if !keys[r.Key] {
			return invalidf("a reminder names %s, which is not an item in the file", r.Key)
		}
		if r.Kind != api.ReminderDue && r.Kind != api.ReminderEarly {
			return invalidf("a reminder for %s has an unknown kind %q", r.Key, r.Kind)
		}
		if r.At.IsZero() {
			return invalidf("a reminder for %s has no time", r.Key)
		}
	}

	origins := map[[2]string]bool{}
	for _, o := range ex.Origins {
		if !ValidBridgeName(o.Machine) || onHub(o.Machine) || !validSegment(o.BeadID) || !validSegment(o.ItemID) {
			return invalidf("an origin {%q, %q, %q} is not valid", o.Machine, o.BeadID, o.ItemID)
		}
		k := [2]string{o.Machine, o.BeadID}
		if origins[k] {
			return invalidf("the origin of %s from %s is listed twice", o.BeadID, o.Machine)
		}
		origins[k] = true
	}

	for _, m := range ex.Machines {
		if !ValidBridgeName(m) || onHub(m) {
			return invalidf("machine %q is not a valid machine name", m)
		}
	}
	return nil
}

// checkExportItem checks one item of an export against the rules an item
// of the hub's lists keeps, and clears the fields the hub sets itself.
func checkExportItem(it *api.ExportItem) error {
	if it.Bridge != HubBridge {
		return invalidf("item %q is on %q: an export holds only the lists the hub keeps", it.ID, it.Bridge)
	}
	if it.Repo != PersonalRepo {
		if _, err := ProjectRepo(it.Repo); err != nil {
			return invalidf("item %q: list %q is neither the personal list nor a project name", it.ID, it.Repo)
		}
	}
	if !api.ValidBeadID(it.ID) {
		return invalidf("item %q in %s has an id that is not valid", it.ID, it.Repo)
	}
	key := ItemKey(HubBridge, it.Repo, it.ID)
	if it.Key != "" && it.Key != key {
		return invalidf("item %s has the key %q, which does not match its list and id", key, it.Key)
	}
	it.Key = key
	fail := func(format string, a ...any) error {
		return invalidf("item %s: %s", key, fmt.Sprintf(format, a...))
	}

	title, err := api.CheckTitle(it.Title)
	if err != nil {
		return fail("%v", err)
	}
	it.Title = title
	if !slices.Contains(restoreKnownStatuses, it.Status) {
		return fail("unknown status %q", it.Status)
	}
	// An item moved from a bridge's personal repo may hold a description
	// as long as a bridge may send, longer than an edit sets.
	if utf8.RuneCountInString(it.Description) > maxDescriptionLen {
		return fail("description is longer than %d characters", maxDescriptionLen)
	}
	if err := api.CheckPriority(it.Priority); err != nil {
		return fail("%v", err)
	}
	if it.Parent != "" && (!validSegment(it.Parent) || it.Parent == it.ID) {
		return fail("parent %q is not valid", it.Parent)
	}
	if it.RemindBefore != nil && (*it.RemindBefore < 0 || *it.RemindBefore > api.MaxLeadMinutes) {
		return fail("remindBefore must be 0 to %d minutes", api.MaxLeadMinutes)
	}
	if it.Repeat != nil {
		if it.DueAt == nil {
			return fail("a repeat needs a due date")
		}
		if _, err := checkRepeat(*it.Repeat); err != nil {
			return fail("%v", err)
		}
	}
	if len(it.Checklist) > MaxChecklist {
		return fail("a checklist holds at most %d entries", MaxChecklist)
	}
	checks := map[string]bool{}
	for _, c := range it.Checklist {
		if strings.TrimSpace(c.ID) == "" || checks[c.ID] {
			return fail("checklist entry %q has no id of its own", c.Text)
		}
		checks[c.ID] = true
		if strings.TrimSpace(c.Text) == "" {
			return fail("a checklist entry cannot be empty")
		}
		if utf8.RuneCountInString(c.Text) > MaxTitleLen {
			return fail("a checklist entry is longer than %d characters", MaxTitleLen)
		}
	}
	// A comment from a bridge's bead keeps the id its tracker gave it, which
	// may be empty, so only its size is checked.
	for _, c := range it.Comments {
		if utf8.RuneCountInString(c.Text) > maxCommentLen {
			return fail("comment %s is longer than %d characters", c.ID, maxCommentLen)
		}
		if utf8.RuneCountInString(c.Author) > maxCommentAuthorLen {
			return fail("comment %s has an author longer than %d characters", c.ID, maxCommentAuthorLen)
		}
	}
	if it.Owner != "" && (!ValidBridgeName(it.Owner) || onHub(it.Owner)) {
		return fail("owner %q is not a valid machine name", it.Owner)
	}
	if it.FirstSeen.IsZero() {
		it.FirstSeen = it.CreatedAt
	}
	it.Pending, it.Error, it.Run = false, "", nil
	return nil
}
