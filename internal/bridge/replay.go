package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/beads"
)

// defaultActor is used when an action carries no author.
const defaultActor = "meat-mode-app"

// replay applies the hub's pending actions in order and reports each result.
// It returns the names of the repos it changed, and stops at the first hub
// error.
func (b *Bridge) replay(ctx context.Context) (map[string]bool, error) {
	actions, err := b.Hub.Actions(ctx)
	if err != nil {
		return nil, err
	}
	touched := map[string]bool{}
	for _, a := range actions {
		if a.Bridge != b.name {
			// The hub should never send these; applying one would write to the
			// wrong machine's repo of the same name.
			b.log().Warn("ignoring an action for another bridge", "action", a.ID, "bridge", a.Bridge)
			continue
		}
		st := b.repos[a.Repo]
		var res api.ActionResult
		if st == nil || !st.Synced() {
			res = api.ActionResult{Error: ErrNotSynced}
		} else {
			res, err = b.apply(ctx, a, st.Repo)
			if err != nil {
				return touched, err
			}
			touched[a.Repo] = true
		}
		if err := b.Hub.Result(ctx, a.ID, res); err != nil {
			return touched, err
		}
		if res.OK {
			b.log().Info("applied action", "action", a.ID, "kind", a.Kind, "repo", a.Repo)
		} else {
			b.log().Warn("action failed", "action", a.ID, "kind", a.Kind, "repo", a.Repo, "err", res.Error)
		}
	}
	return touched, nil
}

// apply runs one action through the replay journal. An error means the
// journal could not be written (or a simulated crash); bd failures become a
// failed result.
func (b *Bridge) apply(ctx context.Context, a api.Action, r Repo) (api.ActionResult, error) {
	e, seen := b.Journal.Get(a.ID)
	if seen && e.Done() {
		return e.Result, nil // bd ran; only the hub report was lost
	}
	resumed := seen
	intent := e.Intent
	if !seen {
		intent = b.now()
		if err := b.Journal.Intent(a, intent); err != nil {
			return api.ActionResult{}, fmt.Errorf("write replay journal: %w", err)
		}
	}
	res := b.runBD(ctx, a, r, resumed, intent)
	if b.afterBD != nil {
		if err := b.afterBD(a); err != nil {
			return api.ActionResult{}, err
		}
	}
	if err := b.Journal.Done(a.ID, res, b.now()); err != nil {
		return api.ActionResult{}, fmt.Errorf("write replay journal: %w", err)
	}
	return res, nil
}

// runBD runs the bd command for an action, after the replay check from
// docs/decisions/0001-stack.md. resumed is set when an earlier run wrote the
// intent but not the outcome.
func (b *Bridge) runBD(ctx context.Context, a api.Action, r Repo, resumed bool, intent time.Time) api.ActionResult {
	actor := a.Author
	if actor == "" {
		actor = defaultActor
	}
	if a.Kind != api.KindAdd && a.BeadID == "" {
		return fail(errors.New("action has no bead id"))
	}
	var err error
	switch a.Kind {
	case api.KindDone:
		if closed, e := b.isClosed(ctx, r.Dir, a.BeadID); e != nil || !closed {
			err = firstErr(e, func() error { return b.Beads.Close(ctx, r.Dir, a.BeadID, DoneReason, actor) })
		}
	case api.KindReopen:
		if closed, e := b.isClosed(ctx, r.Dir, a.BeadID); e != nil || closed {
			err = firstErr(e, func() error { return b.Beads.Reopen(ctx, r.Dir, a.BeadID, actor) })
		}
	case api.KindDismiss:
		if closed, e := b.isClosed(ctx, r.Dir, a.BeadID); e != nil || !closed {
			err = firstErr(e, func() error { return b.Beads.Dismiss(ctx, r.Dir, a.BeadID, actor) })
		}
	case api.KindSnooze:
		err = b.Beads.Defer(ctx, r.Dir, a.BeadID, a.Payload.Until, actor)
	case api.KindNote:
		text := a.Payload.Text
		if strings.TrimSpace(text) == "" {
			return fail(errors.New("note is empty"))
		}
		if resumed {
			found, e := b.hasComment(ctx, r.Dir, a.BeadID, actor, text, intent)
			if e != nil {
				return fail(e)
			}
			if found {
				return api.ActionResult{OK: true}
			}
		}
		err = b.Beads.AddComment(ctx, r.Dir, a.BeadID, actor, text)
	case api.KindAdd:
		return b.add(ctx, a, r, actor, resumed)
	default:
		return fail(fmt.Errorf("unknown action kind %q", a.Kind))
	}
	if err != nil {
		return fail(err)
	}
	return api.ActionResult{OK: true}
}

func (b *Bridge) add(ctx context.Context, a api.Action, r Repo, actor string, resumed bool) api.ActionResult {
	title := strings.TrimSpace(a.Payload.Title)
	if title == "" {
		return fail(errors.New("title is empty"))
	}
	if resumed {
		id, err := b.Beads.FindByActionID(ctx, r.Dir, a.ID)
		if err != nil {
			return fail(err)
		}
		if id != "" {
			return api.ActionResult{OK: true, BeadID: id}
		}
	}
	id, err := b.Beads.Create(ctx, r.Dir, newIssue(a, title, actor))
	if err != nil {
		return fail(err)
	}
	return api.ActionResult{OK: true, BeadID: id}
}

func (b *Bridge) isClosed(ctx context.Context, dir, id string) (bool, error) {
	i, err := b.Beads.Show(ctx, dir, id)
	if err != nil {
		return false, err
	}
	return i.Status == "closed", nil
}

// hasComment reports whether the bead has a comment by author with this text
// made at or after the intent. bd stores comment times to the second.
func (b *Bridge) hasComment(ctx context.Context, dir, id, author, text string, intent time.Time) (bool, error) {
	i, err := b.Beads.Show(ctx, dir, id)
	if err != nil {
		return false, err
	}
	since := intent.Truncate(time.Second)
	for _, c := range i.Comments {
		if c.Author == author && c.Text == text && !c.CreatedAt.Before(since) {
			return true, nil
		}
	}
	return false, nil
}

func firstErr(e error, then func() error) error {
	if e != nil {
		return e
	}
	return then()
}

func fail(err error) api.ActionResult { return api.ActionResult{Error: err.Error()} }

func newIssue(a api.Action, title, actor string) beads.NewIssue {
	return beads.NewIssue{Title: title, Due: a.Payload.Due, ActionID: a.ID, Actor: actor}
}
