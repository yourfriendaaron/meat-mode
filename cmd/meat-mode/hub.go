package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/hub"
	"github.com/yourfriendaaron/meat-mode/internal/notify"
	"github.com/yourfriendaaron/meat-mode/internal/reminders"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// runHub runs the hub with settings from MEAT_MODE_* environment variables:
// the store and API, Web Push and ntfy, and the reminder scheduler.
func runHub(args []string) error {
	if len(args) > 0 {
		return errors.New("hub takes no arguments; it reads MEAT_MODE_* environment variables")
	}
	var notifier notify.Notifier
	return hub.Run(context.Background(), hub.ConfigFromEnv(), hub.Options{
		Push: func(cfg hub.Config, st *store.Store) (hub.PushRegistry, error) {
			reg, n, err := notify.FromEnv(cfg.DataDir, st.DB())
			if err != nil {
				return nil, err
			}
			// The password can be set in the app while the hub runs.
			reg.RequireSession = func() bool {
				if cfg.PasswordHash != "" {
					return true
				}
				h, err := st.Settings()
				return err != nil || h.PasswordHash != ""
			}
			notifier = n
			return reg, nil
		},
		OnStart: func(ctx context.Context, cfg hub.Config, st *store.Store) error {
			s, err := reminders.New(st.DB(), stateItems{st}, notifier, reminders.SystemClock{}, cfg.PublicURL)
			if err != nil {
				return err
			}
			if v := os.Getenv("MEAT_MODE_REMINDER_INTERVAL"); v != "" {
				// End-to-end tests check reminders every second.
				d, err := time.ParseDuration(v)
				if err != nil || d <= 0 {
					return fmt.Errorf("MEAT_MODE_REMINDER_INTERVAL must be a positive duration, not %q", v)
				}
				s.Every = d
			}
			s.KeyOf = st.ReminderKey
			s.Lead = stateItems{st}
			go s.Run(ctx)
			return nil
		},
	})
}

// stateItems feeds the scheduler the items as the app shows them, so a
// snooze takes effect before the bridge has replayed it.
type stateItems struct{ st *store.Store }

func (s stateItems) Items(context.Context) ([]api.Item, error) {
	state, err := s.st.State()
	return state.Items, err
}

// ReminderLead gives the scheduler the lead time kept in the app.
func (s stateItems) ReminderLead(context.Context) (int, error) {
	h, err := s.st.Settings()
	return h.ReminderLead, err
}
