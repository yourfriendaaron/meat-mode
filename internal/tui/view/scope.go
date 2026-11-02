package view

import (
	"path/filepath"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Here is where the viewer was started, so it can open on that project's
// lists alone. A bridge names a repo by its folder relative to the home
// folder, or for a root outside home relative to the root's parent
// (bridge.repoName), so a folder matches a list whose name is its path
// below Home, or the last segments of its path.
type Here struct {
	Home string // the home folder, absolute; may be empty
	Dir  string // the working folder, absolute; empty for no scope
}

// hereLists are the keys of the lists Here is in: those named for the
// deepest folder, from Dir up, that names any. A project on several
// machines gives one list on each. The personal list never matches.
func hereLists(repos []api.Repo, h Here) map[string]bool {
	if h.Dir == "" {
		return nil
	}
	dir := filepath.Clean(h.Dir)
	for {
		rel := ""
		if h.Home != "" {
			if r, err := filepath.Rel(filepath.Clean(h.Home), dir); err == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				rel = filepath.ToSlash(r)
			}
		}
		path := filepath.ToSlash(dir)
		out := map[string]bool{}
		for k := range repos {
			r := &repos[k]
			if isPersonalList(r) || r.Name == "" {
				continue
			}
			if r.Name == rel || strings.HasSuffix(path, "/"+r.Name) {
				out[listKey(r.Bridge, r.Name)] = true
			}
		}
		if len(out) > 0 {
			return out
		}
		up := filepath.Dir(dir)
		if up == dir {
			return nil
		}
		dir = up
	}
}

// scoped is the state and epics cut to the lists in keep.
func scoped(s api.State, e api.Epics, keep map[string]bool) (api.State, api.Epics) {
	items := make([]api.Item, 0, len(s.Items))
	for _, it := range s.Items {
		if keep[listKey(it.Bridge, it.Repo)] {
			items = append(items, it)
		}
	}
	s.Items = items
	var repos []api.RepoEpics
	for _, re := range e.Repos {
		if keep[listKey(re.Bridge, re.Repo)] {
			repos = append(repos, re)
		}
	}
	e.Repos = repos
	return s, e
}

// scopeName is the name the tab bar gives the project in scope.
func (m *Model) scopeName() string {
	for k := range m.state.Repos {
		r := &m.state.Repos[k]
		if m.here[listKey(r.Bridge, r.Name)] {
			return repoLabel(r.Name)
		}
	}
	return ""
}
