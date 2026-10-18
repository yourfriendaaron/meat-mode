// Package reminders fires a notification when an item becomes due or a
// snooze ends, once per (item, fire time).
package reminders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/notify"
)

// Interval is how often Run checks.
const Interval = 30 * time.Second

// MaxAge is how late a missed reminder may still fire. Older ones are
// recorded as fired without notifying.
const MaxAge = 7 * 24 * time.Hour

// ItemSource lists the hub's current items.
type ItemSource interface {
	Items(ctx context.Context) ([]api.Item, error)
}

// Clock lets tests control time.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the real clock.
type SystemClock struct{}

// Now implements Clock.
func (SystemClock) Now() time.Time { return time.Now() }

// After implements Clock.
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Scheduler checks items and notifies for those whose time has come.
type Scheduler struct {
	db        *sql.DB
	src       ItemSource
	notifier  notify.Notifier
	clock     Clock
	publicURL string

	// Every, when set, replaces Interval. The hub sets it from the
	// undocumented MEAT_MODE_REMINDER_INTERVAL variable for end-to-end tests.
	Every time.Duration

	// KeyOf, when set, names the item a reminder is recorded under, so two
	// copies of one task share a reminder: the hub maps a bead created by an
	// add to the provisional item the app showed before. Nil means the
	// item's key.
	KeyOf func(context.Context, api.Item) (string, error)
}

// New creates the scheduler and its reminder_fired table in db.
func New(db *sql.DB, src ItemSource, n notify.Notifier, clock Clock, publicURL string) (*Scheduler, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS reminder_fired (
		item_key TEXT NOT NULL,
		fire_at  INTEGER NOT NULL, -- unix milliseconds
		PRIMARY KEY (item_key, fire_at)
	)`)
	if err != nil {
		return nil, err
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &Scheduler{db: db, src: src, notifier: n, clock: clock, publicURL: strings.TrimRight(publicURL, "/")}, nil
}

// fireTime is when the item should remind: the snooze end if one is set,
// otherwise the due time. Closed items never fire.
func fireTime(it api.Item) (time.Time, bool, bool) {
	if it.Status == "closed" {
		return time.Time{}, false, false
	}
	if it.DeferUntil != nil {
		return *it.DeferUntil, true, true
	}
	if it.DueAt != nil {
		return *it.DueAt, false, true
	}
	return time.Time{}, false, false
}

// Run checks every Interval until ctx ends. The first check runs at once,
// which catches reminders that came due while the hub was down.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("reminder check failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(s.every()):
		}
	}
}

func (s *Scheduler) every() time.Duration {
	if s.Every > 0 {
		return s.Every
	}
	return Interval
}

// Tick runs one check. A reminder is recorded as fired even when delivery
// fails, so a broken push service cannot cause a flood of repeats; the
// delivery errors are returned.
func (s *Scheduler) Tick(ctx context.Context) error {
	items, err := s.src.Items(ctx)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	var errs []error
	for _, it := range items {
		at, snoozed, ok := fireTime(it)
		if !ok || at.After(now) {
			continue
		}
		// link is the item's own key; key is what the fired-once record uses,
		// which for an added bead is the provisional key.
		link := it.Key
		if link == "" {
			link = it.ID
		}
		key := link
		if s.KeyOf != nil {
			if key, err = s.KeyOf(ctx, it); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		// Whole seconds: bd stores times to the second, so a time the app
		// sent with milliseconds comes back from the bridge without them.
		ms := at.Truncate(time.Second).UnixMilli()
		res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO reminder_fired (item_key, fire_at) VALUES (?, ?)`, key, ms)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // already fired
		}
		if now.Sub(at) > MaxAge {
			continue // too old; recorded silently
		}
		body := "Due now"
		if snoozed {
			body = "Snoozed until now"
		}
		err = s.notifier.Notify(ctx, notify.Message{
			Title: truncTitle(it.Title),
			Body:  body,
			URL:   fmt.Sprintf("%s/?item=%s", s.publicURL, url.QueryEscape(link)),
			Tag:   link,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", link, err))
		}
	}
	return errors.Join(errs...)
}

// truncTitle caps a notification title at maxTitleRunes runes: a huge bead
// title would overflow Web Push's 4 KiB payload and fail the whole send.
const maxTitleRunes = 200

func truncTitle(s string) string {
	if utf8.RuneCountInString(s) <= maxTitleRunes {
		return s
	}
	return string([]rune(s)[:maxTitleRunes]) + "…"
}
