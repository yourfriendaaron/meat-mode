// Package api holds the wire types shared by the hub, the bridge and the app.
// docs/api.md describes the routes that carry them.
package api

import "time"

// Item is the hub's copy of one bead.
type Item struct {
	Key         string     `json:"key,omitempty"` // <bridge>/<repo>/<bead id>
	Bridge      string     `json:"bridge,omitempty"`
	Repo        string     `json:"repo,omitempty"`
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"` // open, in_progress, blocked, deferred, closed
	CloseReason string     `json:"closeReason,omitempty"`
	Priority    int        `json:"priority"`
	Labels      []string   `json:"labels,omitempty"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	DueAt       *time.Time `json:"dueAt,omitempty"`
	DeferUntil  *time.Time `json:"deferUntil,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
	Comments    []Comment  `json:"comments"`
	// Parent is the id of the item this one is a sub-task of, in the same
	// list; empty for a top-level item. ParentTitle is the parent's title
	// as the list's tracker last gave it, so a sub-task whose parent is not
	// synced (a tracker item not meant for the user) can still name it.
	Parent      string  `json:"parent,omitempty"`
	ParentTitle string  `json:"parentTitle,omitempty"`
	Checklist   []Check `json:"checklist,omitempty"` // personal items only
	Repeat      *Repeat `json:"repeat,omitempty"`    // personal items only
	// RemindBefore is how many minutes before the due time an early
	// reminder comes; nil uses the hub's ReminderLead. Personal items only.
	RemindBefore *int `json:"remindBefore,omitempty"`
	// Run is the latest agent run on this item; nil when no agent was ever
	// asked. Hub-only: the hub joins it from the bridge's run list.
	Run *Run `json:"run,omitempty"`
	// Hub-only fields, never sent by a bridge.
	Pending   bool       `json:"pending,omitempty"`   // an unapplied action changed this copy
	SeenAt    *time.Time `json:"seenAt,omitempty"`    // when the user last opened it
	FirstSeen time.Time  `json:"firstSeen,omitempty"` // when the hub first received it
	Error     string     `json:"error,omitempty"`     // last failed action on it
}

// Comment is one bead comment.
type Comment struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

// Check is one entry of a personal item's checklist.
type Check struct {
	ID   string `json:"id"` // given by the hub when an edit leaves it empty
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// Edit is the change an edit action makes to a personal item. A nil field
// stays as it is. Checklist replaces the whole list; an empty list clears
// it.
type Edit struct {
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	Due         *time.Time `json:"due,omitempty"`
	ClearDue    bool       `json:"clearDue,omitempty"`
	Priority    *int       `json:"priority,omitempty"` // 0 (highest) to 4, as bd
	Checklist   *[]Check   `json:"checklist,omitempty"`
	Repeat      *Repeat    `json:"repeat,omitempty"`
	ClearRepeat bool       `json:"clearRepeat,omitempty"`
	// RemindBefore is minutes, 0 to MaxLeadMinutes; ClearRemindBefore goes
	// back to the hub's ReminderLead.
	RemindBefore      *int `json:"remindBefore,omitempty"`
	ClearRemindBefore bool `json:"clearRemindBefore,omitempty"`
	// Parent moves the item under another item in its list; "" makes it
	// top-level. The sub-task rules apply (see internal/store/subtasks.go).
	Parent *string `json:"parent,omitempty"`
	// SubtasksDone marks these sub-tasks of the item done. The hub fills it
	// into the Was of a repeating done that reopened them, so its Undo
	// closes them again.
	SubtasksDone []string `json:"subtasksDone,omitempty"`
}

// Repeat units.
const (
	EveryDay   = "day"
	EveryWeek  = "week"
	EveryMonth = "month"
	EveryYear  = "year"
)

// MaxRepeatInterval caps Repeat.Interval.
const MaxRepeatInterval = 100

// MaxLeadMinutes caps a reminder lead time: one week.
const MaxLeadMinutes = 7 * 24 * 60

// Repeat makes a personal item come back after it is done. The item's due
// time is the current occurrence; the rule needs one.
type Repeat struct {
	Every    string `json:"every"`              // EveryDay, EveryWeek, EveryMonth or EveryYear
	Interval int    `json:"interval,omitempty"` // every n units, 1 to MaxRepeatInterval; 0 means 1
	// Weekdays picks the days of a weekly rule, 0 (Sunday) to 6. Empty
	// means the due date's weekday.
	Weekdays []int `json:"weekdays,omitempty"`
	// FromDone counts the next date from the day the item is done instead
	// of from the schedule.
	FromDone bool `json:"fromDone,omitempty"`
	// TimeZone is the IANA zone whose wall-clock time and date the rule
	// keeps, so 8:30pm stays 8:30pm across daylight saving changes.
	TimeZone string `json:"timeZone"`
	// Until is the last date the series may land on, "YYYY-MM-DD" in
	// TimeZone, inclusive. Empty means it repeats for good.
	Until string `json:"until,omitempty"`
	// Start is the occurrence the schedule counts from. The hub sets it
	// from the due time whenever the rule or the due time is set; a client
	// sends none.
	Start *time.Time `json:"start,omitempty"`
}

// Repo is one beads database a bridge found.
type Repo struct {
	Bridge   string `json:"bridge,omitempty"`
	Name     string `json:"name"`
	Personal bool   `json:"personal"` // the hub's own personal list; from a bridge, only an older one's personal repo
	OptedIn  bool   `json:"optedIn"`  // synced: switched on in the app, or by the repo's config file
	// AgentsAllowed: an ask may wake an agent on this repo's items. Off by
	// default; switched in the app (the agents action kind).
	AgentsAllowed bool `json:"agentsAllowed"`
	// Hub-only: a sync switch the bridge has not applied yet. OptedIn then
	// shows the position it was switched to.
	Pending bool `json:"pending,omitempty"`
	// Hub-only: an agents switch the bridge has not applied yet.
	// AgentsAllowed then shows the position it was switched to.
	AgentsPending bool `json:"agentsPending,omitempty"`
	// Tracker is the name the app shows for the repo's task tracker, such
	// as "beads" or "Backlog.md"; empty for the hub's own lists and from
	// an older bridge.
	Tracker string `json:"tracker,omitempty"`
	// Actions are the action kinds the tracker can apply, plus "subtask"
	// for an add under a parent. Nil means every kind (an older bridge, or
	// the hub's own lists); the app hides the others and the hub refuses
	// them.
	Actions []string `json:"actions,omitempty"`
}

// TrackerVersion is one tracker a bridge runs and its tool's version.
type TrackerVersion struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"` // the name the app shows, such as "Backlog.md"
	Version string `json:"version,omitempty"`
	// Raise is the command an agent runs to raise a task for the person,
	// for the app's agent instruction text.
	Raise string `json:"raise,omitempty"`
}

// HarnessInfo is the agent harness a bridge runs, and the limits it
// enforces on each run (docs/harnesses.md).
type HarnessInfo struct {
	Name    string `json:"name"`              // the registry key, such as "codex"
	Title   string `json:"title,omitempty"`   // the name the app shows, such as "Codex"
	Version string `json:"version,omitempty"` // empty when its check failed
	// InstructionFile is the file in a repo the harness reads standing
	// instructions from, such as AGENTS.md.
	InstructionFile string `json:"instructionFile,omitempty"`
	// Budget is how the spending cap is enforced: native, metered or none.
	// With none, a run stops at the time limit only.
	Budget    string  `json:"budget,omitempty"`
	BudgetUSD float64 `json:"budgetUsd,omitempty"` // the cap for one run; 0 when Budget is none
	// TimeoutSeconds is the time limit for one run.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// Steer is true when a note reaches a live run at once; otherwise it
	// waits for the agent's turn to end.
	Steer bool `json:"steer,omitempty"`
}

// Harness budget modes, as in HarnessInfo.Budget.
const (
	BudgetNative  = "native"
	BudgetMetered = "metered"
	BudgetNone    = "none"
)

// Run states. An item with no Run has never had an agent asked (idle).
const (
	RunWorking = "working" // the agent is running
	RunWaiting = "waiting" // the run ended after the agent wrote a note: waiting on the user
	RunDone    = "done"    // the run ended with the bead closed
	RunStopped = "stopped" // the run ended without either; Reason says why
)

// Reasons a run stopped.
const (
	ReasonStopped    = "stopped"     // the user stopped it
	ReasonUsageLimit = "usage-limit" // the agent hit its provider's usage limit
	ReasonTimeout    = "timeout"     // it ran past the wall-clock limit
	ReasonBudget     = "budget"      // it spent its budget for one run
	ReasonRunLimit   = "run-limit"   // the bead used up its lifetime number of runs
	ReasonBusy       = "busy"        // too many runs were live on the machine to start one
	ReasonNotAllowed = "not-allowed" // the repo does not allow agents (on the machine's side)
	ReasonNoReply    = "no-reply"    // it ended without a note or closing the bead
	ReasonFailed     = "failed"      // the agent command failed to start or reported an error
	ReasonCrashed    = "crashed"     // the agent process died
	ReasonRestarted  = "restarted"   // the bridge stopped while it ran
	ReasonOffline    = "offline"     // hub-only: the bridge stopped reporting while it ran
)

// Run is one agent run on one bead, as its bridge reports it.
type Run struct {
	ID    string `json:"id"`    // the bridge's id for this run; a new ask that starts a run gets a new one
	State string `json:"state"` // RunWorking, RunWaiting, RunDone or RunStopped
	// Agent is the name the app shows for the agent ("planner"): the bead's
	// creator, else the bridge's configured agent name.
	Agent string `json:"agent"`
	// Activity is one line in plain words of what the agent is doing now,
	// such as "Editing notes.txt"; while settled, the last thing it did.
	Activity  string     `json:"activity,omitempty"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Reason    string     `json:"reason,omitempty"` // RunStopped only: one of the Reason constants
	Detail    string     `json:"detail,omitempty"` // a plain sentence about Reason, such as when a usage limit resets
	// Queued counts the follow-up notes written to this live run that the
	// agent has not reached yet: the newest Queued of the user's comments.
	Queued int `json:"queued,omitempty"`
	// Count is how many runs this bead has had in its lifetime, this one
	// included. No recovery path resets it.
	Count int `json:"count"`
}

// RunReport is one entry of PUT /bridge/v1/runs.
type RunReport struct {
	Repo   string `json:"repo"`
	BeadID string `json:"beadId"`
	Run
	// Applying is the id of the ask or stop the bridge has already applied
	// to this run but not yet reported the result of, so the hub does not
	// count it a second time.
	Applying string `json:"applying,omitempty"`
}

// Bridge is one paired agent machine.
type Bridge struct {
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"createdAt"`
	LastSeen  *time.Time `json:"lastSeen,omitempty"`
	Version   string     `json:"version,omitempty"`
	BdVersion string     `json:"bdVersion,omitempty"`
	// Trackers are the trackers the bridge last said it runs.
	Trackers []TrackerVersion `json:"trackers,omitempty"`
	// Harness is the agent harness the bridge last said it runs; nil for a
	// bridge that predates harnesses, which runs Claude Code.
	Harness *HarnessInfo `json:"harness,omitempty"`
}

// Action kinds.
const (
	KindDone    = "done"
	KindReopen  = "reopen"
	KindSnooze  = "snooze"
	KindDismiss = "dismiss"
	KindNote    = "note"
	KindAdd     = "add"
	KindSync    = "sync"   // switch a repo's syncing on or off
	KindAsk     = "ask"    // wake an agent on an item, with an optional note
	KindStop    = "stop"   // end an item's live run
	KindAgents  = "agents" // switch whether agents may work in a repo
	// Personal list only.
	KindEdit        = "edit"
	KindNoteEdit    = "note-edit"
	KindNoteDelete  = "note-delete"
	KindNoteRestore = "note-restore" // Undo of note-delete
)

// Action statuses.
const (
	StatusPending   = "pending"
	StatusApplied   = "applied"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Payload carries the fields an action kind needs; unused fields stay empty.
type Payload struct {
	Until    *time.Time `json:"until,omitempty"`    // snooze
	Previous *time.Time `json:"previous,omitempty"` // snooze: value to restore on undo
	Text     string     `json:"text,omitempty"`     // note
	Title    string     `json:"title,omitempty"`    // add
	Due      *time.Time `json:"due,omitempty"`      // add
	Repeat   *Repeat    `json:"repeat,omitempty"`   // add, personal list only; needs Due
	On       *bool      `json:"on,omitempty"`       // sync and agents
	Ask      bool       `json:"ask,omitempty"`      // add: an agent starts on the new item
	Parent   string     `json:"parent,omitempty"`   // add: the id of the item the new one is a sub-task of
	Set      *Edit      `json:"set,omitempty"`      // edit
	// Was is filled in by the hub. On edit it holds the fields Set changed,
	// as they were; on done of a repeating item, the due time and checklist
	// before it moved on, and the sub-tasks it reopened as SubtasksDone.
	Was *Edit `json:"was,omitempty"`
	// note-edit, note-delete and note-restore. note-edit takes Text. The
	// hub fills in Comment on note-edit and note-delete with the comment as
	// it was; note-restore takes a removed Comment back.
	CommentID string   `json:"commentId,omitempty"`
	Comment   *Comment `json:"comment,omitempty"`
}

// NewAction is what the app posts.
type NewAction struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Key     string  `json:"key,omitempty"`
	Bridge  string  `json:"bridge,omitempty"` // add, sync and agents only
	Repo    string  `json:"repo,omitempty"`   // add, sync and agents only
	Payload Payload `json:"payload"`
}

// Action is one entry in the append-only action log.
type Action struct {
	ID        string     `json:"id"`
	Seq       int64      `json:"seq"`
	Kind      string     `json:"kind"`
	Bridge    string     `json:"bridge"`
	Repo      string     `json:"repo"`
	BeadID    string     `json:"beadId,omitempty"` // empty for add until applied
	Payload   Payload    `json:"payload"`
	Author    string     `json:"author"` // <MEAT_MODE_USER>-app
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	AppliedAt *time.Time `json:"appliedAt,omitempty"`
}

// ActionResult is what a bridge reports after replaying an action.
type ActionResult struct {
	OK     bool   `json:"ok"`
	BeadID string `json:"beadId,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Hello is the bridge's greeting.
type Hello struct {
	Version   string `json:"version"`
	BdVersion string `json:"bdVersion"` // beads' version, kept for older hubs; empty when beads is not available
	// Trackers are the trackers whose tools passed their check.
	Trackers []TrackerVersion `json:"trackers,omitempty"`
	// Harness is the agent harness the bridge runs for Ask agent.
	Harness *HarnessInfo `json:"harness,omitempty"`
}

// HelloReply tells the bridge its own name.
type HelloReply struct {
	Bridge     string    `json:"bridge"`
	ServerTime time.Time `json:"serverTime"`
}

// State is everything the app shows.
type State struct {
	Items      []Item    `json:"items"`
	Repos      []Repo    `json:"repos"`
	Bridges    []Bridge  `json:"bridges"`
	Unapplied  int       `json:"unapplied"`
	User       string    `json:"user"`
	ServerTime time.Time `json:"serverTime"`
}

// NewBridge is POST /api/bridges. Password is needed unless this session
// entered the password within the last 10 minutes (docs/api.md, Login).
type NewBridge struct {
	Name     string `json:"name"`
	Password string `json:"password,omitempty"`
}

// NewBridgeReply carries a bridge token, shown once.
type NewBridgeReply struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

// PushSubscription is the browser's PushSubscription.toJSON().
// Kind and Name are Meat Mode's own: Kind is DeviceWeb (the default) or
// DeviceApp, and Name labels the device in Settings.
type PushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
}

// Device kinds: a browser's push subscription, or the Android app.
const (
	DeviceWeb = "web"
	DeviceApp = "app"
)

// Device is one place reminders go, as Settings lists it (docs/api.md,
// Devices). It never carries the endpoint or keys.
type Device struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Confirmed bool       `json:"confirmed"`
	CreatedAt time.Time  `json:"createdAt"`
	LastSeen  time.Time  `json:"lastSeen"`
	LastPush  *time.Time `json:"lastPush"`
}

// DeviceRegistered answers an app registration that awaits confirmation.
type DeviceRegistered struct {
	Device string `json:"device"`
}

// DeviceConfirm is what the app posts back from the confirmation push.
type DeviceConfirm struct {
	Code string `json:"code"`
}

// DeviceToken is the device token the hub issues once, on confirmation.
type DeviceToken struct {
	Token string `json:"token"`
}

// PersonalTodo is what an agent posts to add to the personal list.
type PersonalTodo struct {
	ID     string     `json:"id,omitempty"` // optional UUID; posting it again changes nothing
	Title  string     `json:"title"`
	Due    *time.Time `json:"due,omitempty"`
	Repeat *Repeat    `json:"repeat,omitempty"` // needs Due
	Parent string     `json:"parent,omitempty"` // id of an item this machine added to the same list; the new one is its sub-task
	Note   string     `json:"note,omitempty"`   // added as the first comment, by Author
	Author string     `json:"author,omitempty"` // the agent's name; defaults to the token's machine name
}

// PersonalChange is what an agent posts to change a personal to-do its
// machine added. Kind is edit, done, reopen, dismiss or note.
type PersonalChange struct {
	ID      string  `json:"id,omitempty"` // optional UUID; posting it again changes nothing
	Kind    string  `json:"kind"`
	Payload Payload `json:"payload"` // edit: set; note: text
	Author  string  `json:"author,omitempty"`
}

// PersonalTodoReply names the personal item an agent added.
type PersonalTodoReply struct {
	Key string `json:"key"`
	ID  string `json:"id"`
}
