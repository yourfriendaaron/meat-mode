package hub

import (
	"context"
	"time"
)

// linkedEvery is how often the hub checks whether a linked list should show
// another repo's items (docs/api.md, Linked lists). A bridge going quiet
// changes no row, so nothing else would tell the app.
const linkedEvery = 30 * time.Second

// linkedCheckEvery is linkedEvery, or a quarter of a shorter online window
// so a test's window is noticed in time.
func linkedCheckEvery(window time.Duration) time.Duration {
	if window > 0 && window/4 < linkedEvery {
		return max(window/4, 100*time.Millisecond)
	}
	return linkedEvery
}

// watchLinked tells the app to fetch the state and epics again whenever a
// linked list's source, or which of its repos are online, changes, until
// ctx ends.
func (s *Server) watchLinked(ctx context.Context, every time.Duration) {
	last, err := s.store.LinkedSources()
	if err != nil {
		s.log.Warn("could not read the linked lists", "err", err)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now, err := s.store.LinkedSources()
		if err != nil {
			s.log.Warn("could not read the linked lists", "err", err)
			continue
		}
		if now != last {
			last = now
			s.events.publish()
			s.events.publishEpics()
		}
	}
}
