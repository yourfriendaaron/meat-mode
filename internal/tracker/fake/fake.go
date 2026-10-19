// Package fake is an in-memory tracker for tests. Its Info is the test's to
// set, so the bridge can be tested against a tracker that lacks a
// capability, or that runs no agents.
package fake

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// Store is the folder Detect looks for in a repo, unless Fake.Store says
// otherwise.
const Store = ".fake"

// Fake holds tasks per repo directory. The zero value is not usable; call
// New. Set Info, Store, Ver and CheckErr before handing it to the code under
// test; Tracker returns the value to hand over.
type Fake struct {
	// Inf is what Info returns: by default the name "fake", the title
	// "Fake" and every capability.
	Inf      tracker.Info
	Store    string // the folder Detect matches; Store when empty
	Ver      string // what Check returns
	CheckErr error  // when set, Check fails with it
	Now      func() time.Time

	mu      sync.Mutex
	repos   map[string]map[string]*task
	next    int
	Calls   []string         // one line per write, e.g. "close dir id"
	FailDir map[string]error // calls against these dirs fail with the error
}

type task struct {
	tracker.Issue
	action string
	rev    int
}

// New returns an empty fake with every capability, reporting version 1.0.0.
func New() *Fake {
	return &Fake{
		Inf: tracker.Info{Name: "fake", Title: "Fake", Prune: []string{Store}, Actions: slices.Clone(tracker.Capabilities)},
		Ver: "1.0.0", Now: time.Now, repos: map[string]map[string]*task{}, FailDir: map[string]error{},
	}
}

// Tracker returns f as the tracker to test: one that also implements
// tracker.Agent exactly when Inf lists ask, as tracker.Validate wants.
func (f *Fake) Tracker() tracker.Tracker {
	if f.Inf.Can(api.KindAsk) {
		return &Agent{f}
	}
	return f
}

// Agent is a Fake that runs agents. Its words name the fake's own command,
// fk.
type Agent struct{ *Fake }

// Prompt implements tracker.Agent.
func (a *Agent) Prompt(id string) string {
	return "Work on task " + id + ". Read it with `fk show " + id + "`."
}

// FollowUp implements tracker.Agent.
func (a *Agent) FollowUp(id, text string) string { return "New note on task " + id + ":\n\n" + text }

// Env implements tracker.Agent.
func (a *Agent) Env(actor string) []string { return []string{"FAKE_ACTOR=" + actor} }

// Describe implements tracker.Agent.
func (a *Agent) Describe(words []string) string {
	if len(words) > 0 && words[0] == "fk" {
		return "Using the fake tracker"
	}
	return ""
}

// Info implements tracker.Tracker.
func (f *Fake) Info() tracker.Info { return f.Inf }

// Check implements tracker.Tracker.
func (f *Fake) Check(context.Context) (string, error) {
	if f.CheckErr != nil {
		return "", f.CheckErr
	}
	return f.Ver, nil
}

func (f *Fake) store() string {
	if f.Store != "" {
		return f.Store
	}
	return Store
}

// Detect implements tracker.Tracker: a folder named Store.
func (f *Fake) Detect(_ string, entries []fs.DirEntry) bool {
	for _, e := range entries {
		if e.Name() == f.store() && e.IsDir() {
			return true
		}
	}
	return false
}

// OptedIn implements tracker.Tracker: the repo's .meat-mode.yaml only.
func (f *Fake) OptedIn(dir string) bool { return tracker.RepoOptIn(dir) }

// Put adds or replaces a task in dir. A task without the human label is
// stored but not listed.
func (f *Fake) Put(dir string, i tracker.Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.put(dir, i, "")
}

func (f *Fake) put(dir string, i tracker.Issue, action string) {
	if f.repos[dir] == nil {
		f.repos[dir] = map[string]*task{}
	}
	if i.CreatedAt.IsZero() {
		i.CreatedAt = f.Now().UTC()
	}
	if i.UpdatedAt.IsZero() {
		i.UpdatedAt = i.CreatedAt
	}
	if i.Status == "" {
		i.Status = tracker.StatusOpen
	}
	rev := 0
	if old := f.repos[dir][i.ID]; old != nil {
		rev = old.rev + 1
		if action == "" {
			action = old.action
		}
	}
	f.repos[dir][i.ID] = &task{Issue: clone(i), action: action, rev: rev}
}

// Writes returns a copy of Calls.
func (f *Fake) Writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Calls)
}

// Get returns a copy of a task, with its comments.
func (f *Fake) Get(dir, id string) (tracker.Issue, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.repos[dir][id]
	if !ok {
		return tracker.Issue{}, false
	}
	return f.view(dir, t), true
}

// All returns copies of every task in dir, by id.
func (f *Fake) All(dir string) []tracker.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []tracker.Issue
	for _, t := range f.repos[dir] {
		out = append(out, f.view(dir, t))
	}
	slices.SortFunc(out, func(a, b tracker.Issue) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// view is a copy of t with its version and parent title. Callers hold mu.
func (f *Fake) view(dir string, t *task) tracker.Issue {
	i := clone(t.Issue)
	i.Version = strconv.Itoa(t.rev)
	if p, ok := f.repos[dir][i.Parent]; ok {
		i.ParentTitle = p.Title
	}
	return i
}

func clone(i tracker.Issue) tracker.Issue {
	i.Labels = slices.Clone(i.Labels)
	i.Comments = slices.Clone(i.Comments)
	return i
}

func (f *Fake) task(dir, id string) (*task, error) {
	if err := f.FailDir[dir]; err != nil {
		return nil, err
	}
	t, ok := f.repos[dir][id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", tracker.ErrNotFound, id)
	}
	return t, nil
}

// write records a call on a task and bumps its version. Callers hold mu.
func (f *Fake) write(t *task, call string) {
	f.Calls = append(f.Calls, call)
	t.UpdatedAt = f.Now().UTC()
	t.rev++
}

func (f *Fake) refuse(capability string) error {
	if !f.Inf.Can(capability) {
		return tracker.ErrUnsupported
	}
	return nil
}

// List implements tracker.Tracker: tasks labelled human, open or closed
// within tracker.ClosedWindow.
func (f *Fake) List(_ context.Context, dir string) ([]tracker.Issue, error) {
	f.mu.Lock()
	err := f.FailDir[dir]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var out []tracker.Issue
	for _, i := range f.All(dir) {
		if slices.Contains(i.Labels, tracker.HumanLabel) && tracker.InWindow(i, f.Now()) {
			i.Comments = nil
			out = append(out, i)
		}
	}
	return out, nil
}

// Show implements tracker.Tracker.
func (f *Fake) Show(_ context.Context, dir, id string) (tracker.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.task(dir, id)
	if err != nil {
		return tracker.Issue{}, err
	}
	return f.view(dir, t), nil
}

// Close implements tracker.Tracker. Like bd, it refuses a task with an open
// sub-task.
func (f *Fake) Close(_ context.Context, dir, id, reason, _ string) error {
	if err := f.refuse(api.KindDone); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.task(dir, id)
	if err != nil {
		return err
	}
	for _, c := range f.repos[dir] {
		if c.Parent == id && c.Status != tracker.StatusClosed {
			return tracker.ErrOpenChildren
		}
	}
	now := f.Now().UTC()
	t.Status, t.ClosedAt, t.CloseReason = tracker.StatusClosed, &now, reason
	f.write(t, "close "+dir+" "+id)
	return nil
}

// Reopen implements tracker.Tracker.
func (f *Fake) Reopen(_ context.Context, dir, id, _ string) error {
	if err := f.refuse(api.KindReopen); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.task(dir, id)
	if err != nil {
		return err
	}
	t.Status, t.ClosedAt, t.CloseReason, t.DeferUntil = tracker.StatusOpen, nil, "", nil
	f.write(t, "reopen "+dir+" "+id)
	return nil
}

// Defer implements tracker.Tracker.
func (f *Fake) Defer(_ context.Context, dir, id string, until *time.Time, _ string) error {
	if err := f.refuse(api.KindSnooze); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.task(dir, id)
	if err != nil {
		return err
	}
	if until == nil {
		t.DeferUntil, t.Status = nil, tracker.StatusOpen
	} else {
		u := until.UTC()
		t.DeferUntil, t.Status = &u, tracker.StatusDeferred
	}
	f.write(t, "defer "+dir+" "+id)
	return nil
}

// Dismiss implements tracker.Tracker. Like bd, it fails on a closed task.
func (f *Fake) Dismiss(_ context.Context, dir, id, _ string) error {
	if err := f.refuse(api.KindDismiss); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.task(dir, id)
	if err != nil {
		return err
	}
	if t.Status == tracker.StatusClosed {
		return fmt.Errorf("task %s is already closed", id)
	}
	now := f.Now().UTC()
	t.Status, t.ClosedAt, t.CloseReason = tracker.StatusClosed, &now, "Dismissed"
	f.write(t, "dismiss "+dir+" "+id)
	return nil
}

// AddComment implements tracker.Tracker. Comment times have bd's
// one-second precision.
func (f *Fake) AddComment(_ context.Context, dir, id, author, text string) error {
	if err := f.refuse(api.KindNote); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, err := f.task(dir, id)
	if err != nil {
		return err
	}
	f.next++
	t.Comments = append(t.Comments, tracker.Comment{
		ID: fmt.Sprintf("c%d", f.next), Author: author, Text: text, CreatedAt: f.Now().UTC().Truncate(time.Second),
	})
	f.write(t, "comment "+dir+" "+id)
	return nil
}

// Create implements tracker.Tracker.
func (f *Fake) Create(_ context.Context, dir string, in tracker.NewIssue) (string, error) {
	if in.Parent != "" {
		if err := f.refuse(tracker.CapSubtask); err != nil {
			return "", err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.FailDir[dir]; err != nil {
		return "", err
	}
	if in.Parent != "" {
		if _, ok := f.repos[dir][in.Parent]; !ok {
			return "", fmt.Errorf("%w: %s", tracker.ErrNotFound, in.Parent)
		}
	}
	f.next++
	id := fmt.Sprintf("fake-%d", f.next)
	f.Calls = append(f.Calls, "create "+dir+" "+id)
	var due *time.Time
	if in.Due != nil {
		d := in.Due.UTC()
		due = &d
	}
	f.put(dir, tracker.Issue{ID: id, Title: in.Title, Status: tracker.StatusOpen, Priority: 2, Labels: []string{tracker.HumanLabel},
		CreatedBy: in.Actor, DueAt: due, Parent: in.Parent}, in.ActionID)
	return id, nil
}

// FindByActionID implements tracker.Tracker.
func (f *Fake) FindByActionID(_ context.Context, dir, actionID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.FailDir[dir]; err != nil {
		return "", err
	}
	for _, t := range f.repos[dir] {
		if actionID != "" && t.action == actionID {
			return t.ID, nil
		}
	}
	return "", nil
}
