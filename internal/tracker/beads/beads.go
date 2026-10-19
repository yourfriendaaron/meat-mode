// Package beads is the beads tracker: tasks in a repo's .beads database,
// read and written with the bd command line. CLI implements tracker.Tracker
// and tracker.Agent.
package beads

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// ActionMetaKey is the metadata key that ties a created bead to the Meat Mode
// action that created it.
const ActionMetaKey = "meat_mode_action"

// Bead is one bead as bd prints it with --json.
type Bead struct {
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
	// Parent is the bead this one is a child of (bd's parent-child
	// dependency). bd list and bd show both print it.
	Parent       string       `json:"parent,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

// Dependency is one entry of a bead's dependencies. bd show prints the
// other bead with its title and dependency_type; bd list prints only
// depends_on_id and type.
type Dependency struct {
	ID             string `json:"id,omitempty"`
	Title          string `json:"title,omitempty"`
	DependencyType string `json:"dependency_type,omitempty"`
	DependsOnID    string `json:"depends_on_id,omitempty"`
	Type           string `json:"type,omitempty"`
}

// parentChild is bd's dependency type for a parent.
const parentChild = "parent-child"

// ParentTitle returns the parent's title as bd show printed it; empty from
// bd list, or with no parent.
func (b Bead) ParentTitle() string {
	if b.Parent == "" {
		return ""
	}
	for _, d := range b.Dependencies {
		if d.ID == b.Parent && d.DependencyType == parentChild {
			return d.Title
		}
	}
	return ""
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
// comment does not bump updated_at, so comment_count is part of it. It is
// the task's tracker.Issue.Version.
func (b Bead) Fingerprint() string {
	labels := slices.Clone(b.Labels)
	slices.Sort(labels)
	raw, _ := json.Marshal(struct {
		ID, Title, Status string
		Parent            string
		Due, Defer        *time.Time
		Labels            []string
		Metadata          json.RawMessage
		Updated           time.Time
		Comments          int
	}{b.ID, b.Title, b.Status, b.Parent, b.DueAt, b.DeferUntil, labels, b.Metadata, b.UpdatedAt, b.CommentCount})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Issue converts a bead to the tracker's view of it. bd's statuses are the
// tracker's own, so they pass through as they are.
func (b Bead) Issue() tracker.Issue {
	i := tracker.Issue{
		ID:          b.ID,
		Title:       b.Title,
		Description: b.Description,
		Status:      b.Status,
		Priority:    b.Priority,
		Labels:      b.Labels,
		CreatedBy:   b.CreatedBy,
		CreatedAt:   b.CreatedAt,
		UpdatedAt:   b.UpdatedAt,
		DueAt:       b.DueAt,
		DeferUntil:  b.DeferUntil,
		ClosedAt:    b.ClosedAt,
		Parent:      b.Parent,
		ParentTitle: b.ParentTitle(),
		Version:     b.Fingerprint(),
	}
	if b.CloseReason != nil {
		i.CloseReason = *b.CloseReason
	}
	for _, c := range b.Comments {
		i.Comments = append(i.Comments, tracker.Comment{ID: c.ID, Author: c.Author, Text: c.Text, CreatedAt: c.CreatedAt})
	}
	return i
}

// ActionID returns the Meat Mode action id stored in the bead's metadata.
func (b Bead) ActionID() string {
	var m map[string]any
	if json.Unmarshal(b.Metadata, &m) != nil {
		return ""
	}
	s, _ := m[ActionMetaKey].(string)
	return s
}

// KeepHuman reports whether a listed bead belongs in the human set at now:
// labelled human, and not closed before the tracker's window.
func KeepHuman(b Bead, now time.Time) bool {
	if !slices.Contains(b.Labels, tracker.HumanLabel) {
		return false
	}
	return tracker.InWindow(tracker.Issue{Status: b.Status, ClosedAt: b.ClosedAt}, now)
}
