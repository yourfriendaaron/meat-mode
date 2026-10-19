package bridge

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/beads"
)

// actionIDPattern is the UUID form the hub gives every action id.
var actionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// defaultActor is used when an action carries no author.
const defaultActor = "meat-mode-app"

// maxErrorLen caps a failed action's error text (bd output can be long), so
// a result always fits in one replay journal line. The hub keeps the same
// length.
const maxErrorLen = 2000

// replay applies the hub's pending actions in order and reports each result.
// It returns the names of the repos it changed, and stops at the first hub
// error.
func (b *Bridge) replay(ctx context.Context) (map[string]bool, error) {
	actions, err := b.Hub.Actions(ctx)
	if err != nil {
		return nil, err
	}
	touched := map[string]bool{}
	listed := map[string]map[string]bool{} // repo -> ids listed during this replay
	for _, a := range actions {
		if a.Bridge != b.name {
			// The hub should never send these; applying one would write to the
			// wrong machine's repo of the same name.
			b.log().Warn("ignoring an action for another bridge", "action", a.ID, "bridge", a.Bridge)
			continue
		}
		st := b.repos[a.Repo]
		if b.Runner != nil {
			b.Runner.NoteAuthor(a.Author)
		}
		var res api.ActionResult
		if !actionIDPattern.MatchString(a.ID) {
			// The journal keys on the id; the hub only ever mints UUIDs.
			b.log().Warn("ignoring an action without a valid id", "kind", a.Kind, "repo", a.Repo)
			continue
		} else if a.Kind == api.KindSync || a.Kind == api.KindAgents {
			if res, err = b.applySwitch(ctx, a, st); err != nil {
				return touched, err
			}
			if res.OK {
				touched[a.Repo] = true
			}
		} else if a.Kind == api.KindStop {
			res = b.stop(a)
		} else if a.Kind == api.KindAsk && b.Runner == nil {
			res = api.ActionResult{Error: "this bridge does not run agents"}
		} else if st == nil || !st.Synced() {
			res = api.ActionResult{Error: ErrNotSynced}
		} else if ok, lerr := b.known(ctx, st, a, listed); lerr != nil || !ok {
			res = api.ActionResult{Error: ErrUnknownBead}
			if lerr != nil {
				res = fail(lerr)
			}
		} else {
			res, err = b.apply(ctx, a, st.Repo)
			if err != nil {
				return touched, err
			}
			touched[a.Repo] = true
			if ids := listed[a.Repo]; ids != nil && res.BeadID != "" {
				ids[res.BeadID] = true
			}
			if res.OK {
				b.ask(ctx, a, st, res)
			}
		}
		// The hub drops its own view of an ask or stop once the action is
		// applied, so it must already hold the run as it now stands.
		if res.OK && changesRun(a) && b.Runner != nil {
			bead := a.BeadID
			if a.Kind == api.KindAdd {
				bead = res.BeadID
			}
			if err := b.Runner.ReportApplying(ctx, b.putRuns, a.ID, a.Repo, bead); err != nil {
				return touched, err
			}
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

// changesRun reports whether applying a starts, follows up or stops a run.
func changesRun(a api.Action) bool {
	return a.Kind == api.KindAsk || a.Kind == api.KindStop || (a.Kind == api.KindAdd && a.Payload.Ask)
}

// AgentsOnError is the result for an agents action with on: true. Agents are
// switched on at this machine only.
func AgentsOnError(repo string) string {
	return "switch agents on at this machine: meat-mode bridge agents on " + repo
}

// ErrUnknownRepo is the result for a sync switch on a repo the bridge did
// not find.
const ErrUnknownRepo = "unknown repo"

// applySwitch saves a sync or agents switch from the app and reports the new
// repo list at once. It takes switches only for repos this bridge found
// itself, looked up by name, so the app cannot point it anywhere else.
func (b *Bridge) applySwitch(ctx context.Context, a api.Action, st *repoState) (api.ActionResult, error) {
	kind, save, switches := "sync", b.SaveSwitch, &b.Switches
	if a.Kind == api.KindAgents {
		kind, save, switches = "agents", b.SaveAgents, &b.Agents
	}
	switch {
	case st == nil:
		return api.ActionResult{Error: ErrUnknownRepo}, nil
	case a.Payload.On == nil:
		return api.ActionResult{Error: kind + " switch has no position"}, nil
	case a.Kind == api.KindAgents && *a.Payload.On:
		return api.ActionResult{Error: AgentsOnError(a.Repo)}, nil
	case save == nil:
		return api.ActionResult{Error: "this bridge cannot save " + kind + " switches"}, nil
	}
	on := *a.Payload.On
	if err := save(a.Repo, on); err != nil {
		return fail(fmt.Errorf("save the %s switch: %w", kind, err)), nil
	}
	if *switches == nil {
		*switches = map[string]bool{}
	}
	(*switches)[a.Repo] = on
	// Report the repo list before the result, so the app never shows the old
	// position between the two.
	if err := b.discover(ctx, false); err != nil {
		return api.ActionResult{}, err
	}
	return api.ActionResult{OK: true}, nil
}

// ErrUnknownBead is the result for an action on a bead this bridge never
// listed in that repo.
const ErrUnknownBead = "unknown bead"

// known reports whether the action's bead is one this bridge lists for the
// repo. Only listed beads are ever sent to the hub, so anything else did not
// come from the app. An id missing from the last sync (after a restart, or
// created since) is checked against a fresh list, once per replay.
func (b *Bridge) known(ctx context.Context, st *repoState, a api.Action, listed map[string]map[string]bool) (bool, error) {
	if a.Kind == api.KindAdd {
		return true, nil
	}
	if _, ok := st.fps[a.BeadID]; ok {
		return true, nil
	}
	ids, ok := listed[st.Name]
	if !ok {
		list, err := b.Beads.ListHuman(ctx, st.Dir)
		if err != nil {
			return false, err
		}
		ids = make(map[string]bool, len(list))
		for _, i := range list {
			ids[i.ID] = true
		}
		listed[st.Name] = ids
	}
	return ids[a.BeadID], nil
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
	case api.KindNote, api.KindAsk:
		text := a.Payload.Text
		if strings.TrimSpace(text) == "" {
			if a.Kind == api.KindAsk {
				return api.ActionResult{OK: true} // an ask without a note
			}
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

func fail(err error) api.ActionResult {
	msg := err.Error()
	if utf8.RuneCountInString(msg) > maxErrorLen {
		msg = string([]rune(msg)[:maxErrorLen]) + "…"
	}
	return api.ActionResult{Error: msg}
}

func newIssue(a api.Action, title, actor string) beads.NewIssue {
	return beads.NewIssue{Title: title, Due: a.Payload.Due, ActionID: a.ID, Actor: actor}
}

// stop applies a stop: it ends the item's live run and is applied once the
// run has ended. With no live run it changes nothing.
func (b *Bridge) stop(a api.Action) api.ActionResult {
	if a.BeadID == "" {
		return fail(errors.New("action has no bead id"))
	}
	if b.Runner != nil {
		b.Runner.Stop(a.Repo, a.BeadID)
	}
	return api.ActionResult{OK: true}
}

// ask hands an applied ask, or an add with ask, to the runner, once its note
// is on the bead. No other action ever starts a run.
func (b *Bridge) ask(ctx context.Context, a api.Action, st *repoState, res api.ActionResult) {
	if b.Runner == nil {
		return
	}
	actor := a.Author
	if actor == "" {
		actor = defaultActor
	}
	b.reloadAgents()
	req := askRequest{ActionID: a.ID, Repo: st.Repo, Author: actor, Allowed: b.Agents[a.Repo]}
	switch {
	case a.Kind == api.KindAsk:
		req.BeadID = a.BeadID
		if strings.TrimSpace(a.Payload.Text) != "" {
			req.Text = a.Payload.Text
		}
		if it, ok := st.items[a.BeadID]; ok {
			req.Creator = it.CreatedBy
		} else if i, err := b.Beads.Show(ctx, st.Dir, a.BeadID); err == nil {
			req.Creator = i.CreatedBy
		}
	case a.Kind == api.KindAdd && a.Payload.Ask && res.BeadID != "":
		req.BeadID, req.Creator = res.BeadID, actor
	default:
		return
	}
	b.Runner.Ask(req)
}
