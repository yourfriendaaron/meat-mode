package backlog

import (
	"path"
	"slices"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// AuthorEnv carries the agent's name to its comments. Backlog.md reads no
// variable for a comment's author, so the prompt passes it with
// --comment-author.
const AuthorEnv = "BACKLOG_COMMENT_AUTHOR"

// Prompt implements tracker.Agent. The task's own comments carry its latest
// state, so the prompt says where to look, quotes the user's own words and
// gives the rules.
func (b *Tracker) Prompt(id string, notes []string) string {
	return "You are working on the Backlog.md task " + id + ". Run `backlog task view " + id + " --plain` and read its comments for the latest state. " +
		"Then do the work and close the task with `backlog task edit " + id + " --status Done` when it is done " +
		"(or the last status `backlog config get statuses` lists, when this project names it differently), " +
		"or reply with `backlog task edit " + id + " --comment \"...\" --comment-author \"$" + AuthorEnv + "\"`. " +
		"Never change the task's title. " +
		tracker.UserWords("comment", notes)
}

// FollowUp implements tracker.Agent.
func (b *Tracker) FollowUp(id, text string) string {
	return "New note from the user on task " + id + ":\n\n" + text
}

// Env implements tracker.Agent.
func (b *Tracker) Env(actor string) []string {
	return []string{AuthorEnv + "=" + actor}
}

// Describe implements tracker.Agent for backlog commands.
func (b *Tracker) Describe(words []string) string {
	if len(words) == 0 || path.Base(words[0]) != "backlog" {
		return ""
	}
	sub := ""
	if len(words) > 2 && words[1] == "task" {
		sub = words[2]
	} else if len(words) > 1 {
		sub = words[1]
	}
	switch sub {
	case "view", "list", "search", "board", "overview":
		return "Reading the task"
	case "create":
		return "Adding a task"
	case "config":
		return "Reading the settings"
	case "edit":
		if slices.ContainsFunc(words, func(w string) bool { return w == "--comment" || strings.HasPrefix(w, "--comment=") }) {
			return "Writing a note"
		}
		if slices.ContainsFunc(words, func(w string) bool {
			return w == "-s" || w == "--status" || strings.HasPrefix(w, "--status=")
		}) {
			return "Changing the task's status"
		}
	}
	return "Updating the task"
}
