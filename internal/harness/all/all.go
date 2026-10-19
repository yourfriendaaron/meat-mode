// Package all registers every harness the bridge ships. A new harness adds
// one blank import here; nothing else in the bridge, hub or app changes.
package all

import (
	_ "github.com/yourfriendaaron/meat-mode/internal/harness/codex"
)
