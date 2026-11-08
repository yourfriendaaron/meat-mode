package hub

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func (b *broker) revision() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rev
}

// TestWatchLinkedPublishesWhenTheSourceMoves checks that a linked list
// moving to another machine's copy, which changes no row, still tells the
// app to fetch the state again.
func TestWatchLinkedPublishesWhenTheSourceMoves(t *testing.T) {
	h := newHarness(t, nil, false)
	h.st.SetOnlineWindow(400 * time.Millisecond)
	id := strings.Repeat("cd", api.TrackerIDLen/2)
	for _, b := range []string{"desk", "laptop"} {
		if _, err := h.st.CreateBridge(b); err != nil {
			t.Fatal(err)
		}
		if err := h.st.ReplaceRepos(b, []api.Repo{{Name: "garden", OptedIn: true, TrackerID: id}}); err != nil {
			t.Fatal(err)
		}
		if err := h.st.Touch(b); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := h.srv.events.revision()
	go h.srv.watchLinked(ctx, 50*time.Millisecond)
	// laptop keeps reporting; desk, the source, goes quiet.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := h.st.Touch("laptop"); err != nil {
			t.Fatal(err)
		}
		if h.srv.events.revision() > start {
			st, err := h.st.State()
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range st.Repos {
				if r.Bridge == "desk" && !r.Standby {
					t.Fatal("published, but desk is still the source")
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no event after the source moved")
}

func TestLinkedCheckEvery(t *testing.T) {
	for window, want := range map[time.Duration]time.Duration{
		0:                linkedEvery,
		10 * time.Minute: linkedEvery,
		2 * time.Second:  500 * time.Millisecond,
		time.Millisecond: 100 * time.Millisecond,
	} {
		if got := linkedCheckEvery(window); got != want {
			t.Errorf("linkedCheckEvery(%v) = %v, want %v", window, got, want)
		}
	}
}
