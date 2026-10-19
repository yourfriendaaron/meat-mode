package backlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// dismissedLabel marks a task the person dismissed: it is closed like any
// other, and the label tells Meat Mode it was not needed.
const dismissedLabel = "dismissed"

// actionRef is the reference Create adds so FindByActionID finds the task
// again. Backlog.md keeps references through every later edit.
const actionRef = "meat-mode:action/"

// dueHour is the hour of the day a due date reads as, in the bridge's zone:
// Backlog.md keeps a due date as a day with no time, and 9:00 is the time
// `meat-mode add` gives a due date without one.
const dueHour = 9

// defaultStatuses are Backlog.md's own when the project lists none.
var defaultStatuses = []string{"To Do", "In Progress", "Done"}

// listJSON is `backlog task list --json`.
type listJSON struct {
	SchemaVersion int               `json:"schemaVersion"`
	Kind          string            `json:"kind"`
	Tasks         []json.RawMessage `json:"tasks"`
	NextSkip      *int              `json:"nextSkip"`
}

// viewJSON is `backlog task view --json`.
type viewJSON struct {
	SchemaVersion int      `json:"schemaVersion"`
	Kind          string   `json:"kind"`
	Task          taskJSON `json:"task"`
}

// taskJSON is one task in either. List leaves out the detail fields.
type taskJSON struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Status      string        `json:"status"`
	Archived    bool          `json:"archived"`
	Priority    *string       `json:"priority"`
	Reporter    *string       `json:"reporter"`
	Labels      []string      `json:"labels"`
	ParentID    *string       `json:"parentTaskId"`
	References  []string      `json:"references"`
	CreatedAt   *string       `json:"createdAt"`
	UpdatedAt   *string       `json:"updatedAt"`
	DueDate     *string       `json:"dueDate"`
	IsReady     *bool         `json:"isReady"`
	Path        string        `json:"path"`
	Description *string       `json:"description"`
	Comments    []commentJSON `json:"comments"`
}

type commentJSON struct {
	Index     int     `json:"index"`
	Body      string  `json:"body"`
	CreatedAt *string `json:"createdAt"`
	Author    *string `json:"author"`
}

// List implements tracker.Tracker: the tasks labelled human, with their
// parents' titles, closed ones from the last week.
func (b *Tracker) List(ctx context.Context, dir string) ([]tracker.Issue, error) {
	statuses, err := b.statuses(ctx, dir)
	if err != nil {
		return nil, err
	}
	tasks, err := b.listAll(ctx, dir)
	if err != nil {
		return nil, err
	}
	files, err := taskFiles(dir)
	if err != nil {
		return nil, err
	}
	titles := make(map[string]string, len(tasks))
	for _, t := range tasks {
		titles[idKey(t.ID)] = t.Title
	}
	now := b.now()
	var out []tracker.Issue
	for _, t := range tasks {
		if !hasLabel(t.Labels, tracker.HumanLabel) {
			continue
		}
		is := toIssue(t, statuses)
		if is.Parent != "" {
			is.ParentTitle = titles[idKey(is.Parent)]
		}
		var file []byte
		if path, ok := files[idKey(t.ID)]; ok {
			if file, err = os.ReadFile(path); err != nil {
				return nil, err
			}
		}
		is.Version = fingerprint(is, file)
		if tracker.InWindow(is, now) {
			out = append(out, is)
		}
	}
	return out, nil
}

// Show implements tracker.Tracker.
func (b *Tracker) Show(ctx context.Context, dir, id string) (tracker.Issue, error) {
	if err := checkID(id); err != nil {
		return tracker.Issue{}, err
	}
	statuses, err := b.statuses(ctx, dir)
	if err != nil {
		return tracker.Issue{}, err
	}
	t, err := b.view(ctx, dir, id)
	if err != nil {
		return tracker.Issue{}, err
	}
	is := toIssue(t, statuses)
	if is.Parent != "" {
		// The parent's title is a nicety; a parent that cannot be read
		// leaves it empty rather than failing the task.
		if p, err := b.view(ctx, dir, is.Parent); err == nil {
			is.ParentTitle = p.Title
		}
	}
	var file []byte
	if t.Path != "" {
		path := t.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if file, err = os.ReadFile(path); err != nil {
			return tracker.Issue{}, err
		}
	}
	is.Version = fingerprint(is, file)
	return is, nil
}

// FindByActionID implements tracker.Tracker by the reference Create adds.
func (b *Tracker) FindByActionID(ctx context.Context, dir, actionID string) (string, error) {
	if actionID == "" {
		return "", nil
	}
	tasks, err := b.listAll(ctx, dir)
	if err != nil {
		return "", err
	}
	for _, t := range tasks {
		if slices.Contains(t.References, actionRef+actionID) {
			return t.ID, nil
		}
	}
	return "", nil
}

// createdPattern is the line `backlog task create` prints, which has no
// JSON form.
var createdPattern = regexp.MustCompile(`(?m)^Created task (\S+)\s*$`)

// Create implements tracker.Tracker: a task labelled human, with the action
// id as a reference and the due date as the day it falls on here.
func (b *Tracker) Create(ctx context.Context, dir string, in tracker.NewIssue) (string, error) {
	if strings.TrimSpace(in.Title) == "" {
		return "", errors.New("a task needs a title")
	}
	args := []string{"task", "create", "--labels=" + tracker.HumanLabel}
	if in.ActionID != "" {
		args = append(args, "--ref="+actionRef+in.ActionID)
	}
	if in.Due != nil {
		args = append(args, "--due-date="+in.Due.In(time.Local).Format(time.DateOnly))
	}
	if in.Parent != "" {
		if err := checkID(in.Parent); err != nil {
			return "", err
		}
		args = append(args, "--parent="+in.Parent)
	}
	out, err := b.run(ctx, dir, append(args, "--", in.Title)...)
	if err != nil {
		return "", err
	}
	if m := createdPattern.FindSubmatch(out); m != nil {
		return string(m[1]), nil
	}
	// The task was made but its id could not be read from the output; the
	// action id still finds it.
	if id, err := b.FindByActionID(ctx, dir, in.ActionID); err != nil || id != "" {
		return id, err
	}
	return "", fmt.Errorf("backlog task create printed no task id: %q", firstLine(string(out)))
}

// listAll returns every task in the working copy, archived ones aside. It
// refuses a paged or unknown answer, since a missing task would read as gone.
func (b *Tracker) listAll(ctx context.Context, dir string) ([]taskJSON, error) {
	out, err := b.run(ctx, dir, "task", "list", "--json")
	if err != nil {
		return nil, err
	}
	var l listJSON
	if err := json.Unmarshal(out, &l); err != nil {
		return nil, fmt.Errorf("reading backlog task list: %w", err)
	}
	if l.SchemaVersion != 1 || l.Kind != "task-list" {
		return nil, fmt.Errorf("backlog task list printed schema %d %q; Meat Mode reads schema 1", l.SchemaVersion, l.Kind)
	}
	if l.NextSkip != nil {
		return nil, errors.New("backlog task list printed only part of the list")
	}
	tasks := make([]taskJSON, 0, len(l.Tasks))
	for _, raw := range l.Tasks {
		var t taskJSON
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("reading backlog task list: %w", err)
		}
		if !t.Archived {
			tasks = append(tasks, t)
		}
	}
	return tasks, nil
}

func (b *Tracker) view(ctx context.Context, dir, id string) (taskJSON, error) {
	out, err := b.run(ctx, dir, "task", "view", "--json", "--", id)
	if err != nil {
		return taskJSON{}, err
	}
	var v viewJSON
	if err := json.Unmarshal(out, &v); err != nil {
		return taskJSON{}, fmt.Errorf("reading backlog task view: %w", err)
	}
	if v.SchemaVersion != 1 || v.Kind != "task-view" {
		return taskJSON{}, fmt.Errorf("backlog task view printed schema %d %q; Meat Mode reads schema 1", v.SchemaVersion, v.Kind)
	}
	return v.Task, nil
}

// statuses returns the project's statuses in board order. The last one is
// the closed one, as in Backlog.md's own getTerminalStatus.
func (b *Tracker) statuses(ctx context.Context, dir string) ([]string, error) {
	out, err := b.run(ctx, dir, "config", "get", "statuses")
	if err != nil {
		return nil, err
	}
	return parseStatuses(string(out)), nil
}

// parseStatuses reads `backlog config get statuses`, which joins them with
// ", ".
func parseStatuses(s string) []string {
	var out []string
	for part := range strings.SplitSeq(strings.TrimSpace(s), ", ") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return defaultStatuses
	}
	return out
}

// statusKey compares statuses as Backlog.md does: case and spaces ignored.
func statusKey(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "")
}

// mapStatus maps a Backlog.md status onto Meat Mode's. The last configured
// status is closed and any between the first and the last is in progress.
// The rest are open, or blocked while a dependency is unfinished.
func mapStatus(status string, statuses []string, ready bool) string {
	i := slices.IndexFunc(statuses, func(s string) bool { return statusKey(s) == statusKey(status) })
	switch {
	case i >= 0 && i == len(statuses)-1:
		return tracker.StatusClosed
	case i > 0:
		return tracker.StatusInProgress
	case !ready:
		return tracker.StatusBlocked
	}
	return tracker.StatusOpen
}

// priorities maps Backlog.md's priority names onto 0 (highest) to 4. The
// default set is high, medium and low; a task without one is medium.
var priorities = map[string]int{
	"critical": 0, "urgent": 0, "highest": 0,
	"high": 1, "medium": 2, "normal": 2, "low": 3, "lowest": 4,
}

func mapPriority(p *string) int {
	if p == nil {
		return 2
	}
	if n, ok := priorities[strings.ToLower(strings.TrimSpace(*p))]; ok {
		return n
	}
	return 2
}

// toIssue maps one task. Version and ParentTitle are left to the caller.
func toIssue(t taskJSON, statuses []string) tracker.Issue {
	ready := t.IsReady == nil || *t.IsReady
	is := tracker.Issue{
		ID:          t.ID,
		Title:       t.Title,
		Description: deref(t.Description),
		Status:      mapStatus(t.Status, statuses, ready),
		Priority:    mapPriority(t.Priority),
		CreatedBy:   deref(t.Reporter),
		Parent:      deref(t.ParentID),
	}
	dismissed := false
	for _, l := range t.Labels {
		if strings.EqualFold(l, dismissedLabel) {
			dismissed = true
			continue
		}
		is.Labels = append(is.Labels, l)
	}
	if c := parseTime(t.CreatedAt); c != nil {
		is.CreatedAt = *c
	}
	is.UpdatedAt = is.CreatedAt
	if u := parseTime(t.UpdatedAt); u != nil {
		is.UpdatedAt = *u
	}
	is.DueAt = parseDue(t.DueDate)
	if is.Status == tracker.StatusClosed {
		// Backlog.md keeps no close time; the last edit is the close, or
		// later.
		closed := is.UpdatedAt
		is.ClosedAt = &closed
		if dismissed {
			is.CloseReason = "Dismissed"
		}
	}
	for _, c := range t.Comments {
		cm := tracker.Comment{ID: strconv.Itoa(c.Index), Author: deref(c.Author), Text: c.Body}
		if at := parseTime(c.CreatedAt); at != nil {
			cm.CreatedAt = *at
		}
		is.Comments = append(is.Comments, cm)
	}
	return is
}

// fingerprint is a task's Version: a hash of its file, which holds its
// comments, and of what comes from outside the file. Without the file, the
// fields List reads stand in for it.
func fingerprint(is tracker.Issue, file []byte) string {
	h := sha256.New()
	outside := struct {
		Status, ParentTitle string
	}{is.Status, is.ParentTitle}
	_ = json.NewEncoder(h).Encode(outside)
	if file != nil {
		h.Write(file)
	} else {
		_ = json.NewEncoder(h).Encode(struct {
			Title, Parent string
			Labels        []string
			Priority      int
			DueAt         *time.Time
			UpdatedAt     time.Time
			CloseReason   string
		}{is.Title, is.Parent, is.Labels, is.Priority, is.DueAt, is.UpdatedAt, is.CloseReason})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// parseTime reads Backlog.md's JSON dates: RFC 3339 in UTC, or a day alone
// in older records.
func parseTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, *s); err == nil {
		return &t
	}
	if t, err := time.Parse(time.DateOnly, *s); err == nil {
		return &t
	}
	return nil
}

// parseDue reads a due day as dueHour on that day in the bridge's zone.
func parseDue(s *string) *time.Time {
	if s == nil || len(*s) < len(time.DateOnly) {
		return nil
	}
	d, err := time.Parse(time.DateOnly, (*s)[:len(time.DateOnly)])
	if err != nil {
		return nil
	}
	t := time.Date(d.Year(), d.Month(), d.Day(), dueHour, 0, 0, 0, time.Local)
	return &t
}

func hasLabel(labels []string, want string) bool {
	return slices.ContainsFunc(labels, func(l string) bool { return strings.EqualFold(l, want) })
}

// idKey compares task ids as Backlog.md does, ignoring case.
func idKey(id string) string { return strings.ToLower(id) }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// taskFiles maps each task id to its file in the repo's tasks folder. A file
// is named "<id> - <title>.md".
func taskFiles(dir string) (map[string]string, error) {
	tasks := filepath.Join(dir, backlogDir(dir), "tasks")
	entries, err := os.ReadDir(tasks)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		id, _, ok := strings.Cut(e.Name(), " - ")
		if ok && !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out[idKey(id)] = filepath.Join(tasks, e.Name())
		}
	}
	return out, nil
}

// backlogDir is the repo's store folder, relative to dir, found as
// Backlog.md finds it: the folder a root backlog.config.yml names, else
// backlog/ or .backlog/, the one with a config file first.
func backlogDir(dir string) string {
	if folder, ok := readRootConfig(filepath.Join(dir, rootConfig)); ok && folder != "" && filepath.IsLocal(folder) {
		return filepath.Clean(folder)
	}
	for _, d := range storeDirs {
		for _, f := range configFiles {
			if _, err := os.Stat(filepath.Join(dir, d, f)); err == nil {
				return d
			}
		}
	}
	for _, d := range storeDirs {
		if st, err := os.Stat(filepath.Join(dir, d)); err == nil && st.IsDir() {
			return d
		}
	}
	return storeDirs[0]
}
