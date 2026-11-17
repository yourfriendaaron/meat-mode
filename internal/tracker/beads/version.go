package beads

import (
	"fmt"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// MinVersion is the oldest bd the bridge works with.
const MinVersion = "1.3.0"

// CheckVersion returns an error when v is older than MinVersion or cannot be
// read. A pre-release counts as older than its release.
func CheckVersion(v string) error {
	cmp, ok := tracker.CompareVersion(v, MinVersion)
	if !ok {
		return fmt.Errorf("cannot read bd version %q", v)
	}
	if cmp < 0 {
		return fmt.Errorf("bd %s is too old; Meat Mode needs bd %s or newer", v, MinVersion)
	}
	return nil
}
