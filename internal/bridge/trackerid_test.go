package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/fake"
)

// idTracker is a tracker whose store has the identity id.
type idTracker struct {
	tracker.Tracker
	id string
}

func (t idTracker) StoreID(string) string { return t.id }

// TestTrackerID checks that the bridge sends a hash of the tracker's name
// and its store's identity, never the identity itself, and nothing for a
// tracker or store without one.
func TestTrackerID(t *testing.T) {
	plain := fake.New().Tracker()
	sum := sha256.Sum256([]byte("fake:0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a51"))
	if got := trackerID(Repo{Tracker: idTracker{plain, "0b6f4a8e-1c2d-4e5f-8a9b-0c1d2e3f4a51"}}); got != hex.EncodeToString(sum[:]) {
		t.Errorf("trackerID = %q, want the hash", got)
	}
	if got := trackerID(Repo{Tracker: idTracker{plain, ""}}); got != "" {
		t.Errorf("store without an identity: %q", got)
	}
	if got := trackerID(Repo{Tracker: plain}); got != "" {
		t.Errorf("tracker without StoreID: %q", got)
	}
}
