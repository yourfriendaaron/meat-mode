package tracker

import (
	"slices"
	"strings"
)

// ChildEnv returns env for a tracker's command without the bridge's own
// MEAT_MODE_* settings, the hub token among them, so the command and any
// hook it runs never see them. Each entry of drop is another variable to
// leave out, such as one that would point the tracker at another folder.
func ChildEnv(env []string, drop ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(k, "MEAT_MODE_") && !slices.Contains(drop, k) {
			out = append(out, kv)
		}
	}
	return out
}
