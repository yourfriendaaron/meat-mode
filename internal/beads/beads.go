// Package beads is everything the bridge needs from a beads database, behind
// one interface. CLI implements it over the bd command line.
package beads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// HumanLabel marks a bead as a task for the person.
const HumanLabel = "human"

// ActionMetaKey is the metadata key that ties a created bead to the Meat Mode
// action that created it.
const ActionMetaKey = "meat_mode_action"

// ClosedWindow is how long a closed item stays in the human list.
const ClosedWindow = 7 * 24 * time.Hour

// ErrNotFound is returned when a bead id does not exist.
var ErrNotFound = errors.New("bead not found")

// Beads is the bridge's view of beads. Every method takes the repo directory
// (the one holding .beads/). actor names who made a change.
type Beads interface {
	// Version returns the bd version, such as "1.3.1".
	Version(ctx context.Context) (string, error)
	// ListHuman returns the items labelled human in a repo. Open,
	// in-progress, blocked and deferred items are included, and closed ones
	// from the last ClosedWindow. Comment bodies are not filled.
	ListHuman(ctx context.Context, dir string) ([]Issue, error)
	// Show returns one bead with its comments.
	Show(ctx context.Context, dir, id string) (Issue, error)
	Close(ctx context.Context, dir, id, reason, actor string) error
	Reopen(ctx context.Context, dir, id, actor string) error
	// Defer sets defer_until, or clears it when until is nil.
	Defer(ctx context.Context, dir, id string, until *time.Time, actor string) error
	Dismiss(ctx context.Context, dir, id, actor string) error
	AddComment(ctx context.Context, dir, id, author, text string) error
	// Create makes a human bead and returns its id.
	Create(ctx context.Context, dir string, in NewIssue) (string, error)
	// FindByActionID returns the id of the bead created by that action, or ""
	// when there is none.
	FindByActionID(ctx context.Context, dir, actionID string) (string, error)
}

// NewIssue is what Create needs.
type NewIssue struct {
	Title    string
	Due      *time.Time
	ActionID string // stored in metadata under ActionMetaKey
	Actor    string
}

// Issue is one bead as bd prints it with --json.
type Issue struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description,omitempty"`
	Status       string          `json:"status"`
	Priority     int             `json:"priority"`
	Labels       []string        `json:"labels,omitempty"`
	CreatedBy    string          `json:"created_by,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	DueAt        *time.Time      `json:"due_at,omitempty"`
	DeferUntil   *time.Time      `json:"defer_until,omitempty"`
	ClosedAt     *time.Time      `json:"closed_at,omitempty"`
	CloseReason  *string         `json:"close_reason,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	CommentCount int             `json:"comment_count"`
	Comments     []Comment       `json:"comments,omitempty"`
}

// Comment is one bead comment as bd prints it.
type Comment struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// Fingerprint hashes the listed fields that change when a bead changes. A
// comment does not bump updated_at, so comment_count is part of it.
func (i Issue) Fingerprint() string {
	labels := slices.Clone(i.Labels)
	slices.Sort(labels)
	b, _ := json.Marshal(struct {
		ID, Title, Status string
		Due, Defer        *time.Time
		Labels            []string
		Metadata          json.RawMessage
		Updated           time.Time
		Comments          int
	}{i.ID, i.Title, i.Status, i.DueAt, i.DeferUntil, labels, i.Metadata, i.UpdatedAt, i.CommentCount})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Item converts a bead to the wire type a bridge sends (no key, bridge, repo
// or hub-only fields).
func (i Issue) Item() api.Item {
	it := api.Item{
		ID:          i.ID,
		Title:       i.Title,
		Description: i.Description,
		Status:      i.Status,
		Priority:    i.Priority,
		Labels:      i.Labels,
		CreatedBy:   i.CreatedBy,
		DueAt:       utc(i.DueAt),
		DeferUntil:  utc(i.DeferUntil),
		CreatedAt:   i.CreatedAt.UTC(),
		UpdatedAt:   i.UpdatedAt.UTC(),
		ClosedAt:    utc(i.ClosedAt),
		Comments:    make([]api.Comment, 0, len(i.Comments)),
	}
	if i.CloseReason != nil {
		it.CloseReason = *i.CloseReason
	}
	for _, c := range i.Comments {
		it.Comments = append(it.Comments, api.Comment{ID: c.ID, Author: c.Author, Text: c.Text, CreatedAt: c.CreatedAt.UTC()})
	}
	return it
}

// ActionID returns the Meat Mode action id stored in the bead's metadata.
func (i Issue) ActionID() string {
	var m map[string]any
	if json.Unmarshal(i.Metadata, &m) != nil {
		return ""
	}
	s, _ := m[ActionMetaKey].(string)
	return s
}

// KeepHuman reports whether a listed bead belongs in the human set at now.
func KeepHuman(i Issue, now time.Time) bool {
	if !slices.Contains(i.Labels, HumanLabel) {
		return false
	}
	if i.Status == "closed" {
		return i.ClosedAt != nil && now.Sub(*i.ClosedAt) <= ClosedWindow
	}
	return true
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
