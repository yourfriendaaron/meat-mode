package bridge

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/beads"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/fake"
)

// beadsOnly is the tracker list for walks over beads repos. Detect and
// OptedIn read files only, so bd need not be installed.
var beadsOnly = []tracker.Tracker{&beads.CLI{}}

func writeRepo(t *testing.T, dir, config string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".beads", "metadata.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repoDirs(repos []Repo) []string {
	var out []string
	for _, r := range repos {
		out = append(out, r.Dir)
	}
	slices.Sort(out)
	return out
}

func TestDiscoverWalksTheHomeFolder(t *testing.T) {
	home := t.TempDir()
	in := func(rel string) string { return filepath.Join(home, filepath.FromSlash(rel)) }
	writeRepo(t, in("projects/garden"), "")
	writeRepo(t, in("projects/garden/tools/sprinkler"), "") // nested in a repo
	writeRepo(t, in("notes/deep/down/journal"), "")         // outside ~/projects
	writeRepo(t, in(".notes/house/todo"), "")               // a dot folder that is not a cache
	// bd's global folder holds no database.
	writeFile(t, in(".beads/machine-id"), "0123\n")
	writeFile(t, in(".beads/eventsData/log"), "")
	// A .beads folder without a database is not a repo.
	writeFile(t, in("projects/empty/.beads/config.yaml"), "meat-mode.sync: true\n")
	// An older SQLite database counts.
	writeFile(t, in("projects/old/.beads/beads.db"), "")
	// Pruned anywhere, and under the home folder.
	writeRepo(t, in("projects/garden/node_modules/pkg"), "")
	writeRepo(t, in("projects/garden/.worktrees/feature"), "")
	writeRepo(t, in(".cache/thing"), "")
	writeRepo(t, in(".local/share/containers/storage/x"), "")
	writeRepo(t, in("go/pkg/mod/example.com/m"), "")
	writeRepo(t, in("Library/Caches/app"), "")
	// A linked worktree outside .worktrees is skipped with everything in it.
	writeRepo(t, in("projects/garden-wt"), "")
	writeFile(t, in("projects/garden-wt/.git"), "gitdir: ../garden/.git/worktrees/garden-wt\n")
	writeRepo(t, in("projects/garden-wt/tools/sprinkler"), "")
	// A submodule's .git file points into .git/modules; it is searched.
	writeRepo(t, in("projects/kitchen/vendor/lib"), "")
	writeFile(t, in("projects/kitchen/vendor/lib/.git"), "gitdir: ../../.git/modules/lib\n")
	// Symlinks are not followed.
	elsewhere := t.TempDir()
	writeRepo(t, filepath.Join(elsewhere, "shed"), "")
	if err := os.Symlink(filepath.Join(elsewhere, "shed"), in("projects/shed")); err != nil {
		t.Skipf("cannot symlink: %v", err)
	}
	if err := os.Symlink(in("projects"), in("projects-again")); err != nil {
		t.Fatal(err)
	}
	// An unreadable folder is skipped, not an error.
	locked := in("locked")
	writeRepo(t, filepath.Join(locked, "secret"), "")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	got := repoDirs(Discover(Config{}.ResolveRoots(home), home, beadsOnly, quiet))
	want := []string{
		in(".notes/house/todo"),
		in("notes/deep/down/journal"),
		in("projects/garden"),
		in("projects/garden/tools/sprinkler"),
		in("projects/kitchen/vendor/lib"),
		in("projects/old"),
	}
	if os.Geteuid() == 0 {
		want = append(want, filepath.Join(locked, "secret")) // root reads anything
		slices.Sort(want)
	}
	if !slices.Equal(got, want) {
		t.Errorf("found\n  %v\nwant\n  %v", got, want)
	}
}

func TestDiscoverRootOverride(t *testing.T) {
	home := t.TempDir()
	writeRepo(t, filepath.Join(home, "projects", "garden"), "")
	writeRepo(t, filepath.Join(home, "notes", "journal"), "")
	roots := Config{Roots: []string{"~/notes"}}.ResolveRoots(home)
	got := repoDirs(Discover(roots, home, beadsOnly, quiet))
	if !slices.Equal(got, []string{filepath.Join(home, "notes", "journal")}) {
		t.Errorf("found %v, want only the repo under --root", got)
	}
	// Overlapping roots find a repo once.
	got = repoDirs(Discover([]string{home, filepath.Join(home, "notes")}, home, beadsOnly, quiet))
	if len(got) != 2 {
		t.Errorf("overlapping roots found %v", got)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	writeRepo(t, filepath.Join(root, "garden"), "# settings\nmeat-mode.sync: true # on\n")
	writeRepo(t, filepath.Join(root, "kitchen"), "meat-mode.sync: false\n")
	writeRepo(t, filepath.Join(root, "attic"), "nested:\n  meat-mode.sync: true\n")
	writeRepo(t, filepath.Join(root, "quoted"), "\"meat-mode.sync\": 'true'\n")
	writeRepo(t, filepath.Join(root, "-shed"), "meat-mode.sync: true\n") // the hub refuses the name
	writeRepo(t, filepath.Join(root, "garden-wt"), "meat-mode.sync: true\n")
	if err := os.WriteFile(filepath.Join(root, "garden-wt", ".git"), []byte("gitdir: ../garden/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := Discover([]string{root, filepath.Join(root, "missing")}, root, beadsOnly, quiet)
	want := []Repo{
		{Name: "attic", Dir: filepath.Join(root, "attic"), Tracker: beadsOnly[0]},
		{Name: "garden", Dir: filepath.Join(root, "garden"), Tracker: beadsOnly[0], OptedIn: true},
		{Name: "kitchen", Dir: filepath.Join(root, "kitchen"), Tracker: beadsOnly[0]},
		{Name: "quoted", Dir: filepath.Join(root, "quoted"), Tracker: beadsOnly[0], OptedIn: true},
	}
	if len(got) != len(want) {
		t.Fatalf("Discover = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("repo %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRepoNamesArePaths(t *testing.T) {
	home := t.TempDir()
	writeRepo(t, filepath.Join(home, "projects", "garden"), "")
	writeRepo(t, filepath.Join(home, "work", "garden"), "")
	writeRepo(t, filepath.Join(home, "personal"), "") // an ordinary repo, whatever its name
	outside := t.TempDir()
	writeRepo(t, filepath.Join(outside, "repos", "shed"), "")
	writeRepo(t, filepath.Join(outside, "solo"), "")

	roots := []string{home, filepath.Join(outside, "repos"), filepath.Join(outside, "solo")}
	var got []string
	for _, r := range Discover(roots, home, beadsOnly, quiet) {
		got = append(got, r.Name)
	}
	want := []string{"personal", "projects/garden", "repos/shed", "solo", "work/garden"}
	if !slices.Equal(got, want) {
		t.Errorf("names %v, want %v", got, want)
	}
}

// TestDiscoverTrackers: each folder goes to the first tracker that detects
// it, every tracker's store is pruned, and a tracker's own opt-in applies.
func TestDiscoverTrackers(t *testing.T) {
	root := t.TempDir()
	writeRepo(t, filepath.Join(root, "garden"), "meat-mode.sync: true\n")
	writeFile(t, filepath.Join(root, "garden", fake.Store, "tasks"), "")
	writeFile(t, filepath.Join(root, "shed", fake.Store, "tasks"), "")
	writeFile(t, filepath.Join(root, "shed", tracker.OptInFile), "sync: true\n")
	// A repo inside a store is never reached.
	writeRepo(t, filepath.Join(root, "shed", fake.Store, "inner"), "")
	f := fake.New()
	f.Inf.Rank = 10
	ft := f.Tracker()
	ts := []tracker.Tracker{ft, beadsOnly[0]}
	tracker.Sort(ts)
	got := Discover([]string{root}, root, ts, quiet)
	want := []Repo{
		{Name: "garden", Dir: filepath.Join(root, "garden"), Tracker: beadsOnly[0], OptedIn: true},
		{Name: "shed", Dir: filepath.Join(root, "shed"), Tracker: ft, OptedIn: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Discover = %+v\nwant %+v", got, want)
	}
}
