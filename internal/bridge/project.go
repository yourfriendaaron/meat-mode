package bridge

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// projectFlag adds --project, which every command on the hub's lists takes.
func projectFlag(fs *flag.FlagSet) *string {
	return fs.String("project", "", `the project to use instead of the personal list: a repo name such as projects/garden, or "." for the Git repo of the current folder`)
}

// resolveProject reads --project: "" for the personal list, "." for the
// Git repo of the working folder, named as the bridge names the repos it
// finds, or else a repo name as given.
func resolveProject(env Env, project string) (string, error) {
	project = strings.TrimSpace(project)
	if project != "." {
		if project != "" && !api.ValidRepoName(project) {
			return "", fmt.Errorf("--project %q is not a repo name; use one such as projects/garden, or \".\" for this folder's repo", project)
		}
		return project, nil
	}
	wd, err := env.getwd()
	if err != nil {
		return "", err
	}
	top, err := gitTop(resolvePath(wd))
	if err != nil {
		return "", err
	}
	s, err := LoadSettings(env)
	if err != nil {
		return "", err
	}
	// A repo outside home is named from the scan root it is under, as the
	// walk names it.
	root := top
	for _, r := range s.Roots {
		if r = resolvePath(r); r != "" {
			if _, ok := relTo(r, top); ok {
				root = r
				break
			}
		}
	}
	name := repoName(resolvePath(env.Paths.Home), root, top)
	if !api.ValidRepoName(name) {
		return "", fmt.Errorf("the repo at %s has no name the hub takes; give --project a name", top)
	}
	return name, nil
}

// gitTop is the top folder of the Git checkout dir is in. A linked
// worktree answers with its main checkout, so every worktree of a repo
// names the same project.
func gitTop(dir string) (string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		fi, err := os.Lstat(filepath.Join(d, ".git"))
		switch {
		case err == nil && fi.IsDir():
			return d, nil
		case err == nil:
			if main, ok := worktreeMain(d); ok {
				return main, nil
			}
			return d, nil
		case !errors.Is(err, os.ErrNotExist):
			return "", err
		}
		if filepath.Dir(d) == d {
			return "", errors.New(`--project . needs a folder inside a Git repo; give the project's name instead`)
		}
	}
}

// worktreeMain is the main checkout of the linked worktree at dir, whose
// .git file points into <main>/.git/worktrees/<name>.
func worktreeMain(dir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return "", false
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return "", false
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	worktrees := filepath.Dir(gitdir)
	if filepath.Base(worktrees) != "worktrees" || filepath.Base(filepath.Dir(worktrees)) != ".git" {
		return "", false
	}
	return resolvePath(filepath.Dir(filepath.Dir(worktrees))), true
}
