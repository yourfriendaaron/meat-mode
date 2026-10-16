// Package fakebeads is an in-memory beads.Beads for tests.
package fakebeads

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/beads"
)

// Fake holds beads per repo directory. The zero value is not usable; call New.
type Fake struct {
	mu      sync.Mutex
	repos   map[string]map[string]*beads.Issue
	next    int
	Ver     string
	Now     func() time.Time
	Calls   []string         // one line per write, e.g. "close dir id"
	FailDir map[string]error // calls against these dirs fail with the error
}

var _ beads.Beads = (*Fake)(nil)

// New returns an empty fake reporting bd 1.3.1.
func New() *Fake {
	return &Fake{repos: map[string]map[string]*beads.Issue{}, Ver: "1.3.1", Now: time.Now, FailDir: map[string]error{}}
}

// Put adds or replaces a bead in dir.
func (f *Fake) Put(dir string, i beads.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.repos[dir] == nil {
		f.repos[dir] = map[string]*beads.Issue{}
	}
	if i.CreatedAt.IsZero() {
		i.CreatedAt = f.Now().UTC()
	}
	if i.UpdatedAt.IsZero() {
		i.UpdatedAt = i.CreatedAt
	}
	c := i
	f.repos[dir][i.ID] = &c
}

// Writes returns a copy of Calls.
func (f *Fake) Writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Calls)
}

// Get returns a copy of a bead.
func (f *Fake) Get(dir, id string) (beads.Issue, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.repos[dir][id]
	if !ok {
		return beads.Issue{}, false
	}
	return clone(i), true
}

// All returns copies of every bead in dir.
func (f *Fake) All(dir string) []beads.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []beads.Issue
	for _, i := range f.repos[dir] {
		out = append(out, clone(i))
	}
	slices.SortFunc(out, func(a, b beads.Issue) int { return cmpStr(a.ID, b.ID) })
	return out
}

func cmpStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func clone(i *beads.Issue) beads.Issue {
	c := *i
	c.Labels = slices.Clone(i.Labels)
	c.Comments = slices.Clone(i.Comments)
	c.CommentCount = len(i.Comments)
	return c
}

func (f *Fake) issue(dir, id string) (*beads.Issue, error) {
	if err := f.FailDir[dir]; err != nil {
		return nil, err
	}
	i, ok := f.repos[dir][id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", beads.ErrNotFound, id)
	}
	return i, nil
}

func (f *Fake) touch(i *beads.Issue) { i.UpdatedAt = f.Now().UTC() }

// Version implements beads.Beads.
func (f *Fake) Version(context.Context) (string, error) { return f.Ver, nil }

// ListHuman implements beads.Beads.
func (f *Fake) ListHuman(_ context.Context, dir string, personal bool) ([]beads.Issue, error) {
	f.mu.Lock()
	if err := f.FailDir[dir]; err != nil {
		f.mu.Unlock()
		return nil, err
	}
	f.mu.Unlock()
	var out []beads.Issue
	for _, i := range f.All(dir) {
		if beads.KeepHuman(i, personal, f.Now()) {
			i.Comments = nil
			out = append(out, i)
		}
	}
	return out, nil
}

// Show implements beads.Beads.
func (f *Fake) Show(_ context.Context, dir, id string) (beads.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.issue(dir, id)
	if err != nil {
		return beads.Issue{}, err
	}
	return clone(i), nil
}

// Close implements beads.Beads.
func (f *Fake) Close(_ context.Context, dir, id, reason, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.issue(dir, id)
	if err != nil {
		return err
	}
	f.Calls = append(f.Calls, "close "+dir+" "+id)
	now := f.Now().UTC()
	i.Status, i.ClosedAt, i.CloseReason = "closed", &now, &reason
	f.touch(i)
	return nil
}

// Reopen implements beads.Beads.
func (f *Fake) Reopen(_ context.Context, dir, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.issue(dir, id)
	if err != nil {
		return err
	}
	f.Calls = append(f.Calls, "reopen "+dir+" "+id)
	i.Status, i.ClosedAt, i.CloseReason, i.DeferUntil = "open", nil, nil, nil
	f.touch(i)
	return nil
}

// Defer implements beads.Beads.
func (f *Fake) Defer(_ context.Context, dir, id string, until *time.Time, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.issue(dir, id)
	if err != nil {
		return err
	}
	f.Calls = append(f.Calls, "defer "+dir+" "+id)
	if until == nil {
		i.DeferUntil, i.Status = nil, "open"
	} else {
		u := until.UTC()
		i.DeferUntil, i.Status = &u, "deferred"
	}
	f.touch(i)
	return nil
}

// Dismiss implements beads.Beads. Like bd, it fails on a closed bead.
func (f *Fake) Dismiss(_ context.Context, dir, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.issue(dir, id)
	if err != nil {
		return err
	}
	if i.Status == "closed" {
		return fmt.Errorf("issue %s is already closed", id)
	}
	f.Calls = append(f.Calls, "dismiss "+dir+" "+id)
	now := f.Now().UTC()
	reason := "Dismissed"
	i.Status, i.ClosedAt, i.CloseReason = "closed", &now, &reason
	f.touch(i)
	return nil
}

// AddComment implements beads.Beads. Comment times have bd's one-second
// precision.
func (f *Fake) AddComment(_ context.Context, dir, id, author, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.issue(dir, id)
	if err != nil {
		return err
	}
	f.Calls = append(f.Calls, "comment "+dir+" "+id)
	f.next++
	i.Comments = append(i.Comments, beads.Comment{
		ID: fmt.Sprintf("c%d", f.next), IssueID: id, Author: author, Text: text,
		CreatedAt: f.Now().UTC().Truncate(time.Second),
	})
	i.CommentCount = len(i.Comments)
	return nil
}

// Create implements beads.Beads.
func (f *Fake) Create(_ context.Context, dir string, in beads.NewIssue) (string, error) {
	f.mu.Lock()
	if err := f.FailDir[dir]; err != nil {
		f.mu.Unlock()
		return "", err
	}
	f.next++
	id := fmt.Sprintf("fake-%d", f.next)
	f.Calls = append(f.Calls, "create "+dir+" "+id)
	f.mu.Unlock()
	meta, _ := json.Marshal(map[string]string{beads.ActionMetaKey: in.ActionID})
	var due *time.Time
	if in.Due != nil {
		d := in.Due.UTC()
		due = &d
	}
	f.Put(dir, beads.Issue{ID: id, Title: in.Title, Status: "open", Priority: 2, Labels: []string{beads.HumanLabel},
		CreatedBy: in.Actor, DueAt: due, Metadata: meta})
	return id, nil
}

// FindByActionID implements beads.Beads.
func (f *Fake) FindByActionID(_ context.Context, dir, actionID string) (string, error) {
	f.mu.Lock()
	if err := f.FailDir[dir]; err != nil {
		f.mu.Unlock()
		return "", err
	}
	f.mu.Unlock()
	for _, i := range f.All(dir) {
		if i.ActionID() == actionID {
			return i.ID, nil
		}
	}
	return "", nil
}
