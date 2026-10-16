package bridge

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// PersonalRepo is the repo name of the personal database.
const PersonalRepo = "personal"

// optInKey is the .beads/config.yaml key that opts a repo into syncing.
const optInKey = "meat-mode.sync"

// Repo is one beads database the bridge found.
type Repo struct {
	Name     string
	Dir      string
	Personal bool
	OptedIn  bool
}

// Synced reports whether the bridge sends this repo's items.
func (r Repo) Synced() bool { return r.Personal || r.OptedIn }

// Discover finds the beads repos: each top-level directory under a root that
// holds .beads/ (Git worktrees skipped), plus the personal repo. Names are
// directory names; the first root wins on a clash.
func Discover(roots []string, personal string, log *slog.Logger) []Repo {
	var out []Repo
	seen := map[string]bool{}
	personalAbs := ""
	if personal != "" {
		personalAbs, _ = filepath.Abs(personal)
		if isDir(filepath.Join(personal, ".beads")) {
			out = append(out, Repo{Name: PersonalRepo, Dir: personal, Personal: true, OptedIn: true})
			seen[PersonalRepo] = true
		} else {
			log.Warn("personal repo has no .beads directory", "dir", personal)
		}
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			log.Warn("cannot read root", "root", root, "err", err)
			continue
		}
		for _, e := range entries {
			dir := filepath.Join(root, e.Name())
			if !isDir(dir) || !isDir(filepath.Join(dir, ".beads")) {
				continue
			}
			if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && !fi.IsDir() {
				continue // a Git worktree shares its main checkout's beads
			}
			if abs, _ := filepath.Abs(dir); abs == personalAbs {
				continue
			}
			if seen[e.Name()] {
				log.Warn("skipping repo with a duplicate name", "dir", dir)
				continue
			}
			seen[e.Name()] = true
			out = append(out, Repo{Name: e.Name(), Dir: dir, OptedIn: OptedIn(dir)})
		}
	}
	slices.SortFunc(out, func(a, b Repo) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
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
