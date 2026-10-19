package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Files attached to items of the lists the hub keeps (docs/api.md,
// Attachments). The bytes live in AttachmentDir in the data directory, one
// file per attachment named by its id; the metadata lives in the
// attachments table, keyed by the item.

// AttachmentDir is the folder in the data directory that holds the files.
const AttachmentDir = "attachments"

// The purge: a removed attachment is purged this long after removal, by a
// sweep that runs at start and at this interval.
const (
	AttachmentPurgeAfter = 24 * time.Hour
	AttachmentSweepEvery = 10 * time.Minute
)

// tempPrefix starts the name of a file being written in AttachmentDir. The
// rest of the name is the key it is tracked under while in flight.
const tempPrefix = ".tmp-"

// ErrTooLarge is a file past a cap; the hub answers 413.
var ErrTooLarge = errors.New("too large")

func tooLargef(format string, a ...any) error {
	return &userError{ErrTooLarge, fmt.Sprintf(format, a...)}
}

// ErrFileMissing is an attachment whose file is gone or changed on disk.
// An export stops on it rather than leave the file out.
var ErrFileMissing = errors.New("attachment file missing")

func (s *Store) attachmentDir() string { return filepath.Join(s.dir, AttachmentDir) }

func (s *Store) attachmentPath(id string) string { return filepath.Join(s.attachmentDir(), id) }

// newAttachmentID returns 32 lowercase hex characters from 16 random bytes.
func newAttachmentID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// validAttachmentID reports whether id is 32 lowercase hex characters.
func validAttachmentID(id string) bool { return isLowerHex(id, 32) }

func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range []byte(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// cleanAttachmentName cleans a file name as docs/api.md, Attachments, says:
// control characters dropped, slashes made underscores, trimmed, cut to
// api.MaxAttachmentName bytes on a character boundary, "file" when empty. A
// name that is not UTF-8 is refused.
func cleanAttachmentName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", invalidf("the file name is not valid UTF-8")
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return -1
		case r == '/' || r == '\\':
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if len(name) > api.MaxAttachmentName {
		cut := api.MaxAttachmentName
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = strings.TrimSpace(name[:cut])
	}
	if name == "" {
		name = "file"
	}
	return name, nil
}

// sniffType is the media type of a file's first bytes, without parameters.
func sniffType(head []byte) string {
	t, _, _ := strings.Cut(http.DetectContentType(head), ";")
	return strings.TrimSpace(t)
}

// fileSize writes a byte count for the user: MB to one decimal below a GB,
// GB to two decimals from there, rounded up so a size past a cap never
// reads as the cap itself. Whole numbers drop their decimals.
func fileSize(n int64) string {
	unit, div, scale := "MB", float64(1<<20), 10.0
	if n >= 1<<30 {
		unit, div, scale = "GB", float64(1<<30), 100.0
	}
	v := math.Ceil(float64(n)/div*scale) / scale
	return strconv.FormatFloat(v, 'f', -1, 64) + " " + unit
}

// track marks files in flight under keys, so the sweep leaves them alone
// while they are written and recorded. The returned func ends it.
func (s *Store) track(keys ...string) func() {
	s.mu.Lock()
	if s.inflight == nil {
		s.inflight = map[string]bool{}
	}
	for _, k := range keys {
		s.inflight[k] = true
	}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		for _, k := range keys {
			delete(s.inflight, k)
		}
		s.mu.Unlock()
	}
}

// errOverLimit is a file longer than writeAttachmentFile was allowed.
var errOverLimit = errors.New("over the limit")

// headBuf keeps the first 512 bytes written to it, for sniffing.
type headBuf []byte

func (h *headBuf) Write(p []byte) (int, error) {
	if room := 512 - len(*h); room > 0 {
		*h = append(*h, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// writeAttachmentFile writes at most limit bytes of r as the file of
// attachment id: to a temporary name in the same folder, synced, renamed
// into place, and the folder synced, so a crash leaves at most a stray
// temporary file. It returns the size, the SHA-256 and the sniffed type. A
// longer r answers errOverLimit and leaves no file. The caller tracks id.
func (s *Store) writeAttachmentFile(id string, r io.Reader, limit int64) (size int64, sum, typ string, err error) {
	dir := s.attachmentDir()
	tmp := filepath.Join(dir, tempPrefix+id)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return 0, "", "", err
	}
	defer func() {
		if f != nil {
			f.Close()
		}
		if err != nil {
			os.Remove(tmp)
		}
	}()
	h := sha256.New()
	var head headBuf
	size, err = io.Copy(io.MultiWriter(f, h, &head), io.LimitReader(r, limit+1))
	if err != nil {
		return 0, "", "", err
	}
	if size > limit {
		return 0, "", "", errOverLimit
	}
	if err = f.Sync(); err != nil {
		return 0, "", "", err
	}
	err = f.Close()
	f = nil
	if err != nil {
		return 0, "", "", err
	}
	if err = os.Rename(tmp, s.attachmentPath(id)); err != nil {
		return 0, "", "", err
	}
	if err = syncDir(dir); err != nil {
		os.Remove(s.attachmentPath(id))
		return 0, "", "", err
	}
	return size, hex.EncodeToString(h.Sum(nil)), sniffType(head), nil
}

// syncDir syncs a folder, so a rename in it survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// attachmentRoom checks that an item of the hub's lists can take one more
// file of size bytes (-1 when not yet known): the item exists, and no cap
// would be passed.
func attachmentRoom(q rowQuerier, repo, itemID, name string, size int64) error {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM items WHERE bridge = ? AND repo = ? AND id = ?`,
		HubBridge, repo, itemID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return notFoundf("no item %q", ItemKey(HubBridge, repo, itemID))
	}
	if size > api.MaxAttachmentBytes {
		return tooLargef("%s is %s; a file can be at most %s.", name, fileSize(size), fileSize(api.MaxAttachmentBytes))
	}
	if err := q.QueryRow(`SELECT COUNT(*) FROM attachments WHERE bridge = ? AND repo = ? AND item_id = ? AND removed_at IS NULL`,
		HubBridge, repo, itemID).Scan(&n); err != nil {
		return err
	}
	if n >= api.MaxAttachmentsPerItem {
		return tooLargef("This item already has %d files; an item can hold at most %d.", n, api.MaxAttachmentsPerItem)
	}
	var total int64
	if err := q.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM attachments`).Scan(&total); err != nil {
		return err
	}
	if size > 0 && total+size > api.MaxAttachmentsBytes {
		return tooLargef("The hub's files would come to %s; they can be at most %s.", fileSize(total+size), fileSize(api.MaxAttachmentsBytes))
	}
	return nil
}

// AddAttachment keeps body as a file attached to the item key, which must
// be on a list the hub keeps. name is cleaned; declared is the body's size
// when the client gave it, -1 when not, so a file past a cap is refused
// before it is read. The type is sniffed from the bytes. A cap passed
// answers ErrTooLarge.
func (s *Store) AddAttachment(key, name string, declared int64, body io.Reader) (api.Attachment, error) {
	bridge, repo, itemID, ok := SplitKey(key)
	if !ok {
		return api.Attachment{}, invalidf("item key %q is not valid", key)
	}
	if !onHub(bridge) {
		return api.Attachment{}, invalidf("Only items of the personal list and projects take files: an agent's item has nowhere to carry one back to its machine.")
	}
	name, err := cleanAttachmentName(name)
	if err != nil {
		return api.Attachment{}, err
	}
	if err := attachmentRoom(s.db, repo, itemID, name, declared); err != nil {
		return api.Attachment{}, err
	}

	id := newAttachmentID()
	defer s.track(id)()
	size, sum, typ, err := s.writeAttachmentFile(id, body, api.MaxAttachmentBytes)
	if errors.Is(err, errOverLimit) {
		return api.Attachment{}, tooLargef("%s is more than %s; a file can be at most %s.", name,
			fileSize(api.MaxAttachmentBytes), fileSize(api.MaxAttachmentBytes))
	}
	if err != nil {
		return api.Attachment{}, err
	}
	a := api.Attachment{ID: id, Name: name, Type: typ, Size: size, SHA256: sum, CreatedAt: s.now()}
	if err := s.recordAttachment(repo, itemID, a); err != nil {
		os.Remove(s.attachmentPath(id))
		return api.Attachment{}, err
	}
	s.emit(Change{Kind: "items", Bridge: HubBridge, Repo: repo})
	return a, nil
}

// recordAttachment adds the row of a file already written, checking the
// caps again with its real size.
func (s *Store) recordAttachment(repo, itemID string, a api.Attachment) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := attachmentRoom(tx, repo, itemID, a.Name, a.Size); err != nil {
		return err
	}
	if err := insertAttachment(tx, repo, itemID, a); err != nil {
		return err
	}
	return tx.Commit()
}

func insertAttachment(tx *sql.Tx, repo, itemID string, a api.Attachment) error {
	_, err := tx.Exec(`INSERT INTO attachments (id, bridge, repo, item_id, name, type, size, sha256, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, HubBridge, repo, itemID, a.Name, a.Type, a.Size, a.SHA256, fmtTime(a.CreatedAt))
	return err
}

const attachmentCols = `id, name, type, size, sha256, created_at`

func scanAttachment(sc interface{ Scan(...any) error }, extra ...any) (api.Attachment, error) {
	var a api.Attachment
	var created string
	if err := sc.Scan(append([]any{&a.ID, &a.Name, &a.Type, &a.Size, &a.SHA256, &created}, extra...)...); err != nil {
		return a, err
	}
	a.CreatedAt = parseTime(created)
	return a, nil
}

// OpenAttachment returns an attachment that is not removed and its file,
// for the caller to serve and close. An unknown or removed one is
// ErrNotFound.
func (s *Store) OpenAttachment(id string) (api.Attachment, *os.File, error) {
	if !validAttachmentID(id) {
		return api.Attachment{}, nil, notFoundf("no attachment %q", id)
	}
	a, err := scanAttachment(s.db.QueryRow(`SELECT `+attachmentCols+` FROM attachments WHERE id = ? AND removed_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil, notFoundf("no attachment %q", id)
	}
	if err != nil {
		return a, nil, err
	}
	f, err := os.Open(s.attachmentPath(id))
	if err != nil {
		return a, nil, fmt.Errorf("the file of attachment %s: %w", id, err)
	}
	return a, f, nil
}

// RemoveAttachment marks an attachment removed: it leaves its item and is
// no longer served, and RestoreAttachment puts it back until it is purged.
// Removing a removed one changes nothing.
func (s *Store) RemoveAttachment(id string) error {
	if !validAttachmentID(id) {
		return notFoundf("no attachment %q", id)
	}
	var repo string
	var removed sql.NullString
	err := s.db.QueryRow(`SELECT repo, removed_at FROM attachments WHERE id = ?`, id).Scan(&repo, &removed)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundf("no attachment %q", id)
	}
	if err != nil || removed.Valid {
		return err
	}
	if _, err := s.db.Exec(`UPDATE attachments SET removed_at = ? WHERE id = ? AND removed_at IS NULL`, fmtTime(s.now()), id); err != nil {
		return err
	}
	s.emit(Change{Kind: "items", Bridge: HubBridge, Repo: repo})
	return nil
}

// RestoreAttachment puts a removed attachment back on its item. One that
// was purged is ErrNotFound; one its item has no room for is ErrConflict.
// Its file already counts toward the hub's total until it is purged, so
// only the per-item cap can stand in the way.
func (s *Store) RestoreAttachment(id string) (api.Attachment, error) {
	gone := notFoundf("no attachment %q; a removed file is kept for %d hours", id, int(AttachmentPurgeAfter.Hours()))
	if !validAttachmentID(id) {
		return api.Attachment{}, gone
	}
	tx, err := s.db.Begin()
	if err != nil {
		return api.Attachment{}, err
	}
	defer tx.Rollback()
	var repo, itemID string
	var removed sql.NullString
	a, err := scanAttachment(tx.QueryRow(`SELECT `+attachmentCols+`, repo, item_id, removed_at FROM attachments WHERE id = ?`, id),
		&repo, &itemID, &removed)
	if errors.Is(err, sql.ErrNoRows) {
		return a, gone
	}
	if err != nil || !removed.Valid {
		return a, err
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM attachments WHERE bridge = ? AND repo = ? AND item_id = ? AND removed_at IS NULL`,
		HubBridge, repo, itemID).Scan(&n); err != nil {
		return a, err
	}
	if n >= api.MaxAttachmentsPerItem {
		return a, conflictf("This item already has %d files; an item can hold at most %d, so %s cannot be put back.", n, api.MaxAttachmentsPerItem, a.Name)
	}
	if _, err := tx.Exec(`UPDATE attachments SET removed_at = NULL WHERE id = ?`, id); err != nil {
		return a, err
	}
	if err := tx.Commit(); err != nil {
		return a, err
	}
	s.emit(Change{Kind: "items", Bridge: HubBridge, Repo: repo})
	return a, nil
}

// joinAttachments sets each item's Attachments: for an item of the hub's
// lists, those not removed, oldest first; for any other, none, whatever its
// bridge sent. It is the one place an api.Item gets its attachments.
func joinAttachments(q rowsQuerier, items []api.Item) error {
	at := map[string]int{}
	for i := range items {
		items[i].Attachments = nil
		if onHub(items[i].Bridge) {
			at[items[i].Key] = i
		}
	}
	if len(at) == 0 {
		return nil
	}
	rows, err := q.Query(`SELECT ` + attachmentCols + `, repo, item_id FROM attachments
		WHERE removed_at IS NULL ORDER BY rowid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var repo, itemID string
		a, err := scanAttachment(rows, &repo, &itemID)
		if err != nil {
			return err
		}
		if i, ok := at[ItemKey(HubBridge, repo, itemID)]; ok {
			items[i].Attachments = append(items[i].Attachments, a)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, i := range at {
		slices.SortStableFunc(items[i].Attachments, func(a, b api.Attachment) int { return a.CreatedAt.Compare(b.CreatedAt) })
	}
	return nil
}

// SweepReport is what one sweep of the attachments did.
type SweepReport struct {
	Purged  int      // removed attachments purged, row and file
	Strays  []string // files deleted: temporary ones and ones with no row
	Missing []string // ids whose row has no file
}

// SweepAttachments purges removed attachments due for it, deletes stray
// temporary files and files with no row, and reports rows whose file is
// missing. Files in flight are left alone.
func (s *Store) SweepAttachments() (SweepReport, error) {
	var rep SweepReport
	due := s.now().Add(-AttachmentPurgeAfter)
	var purge []string
	if err := s.eachRow(`SELECT id, removed_at FROM attachments WHERE removed_at IS NOT NULL`, nil, func(rows *sql.Rows) error {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return err
		}
		if !parseTime(at).After(due) {
			purge = append(purge, id)
		}
		return nil
	}); err != nil {
		return rep, err
	}
	// The row goes first: a crash before the file goes leaves a file with
	// no row, which the next sweep deletes.
	for _, id := range purge {
		if _, err := s.db.Exec(`DELETE FROM attachments WHERE id = ?`, id); err != nil {
			return rep, err
		}
		if err := os.Remove(s.attachmentPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rep, err
		}
		rep.Purged++
	}

	// List the folder before reading what is in flight, and that before
	// reading the rows: a file listed was tracked before it was made, and
	// stays tracked until its row is committed.
	entries, err := os.ReadDir(s.attachmentDir())
	if err != nil {
		return rep, err
	}
	s.mu.Lock()
	inflight := make(map[string]bool, len(s.inflight))
	for k := range s.inflight {
		inflight[k] = true
	}
	s.mu.Unlock()
	ids, err := queryStrings(s.db, `SELECT id FROM attachments`)
	if err != nil {
		return rep, err
	}
	rows := make(map[string]bool, len(ids))
	for _, id := range ids {
		rows[id] = true
	}

	files := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		files[name] = true
		if key, ok := strings.CutPrefix(name, tempPrefix); ok {
			if inflight[key] {
				continue
			}
		} else if rows[name] || inflight[name] {
			continue
		}
		if err := os.Remove(filepath.Join(s.attachmentDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rep, err
		}
		rep.Strays = append(rep.Strays, name)
	}
	for _, id := range ids {
		if files[id] || inflight[id] {
			continue
		}
		// A file written since the folder was listed is there now.
		if _, err := os.Stat(s.attachmentPath(id)); errors.Is(err, os.ErrNotExist) {
			rep.Missing = append(rep.Missing, id)
		}
	}
	return rep, nil
}
