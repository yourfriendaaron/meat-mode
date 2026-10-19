package bridge

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Repo is one tracker store the bridge found.
type Repo struct {
	// Name is the repo's key: its path relative to the home folder, such as
	// projects/garden.
	Name    string
	Dir     string
	Tracker tracker.Tracker // the tracker whose store this is
	OptedIn bool            // the repo's own files switch syncing on (Tracker.OptedIn)
	Switch  *bool           // the sync switch set in the app, when there is one
}

// Synced reports whether the bridge sends this repo's items: as the app's
// switch says, else as the repo's own files say.
func (r Repo) Synced() bool {
	if r.Switch != nil {
		return *r.Switch
	}
	return r.OptedIn
}

// syncSwitch is the app's sync switch for the repo name in switches, nil
// when it has none.
func syncSwitch(switches map[string]bool, name string) *bool {
	if on, ok := switches[name]; ok {
		return &on
	}
	return nil
}

// Discover walks each root for the stores of trackers, which are in the
// order tracker.Sort gives. The walk skips the folders in pruneNames,
// pruneHomePaths and each tracker's Info.Prune, symlinks, Git worktrees and
// anything it cannot read, and finds repos nested at any depth. A folder
// belongs to the first tracker whose Detect matches it; any other that
// matches is skipped with a log line. Names are paths relative to home (see
// repoName); the first one found wins on a clash.
func Discover(roots []string, home string, trackers []tracker.Tracker, log *slog.Logger) []Repo {
	var out []Repo
	seen := map[string]bool{}    // names
	seenDir := map[string]bool{} // directories, when roots overlap
	homePath := ""
	if home != "" {
		homePath = resolvePath(home)
	}
	for _, root := range roots {
		start := time.Now()
		dirs := 0
		rootPath := resolvePath(root)
		for _, f := range scan(rootPath, homePath, trackers, &dirs, log) {
			dir := f.Dir
			name := repoName(homePath, rootPath, dir)
			switch {
			case seenDir[dir]:
				continue
			case !api.ValidRepoName(name):
				log.Warn("skipping repo whose path the hub refuses (a folder name starting with -, or a control character)", "dir", dir)
				continue
			case seen[name]:
				log.Warn("skipping repo with a duplicate name", "dir", dir)
				continue
			}
			seen[name], seenDir[dir] = true, true
			out = append(out, Repo{Name: name, Dir: dir, Tracker: f.Tracker, OptedIn: f.Tracker.OptedIn(dir)})
		}
		log.Debug("scanned for repos", "root", root, "dirs", dirs, "took", time.Since(start))
	}
	slices.SortFunc(out, func(a, b Repo) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// pruneNames are folders never searched, wherever they are: version control,
// dependency trees, package caches and virtual environments. Trackers add
// their own stores through Info.Prune.
var pruneNames = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".jj": true, ".worktrees": true,
	"node_modules": true, ".pnpm-store": true, ".yarn": true, ".npm": true, ".bun": true, ".deno": true,
	".cache": true, "__pycache__": true, ".venv": true, ".tox": true, ".mypy_cache": true, ".pytest_cache": true,
	".cargo": true, ".rustup": true, ".gradle": true, ".m2": true, ".nuget": true, ".cpan": true,
	".tmp": true, ".Trash": true, ".Trashes": true,
	".vscode": true, ".vscode-server": true, ".cursor": true,
}

// pruneHomePaths are folders under the home folder that hold caches, system
// or application data rather than the user's repos.
var pruneHomePaths = map[string]bool{
	"go/pkg":                      true,
	".local/share/containers":     true,
	".local/share/Trash":          true,
	".local/share/Steam":          true,
	".local/share/flatpak":        true,
	".local/share/mise":           true,
	".local/share/pnpm":           true,
	".local/share/docker":         true,
	".local/state":                true,
	".var":                        true,
	".steam":                      true,
	".mozilla":                    true,
	".docker":                     true,
	".nvm":                        true,
	".pyenv":                      true,
	".rbenv":                      true,
	".sdkman":                     true,
	"snap":                        true,
	"Library/Caches":              true,
	"Library/Containers":          true,
	"Library/Group Containers":    true,
	"Library/Developer":           true,
	"Library/Application Support": true,
	"Library/Logs":                true,
	"Library/Mail":                true,
	"Library/Photos":              true,
}

// found is one tracker store the walk found.
type found struct {
	Dir     string
	Tracker tracker.Tracker
}

// scan returns the directories under root that hold a tracker's store. It
// reads each directory once and counts them in *dirs.
func scan(root, home string, trackers []tracker.Tracker, dirs *int, log *slog.Logger) []found {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Warn("cannot read root", "root", root, "err", err)
		return nil
	}
	prune := map[string]bool{}
	for _, t := range trackers {
		for _, p := range t.Info().Prune {
			prune[p] = true
		}
	}
	var out []found
	var walk func(dir string, entries []fs.DirEntry)
	walk = func(dir string, entries []fs.DirEntry) {
		*dirs++
		for _, e := range entries {
			if e.Name() == ".git" && e.Type().IsRegular() && isWorktree(dir) {
				return // a worktree shares its main checkout's tasks
			}
		}
		if t := detect(dir, entries, trackers, log); t != nil {
			out = append(out, found{Dir: dir, Tracker: t})
		}
		var sub []string
		for _, e := range entries {
			if name := e.Name(); e.IsDir() && !pruneNames[name] && !prune[name] { // symlinks are not dirs here
				sub = append(sub, filepath.Join(dir, name))
			}
		}
		for _, d := range sub {
			if rel, ok := relTo(home, d); ok && pruneHomePaths[rel] {
				continue
			}
			if es, err := os.ReadDir(d); err == nil {
				walk(d, es)
			}
		}
	}
	walk(root, entries)
	return out
}

// detect returns the first of trackers whose store is in dir, logging any
// other that also matches, or nil.
func detect(dir string, entries []fs.DirEntry, trackers []tracker.Tracker, log *slog.Logger) tracker.Tracker {
	var won tracker.Tracker
	for _, t := range trackers {
		if !t.Detect(dir, entries) {
			continue
		}
		if won == nil {
			won = t
			continue
		}
		log.Info("skipping a second tracker in a repo; the first one syncs it", "dir", dir, "tracker", t.Info().Name, "syncs", won.Info().Name)
	}
	return won
}

// repoName is dir relative to home, or for a root outside home, relative to
// the root's parent so the name starts with the root's folder name.
func repoName(home, root, dir string) string {
	if rel, ok := relTo(home, dir); ok && rel != "." {
		return rel
	}
	rel, _ := relTo(filepath.Dir(root), dir)
	return rel
}

// relTo returns path relative to base, slash-separated, when path is inside
// base.
func relTo(base, path string) (string, bool) {
	if base == "" {
		return "", false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// isWorktree reports whether dir is a linked Git worktree: its .git is a file
// pointing into another checkout's .git/worktrees. A submodule's .git file
// points into .git/modules instead and is searched.
func isWorktree(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	return ok && strings.Contains(filepath.ToSlash(gitdir), "/worktrees/")
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// resolvePath is dir with symlinks resolved, so the same directory reached
// through different names compares equal. Unresolvable paths fall back to
// the absolute path.
func resolvePath(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	abs, _ := filepath.Abs(dir)
	return abs
}
