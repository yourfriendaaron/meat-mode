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
