package bridge

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// worktreeBranch is the branch an agent works on for one bead.
func worktreeBranch(bead string) string { return "meat-mode/" + bead }

// worktree returns the bead's Git worktree under Dir/worktrees and the folder
// in it that matches the repo's own, making the worktree on the bead's branch
// when it is missing. Every run on a bead works in the same one, so a resumed
// conversation finds its files.
func (r *AgentRunner) worktree(repo Repo, bead string) (path, cwd string, err error) {
	top, err := git(repo.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", fmt.Errorf("the repo is not in a Git repository, so the agent has no worktree to work in: %w", err)
	}
	rel, err := filepath.Rel(resolvePath(top), resolvePath(repo.Dir))
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("the repo folder is outside its Git checkout")
	}
	path = filepath.Join(r.Dir, "worktrees", filepath.FromSlash(repo.Name), bead)
	cwd = filepath.Join(path, rel)
	if isDir(path) {
		if _, err := git(path, "rev-parse", "--git-dir"); err == nil {
			return path, cwd, nil
		}
		return "", "", fmt.Errorf("%s exists but is not a Git worktree; move it away and ask again", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	_, _ = git(top, "worktree", "prune") // forget worktrees whose folder is gone
	branch := worktreeBranch(bead)
	if _, err := git(top, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		_, err = git(top, "worktree", "add", "--", path, branch)
		if err != nil {
			return "", "", fmt.Errorf("cannot make the agent's worktree: %w", err)
		}
		return path, cwd, nil
	}
	if _, err := git(top, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return "", "", fmt.Errorf("the repo has no commits yet, so the agent has no worktree to work in")
	}
	if _, err := git(top, "worktree", "add", "-b", branch, "--", path, "HEAD"); err != nil {
		return "", "", fmt.Errorf("cannot make the agent's worktree: %w", err)
	}
	return path, cwd, nil
}

// removeWorktree removes a worktree Collect picked, when it is safe to: it
// must be a linked worktree at its own top, on the bead's branch when the
// journal does not name it, with no changes, no untracked files and no
// ignored files outside the folders a build makes again (see rebuildable).
// For a bead that left the synced set, the branch goes too once the default
// branch holds every commit on it. Whatever stays is logged once per reason.
// It reports whether the worktree is gone.
func (r *AgentRunner) removeWorktree(j collectJob) bool {
	if !isDir(j.path) {
		// An idle bead's folder went before the bead left the set.
		if j.left && j.rec.Main != "" && isDir(j.rec.Main) {
			r.dropBranch(j.rec.Main, j.bead)
		}
		return true
	}
	main, why := r.removable(j)
	if why == "" {
		if _, err := git(main, "worktree", "remove", "--", j.path); err != nil {
			why = err.Error()
		}
	}
	if why != "" {
		r.mu.Lock()
		logIt := r.kept[j.path] != why
		r.kept[j.path] = why
		r.mu.Unlock()
		if logIt {
			r.log().Info("keeping an agent worktree", "dir", j.path, "why", why)
		}
		return false
	}
	r.mu.Lock()
	delete(r.kept, j.path)
	r.mu.Unlock()
	r.log().Info("removed an agent worktree", "dir", j.path)
	if j.left {
		r.dropBranch(main, j.bead)
	}
	return true
}

// dropBranch deletes the bead's branch in the main checkout main once the
// default branch holds every commit on it.
func (r *AgentRunner) dropBranch(main, bead string) {
	branch := worktreeBranch(bead)
	if _, err := git(main, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		return
	}
	if !landed(main, branch) {
		r.log().Info("keeping an agent branch with commits not on the default branch", "branch", branch)
		return
	}
	_, _ = git(main, "branch", "-D", "--", branch)
}

// removable returns the main checkout of j's worktree, and why it must stay,
// "" when it may go.
func (r *AgentRunner) removable(j collectJob) (main, why string) {
	out, err := git(j.path, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	paths := strings.Split(out, "\n")
	if err != nil || len(paths) != 3 {
		return "", "it cannot be read as a Git worktree"
	}
	if resolvePath(paths[0]) != resolvePath(j.path) || paths[1] == paths[2] {
		return "", "it is not a linked Git worktree of its own"
	}
	main = filepath.Dir(paths[2])
	if j.rec == nil {
		if b, _ := git(j.path, "symbolic-ref", "-q", "--short", "HEAD"); b != worktreeBranch(j.bead) {
			return "", "the bridge has no record of it and it is not on " + worktreeBranch(j.bead)
		}
	}
	status, err := git(j.path, "status", "--porcelain", "--ignored", "-z")
	if err != nil {
		return "", err.Error()
	}
	for _, e := range strings.Split(status, "\x00") {
		switch {
		case e == "":
		case strings.HasPrefix(e, "?? "):
			return "", "it has untracked files"
		case !strings.HasPrefix(e, "!! "):
			return "", "it has changes"
		case !rebuildable(e[3:]):
			return "", "it has ignored files a build does not make again, such as " + e[3:]
		}
	}
	return main, ""
}

// rebuildDirs are folders of dependencies, caches and build output, which
// a removed worktree may lose because a build or an install makes them again.
var rebuildDirs = map[string]bool{
	"node_modules": true, ".pnpm-store": true, ".venv": true, "venv": true, "__pycache__": true,
	".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true, ".tox": true, ".nox": true,
	"target": true, "dist": true, "build": true, ".next": true, ".nuxt": true, ".svelte-kit": true,
	".turbo": true, ".parcel-cache": true, ".cache": true, "coverage": true, ".gradle": true,
}

// rebuildable reports whether an ignored path, as git status names it, is
// in one of rebuildDirs, or is compiled Python or a Finder file.
func rebuildable(p string) bool {
	p = strings.TrimSuffix(p, "/")
	if strings.HasSuffix(p, ".pyc") || path.Base(p) == ".DS_Store" {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if rebuildDirs[seg] {
			return true
		}
	}
	return false
}

// landed reports whether the repo's default branch holds every commit on
// branch: origin's default as fetched or its local branch, else main or
// master, else what the main checkout has checked out.
func landed(main, branch string) bool {
	var bases []string
	if ref, err := git(main, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		bases = append(bases, ref, "refs/heads/"+path.Base(ref))
	}
	bases = append(bases, "refs/heads/main", "refs/heads/master")
	found := false
	for _, b := range bases {
		if _, err := git(main, "rev-parse", "--verify", "--quiet", b); err != nil {
			continue
		}
		found = true
		if _, err := git(main, "merge-base", "--is-ancestor", "refs/heads/"+branch, b); err == nil {
			return true
		}
	}
	if !found {
		_, err := git(main, "merge-base", "--is-ancestor", "refs/heads/"+branch, "HEAD")
		return err == nil
	}
	return false
}

// unnamed is a worktree folder the journal does not name.
type unnamed struct {
	path, repo, bead string
	made             time.Time // when its .git file was written
}

// unnamedWorktrees finds the folders under root that hold a .git entry and
// are not in named. Each one's parent path names its repo and its own name
// the bead, as worktree lays them out.
func unnamedWorktrees(root string, named map[string]bool) []unnamed {
	var out []unnamed
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == root {
			return nil
		}
		fi, err := os.Lstat(filepath.Join(p, ".git"))
		if err != nil {
			return nil
		}
		if !named[filepath.Clean(p)] {
			rel, _ := filepath.Rel(root, filepath.Dir(p))
			if rel != "." {
				out = append(out, unnamed{path: p, repo: filepath.ToSlash(rel), bead: filepath.Base(p), made: fi.ModTime()})
			}
		}
		return filepath.SkipDir
	})
	return out
}

// removeEmptyDirs removes the empty folders under root, deepest first,
// keeping root.
func removeEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			return filepath.SkipDir
		}
		if p != root {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // fails on a folder that is not empty
	}
}

// git runs git in dir and returns its trimmed output.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "MEAT_MODE_") && !strings.HasPrefix(kv, "GIT_DIR=") && !strings.HasPrefix(kv, "GIT_WORK_TREE=") &&
			!strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s", args[0], firstLineOf(msg))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
