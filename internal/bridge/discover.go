package bridge

import (
	"bufio"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// PersonalRepo is the repo name of the personal database.
const PersonalRepo = "personal"

// optInKey is the .beads/config.yaml key that opts a repo into syncing.
const optInKey = "meat-mode.sync"

// Repo is one beads database the bridge found.
type Repo struct {
	// Name is the repo's key: its path relative to the home folder, such as
	// projects/garden, or PersonalRepo for the personal database.
	Name     string
	Dir      string
	Personal bool
	OptedIn  bool  // the repo's config file has meat-mode.sync: true
	Switch   *bool // the sync switch set in the app, when there is one
}

// Synced reports whether the bridge sends this repo's items: always for the
// personal repo, else as the app's switch says, else as the repo's own
// config file says.
func (r Repo) Synced() bool {
	switch {
	case r.Personal:
		return true
	case r.Switch != nil:
		return *r.Switch
	}
	return r.OptedIn
}

// Discover walks each root for beads repos and adds the personal repo.
// The walk skips the folders in pruneNames and pruneHomePaths, symlinks,
// Git worktrees and anything it cannot read, and finds repos nested at any
// depth. A .beads folder counts only when it holds a database (see
// hasDatabase). Names are paths relative to home (see repoName); the first
// one found wins on a clash.
func Discover(roots []string, home, personal string, log *slog.Logger) []Repo {
	var out []Repo
	seen := map[string]bool{}    // names
	seenDir := map[string]bool{} // directories, when roots overlap
	personalPath := ""
	if personal != "" {
		personalPath = resolvePath(personal)
		if hasDatabase(personal) {
			out = append(out, Repo{Name: PersonalRepo, Dir: personal, Personal: true, OptedIn: true})
			seen[PersonalRepo] = true
		} else {
			log.Warn("personal repo has no beads database", "dir", personal)
		}
	}
	homePath := ""
	if home != "" {
		homePath = resolvePath(home)
	}
	for _, root := range roots {
		start := time.Now()
		dirs := 0
		rootPath := resolvePath(root)
		for _, dir := range scan(rootPath, homePath, &dirs, log) {
			name := repoName(homePath, rootPath, dir)
			switch {
			case dir == personalPath || seenDir[dir]:
				continue
			case !api.ValidRepoName(name):
				log.Warn("skipping repo whose path the hub refuses (a folder name starting with -, or a control character)", "dir", dir)
				continue
			case seen[name]:
				log.Warn("skipping repo with a duplicate name", "dir", dir)
				continue
			}
			seen[name], seenDir[dir] = true, true
			out = append(out, Repo{Name: name, Dir: dir, OptedIn: OptedIn(dir)})
		}
		log.Debug("scanned for beads repos", "root", root, "dirs", dirs, "took", time.Since(start))
	}
	slices.SortFunc(out, func(a, b Repo) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// pruneNames are folders never searched, wherever they are: version control,
// dependency trees, package caches and virtual environments.
var pruneNames = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".jj": true,
	".beads": true, ".worktrees": true,
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

// scan returns the directories under root that hold a beads database. It
// reads each directory once and counts them in *dirs.
func scan(root, home string, dirs *int, log *slog.Logger) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Warn("cannot read root", "root", root, "err", err)
		return nil
	}
	var found []string
	var walk func(dir string, entries []fs.DirEntry)
	walk = func(dir string, entries []fs.DirEntry) {
		*dirs++
		for _, e := range entries {
			if e.Name() == ".git" && e.Type().IsRegular() && isWorktree(dir) {
				return // a worktree shares its main checkout's beads
			}
		}
		var sub []string
		for _, e := range entries {
			name := e.Name()
			switch {
			case name == ".beads" && e.IsDir():
				if hasDatabase(dir) {
					found = append(found, dir)
				}
			case e.IsDir() && !pruneNames[name]: // symlinks are not dirs here
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
	return found
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

// hasDatabase reports whether dir/.beads holds a beads database: the
// metadata.json bd writes on init, or an older SQLite *.db file. bd's own
// ~/.beads folder holds neither.
func hasDatabase(dir string) bool {
	beadsDir := filepath.Join(dir, ".beads")
	if fi, err := os.Stat(filepath.Join(beadsDir, "metadata.json")); err == nil && fi.Mode().IsRegular() {
		return true
	}
	dbs, _ := filepath.Glob(filepath.Join(beadsDir, "*.db"))
	return len(dbs) > 0
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

// OptedIn reports whether dir/.beads/config.yaml has the top-level line
// `meat-mode.sync: true`.
func OptedIn(dir string) bool {
	f, err := os.Open(filepath.Join(dir, ".beads", "config.yaml"))
	if err != nil {
		return false
	}
	defer f.Close()
	on := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := yamlLine(sc.Text()); ok && k == optInKey {
			on = isTrue(v) // the last occurrence wins, as in YAML loaders
		}
	}
	return on
}

// yamlLine splits a top-level `key: value` line, removing quotes and a
// trailing comment.
func yamlLine(line string) (key, val string, ok bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
		return "", "", false
	}
	line = stripComment(line)
	var rest string
	switch line[0] {
	case '"', '\'':
		end := strings.IndexByte(line[1:], line[0])
		if end < 0 {
			return "", "", false
		}
		key, rest = line[1:1+end], line[2+end:]
		rest, ok = strings.CutPrefix(strings.TrimLeft(rest, " \t"), ":")
	default:
		key, rest, ok = strings.Cut(line, ":")
		key = strings.TrimSpace(key)
	}
	if !ok {
		return "", "", false
	}
	return key, unquote(strings.TrimSpace(rest)), true
}

// stripComment drops a # comment that is outside quotes and preceded by
// whitespace.
func stripComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '#' && i > 0 && (s[i-1] == ' ' || s[i-1] == '\t'):
			return strings.TrimRight(s[:i], " \t")
		}
	}
	return s
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

func isTrue(v string) bool {
	switch strings.ToLower(v) {
	case "true", "yes", "on":
		return true
	}
	return false
}
