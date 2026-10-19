package backlog

import (
	"context"
	"regexp"
	"slices"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Close implements tracker.Tracker by moving the task to the last status.
// Backlog.md has no close reason, so reason is not written. A parent closes
// with its sub-tasks open, as it does in Backlog.md.
func (b *Tracker) Close(ctx context.Context, dir, id, reason, actor string) error {
	return b.setStatus(ctx, dir, id, true)
}

// Dismiss implements tracker.Tracker: closed, with the label dismissed.
func (b *Tracker) Dismiss(ctx context.Context, dir, id, actor string) error {
	return b.setStatus(ctx, dir, id, true, "--add-label="+dismissedLabel)
}

// Reopen implements tracker.Tracker by moving the task to the project's
// default status and taking off the dismissed label.
func (b *Tracker) Reopen(ctx context.Context, dir, id, actor string) error {
	return b.setStatus(ctx, dir, id, false, "--remove-label="+dismissedLabel)
}

// Defer implements tracker.Tracker. Backlog.md has no defer field.
func (b *Tracker) Defer(ctx context.Context, dir, id string, until *time.Time, actor string) error {
	return tracker.ErrUnsupported
}

func (b *Tracker) setStatus(ctx context.Context, dir, id string, closed bool, extra ...string) error {
	if err := checkID(id); err != nil {
		return err
	}
	statuses, err := b.statuses(ctx, dir)
	if err != nil {
		return err
	}
	status := statuses[len(statuses)-1]
	if !closed {
		if status, err = b.openStatus(ctx, dir, statuses); err != nil {
			return err
		}
	}
	args := append([]string{"task", "edit", "--status=" + status}, extra...)
	_, err = b.run(ctx, dir, append(args, "--", id)...)
	return err
}

// openStatus is the status a reopened task takes: the project's default
// status for new tasks, or its first status when that is unset or closed.
func (b *Tracker) openStatus(ctx context.Context, dir string, statuses []string) (string, error) {
	out, err := b.run(ctx, dir, "config", "get", "defaultStatus")
	if err != nil {
		return "", err
	}
	def := firstLine(string(out))
	i := slices.IndexFunc(statuses, func(s string) bool { return statusKey(s) == statusKey(def) })
	if i >= 0 && i < len(statuses)-1 {
		return statuses[i], nil
	}
	return statuses[0], nil
}

// ruleLine is a line of only ---, which Backlog.md reserves to separate
// comments and refuses in one.
var ruleLine = regexp.MustCompile(`(?m)^([ \t]*)---[ \t]*$`)

// AddComment implements tracker.Tracker with a comment that records author.
// A line of only --- becomes - - -, the same rule in Markdown, since
// Backlog.md refuses the first.
func (b *Tracker) AddComment(ctx context.Context, dir, id, author, text string) error {
	if err := checkID(id); err != nil {
		return err
	}
	args := []string{"task", "edit", "--comment=" + ruleLine.ReplaceAllString(text, "$1- - -")}
	if author != "" {
		args = append(args, "--comment-author="+author)
	}
	_, err := b.run(ctx, dir, append(args, "--", id)...)
	return err
}
