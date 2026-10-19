package tracker

import "github.com/yourfriendaaron/meat-mode/internal/api"

// refusalWords say what a tracker without a capability cannot do.
var refusalWords = map[string]string{
	api.KindDone:    "mark tasks done",
	api.KindReopen:  "reopen tasks",
	api.KindSnooze:  "snooze",
	api.KindDismiss: "dismiss tasks",
	api.KindNote:    "take notes",
	api.KindAdd:     "add tasks",
	CapSubtask:      "add sub-tasks",
	api.KindAsk:     "start agents",
}

// Refusal is the message for a capability a tracker lacks, such as "ticket
// cannot snooze". The hub and the bridge both refuse with it.
func Refusal(title, capability string) string {
	words := refusalWords[capability]
	if words == "" {
		words = capability
	}
	return title + " cannot " + words
}
