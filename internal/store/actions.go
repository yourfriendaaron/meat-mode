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
	MaxNoteLen  = api.MaxNoteLen
	MaxTitleLen = api.MaxTitleLen
)

// provisionalPrefix starts the id of an added item the bridge has not
// created yet: pending-<action id>.
const provisionalPrefix = api.ProvisionalPrefix

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidActionID reports whether id is a UUID in its usual text form.
func ValidActionID(id string) bool { return uuidRE.MatchString(id) }

const actionCols = `seq, id, kind, bridge, repo, bead_id, payload, author, machine, status, error, created_at, applied_at`

func scanAction(sc interface{ Scan(...any) error }) (api.Action, error) {
	var a api.Action
	var payload, created string
	var applied sql.NullString
	if err := sc.Scan(&a.Seq, &a.ID, &a.Kind, &a.Bridge, &a.Repo, &a.BeadID, &payload,
		&a.Author, &a.Machine, &a.Status, &a.Error, &created, &applied); err != nil {
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
// the bridge that owns its repo. An action on a list the hub keeps is applied
// at once and logged as applied. If an action with the same id exists, it
// returns that one unchanged and created is false.
func (s *Store) AppendAction(na api.NewAction, author string) (a api.Action, created bool, err error) {
	return s.appendAction(na, author, "", nil)
}

// appendAction is AppendAction, recording machine as the paired machine
// whose token posted it ("" for the app). after, when set, runs in the
// same transaction once a new action is stored and applied; a repeated id
// skips it.
func (s *Store) appendAction(na api.NewAction, author, machine string, after func(*sql.Tx, api.Action) error) (a api.Action, created bool, err error) {
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

	a = api.Action{ID: na.ID, Kind: na.Kind, Author: author, Machine: machine, Status: api.StatusPending, CreatedAt: s.now()}
	p := na.Payload
	switch na.Kind {
	case api.KindDone, api.KindReopen, api.KindDismiss, api.KindStop:
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
		add := api.Payload{Title: title, Due: p.Due, Ask: p.Ask, Parent: strings.TrimSpace(p.Parent)}
		if add.Parent != "" && !validSegment(add.Parent) {
			return a, false, invalidf("payload.parent is not a valid item id")
		}
		if p.Repeat != nil {
			if p.Due == nil {
				return a, false, invalidf("a repeat needs payload.due")
			}
			r, err := checkRepeat(*p.Repeat)
			if err != nil {
				return a, false, err
			}
			if err := checkUntil(r, *p.Due); err != nil {
				return a, false, err
			}
			add.Repeat = &r
		}
		if len(p.Labels) > 0 {
			labels, err := cleanLabels(p.Labels)
			if err != nil {
				return a, false, err
			}
			if len(labels) > 0 {
				add.Labels = labels
			}
		}
		p = add
	case api.KindSync, api.KindAgents:
		if p.On == nil {
			return a, false, invalidf("%s needs payload.on", na.Kind)
		}
		p = api.Payload{On: p.On}
	case api.KindLimits:
		if p.Limits == nil {
			return a, false, invalidf("limits needs payload.limits")
		}
		c, err := api.CleanLimitsChange(*p.Limits)
		if err != nil {
			return a, false, invalidf("%s", err.Error())
		}
		p = api.Payload{Limits: &c}
	case api.KindAsk:
		// The note is optional: an ask without one only wakes the agent.
		if strings.TrimSpace(p.Text) == "" {
			p.Text = ""
		}
		if utf8.RuneCountInString(p.Text) > MaxNoteLen {
			return a, false, invalidf("note text is longer than %d characters", MaxNoteLen)
		}
		p = api.Payload{Text: p.Text}
	case api.KindEdit:
		if p.Set == nil {
			return a, false, invalidf("edit needs payload.set")
		}
		set, err := checkEdit(*p.Set)
		if err != nil {
			return a, false, err
		}
		p = api.Payload{Set: &set}
	case api.KindNoteEdit:
		if strings.TrimSpace(p.CommentID) == "" {
			return a, false, invalidf("note-edit needs payload.commentId")
		}
		if strings.TrimSpace(p.Text) == "" {
			return a, false, invalidf("note-edit needs payload.text")
		}
		if utf8.RuneCountInString(p.Text) > MaxNoteLen {
			return a, false, invalidf("note text is longer than %d characters", MaxNoteLen)
		}
		p = api.Payload{CommentID: p.CommentID, Text: p.Text}
	case api.KindNoteDelete:
		if strings.TrimSpace(p.CommentID) == "" {
			return a, false, invalidf("note-delete needs payload.commentId")
		}
		p = api.Payload{CommentID: p.CommentID}
	case api.KindNoteRestore:
		c, err := checkRestore(p.Comment, a.CreatedAt)
		if err != nil {
			return a, false, err
		}
		p = api.Payload{Comment: &c}
	case "":
		return a, false, invalidf("action needs a kind")
	default:
		return a, false, invalidf("unknown action kind %q", na.Kind)
	}
	a.Payload = normalisePayload(p)

	if na.Kind == api.KindLimits {
		if err := checkLimitsAction(tx, na.Bridge, na.Repo, *a.Payload.Limits); err != nil {
			return a, false, err
		}
		a.Bridge = na.Bridge
	} else if na.Kind == api.KindAdd || na.Kind == api.KindSync || na.Kind == api.KindAgents {
		if !ValidBridgeName(na.Bridge) || !ValidRepoName(na.Repo) {
			return a, false, invalidf("%s needs a bridge and a repo", na.Kind)
		}
		if a.Payload.Repeat != nil && !onHub(na.Bridge) {
			return a, false, invalidf("only the personal list and projects take a repeat")
		}
		if len(a.Payload.Labels) > 0 && !onHub(na.Bridge) {
			return a, false, invalidf("only the personal list and projects take tags; a tracker's items keep the labels their tracker gives")
		}
		// A bridge's own personal repo only feeds the personal list.
		var optedIn bool
		err := tx.QueryRow(`SELECT r.opted_in FROM repos r
			JOIN bridges b ON b.name = r.bridge AND b.revoked_at IS NULL
			WHERE r.bridge = ? AND r.name = ? AND (r.personal = 0 OR r.bridge = ?)`,
			na.Bridge, na.Repo, HubBridge).Scan(&optedIn)
		if errors.Is(err, sql.ErrNoRows) {
			return a, false, notFoundf("bridge %q has no repo %q", na.Bridge, na.Repo)
		}
		if err != nil {
			return a, false, err
		}
		if err := checkCapability(tx, na.Bridge, na.Repo, capabilityOf(na.Kind, a.Payload)); err != nil {
			return a, false, err
		}
		if na.Kind == api.KindAdd && a.Payload.Ask {
			if err := checkCapability(tx, na.Bridge, na.Repo, api.KindAsk); err != nil {
				return a, false, err
			}
		}
		switch {
		case na.Kind == api.KindSync && onHub(na.Bridge):
			return a, false, invalidf("the personal list and projects are kept on the hub and have no sync switch")
		case na.Kind == api.KindAgents && onHub(na.Bridge):
			return a, false, invalidf(noPersonalAgents)
		case na.Kind == api.KindAgents && *a.Payload.On:
			// A hub that is taken over must not widen what agents may do.
			return a, false, invalidf("agents are switched on at the agent machine, not in the app: run `meat-mode bridge agents on %s` there", na.Repo)
		case na.Kind == api.KindAdd && !optedIn:
			return a, false, invalidf("repo %q is not synced with Meat Mode", na.Repo)
		case na.Kind == api.KindAdd && a.Payload.Ask:
			if onHub(na.Bridge) {
				return a, false, invalidf(noPersonalAgents)
			}
			if err := checkAgentsAllowed(tx, na.Bridge, na.Repo); err != nil {
				return a, false, err
			}
		}
		if na.Kind == api.KindAdd && a.Payload.Parent != "" {
			if err := checkParent(tx, na.Bridge, na.Repo, a.Payload.Parent); err != nil {
				return a, false, err
			}
		}
		a.Bridge, a.Repo = na.Bridge, na.Repo
	} else {
		b, r, id, ok := SplitKey(na.Key)
		if !ok {
			return a, false, invalidf("%s needs the item key", na.Kind)
		}
		if personalOnly(na.Kind) && !onHub(b) {
			return a, false, invalidf("an agent's item is changed by its agent; to ask for a change, add a note")
		}
		if na.Kind == api.KindAsk && onHub(b) {
			return a, false, invalidf(noPersonalAgents)
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
		if err := checkCapability(tx, b, r, capabilityOf(na.Kind, a.Payload)); err != nil {
			return a, false, err
		}
		if na.Kind == api.KindAsk {
			if err := checkAgentsAllowed(tx, b, r); err != nil {
				return a, false, err
			}
		}
		if na.Kind == api.KindEdit {
			if err := checkEditList(tx, b, r, id, *a.Payload.Set); err != nil {
				return a, false, err
			}
		}
		if na.Kind == api.KindDone || na.Kind == api.KindDismiss {
			if err := s.checkCloses(tx, b, r, id); err != nil {
				return a, false, err
			}
		}
		a.Bridge, a.Repo, a.BeadID = b, r, id
	}

	payload, err := json.Marshal(a.Payload)
	if err != nil {
		return a, false, err
	}
	res, err := tx.Exec(`INSERT INTO actions (id, kind, bridge, repo, bead_id, payload, author, machine, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Kind, a.Bridge, a.Repo, a.BeadID, string(payload), a.Author, a.Machine, a.Status, fmtTime(a.CreatedAt))
	if err != nil {
		return a, false, err
	}
	if a.Seq, err = res.LastInsertId(); err != nil {
		return a, false, err
	}
	var newItems []string
	if onHub(a.Bridge) {
		if newItems, err = s.applyHub(tx, &a); err != nil {
			return a, false, err
		}
	}
	if after != nil {
		if err := after(tx, a); err != nil {
			return a, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return a, false, err
	}
	s.emit(Change{Kind: "actions", Bridge: a.Bridge, Repo: a.Repo})
	if onHub(a.Bridge) {
		s.emit(Change{Kind: "items", Bridge: a.Bridge, Repo: a.Repo, NewItems: newItems})
	}
	return a, true, nil
}

// checkLimitsAction refuses a limits change for anything but a paired
// agent machine whose bridge reported its ceilings, and any value above
// them. The bridge checks again: it is the one that enforces them.
func checkLimitsAction(tx *sql.Tx, bridge, repo string, c api.LimitsChange) error {
	switch {
	case onHub(bridge):
		return invalidf(noPersonalAgents)
	case !ValidBridgeName(bridge):
		return invalidf("limits needs a bridge")
	case repo != "":
		return invalidf("limits is for a machine, not a repo")
	}
	var harness string
	err := tx.QueryRow(`SELECT harness FROM bridges WHERE name = ? AND revoked_at IS NULL`, bridge).Scan(&harness)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundf("no machine %q", bridge)
	}
	if err != nil {
		return err
	}
	h, err := decodeHarness(harness)
	if err != nil {
		return err
	}
	if h == nil || h.Limits == nil {
		return invalidf("the bridge on %s is too old to take limits from the app; update it, or set MEAT_MODE_AGENT_* there", bridge)
	}
	if err := api.CheckLimits(c, h.Limits.Max, bridge); err != nil {
		return invalidf("%s", err.Error())
	}
	return nil
}

// noPersonalAgents refuses an agent on a list the hub keeps.
const noPersonalAgents = "the personal list and projects have no agents"

// checkAgentsAllowed refuses an ask in a repo whose agents switch is off.
func checkAgentsAllowed(tx *sql.Tx, bridge, repo string) error {
	ok, err := agentsAllowedTx(tx, bridge, repo)
	if err != nil {
		return err
	}
	if !ok {
		return invalidf("agents are not allowed in repo %q; switch them on at the agent machine with `meat-mode bridge agents on %s`", repo, repo)
	}
	return nil
}

func normalisePayload(p api.Payload) api.Payload {
	times := []**time.Time{&p.Until, &p.Previous, &p.Due}
	if p.Set != nil {
		times = append(times, &p.Set.Due)
	}
	for _, t := range times {
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

// ItemHistory returns up to limit actions on one item, newest first: every
// action on it, whatever its status, and the adds of its sub-tasks. The hub
// keeps the whole action log, so it reaches back to the item's add. An item
// the hub does not hold is notFound.
func (s *Store) ItemHistory(key string, limit int) ([]api.Action, error) {
	b, r, id, ok := SplitKey(key)
	if !ok {
		return nil, invalidf("item key %q is not valid", key)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE bridge = ? AND repo = ? AND id = ?`, b, r, id).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, notFoundf("no item %q", key)
	}
	return s.queryActions(`SELECT `+actionCols+` FROM actions WHERE bridge = ? AND repo = ? AND bead_id = ?
		UNION ALL
		SELECT `+actionCols+` FROM actions WHERE bridge = ? AND repo = ? AND kind = ? AND bead_id <> ?
			AND json_extract(payload, '$.parent') = ?
		ORDER BY seq DESC LIMIT ?`, b, r, id, b, r, api.KindAdd, id, id, limit)
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
			if !validSegment(r.BeadID) || strings.HasPrefix(r.BeadID, provisionalPrefix) {
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
