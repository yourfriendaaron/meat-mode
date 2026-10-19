package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// beadsName is the beads tracker's registry name. Hello still fills
// BdVersion from it for hubs that predate trackers, and the config's older
// bd key sets its tool.
const beadsName = "beads"

// buildTrackers makes every registered tracker, checks what each declares,
// and sorts them in the order Detect is tried. lookPath is what a tracker
// uses to find its tool when the config names none.
func buildTrackers(cfg Config, getenv func(string) string, lookPath func(string) (string, error)) ([]tracker.Tracker, error) {
	var out []tracker.Tracker
	for _, f := range tracker.Factories() {
		t, err := f.New(tracker.Env{Bin: cfg.trackerBin(f.Name), Getenv: getenv, LookPath: lookPath})
		if err != nil {
			return nil, fmt.Errorf("tracker %s: %w", f.Name, err)
		}
		if err := tracker.Validate(f.Name, t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, errors.New("this bridge was built without any task tracker")
	}
	tracker.Sort(out)
	return out, nil
}

// trackerCheck is one tracker's check: its version, or why it is not
// available.
type trackerCheck struct {
	Tracker tracker.Tracker
	Version string
	Err     error
}

// checkTrackers runs each tracker's check, in order.
func checkTrackers(ctx context.Context, ts []tracker.Tracker) []trackerCheck {
	out := make([]trackerCheck, 0, len(ts))
	for _, t := range ts {
		v, err := t.Check(ctx)
		out = append(out, trackerCheck{Tracker: t, Version: v, Err: err})
	}
	return out
}

// passed returns the trackers whose check passed, with their versions. It
// fails only when none did: with that tracker's own error when there is only
// one, such as bd's when beads is the only tracker.
func passed(checks []trackerCheck) ([]tracker.Tracker, []api.TrackerVersion, error) {
	var ok []tracker.Tracker
	var versions []api.TrackerVersion
	var failed []string
	for _, c := range checks {
		if c.Err != nil {
			failed = append(failed, c.Tracker.Info().Name+": "+c.Err.Error())
			continue
		}
		ok = append(ok, c.Tracker)
		info := c.Tracker.Info()
		versions = append(versions, api.TrackerVersion{Name: info.Name, Title: info.Title, Version: c.Version, Raise: info.Raise})
	}
	switch {
	case len(ok) > 0:
		return ok, versions, nil
	case len(checks) == 0:
		return nil, nil, errors.New("this bridge was built without any task tracker")
	case len(checks) == 1:
		return nil, nil, checks[0].Err
	}
	return nil, nil, errors.New("no task tracker is available (" + strings.Join(failed, "; ") + ")")
}

// beadsVersion is beads' version among versions, or "".
func beadsVersion(versions []api.TrackerVersion) string {
	for _, v := range versions {
		if v.Name == beadsName {
			return v.Version
		}
	}
	return ""
}

// cannot is the failed result for an action the repo's tracker does not
// support, worded as the hub refuses it.
func cannot(t tracker.Tracker, capability string) api.ActionResult {
	return api.ActionResult{Error: tracker.Refusal(t.Info().Title, capability)}
}

// agentOf is the tracker's agent words, or nil for a tracker that runs no
// agents.
func agentOf(t tracker.Tracker) tracker.Agent {
	a, _ := t.(tracker.Agent)
	return a
}
