package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Limits on what the app may post.
const (
	MaxNoteLen  = 10000
	MaxTitleLen = 1000
)

// provisionalPrefix starts the id of an added item the bridge has not
// created yet: pending-<action id>.
const provisionalPrefix = "pending-"

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidActionID reports whether id is a UUID in its usual text form.
func ValidActionID(id string) bool { return uuidRE.MatchString(id) }

const actionCols = `seq, id, kind, bridge, repo, bead_id, payload, author, status, error, created_at, applied_at`

func scanAction(sc interface{ Scan(...any) error }) (api.Action, error) {
	var a api.Action
	var payload, created string
	var applied sql.NullString
	if err := sc.Scan(&a.Seq, &a.ID, &a.Kind, &a.Bridge, &a.Repo, &a.BeadID, &payload,
		&a.Author, &a.Status, &a.Error, &created, &applied); err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(payload), &a.Payload); err != nil {
		return a, err
	}
	a.CreatedAt = parseTime(created)
	a.AppliedAt = parseTimePtr(applied)
	return a, nil
}

func (s *Store) queryActions(query string, args ...any) ([]api.Action, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.Action{}
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Action returns one action by id.
func (s *Store) Action(id string) (api.Action, error) {
	a, err := scanAction(s.db.QueryRow(`SELECT `+actionCols+` FROM actions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, notFoundf("no action %q", id)
	}
	return a, err
}

// AppendAction validates na and appends it to the log as pending, routed to
// the bridge that owns its repo. If an action with the same id exists, it
// returns that one unchanged and created is false.
func (s *Store) AppendAction(na api.NewAction, author string) (a api.Action, created bool, err error) {
	if !ValidActionID(na.ID) {
		return a, false, invalidf("action id must be a UUID")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return a, false, err
	}
	defer tx.Rollback()

	a, err = scanAction(tx.QueryRow(`SELECT `+actionCols+` FROM actions WHERE id = ?`, na.ID))
	if err == nil {
		return a, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return a, false, err
	}

	a = api.Action{ID: na.ID, Kind: na.Kind, Author: author, Status: api.StatusPending, CreatedAt: s.now()}
	p := na.Payload
	switch na.Kind {
	case api.KindDone, api.KindReopen, api.KindDismiss:
		p = api.Payload{}
	case api.KindSnooze:
		// No until clears the snooze: Undo of a first snooze.
		p = api.Payload{Until: p.Until, Previous: p.Previous}
	case api.KindNote:
		if strings.TrimSpace(p.Text) == "" {
			return a, false, invalidf("note needs payload.text")
		}
		if utf8.RuneCountInString(p.Text) > MaxNoteLen {
			return a, false, invalidf("note text is longer than %d characters", MaxNoteLen)
		}
		p = api.Payload{Text: p.Text}
	case api.KindAdd:
		title := strings.TrimSpace(p.Title)
		if title == "" {
			return a, false, invalidf("add needs payload.title")
		}
		if utf8.RuneCountInString(title) > MaxTitleLen {
			return a, false, invalidf("title is longer than %d characters", MaxTitleLen)
		}
		p = api.Payload{Title: title, Due: p.Due}
	case "":
		return a, false, invalidf("action needs a kind")
	default:
		return a, false, invalidf("unknown action kind %q", na.Kind)
	}
	a.Payload = normalisePayload(p)

	if na.Kind == api.KindAdd {
		if !ValidBridgeName(na.Bridge) || !ValidRepoName(na.Repo) {
			return a, false, invalidf("add needs a bridge and a repo")
		}
		var personal, optedIn bool
		err := tx.QueryRow(`SELECT r.personal, r.opted_in FROM repos r
			JOIN bridges b ON b.name = r.bridge AND b.revoked_at IS NULL
			WHERE r.bridge = ? AND r.name = ?`, na.Bridge, na.Repo).Scan(&personal, &optedIn)
		if errors.Is(err, sql.ErrNoRows) {
			return a, false, notFoundf("bridge %q has no repo %q", na.Bridge, na.Repo)
		}
		if err != nil {
			return a, false, err
		}
		if !personal && !optedIn {
			return a, false, invalidf("repo %q is not synced with Meat Mode", na.Repo)
		}
		a.Bridge, a.Repo = na.Bridge, na.Repo
	} else {
		b, r, id, ok := SplitKey(na.Key)
		if !ok {
			return a, false, invalidf("%s needs the item key", na.Kind)
		}
		if strings.HasPrefix(id, provisionalPrefix) {
			return a, false, conflictf("this item is still being created; try again in a moment")
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM items i
			JOIN bridges b ON b.name = i.bridge AND b.revoked_at IS NULL
			WHERE i.bridge = ? AND i.repo = ? AND i.id = ?`, b, r, id).Scan(&n); err != nil {
			return a, false, err
		}
		if n == 0 {
			return a, false, notFoundf("no item %q", na.Key)
		}
		a.Bridge, a.Repo, a.BeadID = b, r, id
	}

	payload, err := json.Marshal(a.Payload)
	if err != nil {
		return a, false, err
	}
	res, err := tx.Exec(`INSERT INTO actions (id, kind, bridge, repo, bead_id, payload, author, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Kind, a.Bridge, a.Repo, a.BeadID, string(payload), a.Author, a.Status, fmtTime(a.CreatedAt))
	if err != nil {
		return a, false, err
	}
	if a.Seq, err = res.LastInsertId(); err != nil {
		return a, false, err
	}
	if err := tx.Commit(); err != nil {
		return a, false, err
	}
	s.emit(Change{Kind: "actions", Bridge: a.Bridge, Repo: a.Repo})
	return a, true, nil
}

func normalisePayload(p api.Payload) api.Payload {
	for _, t := range []**time.Time{&p.Until, &p.Previous, &p.Due} {
		if *t != nil {
			u := (*t).UTC()
			*t = &u
		}
	}
	return p
}

// PendingActions returns the bridge's pending actions, oldest first.
func (s *Store) PendingActions(bridge string) ([]api.Action, error) {
	return s.queryActions(`SELECT `+actionCols+` FROM actions
		WHERE bridge = ? AND status = ? ORDER BY seq`, bridge, api.StatusPending)
}

// RecentActions returns up to limit actions, newest first.
func (s *Store) RecentActions(limit int) ([]api.Action, error) {
	return s.queryActions(`SELECT `+actionCols+` FROM actions ORDER BY seq DESC LIMIT ?`, limit)
}

// ReportResult records a bridge's result for one of its actions. A result
// for an action that already has one changes nothing, so a bridge can retry.
// A success for a cancelled action is recorded as applied, because beads did
// change.
func (s *Store) ReportResult(bridge, id string, r api.ActionResult) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := scanAction(tx.QueryRow(`SELECT `+actionCols+` FROM actions WHERE id = ? AND bridge = ?`, id, bridge))
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundf("no action %q for this bridge", id)
	}
	if err != nil {
		return err
	}
	switch {
	case a.Status == api.StatusApplied || a.Status == api.StatusFailed:
		return nil
	case a.Status == api.StatusCancelled && !r.OK:
		return nil
	}
	if r.OK {
		now := fmtTime(s.now())
		beadID := a.BeadID
		if a.Kind == api.KindAdd {
			if !validSegment(r.BeadID) {
				return invalidf("an add result needs the created beadId")
			}
			beadID = r.BeadID
		}
		_, err = tx.Exec(`UPDATE actions SET status = ?, bead_id = ?, error = '', applied_at = ? WHERE seq = ?`,
			api.StatusApplied, beadID, now, a.Seq)
	} else {
		msg := strings.TrimSpace(r.Error)
		if msg == "" {
			msg = "the bridge could not apply this action"
		}
		msg = truncRunes(msg, maxResultErrorLen)
		_, err = tx.Exec(`UPDATE actions SET status = ?, error = ? WHERE seq = ?`,
			api.StatusFailed, msg, a.Seq)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.emit(Change{Kind: "actions", Bridge: a.Bridge, Repo: a.Repo})
	return nil
}

// CancelAction cancels a pending action. Cancelling a cancelled action
// returns it unchanged; an applied or failed one is a conflict.
func (s *Store) CancelAction(id string) (api.Action, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return api.Action{}, err
	}
	defer tx.Rollback()
	a, err := scanAction(tx.QueryRow(`SELECT `+actionCols+` FROM actions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, notFoundf("no action %q", id)
	}
	if err != nil {
		return a, err
	}
	switch a.Status {
	case api.StatusCancelled:
		return a, nil
	case api.StatusPending:
	default:
		return a, conflictf("action %q is already %s", id, a.Status)
	}
	if _, err := tx.Exec(`UPDATE actions SET status = ? WHERE seq = ?`, api.StatusCancelled, a.Seq); err != nil {
		return a, err
	}
	if err := tx.Commit(); err != nil {
		return a, err
	}
	a.Status = api.StatusCancelled
	s.emit(Change{Kind: "actions", Bridge: a.Bridge, Repo: a.Repo})
	return a, nil
}
