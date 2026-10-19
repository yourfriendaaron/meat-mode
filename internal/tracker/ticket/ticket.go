// Package ticket is the tracker for ticket (https://github.com/wedow/ticket),
// the `tk` script. It reads the .tickets folder directly, since tk has no
// JSON for a task's title or notes, and writes through tk wherever tk has the
// command. docs/trackers/ticket.md describes what it writes.
package ticket

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
const Name = "ticket"

const (
	storeDir      = ".tickets"
	dismissedNote = "Dismissed in Meat Mode"
	// refPrefix starts the external-ref Create writes, ahead of the action id.
	refPrefix = "meat-mode:"
)

func init() {
	tracker.Register(tracker.Factory{Name: Name, New: func(env tracker.Env) (tracker.Tracker, error) {
		bin := env.Bin
		if bin == "" {
			bin = "tk"
		}
		look := env.LookPath
		if look == nil {
			look = exec.LookPath
		}
		return &Ticket{bin: bin, look: look}, nil
	}})
}

// Ticket drives ticket.
type Ticket struct {
	bin  string
	look func(string) (string, error)
}

var _ tracker.Tracker = (*Ticket)(nil)
var _ tracker.Agent = (*Ticket)(nil)

func (*Ticket) Info() tracker.Info {
	return tracker.Info{
		Name:  Name,
		Title: "ticket",
		Rank:  20,
		Prune: []string{storeDir},
		// No snooze: ticket has no field for it.
		Actions: []string{
			api.KindDone, api.KindReopen, api.KindDismiss, api.KindNote,
			api.KindAdd, tracker.CapSubtask, api.KindAsk,
		},
	}
}

var versionNumber = regexp.MustCompile(`\d+\.\d+(\.\d+)?\S*`)

// Check finds tk. A release of tk prints no version; one that does is
// reported.
func (t *Ticket) Check(ctx context.Context) (string, error) {
	path, err := t.look(t.bin)
	if err != nil {
		return "", fmt.Errorf("tk not found: %w", err)
	}
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", nil
	}
	return versionNumber.FindString(string(out)), nil
}

// Detect matches a .tickets folder that holds a ticket file or nothing at
// all, so a repo that has only made the folder still counts.
func (*Ticket) Detect(dir string, entries []fs.DirEntry) bool {
	found := false
	for _, e := range entries {
		if e.Name() == storeDir && e.IsDir() {
			found = true
		}
	}
	if !found {
		return false
	}
	files, err := os.ReadDir(filepath.Join(dir, storeDir))
	if err != nil {
		return false
	}
	if len(files) == 0 {
		return true
	}
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
			return true
		}
	}
	return false
}

func (*Ticket) OptedIn(dir string) bool { return tracker.RepoOptIn(dir) }

func (t *Ticket) List(_ context.Context, dir string) ([]tracker.Issue, error) {
	files, err := os.ReadDir(filepath.Join(dir, storeDir))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []tracker.Issue
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		is, err := read(dir, strings.TrimSuffix(f.Name(), ".md"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // removed between the listing and the read
			}
			return nil, err
		}
		if !forPerson(is) || !tracker.InWindow(is, now) {
			continue
		}
		is.Comments = nil
		out = append(out, is)
	}
	return out, nil
}

func forPerson(is tracker.Issue) bool {
	for _, l := range is.Labels {
		if l == tracker.HumanLabel {
			return true
		}
	}
	return false
}

func (t *Ticket) Show(_ context.Context, dir, id string) (tracker.Issue, error) {
	return read(dir, id)
}

// read parses one ticket file. A missing file is tracker.ErrNotFound.
func read(dir, id string) (tracker.Issue, error) {
	if !validID(id) {
		return tracker.Issue{}, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
	}
	path := filepath.Join(dir, storeDir, id+".md")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return tracker.Issue{}, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
	}
	if err != nil {
		return tracker.Issue{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return tracker.Issue{}, err
	}
	sum := sha256.Sum256(data)
	return parseTicket(data).issue(id, hex.EncodeToString(sum[:8]), info.ModTime()), nil
}

// validID keeps an id inside the store folder: tk's own ids are letters,
// digits and dashes.
func validID(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\`) && !strings.HasPrefix(id, ".")
}

func (t *Ticket) Close(ctx context.Context, dir, id, _, _ string) error {
	return t.run(ctx, dir, id, "close", id)
}

func (t *Ticket) Reopen(ctx context.Context, dir, id, _ string) error {
	return t.run(ctx, dir, id, "reopen", id)
}

func (*Ticket) Defer(context.Context, string, string, *time.Time, string) error {
	return tracker.ErrUnsupported
}

// Dismiss closes the ticket and notes why, since ticket has no status for
// it; List reads the note back as the close reason.
func (t *Ticket) Dismiss(ctx context.Context, dir, id, actor string) error {
	if err := t.run(ctx, dir, id, "close", id); err != nil {
		return err
	}
	return t.AddComment(ctx, dir, id, actor, dismissedNote)
}

// AddComment writes the note with its author on a first line, which Show
// strips off again.
func (t *Ticket) AddComment(ctx context.Context, dir, id, author, text string) error {
	if author != "" && strings.ContainsAny(author, " \t\n") {
		author = strings.Join(strings.Fields(author), "-")
	}
	if author != "" {
		text = "by " + author + ":\n\n" + text
	}
	return t.run(ctx, dir, id, "add-note", id, text)
}

func (t *Ticket) Create(ctx context.Context, dir string, in tracker.NewIssue) (string, error) {
	title := strings.Join(strings.Fields(in.Title), " ")
	args := []string{"create", title, "--tags", tracker.HumanLabel, "-a", in.Actor}
	if in.Parent != "" {
		if !validID(in.Parent) {
			return "", fmt.Errorf("%s: %w", in.Parent, tracker.ErrNotFound)
		}
		args = append(args, "--parent", in.Parent)
	}
	if in.ActionID != "" {
		args = append(args, "--external-ref", refPrefix+in.ActionID)
	}
	if err := os.MkdirAll(filepath.Join(dir, storeDir), 0o755); err != nil {
		return "", err
	}
	out, err := t.exec(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(out)
	if !validID(id) || strings.ContainsAny(id, " \n") {
		return "", fmt.Errorf("tk create printed %q, not a ticket id", out)
	}
	if in.Due != nil {
		if err := setDue(dir, id, *in.Due); err != nil {
			return id, fmt.Errorf("ticket %s created, but its due date was not kept: %w", id, err)
		}
	}
	return id, nil
}

// setDue adds a due: line to the frontmatter, the shape tk parses (a key at
// column 0), through a temp file and a rename so a reader never sees half a
// file. tk has no flag for it.
func setDue(dir, id string, due time.Time) error {
	path := filepath.Join(dir, storeDir, id+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	end := -1
	if len(lines) > 0 && lines[0] == "---" {
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				end = i
				break
			}
		}
	}
	if end < 0 {
		return errors.New("no frontmatter")
	}
	line := "due: " + due.UTC().Format(time.RFC3339)
	lines = append(lines[:end], append([]string{line}, lines[end:]...)...)
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+id+".md.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(strings.Join(lines, "\n")); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// FindByActionID finds the ticket whose external-ref names the action.
func (t *Ticket) FindByActionID(_ context.Context, dir, actionID string) (string, error) {
	files, err := os.ReadDir(filepath.Join(dir, storeDir))
	if err != nil {
		return "", err
	}
	want := refPrefix + actionID
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, storeDir, f.Name()))
		if err != nil {
			continue
		}
		if parseTicket(data).fields["external-ref"] == want {
			return strings.TrimSuffix(f.Name(), ".md"), nil
		}
	}
	return "", nil
}

// run checks that ticket id exists, then runs tk. Without the check, tk would
// match a partial id.
func (t *Ticket) run(ctx context.Context, dir, id string, args ...string) error {
	if !validID(id) {
		return fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
	}
	if _, err := os.Stat(filepath.Join(dir, storeDir, id+".md")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
		}
		return err
	}
	_, err := t.exec(ctx, dir, args...)
	return err
}

// exec runs tk in the repo, pointed at its .tickets folder so a repo nested
// in another is never mistaken for the outer one.
func (t *Ticket) exec(ctx context.Context, dir string, args ...string) (string, error) {
	path, err := t.look(t.bin)
	if err != nil {
		return "", fmt.Errorf("tk not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TICKETS_DIR="+filepath.Join(dir, storeDir))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tk %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// Prompt tells an agent run where the task is and how to answer.
func (*Ticket) Prompt(id string) string {
	return fmt.Sprintf(`A person asked you about the ticket %[1]s. Read it, with its notes, using `+"`tk show %[1]s`"+`.

Do what it asks. When you have an answer or a question for them, reply with a note: `+"`tk add-note %[1]s \"<your reply>\"`"+`. When the work is finished, close it: `+"`tk close %[1]s`"+`.`, id)
}

func (*Ticket) FollowUp(id, text string) string {
	return fmt.Sprintf("The person added a note to ticket %s:\n\n%s\n\nRead the ticket again with `tk show %s` if you need the rest, then carry on.", id, text, id)
}

// Env sets nothing: tk has no actor variable.
func (*Ticket) Env(string) []string { return nil }

func (*Ticket) Describe(words []string) string {
	if len(words) < 2 || filepath.Base(words[0]) != "tk" {
		return ""
	}
	switch words[1] {
	case "show":
		return "Reading the ticket"
	case "add-note":
		return "Adding a note"
	case "close":
		return "Closing the ticket"
	case "reopen":
		return "Reopening the ticket"
	case "create":
		return "Creating a ticket"
	case "ls", "list", "ready", "blocked", "closed", "query":
		return "Looking through the tickets"
	}
	return ""
}
