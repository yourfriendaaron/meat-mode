package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// The personal list lives on the hub itself, as repo PersonalRepo of the
// reserved bridge HubBridge. No bridge can hold that name or its token, so
// actions on it are applied by the hub the moment they are appended. Every
// other repo of HubBridge is a project: a list kept the same way for a repo
// with no tracker, made by the first add an agent posts to it.
const (
	HubBridge    = "hub"
	PersonalRepo = "personal"
)

// closedShownFor is how long the hub keeps showing a closed personal item,
// matching the closed items a bridge sends.
const closedShownFor = 7 * 24 * time.Hour

// onHub reports whether bridge is the hub's own, whose repos are the
// personal list and the projects, which the hub keeps and changes itself.
func onHub(bridge string) bool { return bridge == HubBridge }

// hubItem reads one item of a list the hub keeps, with its hub-local times.
func hubItem(tx *sql.Tx, repo, id string) (api.Item, error) {
	var data, first string
	var seen sql.NullString
	err := tx.QueryRow(`SELECT data, first_seen, seen_at FROM items WHERE bridge = ? AND repo = ? AND id = ?`,
		HubBridge, repo, id).Scan(&data, &first, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return api.Item{}, notFoundf("no item %q", ItemKey(HubBridge, repo, id))
	}
	if err != nil {
		return api.Item{}, err
	}
	return decodeItem(HubBridge, repo, data, first, seen)
}

// putHubItem stores an item of a list the hub keeps. firstSeen and seenAt
// apply to a new row only; an existing row keeps its own.
func putHubItem(tx *sql.Tx, repo string, it api.Item, firstSeen time.Time, seenAt *time.Time) error {
	it.Key, it.Bridge, it.Repo = "", "", ""
	it.Pending, it.SeenAt, it.Error, it.FirstSeen = false, nil, "", time.Time{}
	if it.Comments == nil {
		it.Comments = []api.Comment{}
	}
	data, err := json.Marshal(it)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO items (bridge, repo, id, data, first_seen, seen_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (bridge, repo, id) DO UPDATE SET data = excluded.data`,
		HubBridge, repo, it.ID, string(data), fmtTime(firstSeen), fmtTimePtr(seenAt))
	return err
}

// freeHubID returns want, or want with the first free -2, -3… suffix, so an
// id is never taken twice in one of the hub's lists.
func freeHubID(tx *sql.Tx, repo, want string) (string, error) {
	for n := 1; ; n++ {
		id := want
		if n > 1 {
			id = fmt.Sprintf("%s-%d", want, n)
		}
		var taken int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM items WHERE bridge = ? AND repo = ? AND id = ?`,
			HubBridge, repo, id).Scan(&taken); err != nil {
			return "", err
		}
		if taken == 0 {
			return id, nil
		}
	}
}

// apply changes an item the way an action would change its bead. A note's
// comment, or an ask's, takes commentID. A stop changes no bead.
func apply(it *api.Item, a api.Action, commentID string) {
	switch a.Kind {
	case api.KindStop:
		return
	case api.KindDone, api.KindDismiss:
		if a.Payload.Was != nil {
			// A repeating personal item moved on to its next date instead.
			break
		}
		it.Status = "closed"
		it.CloseReason = ReasonDone
		if a.Kind == api.KindDismiss {
			it.CloseReason = ReasonDismissed
		}
		t := a.CreatedAt
		it.ClosedAt = &t
	case api.KindReopen:
		it.Status = "open"
		it.CloseReason = ""
		it.ClosedAt = nil
	case api.KindSnooze:
		it.DeferUntil = a.Payload.Until
	case api.KindNote, api.KindAsk:
		if a.Payload.Text == "" {
			break
		}
		it.Comments = append(append([]api.Comment{}, it.Comments...), api.Comment{
			ID:        commentID,
			Author:    a.Author,
			Text:      a.Payload.Text,
			CreatedAt: a.CreatedAt,
		})
	}
	if a.CreatedAt.After(it.UpdatedAt) {
		it.UpdatedAt = a.CreatedAt
	}
}

// applyHub applies an action on a list the hub keeps and records it as
// applied, with the created item's id for an add. It returns the keys of
// items it created.
func (s *Store) applyHub(tx *sql.Tx, a *api.Action) ([]string, error) {
	var created []string
	if a.Kind == api.KindAdd {
		id, err := freeHubID(tx, a.Repo, fmt.Sprintf("mm-%d", a.Seq))
		if err != nil {
			return nil, err
		}
		it := api.Item{
			ID:        id,
			Title:     a.Payload.Title,
			Status:    "open",
			Priority:  2,
			CreatedBy: a.Author,
			DueAt:     a.Payload.Due,
			Parent:    a.Payload.Parent,
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.CreatedAt,
		}
		if r := a.Payload.Repeat; r != nil && a.Payload.Due != nil {
			it.Repeat = ptr(*r)
			it.Repeat.Start = ptr(*a.Payload.Due)
		}
		if err := putHubItem(tx, a.Repo, it, a.CreatedAt, nil); err != nil {
			return nil, err
		}
		a.BeadID = id
		created = append(created, ItemKey(HubBridge, a.Repo, id))
	} else {
		it, err := hubItem(tx, a.Repo, a.BeadID)
		if err != nil {
			return nil, err
		}
		if err := applyPersonal(&it, a); err != nil {
			return nil, err
		}
		apply(&it, *a, a.ID)
		if err := putHubItem(tx, a.Repo, it, it.FirstSeen, it.SeenAt); err != nil {
			return nil, err
		}
		if err := applyHubSubtasks(tx, a); err != nil {
			return nil, err
		}
	}
	// The payload is stored again: an edit or a note change records in it
	// what it replaced.
	payload, err := json.Marshal(a.Payload)
	if err != nil {
		return nil, err
	}
	now := s.now()
	a.Bridge, a.Status, a.Error, a.AppliedAt = HubBridge, api.StatusApplied, "", &now
	_, err = tx.Exec(`UPDATE actions SET bridge = ?, repo = ?, bead_id = ?, payload = ?, status = ?, error = '', applied_at = ? WHERE seq = ?`,
		a.Bridge, a.Repo, a.BeadID, string(payload), a.Status, fmtTime(now), a.Seq)
	return created, err
}

// legacyItem is a bead from a bridge's personal repo, with the times the
// hub kept for it.
type legacyItem struct {
	item      api.Item
	firstSeen time.Time
	seenAt    *time.Time
}

// importPersonal copies beads from a bridge's personal repo into the
// personal list. Each bead arrives once: one already imported is skipped,
// whatever it holds now, because the hub's copy is the one that changes.
// An id taken by another item gets a suffix. Reminders already fired for a
// bead stay fired. A bead carries no repeat: one is set only by an add or
// edit on the hub, which checks it. It returns the keys of the items it
// created.
func importPersonal(tx *sql.Tx, bridge, repo string, items []legacyItem) ([]string, error) {
	var created []string
	for _, l := range items {
		it := l.item
		it.Repeat = nil
		var done int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM personal_origins WHERE bridge = ? AND bead_id = ?`,
			bridge, it.ID).Scan(&done); err != nil {
			return nil, err
		}
		if done > 0 {
			continue
		}
		beadID := it.ID
		id, err := freeHubID(tx, PersonalRepo, beadID)
		if err != nil {
			return nil, err
		}
		it.ID = id
		if err := putHubItem(tx, PersonalRepo, it, l.firstSeen, l.seenAt); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO personal_origins (bridge, bead_id, item_id) VALUES (?, ?, ?)`,
			bridge, beadID, id); err != nil {
			return nil, err
		}
		if err := moveReminders(tx, ItemKey(bridge, repo, beadID), ItemKey(HubBridge, PersonalRepo, id)); err != nil {
			return nil, err
		}
		created = append(created, ItemKey(HubBridge, PersonalRepo, id))
	}
	return created, nil
}

// moveReminders rekeys the reminder scheduler's sent marks, in those of its
// tables that exist.
func moveReminders(tx *sql.Tx, from, to string) error {
	tables, err := reminderTablesIn(tx)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if _, err := tx.Exec(`UPDATE OR IGNORE `+table+` SET item_key = ? WHERE item_key = ?`, to, from); err != nil {
			return err
		}
	}
	return nil
}

// originOf returns the personal item a bridge's bead was imported as.
func originOf(tx *sql.Tx, bridge, beadID string) (string, bool, error) {
	var id string
	err := tx.QueryRow(`SELECT item_id FROM personal_origins WHERE bridge = ? AND bead_id = ?`, bridge, beadID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

// TransferPersonal moves what the hub holds of each bridge's personal repo
// into the personal list: the stored items, then the actions still waiting
// for that bridge, applied on the hub. An action on a bead the hub never
// had is cancelled. The bridge's personal repo stays known, so an older
// bridge that still sends it feeds new beads to the list (see ReplaceItems).
// Safe to run any number of times; Open runs it on every start.
func (s *Store) TransferPersonal() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	type repo struct{ bridge, name string }
	var legacy []repo
	rows, err := tx.Query(`SELECT bridge, name FROM repos WHERE personal = 1 AND name = ? AND bridge != ?`,
		PersonalRepo, HubBridge)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r repo
		if err := rows.Scan(&r.bridge, &r.name); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	changed := false
	var created []string
	for _, r := range legacy {
		var items []legacyItem
		rows, err := tx.Query(`SELECT data, first_seen, seen_at FROM items WHERE bridge = ? AND repo = ? ORDER BY id`, r.bridge, r.name)
		if err != nil {
			return err
		}
		for rows.Next() {
			var data, first string
			var seen sql.NullString
			if err := rows.Scan(&data, &first, &seen); err != nil {
				rows.Close()
				return err
			}
			it, err := decodeItem(r.bridge, r.name, data, first, seen)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, legacyItem{it, it.FirstSeen, it.SeenAt})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		keys, err := importPersonal(tx, r.bridge, r.name, items)
		if err != nil {
			return err
		}
		created = append(created, keys...)
		if _, err := tx.Exec(`DELETE FROM items WHERE bridge = ? AND repo = ?`, r.bridge, r.name); err != nil {
			return err
		}
		changed = changed || len(items) > 0

		pending, err := queryActionsTx(tx, `SELECT `+actionCols+` FROM actions
			WHERE bridge = ? AND repo = ? AND status = ? ORDER BY seq`, r.bridge, r.name, api.StatusPending)
		if err != nil {
			return err
		}
		for _, a := range pending {
			changed = true
			if a.Kind != api.KindAdd {
				id, ok, err := originOf(tx, r.bridge, a.BeadID)
				if err != nil {
					return err
				}
				if !ok {
					if _, err := tx.Exec(`UPDATE actions SET status = ? WHERE seq = ?`, api.StatusCancelled, a.Seq); err != nil {
						return err
					}
					continue
				}
				a.BeadID = id
			}
			a.Repo = PersonalRepo
			keys, err := s.applyHub(tx, &a)
			if err != nil {
				return err
			}
			created = append(created, keys...)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if changed {
		s.emit(Change{Kind: "items", Bridge: HubBridge, Repo: PersonalRepo, NewItems: created})
	}
	return nil
}

func queryActionsTx(tx *sql.Tx, query string, args ...any) ([]api.Action, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.Action
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// isLegacyPersonal reports whether a bridge's repo is the personal repo an
// older bridge still sends.
func isLegacyPersonal(tx *sql.Tx, bridge, repo string) (bool, error) {
	if bridge == HubBridge {
		return false, nil
	}
	var personal bool
	err := tx.QueryRow(`SELECT personal FROM repos WHERE bridge = ? AND name = ?`, bridge, repo).Scan(&personal)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return personal, err
}

// hiddenClosed reports whether a closed personal item is old enough to
// leave the app, as a closed bead leaves a bridge's item set.
func hiddenClosed(it api.Item, now time.Time) bool {
	return it.Status == "closed" && it.ClosedAt != nil && it.ClosedAt.Before(now.Add(-closedShownFor))
}

// reservedBridge is the error for a bridge name the hub keeps for itself.
func reservedBridge(name string) error {
	if strings.EqualFold(name, HubBridge) {
		return invalidf("the name %q is reserved for the hub's own list", name)
	}
	return nil
}

// importFromBridge feeds an older bridge's personal repo set to the
// personal list and commits.
func (s *Store) importFromBridge(tx *sql.Tx, bridge, repo string, items []api.Item) error {
	now := s.now()
	l := make([]legacyItem, len(items))
	for i, it := range items {
		l[i] = legacyItem{item: it, firstSeen: now}
	}
	created, err := importPersonal(tx, bridge, repo, l)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE repos SET synced_at = ? WHERE bridge = ? AND name = ?`, fmtTime(now), bridge, repo); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(created) > 0 {
		s.emit(Change{Kind: "items", Bridge: HubBridge, Repo: PersonalRepo, NewItems: created})
	}
	return nil
}

// noteNamespace derives the id of the note an agent adds with a to-do.
var noteNamespace = uuid.MustParse("5d0c2f3a-7b1e-4c6d-9a48-2e1f0b7c3d95")

// AddPersonal adds an agent's to-do to the personal list, or to the
// project when it is not empty, with its note as the first comment, for
// the agents of the machine whose token is bridge's. The first add to a
// project makes it. Both are by t.Author, or by the machine's name when it
// is empty. The item is recorded as that machine's, so its agents may
// change it later. A repeated t.ID returns the item it added and changes
// nothing, except that a note lost to a failure after the add is written.
func (s *Store) AddPersonal(t api.PersonalTodo, bridge, project string) (key string, created bool, err error) {
	repo, err := ProjectRepo(project)
	if err != nil {
		return "", false, err
	}
	author, err := agentAuthor(t.Author, bridge)
	if err != nil {
		return "", false, err
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if strings.TrimSpace(t.Note) == "" {
		t.Note = ""
	}
	if utf8.RuneCountInString(t.Note) > MaxNoteLen {
		return "", false, invalidf("note is longer than %d characters", MaxNoteLen)
	}
	if repo != PersonalRepo {
		// A project with nothing in it is left out of State, so one made
		// by an add that then fails shows nowhere.
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO repos (bridge, name, personal, opted_in) VALUES (?, ?, 0, 1)`,
			HubBridge, repo); err != nil {
			return "", false, err
		}
	}
	t.Parent = strings.TrimSpace(t.Parent)
	if t.Parent != "" {
		// The parent is one this machine added, like any item its agents
		// read; a retry of an add already made skips the check, since the
		// parent may have closed since.
		if _, err := s.Action(t.ID); errors.Is(err, ErrNotFound) {
			if _, err := s.ownedPersonal(bridge, repo, t.Parent); err != nil {
				return "", false, err
			}
		}
	}
	a, created, err := s.appendAction(api.NewAction{ID: t.ID, Kind: api.KindAdd, Bridge: HubBridge, Repo: repo,
		Payload: api.Payload{Title: t.Title, Due: t.Due, Repeat: t.Repeat, Parent: t.Parent}}, author, func(tx *sql.Tx, a api.Action) error {
		return recordOwner(tx, a.Repo, a.BeadID, bridge)
	})
	if err != nil {
		return "", false, err
	}
	if a.Kind != api.KindAdd || !onHub(a.Bridge) || a.Repo != repo {
		return "", false, conflictf("action %q is not a to-do added to this list", t.ID)
	}
	key = ItemKey(HubBridge, repo, a.BeadID)
	// The note's id comes from the add's, so a retry after a failure between
	// the two writes stores the note once and a plain retry adds none.
	if t.Note != "" {
		if _, _, err := s.AppendAction(api.NewAction{ID: uuid.NewSHA1(noteNamespace, []byte(t.ID)).String(), Kind: api.KindNote, Key: key,
			Payload: api.Payload{Text: t.Note}}, author); err != nil && (created || !errors.Is(err, ErrNotFound)) {
			// A retry for an item since deleted or moved has nothing to add to.
			return key, created, err
		}
	}
	return key, created, nil
}
