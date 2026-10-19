package ticket

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// ticketFile is what one ticket file says. ticket parses its frontmatter
// with sed and awk, line by line: keys at column 0, arrays inline as
// [a, b]. This parser reads the same shape and nothing else.
type ticketFile struct {
	fields      map[string]string
	title       string
	description string
	notes       []tracker.Comment
}

// noteStamp is the line ticket's add-note writes above each note.
var noteStamp = regexp.MustCompile(`^\*\*(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ)\*\*$`)

// authorLine is the first line of a note Meat Mode wrote. ticket keeps no
// author, so the note carries its own.
var authorLine = regexp.MustCompile(`^by (\S+):$`)

// defaultAuthor is who a note without an author line gets: it came from ticket
// itself or an agent.
const defaultAuthor = "agent"

// parseTicket reads a ticket file. Frontmatter is the lines between the first
// line, which must be ---, and the next --- line; a body line that looks like
// a key is not one.
func parseTicket(data []byte) ticketFile {
	t := ticketFile{fields: map[string]string{}}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	i := 0
	if len(lines) > 0 && lines[0] == "---" {
		for i = 1; i < len(lines) && lines[i] != "---"; i++ {
			k, v, ok := strings.Cut(lines[i], ":")
			if !ok || k == "" || k[0] == ' ' || k[0] == '\t' || k[0] == '#' {
				continue
			}
			t.fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		i++
	}
	body := lines[i:]

	// The title is the first # heading; the description runs from there to
	// the first section ticket writes (Design, Acceptance Criteria, Notes).
	start := 0
	for n, l := range body {
		if strings.HasPrefix(l, "# ") {
			t.title = strings.TrimSpace(l[2:])
			start = n + 1
			break
		}
	}
	end := len(body)
	notesAt := -1
	for n := start; n < len(body); n++ {
		l := strings.TrimRight(body[n], " \t")
		if l == "## Notes" && notesAt < 0 {
			notesAt = n
		}
		if end == len(body) && (l == "## Design" || l == "## Acceptance Criteria" || l == "## Notes") {
			end = n
		}
	}
	t.description = strings.TrimSpace(strings.Join(body[start:end], "\n"))
	if notesAt >= 0 {
		t.notes = parseNotes(body[notesAt+1:])
	}
	return t
}

// parseNotes reads the **<timestamp>** blocks under ## Notes.
func parseNotes(lines []string) []tracker.Comment {
	var out []tracker.Comment
	var at time.Time
	var text []string
	flush := func() {
		if at.IsZero() {
			return
		}
		c := tracker.Comment{Author: defaultAuthor, CreatedAt: at}
		body := strings.TrimSpace(strings.Join(text, "\n"))
		if first, rest, _ := strings.Cut(body, "\n"); authorLine.MatchString(first) {
			c.Author = authorLine.FindStringSubmatch(first)[1]
			body = strings.TrimSpace(rest)
		}
		c.Text = body
		c.ID = strconv.Itoa(len(out) + 1)
		out = append(out, c)
	}
	for _, l := range lines {
		if m := noteStamp.FindStringSubmatch(strings.TrimRight(l, " \t")); m != nil {
			flush()
			at, _ = time.Parse(time.RFC3339, m[1])
			text = text[:0]
			continue
		}
		text = append(text, l)
	}
	flush()
	return out
}

// list reads an inline array such as [a, b].
func list(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.Trim(strings.TrimSpace(s), `"'`); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// issue turns a parsed file into a task. The file's id, bytes version and
// modification time come from the caller.
func (t ticketFile) issue(id, version string, mtime time.Time) tracker.Issue {
	is := tracker.Issue{
		ID:          id,
		Title:       t.title,
		Description: t.description,
		Status:      tracker.StatusOpen,
		Priority:    2,
		Labels:      list(t.fields["tags"]),
		CreatedBy:   t.fields["assignee"],
		Parent:      t.fields["parent"],
		UpdatedAt:   mtime,
		Comments:    t.notes,
		Version:     version,
	}
	switch t.fields["status"] {
	case "in_progress":
		is.Status = tracker.StatusInProgress
	case "closed", "done":
		is.Status = tracker.StatusClosed
	}
	if n, err := strconv.Atoi(t.fields["priority"]); err == nil && n >= 0 && n <= 4 {
		is.Priority = n
	}
	if c, err := time.Parse(time.RFC3339, t.fields["created"]); err == nil {
		is.CreatedAt = c
	} else {
		is.CreatedAt = mtime
	}
	if d, err := time.Parse(time.RFC3339, t.fields["due"]); err == nil {
		is.DueAt = &d
	}
	if is.Status == tracker.StatusClosed {
		// ticket keeps no close time; the file's last write is the best guide.
		closed := mtime
		is.ClosedAt = &closed
		if n := len(is.Comments); n > 0 && is.Comments[n-1].Text == dismissedNote {
			is.CloseReason = "dismissed"
		}
	}
	return is
}
