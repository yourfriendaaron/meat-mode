// Package tracker is the bridge's view of an agent task tracker: beads,
// Backlog.md, ticket. Each tracker lives in its own package, implements
// Tracker and registers a Factory in its init function; the bridge, hub and
// app know trackers only through what Info says. docs/trackers.md is the
// contract.
package tracker

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// HumanLabel is the label or tag that marks a task for the person, in every
// tracker that has labels.
const HumanLabel = api.HumanLabel

// ClosedWindow is how long a closed task stays in the list a tracker
// returns.
const ClosedWindow = 7 * 24 * time.Hour

// Statuses a tracker maps its own onto. They are api.Item's.
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusBlocked    = "blocked"
	StatusDeferred   = "deferred"
	StatusClosed     = "closed"
)

// CapSubtask is the capability to add a task under a parent (an add whose
// payload has a parent). The other capabilities are action kinds.
const CapSubtask = "subtask"

// Capabilities are the action kinds and CapSubtask a tracker may list in
// Info.Actions.
var Capabilities = []string{
	api.KindDone, api.KindReopen, api.KindSnooze, api.KindDismiss,
	api.KindNote, api.KindAdd, CapSubtask, api.KindAsk,
}

// ErrNotFound is returned when a task id does not exist.
var ErrNotFound = errors.New("task not found")

// ErrOpenChildren is returned when a close is refused because the task has
// an open sub-task.
var ErrOpenChildren = errors.New("it has open sub-tasks; finish or remove them first")

// ErrUnsupported is returned by a method for an action the tracker does not
// list in Info.Actions.
var ErrUnsupported = errors.New("this tracker cannot do that")

// Tracker is one kind of task store in a repo. Every method that takes dir
// takes the repo directory, the one Detect matched. actor and author name
// who made a change. A method for a capability the tracker does not list in
// Info.Actions returns ErrUnsupported.
type Tracker interface {
	// Info describes the tracker. It never changes.
	Info() Info
	// Check returns the version of the tool the tracker drives, or an error
	// when it is missing or too old. A tracker that drives no tool returns
	// "" and nil.
	Check(ctx context.Context) (string, error)
	// Detect reports whether dir holds this tracker's store. entries are
	// dir's own entries, already read; Detect may stat further but must stay
	// cheap, as the bridge calls it for every folder it walks.
	Detect(dir string, entries []fs.DirEntry) bool
	// OptedIn reports whether the repo's own files switch syncing on.
	// Every tracker honours RepoOptIn; it may add its own place.
	OptedIn(dir string) bool
	// List returns the tasks for the person in a repo: open, in-progress,
	// blocked and deferred ones, and closed ones from the last
	// ClosedWindow. Comments are not filled; Version is.
	List(ctx context.Context, dir string) ([]Issue, error)
	// Show returns one task with its comments, or ErrNotFound.
	Show(ctx context.Context, dir, id string) (Issue, error)
	Close(ctx context.Context, dir, id, reason, actor string) error
	Reopen(ctx context.Context, dir, id, actor string) error
	// Defer hides the task until a time, or clears that when until is nil.
	Defer(ctx context.Context, dir, id string, until *time.Time, actor string) error
	// Dismiss closes the task as not needed.
	Dismiss(ctx context.Context, dir, id, actor string) error
	AddComment(ctx context.Context, dir, id, author, text string) error
	// Create makes a task for the person and returns its id.
	Create(ctx context.Context, dir string, in NewIssue) (string, error)
	// FindByActionID returns the id of the task Create made for that action,
	// or "" when there is none.
	FindByActionID(ctx context.Context, dir, actionID string) (string, error)
}

// Agent is what a tracker that lists api.KindAsk also implements: the words
// an agent run gets about the task.
type Agent interface {
	// Prompt is the first message of a run on task id: where to read the
	// task and its notes, how to close it and how to reply with a note.
	Prompt(id string) string
	// FollowUp is the message for a note the person added to a live run.
	FollowUp(id, text string) string
	// Env is the variables the agent gets on top of the bridge's own, such
	// as BEADS_ACTOR=<actor>. The bridge drops any inherited variable of
	// the same name.
	Env(actor string) []string
	// Describe names one of the tracker's own commands in a few plain
	// words, such as "Closing the task", for the activity line; "" when
	// words is not its command. words is the command split on spaces.
	Describe(words []string) string
}

// Info is what the bridge, hub and app know about a tracker.
type Info struct {
	// Name is the registry key and the name in settings: lower case, such
	// as "beads".
	Name string
	// Title is the name the app shows, such as "Backlog.md".
	Title string
	// Rank orders trackers when more than one detects the same folder; the
	// lowest wins and the others are skipped with a log line.
	Rank int
	// Prune are folder names the bridge never walks into, such as the
	// tracker's own store folder.
	Prune []string
	// Actions are the capabilities the tracker supports, from Capabilities.
	Actions []string
	// Raise is the shell command an agent runs to raise a task for the
	// person, with the example title "Approve the footer text" and the
	// description "What is needed, why, and where to find it.". The app
	// puts it in the instruction text for agents on the machine.
	Raise string
}

// Can reports whether the tracker supports a capability.
func (i Info) Can(capability string) bool { return slices.Contains(i.Actions, capability) }

// NewIssue is what Create needs.
type NewIssue struct {
	Title    string
	Due      *time.Time
	Parent   string // the parent task's id; empty for none
	ActionID string // the Meat Mode action that creates it, for FindByActionID
	Actor    string
}

// Issue is one task as a tracker gives it.
type Issue struct {
	ID          string
	Title       string
	Description string
	Status      string // one of the Status constants
	CloseReason string
	Priority    int // 0 (highest) to 4, as beads; trackers map theirs
	Labels      []string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DueAt       *time.Time
	DeferUntil  *time.Time
	ClosedAt    *time.Time
	Parent      string // the parent task's id; empty for a top-level task
	ParentTitle string // the parent's title when the tracker gives it
	Comments    []Comment
	// Version changes whenever anything the hub shows of the task changes,
	// a new comment included. List fills it; the bridge compares it to
	// decide what to read again with Show.
	Version string
}

// Comment is one note on a task.
type Comment struct {
	ID        string
	Author    string
	Text      string
	CreatedAt time.Time
}

// Item converts a task to the wire type a bridge sends (no key, bridge,
// repo or hub-only fields).
func (i Issue) Item() api.Item {
	it := api.Item{
		ID:          i.ID,
		Title:       i.Title,
		Description: i.Description,
		Status:      i.Status,
		CloseReason: i.CloseReason,
		Priority:    i.Priority,
		Labels:      i.Labels,
		CreatedBy:   i.CreatedBy,
		DueAt:       utc(i.DueAt),
		DeferUntil:  utc(i.DeferUntil),
		CreatedAt:   i.CreatedAt.UTC(),
		UpdatedAt:   i.UpdatedAt.UTC(),
		ClosedAt:    utc(i.ClosedAt),
		Parent:      i.Parent,
		ParentTitle: i.ParentTitle,
		Comments:    make([]api.Comment, 0, len(i.Comments)),
	}
	for _, c := range i.Comments {
		it.Comments = append(it.Comments, api.Comment{ID: c.ID, Author: c.Author, Text: c.Text, CreatedAt: c.CreatedAt.UTC()})
	}
	return it
}

// InWindow reports whether a task belongs in List's result at now: not
// closed, or closed within ClosedWindow.
func InWindow(i Issue, now time.Time) bool {
	if i.Status != StatusClosed {
		return true
	}
	return i.ClosedAt != nil && now.Sub(*i.ClosedAt) <= ClosedWindow
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
