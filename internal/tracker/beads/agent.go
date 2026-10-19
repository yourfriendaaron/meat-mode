package beads

import (
	"path"
	"regexp"
	"strings"
)

// Prompt implements tracker.Agent. The bead's own notes carry the task and
// the person's latest words, so the prompt only says where to look and the
// rules.
func (c *CLI) Prompt(id string) string {
	return "You are working on the task in bead " + id + ". Run `bd show " + id + "` and read its comments for the latest state. " +
		"Then do the work and close the bead with `bd close " + id + "` when it is done, or reply with `bd comment " + id + " \"...\"`. " +
		"Follow this repository's own instruction files. " +
		"When something is denied or needs the user, stop and say what you need in a comment. " +
		"Never send, post, spend, delete or push anything for the user."
}

// FollowUp implements tracker.Agent.
func (c *CLI) FollowUp(id, text string) string {
	return "New note from the user on bead " + id + ":\n\n" + text
}

// Env implements tracker.Agent: BEADS_ACTOR, so the agent's notes carry its
// name.
func (c *CLI) Env(actor string) []string {
	return []string{"BEADS_ACTOR=" + actor}
}

// plainWord is a subcommand safe to look at.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// Describe implements tracker.Agent: bd commands are named for what they do
// to the bead.
func (c *CLI) Describe(words []string) string {
	if len(words) == 0 || path.Base(words[0]) != "bd" {
		return ""
	}
	second := ""
	if len(words) > 1 && plainWord.MatchString(words[1]) && !strings.HasPrefix(words[1], "-") {
		second = words[1]
	}
	switch second {
	case "comments":
		if len(words) > 2 && words[2] == "add" {
			return "Writing a note"
		}
		return "Reading the bead"
	case "show", "list", "ready", "search":
		return "Reading the bead"
	case "comment":
		return "Writing a note"
	case "close":
		return "Closing the bead"
	}
	return "Updating the bead"
}
