package store

import (
	"database/sql"
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// OnlineWindow is how recently a bridge must have been heard from to count
// as online, here and in the app (web/src/model.ts, ONLINE_WINDOW_MS).
// SetOnlineWindow changes it for one store, for tests.
const OnlineWindow = 2 * time.Minute

// SetOnlineWindow sets how recently a bridge must have been heard from for
// a linked list to take its items; zero or less means OnlineWindow.
func (s *Store) SetOnlineWindow(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.online = d
}

// onlineWindow is the window SetOnlineWindow set, or OnlineWindow.
func (s *Store) onlineWindow() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.online <= 0 {
		return OnlineWindow
	}
	return s.online
}

// A linked list is the repos that share a tracker id: clones of one
// tracker store, on one machine or several, that hold the same tasks
// (docs/api.md, Linked lists). The list shows the items of one of them at a
// time, its source, so every task shows once.

// linkMember is one repo of a linked list.
type linkMember struct {
	bridge, name string
	optedIn      bool
	online       bool
	created      string // the bridge's created_at, which orders the members
}

func (m linkMember) key() string { return m.bridge + "/" + m.name }

// linkGroup is one linked list: its members in their fixed order, the
// first the primary, and the source, -1 when no member is opted in.
type linkGroup struct {
	members []linkMember
	source  int
}

// primary is the key the whole list goes by: its first repo's.
func (g linkGroup) primary() string { return g.members[0].key() }

// links are the linked lists, with lookups by repo key.
type links struct {
	groups []linkGroup
	of     map[string]int // repo key -> index into groups
}

// group returns the linked list the repo with key is in, if any.
func (l links) group(key string) (linkGroup, bool) {
	i, ok := l.of[key]
	if !ok {
		return linkGroup{}, false
	}
	return l.groups[i], true
}

// hidden reports whether the repo with key is a linked list's standby: the
// list shows another repo's items, so this one's are left out.
func (l links) hidden(key string) bool {
	g, ok := l.group(key)
	if !ok || g.source < 0 {
		return false
	}
	return g.members[g.source].key() != key
}

// sources is a fingerprint of every linked list's source and which of its
// repos are online, which changes when any list's items or epics move to
// another repo.
func (l links) sources() string {
	var b strings.Builder
	for _, g := range l.groups {
		b.WriteString(g.primary())
		for _, m := range g.members {
			// Epic sets follow which members are online, apart from the
			// items' source.
			if m.online {
				b.WriteString(" +")
			} else {
				b.WriteString(" -")
			}
		}
		b.WriteByte('>')
		if g.source >= 0 {
			b.WriteString(g.members[g.source].key())
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// linkedLists reads the linked lists: the repos of active bridges that
// share a tracker id, two or more to a list. Members are ordered by when
// their bridge was added, then bridge and repo name, so the primary never
// changes while its bridge stays. The source is the first member that is
// opted in and whose bridge is online; with none online, the first opted
// in, so the list keeps showing what it last had.
func linkedLists(q rowsQuerier, now time.Time, window time.Duration) (links, error) {
	rows, err := q.Query(`SELECT r.tracker_id, r.bridge, r.name, r.opted_in, b.created_at, b.last_seen FROM repos r
		JOIN bridges b ON b.name = r.bridge AND b.revoked_at IS NULL
		WHERE r.tracker_id != '' AND r.personal = 0 AND r.bridge != ?`, HubBridge)
	if err != nil {
		return links{}, err
	}
	defer rows.Close()
	byID := map[string][]linkMember{}
	var ids []string
	for rows.Next() {
		var id string
		var m linkMember
		var last sql.NullString
		if err := rows.Scan(&id, &m.bridge, &m.name, &m.optedIn, &m.created, &last); err != nil {
			return links{}, err
		}
		if seen := parseTimePtr(last); seen != nil && now.Sub(*seen) <= window {
			m.online = true
		}
		if _, ok := byID[id]; !ok {
			ids = append(ids, id)
		}
		byID[id] = append(byID[id], m)
	}
	if err := rows.Err(); err != nil {
		return links{}, err
	}
	out := links{of: map[string]int{}}
	for _, id := range ids {
		ms := byID[id]
		if len(ms) < 2 {
			continue
		}
		slices.SortFunc(ms, func(a, b linkMember) int {
			ta, tb := parseTime(a.created), parseTime(b.created)
			if c := ta.Compare(tb); c != 0 {
				return c
			}
			if c := strings.Compare(a.bridge, b.bridge); c != 0 {
				return c
			}
			return strings.Compare(a.name, b.name)
		})
		g := linkGroup{members: ms, source: -1}
		for i, m := range ms {
			if m.optedIn && m.online {
				g.source = i
				break
			}
		}
		if g.source < 0 {
			for i, m := range ms {
				if m.optedIn {
					g.source = i
					break
				}
			}
		}
		out.groups = append(out.groups, g)
	}
	// Order the lists by primary, so sources() is stable.
	slices.SortFunc(out.groups, func(a, b linkGroup) int { return strings.Compare(a.primary(), b.primary()) })
	for i, g := range out.groups {
		for _, m := range g.members {
			out.of[m.key()] = i
		}
	}
	return out, nil
}

// LinkedSources is a fingerprint of which repo each linked list shows now.
// The hub checks it on a timer, as a bridge going quiet changes no row.
func (s *Store) LinkedSources() (string, error) {
	l, err := linkedLists(s.db, s.now(), s.onlineWindow())
	if err != nil {
		return "", err
	}
	return l.sources(), nil
}

// migrateTrackerID adds repos.tracker_id: the tracker store's own identity
// as the bridge last reported it (api.Repo.TrackerID), ” for none. It is
// Go so that the tests of older migrations, which step user_version back,
// can run it again.
func (s *Store) migrateTrackerID(tx *sql.Tx) error {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('repos') WHERE name = 'tracker_id'`).Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err := tx.Exec(`ALTER TABLE repos ADD COLUMN tracker_id TEXT NOT NULL DEFAULT ''`)
	return err
}

// markLinked sets Group and Standby on the repos of linked lists, and gives
// every repo of a list its primary's colour and icon, so the list looks the
// same whichever repo it shows.
func markLinked(repos []api.Repo, l links) {
	at := map[string]int{}
	for i, r := range repos {
		at[r.Bridge+"/"+r.Name] = i
	}
	for i := range repos {
		r := &repos[i]
		key := r.Bridge + "/" + r.Name
		g, ok := l.group(key)
		if !ok {
			continue
		}
		r.Group, r.Standby = g.primary(), l.hidden(key)
		if p, ok := at[g.primary()]; ok && p != i {
			r.Colour, r.Icon = repos[p].Colour, repos[p].Icon
		}
	}
}
