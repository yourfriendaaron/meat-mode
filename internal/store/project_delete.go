package store

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// A project deleted in the app is hidden, not dropped: its items stay, the
// open ones closed as Dismissed, so the Deleted view shows them for the
// usual 7 days and the delete has an Undo. A hidden project is left out of
// State and of Projects until an add or a reopen in it, or the Undo, shows
// it again.

// isProjectKind reports whether an action kind acts on a whole project.
func isProjectKind(kind string) bool {
	return kind == api.KindProjectDelete || kind == api.KindProjectRestore
}

// checkProjectTarget refuses a project-delete or project-restore on
// anything but a project the hub keeps.
func checkProjectTarget(tx *sql.Tx, kind, bridge, repo string) error {
	if !onHub(bridge) || repo == PersonalRepo || !ValidRepoName(repo) {
		return invalidf("%s is for a project; the personal list and a machine's repos cannot be deleted", kind)
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM repos WHERE bridge = ? AND name = ? AND personal = 0`, HubBridge, repo).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return notFoundf("no project %q", repo)
	}
	return nil
}

// cleanProjectItems trims a project-restore's ids and drops repeats.
func cleanProjectItems(ids []string) ([]string, error) {
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validSegment(id) {
			return nil, invalidf("payload.items holds %q, which is not a valid item id", id)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// applyProject applies a project-delete or project-restore. A delete
// closes every open item of the project as Dismissed and hides it, and
// records the ids it closed in the payload. A restore reopens those of its
// ids that are still deleted, records them, and shows the project again.
func applyProject(tx *sql.Tx, a *api.Action) error {
	ids := a.Payload.Items
	if a.Kind == api.KindProjectDelete {
		var err error
		if ids, err = queryStrings(tx, `SELECT id FROM items WHERE bridge = ? AND repo = ? ORDER BY id`, HubBridge, a.Repo); err != nil {
			return err
		}
	}
	changed := []string{}
	for _, id := range ids {
		it, err := hubItem(tx, a.Repo, id)
		if errors.Is(err, ErrNotFound) && a.Kind == api.KindProjectRestore {
			continue
		}
		if err != nil {
			return err
		}
		closed := it.Status == "closed"
		var kind string
		switch {
		case a.Kind == api.KindProjectDelete && !closed:
			kind = api.KindDismiss
		case a.Kind == api.KindProjectRestore && closed && it.CloseReason == ReasonDismissed:
			kind = api.KindReopen
		default:
			continue
		}
		apply(&it, api.Action{Kind: kind, CreatedAt: a.CreatedAt}, "")
		if err := putHubItem(tx, a.Repo, it, it.FirstSeen, it.SeenAt); err != nil {
			return err
		}
		changed = append(changed, id)
	}
	a.Payload = api.Payload{Items: changed}
	_, err := tx.Exec(`UPDATE repos SET hidden = ? WHERE bridge = ? AND name = ?`,
		a.Kind == api.KindProjectDelete, HubBridge, a.Repo)
	return err
}

// showProject shows a hidden project again, as an add or a reopen in it
// does.
func showProject(tx *sql.Tx, repo string) error {
	if repo == PersonalRepo {
		return nil
	}
	_, err := tx.Exec(`UPDATE repos SET hidden = 0 WHERE bridge = ? AND name = ? AND hidden != 0`, HubBridge, repo)
	return err
}

// hiddenProjects returns the names of the hidden projects.
func hiddenProjects(q rowsQuerier) ([]string, error) {
	return queryStrings(q, `SELECT name FROM repos WHERE bridge = ? AND hidden != 0 ORDER BY name`, HubBridge)
}
