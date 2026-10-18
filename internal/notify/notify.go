// Package notify delivers reminders to the user: Web Push to subscribed
// browsers, and optionally ntfy.
package notify

import (
	"context"
	"errors"
)

// Message is one notification.
type Message struct {
	Title string
	Body  string
	URL   string // opened when the notification is tapped
	Tag   string // same tag replaces an earlier notification
	// Actions are the notification's buttons, and Key the item they act on;
	// see docs/api.md, Agent push. A reminder has none.
	Actions []Action
	Key     string
}

// Action names of a notification's buttons.
const (
	ActionReply = "reply" // opens the item at the note box
	ActionOpen  = "open"  // opens the item
	ActionRetry = "retry" // asks the agent again
)

// Action is one button on a notification.
type Action struct {
	Action string `json:"action"` // ActionReply, ActionOpen or ActionRetry
	Title  string `json:"title"`
	// URL is the page a reply or open button shows, for ntfy's view
	// actions. Web Push leaves it to the service worker.
	URL string `json:"-"`
}

// Notifier delivers a Message.
type Notifier interface {
	Notify(ctx context.Context, m Message) error
}

// Multi fans a message out to several notifiers. Every notifier is tried;
// the errors come back joined.
type Multi []Notifier

// Notify implements Notifier.
func (mu Multi) Notify(ctx context.Context, m Message) error {
	var errs []error
	for _, n := range mu {
		if err := n.Notify(ctx, m); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
