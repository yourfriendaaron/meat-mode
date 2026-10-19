package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// CLI is the beads tracker. It runs `bd -C <dir> ... --json`; every call
// puts its flags first, then `--`, then bead ids and text, because bd reads
// flags anywhere on the line.
type CLI struct {
	Bin     string        // bd executable; "bd" when empty
	Timeout time.Duration // per call; one minute when zero
	Now     func() time.Time
	// Missing is set when bd was looked up on PATH and not found, so Check
	// says so in plain words.
	Missing bool
}

var (
	_ tracker.Tracker = (*CLI)(nil)
	_ tracker.Agent   = (*CLI)(nil)
)

// Error is a failed bd call.
type Error struct {
	Args    []string
	Message string
}

func (e *Error) Error() string {
	return "bd " + strings.Join(e.Args, " ") + ": " + e.Message
}

// idPattern is the shape of a bead id the bridge will pass to bd.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ErrBadID is returned for a bead id that does not look like one.
var ErrBadID = errors.New("not a valid bead id")

// CheckID rejects anything that is not a plain bead id, such as a value that
// starts with "-" and would reach bd as a flag.
func CheckID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrBadID, id)
	}
	return nil
}

func (c *CLI) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// run runs bd in dir and returns stdout. A nonzero exit becomes *Error, with
// the message from bd's {"error": ...} JSON when it printed one.
func (c *CLI) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	bin := c.Bin
	if bin == "" {
		bin = "bd"
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := args
	if dir != "" {
		full = append([]string{"-C=" + dir}, args...)
	}
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Env = append(childEnv(os.Environ()), "BD_NON_INTERACTIVE=1", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	msg := ""
	var body struct {
		Error string `json:"error"`
	}
	if i := bytes.IndexByte(stdout.Bytes(), '{'); i >= 0 && json.Unmarshal(stdout.Bytes()[i:], &body) == nil {
		msg = body.Error
	}
	if msg == "" {
		msg = errorLine(stderr.String())
	}
	if msg == "" {
		msg = err.Error()
	}
	e := &Error{Args: args, Message: msg}
	if strings.Contains(msg, "not found") || strings.Contains(msg, "no issues found") {
		return nil, fmt.Errorf("%w: %w", tracker.ErrNotFound, e)
	}
	return nil, e
}

// childEnv drops the bridge's own MEAT_MODE_* settings, the hub token among
// them, so bd and any hook it runs never see them.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "MEAT_MODE_") {
			out = append(out, kv)
		}
	}
	return out
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Error:")); t != "" {
			return t
		}
	}
	return ""
}

// errorLine is the first line of bd's stderr that is not a "Warning:" notice,
// such as the permissions warning, so the real failure is the one reported.
func errorLine(s string) string {
	var rest []string
	for line := range strings.SplitSeq(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "Warning:") {
			rest = append(rest, line)
		}
	}
	if m := firstLine(strings.Join(rest, "\n")); m != "" {
		return m
	}
	return firstLine(s)
}

// decode reads the JSON value in out, skipping any text bd printed first,
// such as a permissions warning. A warning may itself hold a "[" or "{", so
// it tries each candidate start until one parses.
func decode(out []byte, v any) error {
	var firstErr error
	for rest := out; ; {
		i := bytes.IndexAny(rest, "[{")
		if i < 0 {
			break
		}
		rest = rest[i:]
		dec := json.NewDecoder(bytes.NewReader(rest))
		err := dec.Decode(v)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
		rest = rest[1:]
	}
	if firstErr != nil {
		return firstErr
	}
	return fmt.Errorf("bd printed no JSON: %q", firstLine(string(out)))
}

// Version returns the bd version, such as "1.3.1".
func (c *CLI) Version(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "", "version", "--json")
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := decode(out, &v); err != nil {
		return "", err
	}
	if v.Version == "" {
		return "", errors.New("bd version printed no version")
	}
	return v.Version, nil
}

// List implements tracker.Tracker: the beads labelled human.
func (c *CLI) List(ctx context.Context, dir string) ([]tracker.Issue, error) {
	list, err := c.listHuman(ctx, dir)
	if err != nil {
		return nil, err
	}
	out := make([]tracker.Issue, 0, len(list))
	for _, b := range list {
		i := b.Issue()
		i.Comments = nil
		out = append(out, i)
	}
	return out, nil
}

// listHuman returns the beads labelled human, open or recently closed.
func (c *CLI) listHuman(ctx context.Context, dir string) ([]Bead, error) {
	out, err := c.run(ctx, dir, "list", "--all", "-n", "0", "--json", "-l", tracker.HumanLabel)
	if err != nil {
		return nil, err
	}
	var all []Bead
	if err := decode(out, &all); err != nil {
		return nil, err
	}
	now := c.now()
	keep := all[:0]
	for _, i := range all {
		if KeepHuman(i, now) {
			keep = append(keep, i)
		}
	}
	return keep, nil
}

// Show implements tracker.Tracker.
func (c *CLI) Show(ctx context.Context, dir, id string) (tracker.Issue, error) {
	b, err := c.ShowBead(ctx, dir, id)
	if err != nil {
		return tracker.Issue{}, err
	}
	return b.Issue(), nil
}

// ShowBead returns one bead with its comments, as bd prints it.
func (c *CLI) ShowBead(ctx context.Context, dir, id string) (Bead, error) {
	if err := CheckID(id); err != nil {
		return Bead{}, err
	}
	out, err := c.run(ctx, dir, "show", "--include-comments", "--json", "--", id)
	if err != nil {
		return Bead{}, err
	}
	var list []Bead
	if err := decode(out, &list); err != nil {
		return Bead{}, err
	}
	if len(list) == 0 {
		return Bead{}, fmt.Errorf("%w: %s", tracker.ErrNotFound, id)
	}
	return list[0], nil
}

// Close implements tracker.Tracker.
func (c *CLI) Close(ctx context.Context, dir, id, reason, actor string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := c.run(ctx, dir, "close", "--reason="+reason, "--actor="+actor, "--json", "--", id)
	if err != nil && strings.Contains(err.Error(), "open child issue") {
		return tracker.ErrOpenChildren
	}
	return err
}

// Reopen implements tracker.Tracker.
func (c *CLI) Reopen(ctx context.Context, dir, id, actor string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := c.run(ctx, dir, "reopen", "--actor="+actor, "--json", "--", id)
	return err
}

// Defer implements tracker.Tracker.
func (c *CLI) Defer(ctx context.Context, dir, id string, until *time.Time, actor string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	v := ""
	if until != nil {
		v = until.UTC().Format(time.RFC3339)
	}
	_, err := c.run(ctx, dir, "update", "--defer="+v, "--actor="+actor, "--json", "--", id)
	return err
}

// Dismiss implements tracker.Tracker with bd human dismiss. bd prints no
// JSON for it.
func (c *CLI) Dismiss(ctx context.Context, dir, id, actor string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := c.run(ctx, dir, "human", "dismiss", "--actor="+actor, "--", id)
	return err
}

// AddComment implements tracker.Tracker.
func (c *CLI) AddComment(ctx context.Context, dir, id, author, text string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := c.run(ctx, dir, "comments", "add", "--author="+author, "--actor="+author, "--json", "--", id, text)
	return err
}

// Create implements tracker.Tracker: a bead labelled human, with the action
// id in its metadata. Every value rides in a --flag=value argument, so none
// can be read as a flag of its own.
func (c *CLI) Create(ctx context.Context, dir string, in tracker.NewIssue) (string, error) {
	meta, _ := json.Marshal(map[string]string{ActionMetaKey: in.ActionID})
	args := []string{"create", "--title=" + in.Title, "-l", tracker.HumanLabel, "--metadata=" + string(meta), "--json"}
	if in.Actor != "" {
		args = append(args, "--actor="+in.Actor)
	}
	if in.Due != nil {
		args = append(args, "--due="+in.Due.UTC().Format(time.RFC3339))
	}
	if in.Parent != "" {
		if err := CheckID(in.Parent); err != nil {
			return "", err
		}
		args = append(args, "--parent="+in.Parent)
	}
	out, err := c.run(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	var created Bead
	if err := decode(out, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", errors.New("bd create printed no id")
	}
	return created.ID, nil
}

// FindByActionID implements tracker.Tracker.
func (c *CLI) FindByActionID(ctx context.Context, dir, actionID string) (string, error) {
	out, err := c.run(ctx, dir, "list", "--all", "-n", "0", "--metadata-field="+ActionMetaKey+"="+actionID, "--json")
	if err != nil {
		return "", err
	}
	var list []Bead
	if err := decode(out, &list); err != nil {
		return "", err
	}
	for _, i := range list {
		if i.ActionID() == actionID {
			return i.ID, nil
		}
	}
	return "", nil
}
