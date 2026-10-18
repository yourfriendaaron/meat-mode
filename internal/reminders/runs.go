package reminders

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/notify"
)

// RunLate is how long after a run ended the hub may first see the end and
// still push it. An older end, such as one a bridge reports on its first
// sync with a new hub, is recorded silently.
const RunLate = time.Hour

// RunTagPrefix starts the tag of an agent push, so a run's end replaces the
// item's earlier one but not its reminder.
const RunTagPrefix = "agent:"

// runTag is the tag of an item's agent pushes: the prefix plus the first 16
// hex characters of the key's SHA-256. A key can fill most of Web Push's
// 4 KiB payload on its own, so the tag stays short; it is stable per item,
// so a later end still replaces an earlier one.
func runTag(key string) string {
	sum := sha256.Sum256([]byte(key))
	return RunTagPrefix + hex.EncodeToString(sum[:8])
}

// reasonWords says why a run stopped, after "<agent> stopped: ".
var reasonWords = map[string]string{
	api.ReasonUsageLimit: "usage limit reached",
	api.ReasonTimeout:    "out of time",
	api.ReasonBudget:     "budget spent",
	api.ReasonRunLimit:   "no runs left on this task",
	api.ReasonBusy:       "too many agents running",
	api.ReasonNotAllowed: "agents are not allowed in this repo",
	api.ReasonNoReply:    "ended without a reply",
	api.ReasonFailed:     "the agent command failed",
	api.ReasonCrashed:    "the agent crashed",
	api.ReasonRestarted:  "the bridge restarted",
	api.ReasonOffline:    "the machine stopped reporting",
}

func createRunTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS run_pushed (
		item_key TEXT NOT NULL,
		run_id   TEXT NOT NULL,
		state    TEXT NOT NULL,
		PRIMARY KEY (item_key, run_id, state)
	)`)
	return err
}

// Wake asks Run to check agent runs now, as when a bridge reported its run
// list. It never blocks.
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// CheckRuns sends a push for each run end it has not pushed yet.
func (s *Scheduler) CheckRuns(ctx context.Context) error {
	items, err := s.src.Items(ctx)
	if err != nil {
		return err
	}
	return s.checkRuns(ctx, items, s.clock.Now())
}

// checkRuns sends one push per run end, keyed on the item, the run's id and
// its state. An end is recorded even when it sends nothing: a run the user
// stopped, one on an item the user closed in the app after the run started,
// or one older than RunLate. A working run, the hub's provisional ones
// included, never pushes.
func (s *Scheduler) checkRuns(ctx context.Context, items []api.Item, now time.Time) error {
	var errs []error
	for _, it := range items {
		r := it.Run
		if r == nil || it.Key == "" {
			continue
		}
		switch r.State {
		case api.RunWaiting, api.RunDone, api.RunStopped:
		default:
			continue
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM run_pushed WHERE item_key = ? AND run_id = ? AND state = ?`,
			it.Key, r.ID, r.State).Scan(&n); err != nil {
			errs = append(errs, err)
			continue
		}
		if n > 0 {
			continue
		}
		quiet := r.Reason == api.ReasonStopped || (r.EndedAt != nil && now.Sub(*r.EndedAt) > RunLate)
		if !quiet && s.ClosedInApp != nil {
			closed, err := s.ClosedInApp(ctx, it.Key, r.StartedAt)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			quiet = closed
		}
		res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO run_pushed VALUES (?, ?, ?)`, it.Key, r.ID, r.State)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if added, err := res.RowsAffected(); err != nil || added == 0 || quiet {
			continue
		}
		m := s.runMessage(it)
		if err := s.notifier.Notify(ctx, m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Tag, err))
		}
	}
	return errors.Join(errs...)
}

// runMessage is the push for the end of an item's run: "<agent> replied",
// "<agent> finished" or "<agent> stopped: <reason>", with the item's title
// as the body. Reply and Open open the item, Reply at its note box; a
// stopped run also offers Retry.
func (s *Scheduler) runMessage(it api.Item) notify.Message {
	r := it.Run
	agent := strings.TrimSpace(r.Agent)
	if agent == "" {
		agent = "The agent"
	}
	agent = truncTitle(agent)
	link := fmt.Sprintf("%s/?item=%s", s.publicURL, url.QueryEscape(it.Key))
	m := notify.Message{
		Body:    truncTitle(it.Title),
		URL:     link,
		Tag:     runTag(it.Key),
		Key:     it.Key,
		Channel: notify.ChannelAgents,
		Actions: []notify.Action{
			{Action: notify.ActionReply, Title: "Reply", URL: link + "&reply=1"},
			{Action: notify.ActionOpen, Title: "Open", URL: link},
		},
	}
	switch r.State {
	case api.RunWaiting:
		m.Title = agent + " replied"
	case api.RunDone:
		m.Title = agent + " finished"
	default:
		m.Title = agent + " stopped"
		if w, ok := reasonWords[r.Reason]; ok {
			m.Title += ": " + w
		}
		if d := strings.TrimSpace(r.Detail); d != "" {
			m.Body += "\n" + truncTitle(d)
		}
		m.Actions = append(m.Actions, notify.Action{Action: notify.ActionRetry, Title: "Retry"})
	}
	return m
}
