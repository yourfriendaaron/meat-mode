package store

import (
	"archive/zip"
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// ErrHubInUse refuses a restore into a hub that already holds data: any
// item, any action, or any machine other than the hub, revoked ones
// included. It matches ErrConflict, so the hub answers 409.
var ErrHubInUse error = &userError{ErrConflict, "This hub already has data, so it cannot be restored into. " +
	"Restore goes only into a fresh hub, so nothing a hub holds is lost; start a new hub with an empty data directory and restore there."}

// CancelledByRestore is the error a restore gives each action that was
// still pending, on a machine's repo or on a list the hub keeps, so a
// machine paired again never replays it and nothing waits forever.
const CancelledByRestore = "Not sent: the hub was restored from an export."

// exportUpgrades turns an export of version i+1 into one of version i+2,
// one step each, so a file of any older version reaches api.ExportVersion
// one step at a time. When ExportVersion goes up, add the step from the
// version before; TestExportUpgradesReachTheCurrentVersion holds the count.
var exportUpgrades = []func(json.RawMessage) (json.RawMessage, error){
	upgradeExportV1,
	upgradeExportV2,
}

// upgradeExportV1 turns version 1 into version 2, which added attachments
// and the tags of hub list items. A version 1 file has no attachments. Its
// items may carry labels from beads moved off a machine's personal repo,
// which version 1 never showed; those that break the tag rules are dropped,
// so a file that restored before still restores.
func upgradeExportV1(raw json.RawMessage) (json.RawMessage, error) {
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	file["version"] = json.RawMessage("2")
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(file["items"], &items); err != nil {
		return nil, err
	}
	for _, it := range items {
		var labels []string
		if json.Unmarshal(it["labels"], &labels) != nil {
			continue
		}
		b, err := json.Marshal(keptLabels(labels))
		if err != nil {
			return nil, err
		}
		it["labels"] = b
	}
	b, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	file["items"] = b
	return json.Marshal(file)
}

// restoreKnownStatuses are the item statuses a restored item may have.
var restoreKnownStatuses = []string{"open", "in_progress", "blocked", "deferred", "closed"}

// restoreActionStatuses are the action statuses a restored action may have.
var restoreActionStatuses = []string{api.StatusPending, api.StatusApplied, api.StatusFailed, api.StatusCancelled}

// The restore's body limits: the JSON of an export, bare or in its zip,
// and a whole zip, which adds the attachments' files.
const (
	MaxExportJSON  = 64 << 20
	MaxRestoreFile = MaxExportJSON + api.MaxAttachmentsBytes
)

// zipMagic starts a zip file; anything else is read as bare JSON.
var zipMagic = []byte("PK\x03\x04")

// Restore loads an export (docs/api.md, Export and restore) into a hub that
// holds no data yet: the zip an export is, read by its first bytes, or the
// bare JSON file of an older hub. It reads the file, upgrades an older
// version step by step, checks all of it, and only then writes it, the
// attachments' files first and the rest in one transaction, so a file that
// fails a check changes nothing. A hub with data answers ErrHubInUse and is
// left as it was. The settings already set are replaced by the export's;
// the setup wizard is not recorded as done. An error reading r is returned
// wrapped, so the caller can tell a body cut short or too large.
func (s *Store) Restore(r io.Reader) (api.RestoreResult, error) {
	// A second restore waits, then finds the hub in use.
	s.restoring.Lock()
	defer s.restoring.Unlock()
	if err := s.checkUnused(s.db); err != nil {
		return api.RestoreResult{}, err
	}
	br := bufio.NewReader(r)
	if head, _ := br.Peek(len(zipMagic)); bytes.Equal(head, zipMagic) {
		return s.restoreZip(br)
	}
	file, err := readLimited(br, MaxExportJSON, "This file is larger than an export can be (%s).")
	if err != nil {
		return api.RestoreResult{}, err
	}
	ex, err := readExport(file)
	if err != nil {
		return api.RestoreResult{}, err
	}
	if n := countAttachments(ex); n > 0 {
		return api.RestoreResult{}, invalidf("This file lists %d attached files, which travel in the zip file it came in. Restore that zip file instead.", n)
	}
	if err := checkExport(&ex); err != nil {
		return api.RestoreResult{}, err
	}
	return s.load(ex)
}

// readLimited reads all of r, refusing more than limit bytes with
// ErrTooLarge and tooBig, which takes the limit's size.
func readLimited(r io.Reader, limit int64, tooBig string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read the file: %w", err)
	}
	if int64(len(b)) > limit {
		return nil, tooLargef(tooBig, fileSize(limit))
	}
	return b, nil
}

func countAttachments(ex api.Export) int {
	n := 0
	for _, it := range ex.Items {
		n += len(it.Attachments)
	}
	return n
}

// checkUnused answers ErrHubInUse for a hub with any item, any action, or
// any machine other than the hub, revoked ones included.
func (s *Store) checkUnused(q rowQuerier) error {
	var used bool
	if err := q.QueryRow(`SELECT EXISTS (SELECT 1 FROM items) OR EXISTS (SELECT 1 FROM actions)
		OR EXISTS (SELECT 1 FROM bridges WHERE name != ?)`, HubBridge).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrHubInUse
	}
	return nil
}

// restoreZip restores an export's zip. Go reads a zip from its end, so the
// upload is saved to a temporary file in the attachments folder first and
// deleted when done. Entry names are checked, never used as paths: each
// file is written under the id export.json gives it.
func (s *Store) restoreZip(r io.Reader) (api.RestoreResult, error) {
	var res api.RestoreResult
	key := "restore-" + newAttachmentID()
	defer s.track(key)()
	spool := filepath.Join(s.attachmentDir(), tempPrefix+key)
	f, err := os.OpenFile(spool, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return res, err
	}
	defer os.Remove(spool)
	defer f.Close()
	size, err := io.Copy(f, io.LimitReader(r, MaxRestoreFile+1))
	if err != nil {
		return res, fmt.Errorf("read the file: %w", err)
	}
	if size > MaxRestoreFile {
		return res, tooLargef("This file is larger than an export can be (%s).", fileSize(MaxRestoreFile))
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return res, invalidf("This file is not a Meat Mode export: it is not a zip file that can be read (%v).", err)
	}

	entries := map[string]*zip.File{}
	for _, zf := range zr.File {
		if entries[zf.Name] != nil {
			return res, invalidf("This export holds %q twice.", zf.Name)
		}
		if id, ok := strings.CutPrefix(zf.Name, api.ExportAttachmentDir); zf.Name != api.ExportJSONName && (!ok || !validAttachmentID(id)) {
			return res, invalidf("This export holds %q, which is not part of an export: an export holds only %s and %s<id> for each attached file.",
				zf.Name, api.ExportJSONName, api.ExportAttachmentDir)
		}
		entries[zf.Name] = zf
	}
	je := entries[api.ExportJSONName]
	if je == nil {
		return res, invalidf("This file is not a Meat Mode export: the zip has no %s.", api.ExportJSONName)
	}
	jr, err := je.Open()
	if err != nil {
		return res, invalidf("This export's %s could not be read: %v", api.ExportJSONName, err)
	}
	file, err := readLimited(jr, MaxExportJSON, "This export's "+api.ExportJSONName+" is larger than it can be (%s).")
	jr.Close()
	if err != nil {
		if !errors.Is(err, ErrTooLarge) {
			err = invalidf("This export's %s could not be read: %v", api.ExportJSONName, err)
		}
		return res, err
	}
	ex, err := readExport(file)
	if err != nil {
		return res, err
	}
	if err := checkExport(&ex); err != nil {
		return res, err
	}

	// Every attachment listed has its entry with its size, and every entry
	// is listed.
	listed := map[string]bool{api.ExportJSONName: true}
	var ids []string
	for _, it := range ex.Items {
		for _, a := range it.Attachments {
			name := api.ExportAttachmentDir + a.ID
			listed[name] = true
			ids = append(ids, a.ID)
			zf := entries[name]
			if zf == nil {
				return res, invalidf("%s (attachment %s) on item %s has no file in the export.", a.Name, a.ID, it.Key)
			}
			if zf.UncompressedSize64 != uint64(a.Size) {
				return res, invalidf("%s (attachment %s) on item %s is %d bytes in the export, but its item lists %d.",
					a.Name, a.ID, it.Key, zf.UncompressedSize64, a.Size)
			}
		}
	}
	for name := range entries {
		if !listed[name] {
			return res, invalidf("This export holds %s, which no item lists.", name)
		}
	}

	// The files go in before the rows, each under the id its item gives,
	// checked against its size and SHA-256 as it is written; any failure
	// deletes the ones written.
	defer s.track(ids...)()
	var written []string
	ok := false
	defer func() {
		if !ok {
			for _, id := range written {
				os.Remove(s.attachmentPath(id))
			}
		}
	}()
	for i := range ex.Items {
		it := &ex.Items[i]
		for j := range it.Attachments {
			a := &it.Attachments[j]
			if err := s.restoreFile(entries[api.ExportAttachmentDir+a.ID], it.Key, a, &written); err != nil {
				return res, err
			}
		}
	}
	res, err = s.load(ex)
	if err != nil {
		return res, err
	}
	ok = true
	return res, nil
}

// restoreFile writes one attachment's entry as its file and checks it
// against the size and SHA-256 the export lists. The type is sniffed from
// the bytes, as for an upload.
func (s *Store) restoreFile(zf *zip.File, key string, a *api.Attachment, written *[]string) error {
	rc, err := zf.Open()
	if err != nil {
		return invalidf("%s (attachment %s) on item %s could not be read from the export: %v", a.Name, a.ID, key, err)
	}
	defer rc.Close()
	in := &readErr{r: rc}
	size, sum, typ, err := s.writeAttachmentFile(a.ID, in, a.Size)
	if errors.Is(err, errOverLimit) || (err == nil && (size != a.Size || sum != a.SHA256)) {
		if err == nil {
			*written = append(*written, a.ID)
		}
		return invalidf("%s (attachment %s) on item %s does not match the size and SHA-256 its item lists.", a.Name, a.ID, key)
	}
	if in.err != nil {
		return invalidf("%s (attachment %s) on item %s could not be read from the export: %v", a.Name, a.ID, key, in.err)
	}
	if err != nil {
		return fmt.Errorf("write %s (attachment %s) on item %s: %w", a.Name, a.ID, key, err)
	}
	*written = append(*written, a.ID)
	a.Type = typ
	return nil
}

// readErr remembers the first error reading r, so a broken entry in the
// zip can be told from a failure to write its file.
type readErr struct {
	r   io.Reader
	err error
}

func (e *readErr) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

// load writes a checked export in one transaction.
func (s *Store) load(ex api.Export) (api.RestoreResult, error) {
	var res api.RestoreResult
	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if err := s.checkUnused(tx); err != nil {
		return res, err
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
		item.Run, item.Attachments = nil, nil
		if err := putHubItem(tx, it.Repo, item, first, seen); err != nil {
			return res, err
		}
		if it.Owner != "" {
			if err := recordOwner(tx, it.Repo, it.ID, it.Owner); err != nil {
				return res, err
			}
		}
	}
	// Rows in the file's order, oldest first, after every item they name.
	for _, it := range ex.Items {
		for _, a := range it.Attachments {
			if err := insertAttachment(tx, it.Repo, it.ID, a); err != nil {
				return res, err
			}
			res.Attachments++
		}
	}

	for _, a := range ex.Actions {
		// A pending action has nothing to apply it once the log is loaded:
		// a machine may never pair again, and an action on a list the hub
		// keeps was applied by the hub that wrote the file, not this one.
		if a.Status == api.StatusPending {
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
	files := map[string]bool{}
	var total int64
	for i := range ex.Items {
		it := &ex.Items[i]
		if err := checkExportItem(it); err != nil {
			return err
		}
		if keys[it.Key] {
			return invalidf("item %s is listed twice", it.Key)
		}
		keys[it.Key] = true
		for _, a := range it.Attachments {
			if files[a.ID] {
				return invalidf("attachment %s is listed twice", a.ID)
			}
			files[a.ID] = true
			total += a.Size
		}
	}
	if total > api.MaxAttachmentsBytes {
		return invalidf("The export's files come to %s; a hub keeps at most %s.", fileSize(total), fileSize(api.MaxAttachmentsBytes))
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
		if overTruncated(a.Author, maxCommentAuthorLen) {
			return invalidf("action %s has an author longer than %d characters", a.ID, maxCommentAuthorLen)
		}
		if strings.ContainsFunc(a.Author, unicode.IsControl) {
			return invalidf("action %s has an author with a control character in it", a.ID)
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

// overTruncated reports whether s is longer than the hub leaves text it
// truncates: limit runes and an ellipsis (api.TruncRunes). A hub's own
// export holds text cut that way, from a bridge or an agent.
func overTruncated(s string, limit int) bool {
	return utf8.RuneCountInString(s) > limit+1
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
	if overTruncated(it.Description, maxDescriptionLen) {
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
	if overTruncated(it.ParentTitle, maxTitleLen) {
		return fail("parentTitle is longer than %d characters", maxTitleLen)
	}
	if overTruncated(it.CreatedBy, maxCommentAuthorLen) {
		return fail("createdBy is longer than %d characters", maxCommentAuthorLen)
	}
	if overTruncated(it.CloseReason, api.DetailLimit) {
		return fail("closeReason is longer than %d characters", api.DetailLimit)
	}
	// A comment from a bridge's bead keeps the id its tracker gave it, which
	// may be empty, so only its size is checked.
	for _, c := range it.Comments {
		if overTruncated(c.ID, maxCommentAuthorLen) {
			return fail("a comment has an id longer than %d characters", maxCommentAuthorLen)
		}
		if overTruncated(c.Text, maxCommentLen) {
			return fail("comment %s is longer than %d characters", c.ID, maxCommentLen)
		}
		if overTruncated(c.Author, maxCommentAuthorLen) {
			return fail("comment %s has an author longer than %d characters", c.ID, maxCommentAuthorLen)
		}
	}
	labels, err := cleanLabels(it.Labels)
	if err != nil {
		return fail("%v", err)
	}
	it.Labels = labels
	if len(it.Attachments) > api.MaxAttachmentsPerItem {
		return fail("it has %d files; an item can hold at most %d", len(it.Attachments), api.MaxAttachmentsPerItem)
	}
	for _, a := range it.Attachments {
		if !validAttachmentID(a.ID) {
			return fail("attachment %q has an id that is not 32 lowercase hex characters", a.ID)
		}
		if clean, err := cleanAttachmentName(a.Name); err != nil || clean != a.Name {
			return fail("attachment %s has a name %q that is not a clean file name", a.ID, a.Name)
		}
		if a.Size < 0 || a.Size > api.MaxAttachmentBytes {
			return fail("%s (attachment %s) is %s; a file can be at most %s", a.Name, a.ID, fileSize(a.Size), fileSize(api.MaxAttachmentBytes))
		}
		if !isLowerHex(a.SHA256, 64) {
			return fail("attachment %s has a sha256 that is not 64 lowercase hex characters", a.ID)
		}
		if a.CreatedAt.IsZero() {
			return fail("attachment %s has no time", a.ID)
		}
	}
	if it.Owner != "" && (!ValidBridgeName(it.Owner) || onHub(it.Owner)) {
		return fail("owner %q is not a valid machine name", it.Owner)
	}
	if it.CreatedAt.IsZero() {
		return fail("has no time")
	}
	if it.FirstSeen.IsZero() {
		it.FirstSeen = it.CreatedAt
	}
	it.Pending, it.Error, it.Run = false, "", nil
	return nil
}
