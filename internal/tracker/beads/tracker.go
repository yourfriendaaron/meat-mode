package beads

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Name is the tracker's registry key.
const Name = "beads"

// optInKey is the .beads/config.yaml key that opts a repo into syncing.
const optInKey = "meat-mode.sync"

func init() {
	tracker.Register(tracker.Factory{Name: Name, New: New})
}

// New builds the beads tracker. With no Bin it looks bd up on PATH, so the
// path is absolute and a missing bd is reported by Check.
func New(env tracker.Env) (tracker.Tracker, error) {
	c := &CLI{Bin: env.Bin}
	if c.Bin != "" {
		if abs, err := filepath.Abs(c.Bin); err == nil {
			c.Bin = abs
		}
		return c, nil
	}
	look := env.LookPath
	if look == nil {
		look = exec.LookPath
	}
	p, err := look("bd")
	if err != nil {
		c.Missing = true
		return c, nil
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	c.Bin = p
	return c, nil
}

// Info implements tracker.Tracker. beads supports every capability.
func (c *CLI) Info() tracker.Info {
	return tracker.Info{
		Name:    Name,
		Title:   "beads",
		Rank:    0,
		Prune:   []string{".beads"},
		Actions: slices.Clone(tracker.Capabilities),
		Raise:   `bd create "Approve the footer text" -l human -d "What is needed, why, and where to find it."`,
	}
}

// Check implements tracker.Tracker: bd must run and be MinVersion or newer.
func (c *CLI) Check(ctx context.Context) (string, error) {
	if c.Missing {
		return "", errors.New("bd is not on PATH; install beads " + MinVersion + " or newer first")
	}
	v, err := c.Version(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot run bd: %w", err)
	}
	if err := CheckVersion(v); err != nil {
		return "", err
	}
	return v, nil
}

// Detect implements tracker.Tracker: a .beads folder that holds a database.
func (c *CLI) Detect(dir string, entries []fs.DirEntry) bool {
	for _, e := range entries {
		if e.Name() == ".beads" && e.IsDir() {
			return hasDatabase(dir)
		}
	}
	return false
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

// OptedIn implements tracker.Tracker: the top-level line
// `meat-mode.sync: true` in .beads/config.yaml, or the repo's
// .meat-mode.yaml.
func (c *CLI) OptedIn(dir string) bool {
	return tracker.ConfigTrue(filepath.Join(dir, ".beads", "config.yaml"), optInKey) || tracker.RepoOptIn(dir)
}
