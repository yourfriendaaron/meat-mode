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

// Repo is one beads database a bridge found.
type Repo struct {
	Bridge   string `json:"bridge,omitempty"`
	Name     string `json:"name"`
	Personal bool   `json:"personal"`
	OptedIn  bool   `json:"optedIn"` // synced: switched on in the app, or by the repo's config file
	// Hub-only: a sync switch the bridge has not applied yet. OptedIn then
	// shows the position it was switched to.
	Pending bool `json:"pending,omitempty"`
}

// Bridge is one paired agent machine.
type Bridge struct {
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"createdAt"`
	LastSeen  *time.Time `json:"lastSeen,omitempty"`
	Version   string     `json:"version,omitempty"`
	BdVersion string     `json:"bdVersion,omitempty"`
}

// Action kinds.
const (
	KindDone    = "done"
	KindReopen  = "reopen"
	KindSnooze  = "snooze"
	KindDismiss = "dismiss"
	KindNote    = "note"
	KindAdd     = "add"
	KindSync    = "sync" // switch a repo's syncing on or off
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
	On       *bool      `json:"on,omitempty"`       // sync
}

// NewAction is what the app posts.
type NewAction struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Key     string  `json:"key,omitempty"`
	Bridge  string  `json:"bridge,omitempty"` // add and sync only
	Repo    string  `json:"repo,omitempty"`   // add and sync only
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
	BdVersion string `json:"bdVersion"`
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

// NewBridgeReply carries a bridge token, shown once.
type NewBridgeReply struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

// PushSubscription is the browser's PushSubscription.toJSON().
type PushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}
