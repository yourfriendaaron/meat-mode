package bridge

import (
	"errors"
	"net/http"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// hubPass is what the epics and icon passes share: the state of each repo
// the pass covers, a pause on a hub without the pass's route, and one
// handling of the hub's reply. Each pass keeps its own read and hash; X is
// what it keeps of a repo beyond these.
type hubPass[X any] struct {
	what       string        // "epics" or "icons", for the log
	backoff    time.Duration // how long a hub without the route gets nothing
	repos      map[string]*passRepo[X]
	offUntil   time.Time
	goneLogged bool
}

// passRepo is what a pass knows of one repo.
type passRepo[X any] struct {
	dir     string
	tracker tracker.Tracker
	readAt  time.Time // the last read
	took    bool      // the hub took a value since the start or the last 409
	sent    string    // hash of the value it took
	sentAt  time.Time
	// refused holds the hashes the hub refused (400 or 413); none is sent
	// again, and each is logged once.
	refused map[string]bool
	more    X
}

// begin keeps the state of the repos named and drops the rest, switched off
// or gone, and reports whether the pass sends at all this round: not while
// a hub without its route waits out the backoff.
func (p *hubPass[X]) begin(names []string, now time.Time) bool {
	keep := make(map[string]bool, len(names))
	for _, name := range names {
		keep[name] = true
	}
	if p.repos == nil {
		p.repos = map[string]*passRepo[X]{}
	}
	for name := range p.repos {
		if !keep[name] {
			delete(p.repos, name)
		}
	}
	return !now.Before(p.offUntil)
}

// due returns the repo's state, fresh when its folder or tracker changed,
// and whether a read is due: none was made in the last every. A due read is
// recorded as made now.
func (p *hubPass[X]) due(name string, r Repo, now time.Time, every time.Duration) (*passRepo[X], bool) {
	s := p.repos[name]
	if s == nil || s.dir != r.Dir || s.tracker != r.Tracker {
		s = &passRepo[X]{dir: r.Dir, tracker: r.Tracker, refused: map[string]bool{}}
		p.repos[name] = s
	}
	if !s.readAt.IsZero() && now.Sub(s.readAt) < every {
		return s, false
	}
	s.readAt = now
	return s, true
}

// unchanged reports whether the value with hash sum need not go: the hub
// refused it, or took it less than resend ago.
func (s *passRepo[X]) unchanged(sum string, now time.Time, resend time.Duration) bool {
	return s.refused[sum] || (s.took && sum == s.sent && now.Sub(s.sentAt) < resend)
}

// hubReply is how a pass treats the hub's answer to a send.
type hubReply int

const (
	replyOK       hubReply = iota
	replyGone              // 404: the hub has no route for the pass
	replyConflict          // 409: the hub's repo list is not the bridge's
	replyRefused           // 400 or 413: the hub refused this value
	replyFailed            // anything else
)

func classify(err error) hubReply {
	var h *HTTPError
	switch {
	case err == nil:
		return replyOK
	case errors.As(err, &h) && h.Status == http.StatusNotFound:
		return replyGone
	case errors.As(err, &h) && h.Status == http.StatusConflict:
		return replyConflict
	case refusedSet(err):
		return replyRefused
	}
	return replyFailed
}

// settle applies the hub's answer to a send of the value with hash sum.
// stop ends the pass for this round; a non-nil error is what the pass
// returns. A 404 pauses the pass for its backoff and a 409 makes the next
// round report the repos again; both look at the repo again on the next
// round the pass runs. Any other failure waits for the next due read.
func (p *hubPass[X]) settle(b *Bridge, name string, s *passRepo[X], sum string, err error) (stop bool, _ error) {
	switch classify(err) {
	case replyOK:
		s.took, s.sent, s.sentAt = true, sum, b.now()
	case replyGone:
		if !p.goneLogged {
			b.log().Warn("the hub does not take "+p.what+"; sending none to it for an hour", "err", err)
			p.goneLogged = true
		}
		s.readAt = time.Time{}
		p.offUntil = b.now().Add(p.backoff)
		return true, nil
	case replyConflict:
		b.log().Warn("the hub does not hold this repo as reported; reporting the repos again", "pass", p.what, "repo", name, "err", err)
		b.reposSent = nil
		s.took, s.sent, s.readAt = false, "", time.Time{}
	case replyRefused:
		b.log().Warn("the hub refused this repo's "+p.what+"; not sending the same again", "repo", name, "hash", sum, "err", err)
		s.refused[sum] = true
	default:
		return true, hubError{err}
	}
	return false, nil
}
