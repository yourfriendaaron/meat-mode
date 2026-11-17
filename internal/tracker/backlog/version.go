package backlog

import (
	"fmt"

	"github.com/yourfriendaaron/meat-mode/internal/tracker"
)

// checkVersion returns an error when v is older than MinVersion or cannot be
// read. A pre-release counts as older than its release.
func checkVersion(v string) error {
	cmp, ok := tracker.CompareVersion(v, MinVersion)
	if !ok {
		return fmt.Errorf("cannot read the backlog version %q", v)
	}
	if cmp < 0 {
		return fmt.Errorf("backlog %s is too old; Meat Mode needs Backlog.md %s or newer", v, MinVersion)
	}
	return nil
}
