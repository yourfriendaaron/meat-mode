package bridge

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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

// removeWorktree removes a bead's worktree and branch once the bead has left
// the synced set, when Git agrees they are safe to remove: `git worktree
// remove` refuses a worktree with changes or untracked files, and `git
// branch -d` a branch with commits not merged. Whatever Git refuses stays.
// It reports whether the worktree is gone.
func (r *AgentRunner) removeWorktree(path, bead string) bool {
	if !isDir(path) {
		return true
	}
	common, err := git(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		r.log().Warn("cannot read an agent worktree; leaving it", "dir", path, "err", err)
		return false
	}
	main := filepath.Dir(common)
	if _, err := git(main, "worktree", "remove", "--", path); err != nil {
		r.log().Info("keeping an agent worktree with changes in it", "dir", path)
		return false
	}
	if _, err := git(main, "branch", "-d", worktreeBranch(bead)); err != nil {
		r.log().Info("keeping an agent branch with unmerged commits", "branch", worktreeBranch(bead))
	}
	return true
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
