// Package backlog is the tracker for Backlog.md (github.com/MrLesk/Backlog.md):
// tasks kept as Markdown files in a repo's backlog/ folder. It drives the
// `backlog` CLI and reads its --json output; it never edits a task file
// itself. docs/trackers/backlog.md describes what each action writes.
package backlog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Name is the tracker's registry key.
const Name = "backlog"

// MinVersion is the oldest Backlog.md the tracker works with: the first with
// due dates and with references in `task list --json`.
const MinVersion = "1.51.0"

// Folders Backlog.md keeps its store in, and its config file names, as its
// own resolveBacklogDirectory reads them (src/utils/backlog-directory.ts).
var (
	storeDirs   = []string{"backlog", ".backlog"}
	configFiles = []string{"config.yml", "config.yaml"}
)

// rootConfig is the repo-root config file, which may name a custom folder.
const rootConfig = "backlog.config.yml"

func init() {
	tracker.Register(tracker.Factory{Name: Name, New: func(env tracker.Env) (tracker.Tracker, error) {
		bin := env.Bin
		if bin == "" && env.LookPath != nil {
			// Resolved through the bridge's LookPath, so the service PATH
			// gets its folder; a missing one fails Check.
			bin, _ = env.LookPath("backlog")
		}
		return &Tracker{Bin: bin}, nil
	}})
}

// Tracker drives the backlog CLI in a repo.
type Tracker struct {
	Bin     string        // backlog executable; "backlog" on PATH when empty
	Timeout time.Duration // per call; one minute when zero
	Now     func() time.Time
}

var (
	_ tracker.Tracker = (*Tracker)(nil)
	_ tracker.Agent   = (*Tracker)(nil)
)

// Info implements tracker.Tracker. Backlog.md has no defer field, so snooze
// is left out.
func (b *Tracker) Info() tracker.Info {
	return tracker.Info{
		Name:  Name,
		Title: "Backlog.md",
		Rank:  10,
		Prune: storeDirs,
		Actions: []string{
			api.KindDone, api.KindReopen, api.KindDismiss, api.KindNote,
			api.KindAdd, tracker.CapSubtask, api.KindAsk,
		},
	}
}

// Check implements tracker.Tracker.
func (b *Tracker) Check(ctx context.Context) (string, error) {
	out, err := b.run(ctx, "", "--version")
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(out))
	if err := checkVersion(v); err != nil {
		return "", err
	}
	return v, nil
}

// Detect implements tracker.Tracker. It matches the folders Backlog.md itself
// takes for a project root: a backlog.config.yml naming the project, or a
// backlog/ or .backlog/ folder holding a config file. A bare backlog/ folder
// is not enough, since plenty of repos have one.
func (b *Tracker) Detect(dir string, entries []fs.DirEntry) bool {
	for _, e := range entries {
		switch {
		case e.Name() == rootConfig && !e.IsDir():
			if _, ok := readRootConfig(filepath.Join(dir, rootConfig)); ok {
				return true
			}
		case e.IsDir() && isStoreDir(e.Name()):
			for _, f := range configFiles {
				if st, err := os.Stat(filepath.Join(dir, e.Name(), f)); err == nil && st.Mode().IsRegular() {
					return true
				}
			}
		}
	}
	return false
}

func isStoreDir(name string) bool {
	for _, d := range storeDirs {
		if name == d {
			return true
		}
	}
	return false
}

// readRootConfig reads a root backlog.config.yml as Backlog.md's
// parseBacklogConfigMetadata does: ok when it sets project_name, without
// which Backlog.md ignores the file, and the folder backlog_directory names.
func readRootConfig(path string) (folder string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		k, v, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "project_name", "projectName":
			ok = ok || v != ""
		case "backlog_directory", "backlogDirectory":
			folder = v
		}
	}
	return folder, ok
}

// OptedIn implements tracker.Tracker. Backlog.md rewrites its config file
// from known keys, so the repo-root .meat-mode.yaml is the only switch.
func (b *Tracker) OptedIn(dir string) bool { return tracker.RepoOptIn(dir) }

func (b *Tracker) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Error is a failed backlog call.
type Error struct {
	Args    []string
	Message string
}

func (e *Error) Error() string {
	return "backlog " + strings.Join(e.Args, " ") + ": " + e.Message
}

// idPattern is the shape of a task id the tracker will pass to backlog, such
// as TASK-12 or back-760.1.
var idPattern = regexp.MustCompile(`^[A-Za-z]+-[A-Za-z0-9]+(?:[._-][A-Za-z0-9]+)*$`)

// checkID rejects anything that is not a plain task id. A malformed id
// cannot name a task, so it is reported as not found.
func checkID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%w: %q is not a Backlog.md task id", tracker.ErrNotFound, id)
	}
	return nil
}

// run runs backlog in dir and returns stdout. A nonzero exit becomes *Error
// with backlog's first line of output; "not found" wraps tracker.ErrNotFound.
func (b *Tracker) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	bin := b.Bin
	if bin == "" {
		bin = "backlog"
	}
	timeout := b.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(childEnv(os.Environ()), "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("backlog is not installed: %w", err)
	}
	msg := firstLine(stderr.String())
	if msg == "" {
		msg = firstLine(stdout.String())
	}
	if msg == "" {
		msg = err.Error()
	}
	e := &Error{Args: args, Message: msg}
	if strings.Contains(msg, "not found") {
		return nil, fmt.Errorf("%w: %w", tracker.ErrNotFound, e)
	}
	return nil, e
}

// childEnv drops the bridge's own MEAT_MODE_* settings, the hub token among
// them, and BACKLOG_CWD, which would point backlog at another folder.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "MEAT_MODE_") && !strings.HasPrefix(kv, "BACKLOG_CWD=") {
			out = append(out, kv)
		}
	}
	return out
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
