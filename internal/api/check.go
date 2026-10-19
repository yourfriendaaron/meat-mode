package api

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// The checks in this file are the hub's rules for what a bridge, the app or
// an agent sends. The hub's store and the fake hub the bridge tests run
// against both call them, so the two cannot drift apart.

// ErrInvalid is matched by every error a check here returns: the request
// breaks the API's rules, which the hub answers with 400. The message is a
// plain sentence for the sender.
var ErrInvalid = errors.New("invalid request")

type invalidError struct{ msg string }

func (e *invalidError) Error() string { return e.msg }
func (e *invalidError) Unwrap() error { return ErrInvalid }

// Invalidf returns an error matching ErrInvalid with a plain message.
func Invalidf(format string, a ...any) error {
	return &invalidError{fmt.Sprintf(format, a...)}
}

// ProvisionalPrefix starts the id of an added item the bridge has not
// created yet: pending-<action id>. No bead may use it.
const ProvisionalPrefix = "pending-"

// Limits on what the app or an agent may post; longer text is refused.
const (
	MaxNoteLen  = 10000
	MaxTitleLen = 1000
)

// Limits on the text a bridge sends, so one huge bead cannot bloat the
// hub's database or the state the app downloads. Longer text is truncated,
// not refused: one long bead must not stop a repo syncing.
const (
	ItemTitleLimit       = 500
	ItemDescriptionLimit = 20000
	CommentLimit         = 20000
	CommentAuthorLimit   = 200
	RunIDLimit           = 200
	AgentNameLimit       = 200
	ActivityLimit        = 500
	DetailLimit          = 2000
)

// MaxRunCount bounds the counts a bridge may report: real ones stay far
// below it, and a huge one would only be noise.
const MaxRunCount = 1000000

// TruncRunes cuts s to at most limit runes (never mid-rune) and appends an
// ellipsis when it cut anything.
func TruncRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}

// ValidBeadID reports whether id can be the id of a bead a bridge reports:
// a valid segment outside the provisional prefix.
func ValidBeadID(id string) bool {
	return ValidSegment(id) && !strings.HasPrefix(id, ProvisionalPrefix)
}

// CheckTitle validates a title the app or an agent posts and returns it
// trimmed.
func CheckTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return title, Invalidf("title cannot be empty")
	}
	if utf8.RuneCountInString(title) > MaxTitleLen {
		return title, Invalidf("title is longer than %d characters", MaxTitleLen)
	}
	return title, nil
}

// CheckPriority validates a priority: 0 (highest) to 4, as bd.
func CheckPriority(p int) error {
	if p < 0 || p > 4 {
		return Invalidf("priority must be 0 to 4")
	}
	return nil
}

// CheckRepeatInterval validates Repeat.Interval: 1 to MaxRepeatInterval, or
// 0, which means 1.
func CheckRepeatInterval(n int) error {
	if n < 0 || n > MaxRepeatInterval {
		return Invalidf("repeat.interval must be 1 to %d", MaxRepeatInterval)
	}
	return nil
}

// CheckRepos validates the repo list of PUT /bridge/v1/repos and returns it
// without the hub-only fields, which a bridge never sets.
func CheckRepos(repos []Repo) ([]Repo, error) {
	out := make([]Repo, 0, len(repos))
	seen := map[string]bool{}
	for _, r := range repos {
		if !ValidRepoName(r.Name) {
			return nil, Invalidf("repo name %q is not valid", r.Name)
		}
		if seen[r.Name] {
			return nil, Invalidf("repo %q is listed twice", r.Name)
		}
		seen[r.Name] = true
		r.Bridge, r.Pending, r.AgentsPending = "", false, false
		out = append(out, r)
	}
	return out, nil
}

// CheckItems validates the item set of PUT /bridge/v1/items and cleans it
// in place: hub-only fields cleared, text truncated to the limits, and nil
// comments made empty. The repo it names is checked by ValidRepoName, and
// against the bridge's repo list by the hub.
func CheckItems(items []Item) error {
	seen := map[string]bool{}
	for i := range items {
		it := &items[i]
		if !ValidSegment(it.ID) {
			return Invalidf("item id %q is not valid", it.ID)
		}
		if strings.HasPrefix(it.ID, ProvisionalPrefix) {
			return Invalidf("item id %q uses the reserved prefix %q", it.ID, ProvisionalPrefix)
		}
		if seen[it.ID] {
			return Invalidf("item %q is listed twice", it.ID)
		}
		seen[it.ID] = true
		it.Key, it.Bridge, it.Repo = "", "", ""
		it.Pending, it.SeenAt, it.Error, it.FirstSeen, it.Run = false, nil, "", time.Time{}, nil
		it.Title = TruncRunes(it.Title, ItemTitleLimit)
		it.Description = TruncRunes(it.Description, ItemDescriptionLimit)
		for j := range it.Comments {
			it.Comments[j].Text = TruncRunes(it.Comments[j].Text, CommentLimit)
			it.Comments[j].Author = TruncRunes(it.Comments[j].Author, CommentAuthorLimit)
		}
		if it.Comments == nil {
			it.Comments = []Comment{}
		}
	}
	return nil
}

// bridgeReasons are the reasons a bridge may report; offline is the hub's.
var bridgeReasons = map[string]bool{
	ReasonStopped: true, ReasonUsageLimit: true, ReasonTimeout: true,
	ReasonBudget: true, ReasonRunLimit: true, ReasonBusy: true,
	ReasonNotAllowed: true, ReasonNoReply: true, ReasonFailed: true,
	ReasonCrashed: true, ReasonRestarted: true,
}

// CheckRuns validates the run list of PUT /bridge/v1/runs and returns it
// cleaned: text truncated to the limits, times in UTC, and no reason on a
// run that did not stop.
func CheckRuns(reports []RunReport) ([]RunReport, error) {
	type target struct{ repo, bead string }
	seen := map[target]bool{}
	out := make([]RunReport, 0, len(reports))
	for _, r := range reports {
		run, err := checkRun(r)
		if err != nil {
			return nil, err
		}
		if r.Applying != "" && !ValidSegment(r.Applying) {
			return nil, Invalidf("the run on %q has a bad applying id", r.BeadID)
		}
		t := target{r.Repo, r.BeadID}
		if seen[t] {
			return nil, Invalidf("bead %q in repo %q has two runs", r.BeadID, r.Repo)
		}
		seen[t] = true
		r.Run = run
		out = append(out, r)
	}
	return out, nil
}

// checkRun validates one reported run and returns it cleaned.
func checkRun(r RunReport) (Run, error) {
	run := r.Run
	if !ValidRepoName(r.Repo) {
		return run, Invalidf("run repo %q is not valid", r.Repo)
	}
	if !ValidBeadID(r.BeadID) {
		return run, Invalidf("run beadId %q is not valid", r.BeadID)
	}
	if strings.TrimSpace(run.ID) == "" {
		return run, Invalidf("the run on %q needs an id", r.BeadID)
	}
	switch run.State {
	case RunWorking, RunWaiting, RunDone:
		run.Reason = ""
	case RunStopped:
		if !bridgeReasons[run.Reason] {
			return run, Invalidf("the run on %q has unknown reason %q", r.BeadID, run.Reason)
		}
	default:
		return run, Invalidf("the run on %q has unknown state %q", r.BeadID, run.State)
	}
	if run.StartedAt.IsZero() {
		return run, Invalidf("the run on %q needs startedAt", r.BeadID)
	}
	// Only a working run is open; every settled one says when it ended, so
	// the hub can tell a fresh end from an old one.
	if run.State == RunWorking && run.EndedAt != nil {
		return run, Invalidf("the working run on %q has endedAt", r.BeadID)
	}
	if run.State != RunWorking && run.EndedAt == nil {
		return run, Invalidf("the settled run on %q needs endedAt", r.BeadID)
	}
	if run.EndedAt != nil && run.EndedAt.Before(run.StartedAt) {
		return run, Invalidf("the run on %q ended before it started", r.BeadID)
	}
	if run.Count < 0 || run.Queued < 0 {
		return run, Invalidf("the run on %q has a negative count", r.BeadID)
	}
	if run.Count > MaxRunCount || run.Queued > MaxRunCount {
		return run, Invalidf("the run on %q has a count too large to be real", r.BeadID)
	}
	run.ID = TruncRunes(run.ID, RunIDLimit)
	run.Agent = TruncRunes(run.Agent, AgentNameLimit)
	run.Activity = TruncRunes(run.Activity, ActivityLimit)
	run.Detail = TruncRunes(run.Detail, DetailLimit)
	run.StartedAt = run.StartedAt.UTC()
	if run.EndedAt != nil {
		t := run.EndedAt.UTC()
		run.EndedAt = &t
	}
	return run, nil
}
