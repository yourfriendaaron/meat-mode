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
)

// CLI implements Beads by running `bd -C <dir> ... --json`. Every call puts
// its flags first, then `--`, then bead ids and text, because bd reads flags
// anywhere on the line.
type CLI struct {
	Bin     string        // bd executable; "bd" when empty
	Timeout time.Duration // per call; one minute when zero
	Now     func() time.Time
}

var _ Beads = (*CLI)(nil)

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
	cmd.Env = append(os.Environ(), "BD_NON_INTERACTIVE=1", "NO_COLOR=1")
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
		return nil, fmt.Errorf("%w: %w", ErrNotFound, e)
	}
	return nil, e
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

// Version implements Beads.
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

// ListHuman implements Beads.
func (c *CLI) ListHuman(ctx context.Context, dir string) ([]Issue, error) {
	out, err := c.run(ctx, dir, "list", "--all", "-n", "0", "--json", "-l", HumanLabel)
	if err != nil {
		return nil, err
	}
	var all []Issue
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

// Show implements Beads.
func (c *CLI) Show(ctx context.Context, dir, id string) (Issue, error) {
	if err := CheckID(id); err != nil {
		return Issue{}, err
	}
	out, err := c.run(ctx, dir, "show", "--include-comments", "--json", "--", id)
	if err != nil {
		return Issue{}, err
	}
	var list []Issue
	if err := decode(out, &list); err != nil {
		return Issue{}, err
	}
	if len(list) == 0 {
		return Issue{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return list[0], nil
}

// Close implements Beads.
func (c *CLI) Close(ctx context.Context, dir, id, reason, actor string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := c.run(ctx, dir, "close", "--reason="+reason, "--actor="+actor, "--json", "--", id)
	return err
}

// Reopen implements Beads.
func (c *CLI) Reopen(ctx context.Context, dir, id, actor string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := c.run(ctx, dir, "reopen", "--actor="+actor, "--json", "--", id)
	return err
}

// Defer implements Beads.
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

// Dismiss implements Beads. bd prints no JSON for it.
func (c *CLI) Dismiss(ctx context.Context, dir, id, actor string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := c.run(ctx, dir, "human", "dismiss", "--actor="+actor, "--", id)
	return err
}

// AddComment implements Beads.
func (c *CLI) AddComment(ctx context.Context, dir, id, author, text string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := c.run(ctx, dir, "comments", "add", "--author="+author, "--actor="+author, "--json", "--", id, text)
	return err
}

// Create implements Beads. Every value rides in a --flag=value argument, so
// none can be read as a flag of its own.
func (c *CLI) Create(ctx context.Context, dir string, in NewIssue) (string, error) {
	meta, _ := json.Marshal(map[string]string{ActionMetaKey: in.ActionID})
	args := []string{"create", "--title=" + in.Title, "-l", HumanLabel, "--metadata=" + string(meta), "--json"}
	if in.Actor != "" {
		args = append(args, "--actor="+in.Actor)
	}
	if in.Due != nil {
		args = append(args, "--due="+in.Due.UTC().Format(time.RFC3339))
	}
	out, err := c.run(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	var created Issue
	if err := decode(out, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", errors.New("bd create printed no id")
	}
	return created.ID, nil
}

// FindByActionID implements Beads.
func (c *CLI) FindByActionID(ctx context.Context, dir, actionID string) (string, error) {
	out, err := c.run(ctx, dir, "list", "--all", "-n", "0", "--metadata-field="+ActionMetaKey+"="+actionID, "--json")
	if err != nil {
		return "", err
	}
	var list []Issue
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
