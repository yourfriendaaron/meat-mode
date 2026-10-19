// Package reminders fires a notification when an item becomes due or a
// snooze ends, once per (item, fire time), and an early reminder a lead
// time before the due time, once per (item, due time). It also pushes the
// end of an agent run, once per (item, run, state); see runs.go.
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

// Late is how long after its moment a reminder counts as missed: it came due
// while the hub was stopped or its checks were failing.
const Late = 5 * time.Minute

// arrivedLate reports whether the hub first saw an item more than Late after
// the moment at. Within Late it still reminds, so a to-do added moments
// before it comes due is not lost to a slow sync.
func arrivedLate(first, at time.Time) bool { return first.Sub(at) > Late }

// MissedTag is the tag of the one reminder that sums up several missed ones.
const MissedTag = "missed-reminders"

// ItemSource lists the hub's current items.
type ItemSource interface {
	Items(ctx context.Context) ([]api.Item, error)
}

// LeadSource gives the hub's reminder lead time in minutes, 0 for none.
type LeadSource interface {
	ReminderLead(ctx context.Context) (int, error)
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

	// Every, when set, replaces Interval. The hub sets it from
	// MEAT_MODE_REMINDER_INTERVAL, a setting for end-to-end tests.
	Every time.Duration

	// Lead, when set, gives the lead time of items without their own
	// RemindBefore. Nil means none.
	Lead LeadSource

	// KeyOf, when set, names the item a reminder is recorded under, so two
	// copies of one task share a reminder, and when the hub first knew of
	// the task: the hub maps a bead created by an add to the provisional
	// item the app showed before. Nil means the item's key and FirstSeen.
	KeyOf func(context.Context, api.Item) (string, time.Time, error)

	// ClosedInApp, when set, reports whether the user closed the item with
	// key in the app at or after since; a run end on such an item sends no
	// push. Nil means never.
	ClosedInApp func(ctx context.Context, key string, since time.Time) (bool, error)

	// Allows, when set, reports whether the item with key can take an
	// action of kind, so a reminder leaves out the Done or Snooze button
	// its list's tracker cannot apply. Nil means every kind.
	Allows func(ctx context.Context, key, kind string) (bool, error)

	wake chan struct{}
}

// New creates the scheduler and its tables in db: reminder_fired for the
// reminders at the due time or a snooze end, reminder_early for the early
// ones, keyed by the due time they came before, and run_pushed for the run
// ends pushed.
func New(db *sql.DB, src ItemSource, n notify.Notifier, clock Clock, publicURL string) (*Scheduler, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS reminder_fired (
		item_key TEXT NOT NULL,
		fire_at  INTEGER NOT NULL, -- unix milliseconds
		PRIMARY KEY (item_key, fire_at)
	);
	CREATE TABLE IF NOT EXISTS reminder_early (
		item_key TEXT NOT NULL,
		due_at   INTEGER NOT NULL, -- unix milliseconds
		PRIMARY KEY (item_key, due_at)
	)`)
	if err != nil {
		return nil, err
	}
	if err := createRunTable(db); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &Scheduler{db: db, src: src, notifier: n, clock: clock, publicURL: strings.TrimRight(publicURL, "/"),
		wake: make(chan struct{}, 1)}, nil
}

// fireTime is when the item should remind: the snooze end if one is set,
// otherwise the due time. A due time after the snooze end takes over once
// it comes, so a due date moved past an old snooze still reminds. Closed
// items never fire.
func fireTime(it api.Item, now time.Time) (time.Time, bool, bool) {
	if it.Status == "closed" {
		return time.Time{}, false, false
	}
	if it.DeferUntil != nil {
		if it.DueAt != nil && it.DueAt.After(*it.DeferUntil) && !it.DueAt.After(now) {
			return *it.DueAt, false, true
		}
		return *it.DeferUntil, true, true
	}
	if it.DueAt != nil {
		return *it.DueAt, false, true
	}
	return time.Time{}, false, false
}

// earlyTime is when the item's early reminder comes, lead before its due
// time. There is none without a lead, for a closed item, or when it would
// fall inside a snooze or on its end: the user picked the snooze end.
func earlyTime(it api.Item, lead time.Duration) (time.Time, bool) {
	if lead <= 0 || it.Status == "closed" || it.DueAt == nil {
		return time.Time{}, false
	}
	at := it.DueAt.Add(-lead)
	if it.DeferUntil != nil && !at.After(*it.DeferUntil) {
		return time.Time{}, false
	}
	return at, true
}

// Run checks every Interval until ctx ends. The first check runs at once,
// which catches reminders that came due while the hub was down. A Wake
// checks agent runs in between.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("reminder check failed", "err", err)
		}
		next := s.clock.After(s.every())
		for waiting := true; waiting; {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
				if err := s.CheckRuns(ctx); err != nil && ctx.Err() == nil {
					slog.Error("agent run check failed", "err", err)
				}
			case <-next:
				waiting = false
			}
		}
	}
}

func (s *Scheduler) every() time.Duration {
	if s.Every > 0 {
		return s.Every
	}
	return Interval
}

// due is one reminder a check sends.
type due struct {
	msg  notify.Message
	late bool
}

// Tick runs one check. A reminder is recorded as fired even when delivery
// fails, so a broken push service cannot cause a flood of repeats; the
// delivery errors are returned. Reminders more than Late late go out as
// they are when there is one, and as one summary when there are more.
func (s *Scheduler) Tick(ctx context.Context) error {
	items, err := s.src.Items(ctx)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	var errs []error
	hubLead, leadOK := 0, true
	if s.Lead != nil {
		if hubLead, err = s.Lead.ReminderLead(ctx); err != nil {
			errs = append(errs, fmt.Errorf("reminder lead time: %w", err))
			leadOK = false
		}
	}
	var out []due
	for _, it := range items {
		// link is the item's own key; key is what the fired-once record uses,
		// which for an added bead is the provisional key.
		link := it.Key
		if link == "" {
			link = it.ID
		}
		key, first := link, it.FirstSeen
		if s.KeyOf != nil {
			if key, first, err = s.KeyOf(ctx, it); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		// Done and Snooze act on the item from the notification; a summary
		// of several missed reminders drops them.
		msg := notify.Message{
			Title:   truncTitle(it.Title),
			URL:     fmt.Sprintf("%s/?item=%s", s.publicURL, url.QueryEscape(link)),
			Tag:     link,
			Channel: notify.ChannelReminders,
			Key:     link,
			Actions: s.buttons(ctx, link),
		}

		lead := hubLead
		if it.RemindBefore != nil {
			lead = *it.RemindBefore
		} else if !leadOK {
			lead = 0
		}
		if at, ok := earlyTime(it, time.Duration(lead)*time.Minute); ok && !at.After(now) && it.DueAt.After(now) {
			sent, err := s.record(ctx, `reminder_early (item_key, due_at)`, key, *it.DueAt)
			switch {
			case err != nil:
				errs = append(errs, err)
			case sent && !arrivedLate(first, at):
				m := msg
				m.Body = "Due in " + span(it.DueAt.Sub(now))
				out = append(out, due{m, now.Sub(at) > Late})
			}
		}

		at, snoozed, ok := fireTime(it, now)
		if !ok || at.After(now) {
			continue
		}
		sent, err := s.record(ctx, `reminder_fired (item_key, fire_at)`, key, at)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// An item the hub first saw more than Late after its moment, such as
		// an old bead on a bridge's first sync, is recorded silently.
		if !sent || arrivedLate(first, at) {
			continue
		}
		m, late := msg, now.Sub(at) > Late
		switch {
		case snoozed && late:
			m.Body = "Snoozed until " + span(now.Sub(at)) + " ago"
		case snoozed:
			m.Body = "Snoozed until now"
		case late:
			m.Body = "Due " + span(now.Sub(at)) + " ago"
		default:
			m.Body = "Due now"
		}
		out = append(out, due{m, late})
	}

	var late []notify.Message
	for _, d := range out {
		if d.late {
			late = append(late, d.msg)
			continue
		}
		if err := s.notifier.Notify(ctx, d.msg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.msg.Tag, err))
		}
	}
	if err := s.checkRuns(ctx, items, now); err != nil {
		errs = append(errs, err)
	}
	switch {
	case len(late) == 1:
		if err := s.notifier.Notify(ctx, late[0]); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", late[0].Tag, err))
		}
	case len(late) > 1:
		if err := s.notifier.Notify(ctx, s.missed(late)); err != nil {
			errs = append(errs, fmt.Errorf("missed reminders: %w", err))
		}
	}
	return errors.Join(errs...)
}

// buttons are a reminder's Done and Snooze, less any the item's list
// cannot take. A failed check keeps the button: the hub checks the action
// again when the button posts it.
func (s *Scheduler) buttons(ctx context.Context, key string) []notify.Action {
	all := []notify.Action{
		{Action: notify.ActionDone, Title: "Done"},
		{Action: notify.ActionSnooze, Title: "Snooze 1 hour"},
	}
	if s.Allows == nil {
		return all
	}
	var out []notify.Action
	for _, a := range all {
		if ok, err := s.Allows(ctx, key, a.Action); ok || err != nil {
			out = append(out, a)
		}
	}
	return out
}

// record notes that the reminder for key at t was sent, and reports whether
// it is new. Times are whole seconds: bd stores times to the second, so a
// time the app sent with milliseconds comes back from the bridge without
// them.
func (s *Scheduler) record(ctx context.Context, into, key string, t time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO `+into+` VALUES (?, ?)`, key, t.Truncate(time.Second).UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// maxMissedTitles is how many titles the summary of missed reminders names.
const maxMissedTitles = 3

// missed is the one reminder for several that came due while reminders were
// off. It opens the app.
func (s *Scheduler) missed(late []notify.Message) notify.Message {
	var titles []string
	for i, m := range late {
		if i == maxMissedTitles {
			titles = append(titles, fmt.Sprintf("and %d more", len(late)-maxMissedTitles))
			break
		}
		titles = append(titles, m.Title)
	}
	return notify.Message{
		Title:   fmt.Sprintf("%d reminders came due while reminders were off", len(late)),
		Body:    strings.Join(titles, ", "),
		URL:     s.publicURL + "/",
		Tag:     MissedTag,
		Channel: notify.ChannelReminders,
	}
}

// span says a duration in words, in its two largest units: "1 minute",
// "2 hours 30 minutes", "3 days". It rounds to the minute and says at least
// a minute.
func span(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 1 {
		m = 1
	}
	days, hours, mins := m/(24*60), m/60%24, m%60
	switch {
	case days > 0 && hours > 0:
		return count(days, "day") + " " + count(hours, "hour")
	case days > 0:
		return count(days, "day")
	case hours > 0 && mins > 0:
		return count(hours, "hour") + " " + count(mins, "minute")
	case hours > 0:
		return count(hours, "hour")
	}
	return count(mins, "minute")
}

func count(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
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
