// Package all registers every tracker the bridge ships. A new tracker adds
// one blank import here; nothing else in the bridge, hub or app changes.
package all

import (
	_ "github.com/yourfriendaaron/meat-mode/internal/tracker/backlog"
	_ "github.com/yourfriendaaron/meat-mode/internal/tracker/beads"
	_ "github.com/yourfriendaaron/meat-mode/internal/tracker/ticket"
)
