package store

import (
	"encoding/json"
	"math"
	"slices"
	"strings"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// Limits on what a bridge says about its harness. Longer text is cut, as
// with its trackers: a long field must not stop a hello.
const (
	maxHarnessNameLen = 64
	maxInstructionLen = 128
	maxBudgetUSD      = 1e6
)

// cleanHarness trims and bounds a hello's harness and returns nil for none
// or one without a name, which the app shows as Claude Code with no limits.
// A budget mode it does not know reads as unknown, and a cap or time limit
// below zero as none.
func cleanHarness(h *api.HarnessInfo) *api.HarnessInfo {
	if h == nil {
		return nil
	}
	out := api.HarnessInfo{
		Name:            truncRunes(strings.TrimSpace(h.Name), maxHarnessNameLen),
		Title:           truncRunes(strings.TrimSpace(h.Title), maxHarnessNameLen),
		Version:         truncRunes(strings.TrimSpace(h.Version), maxVersionLen),
		InstructionFile: truncRunes(strings.TrimSpace(h.InstructionFile), maxInstructionLen),
		TimeoutSeconds:  max(h.TimeoutSeconds, 0),
		Steer:           h.Steer,
	}
	if out.Name == "" {
		return nil
	}
	switch h.Budget {
	case api.BudgetNative, api.BudgetMetered:
		out.Budget = h.Budget
		if h.BudgetUSD > 0 && !math.IsInf(h.BudgetUSD, 0) {
			out.BudgetUSD = min(h.BudgetUSD, maxBudgetUSD)
		}
	case api.BudgetNone:
		out.Budget = h.Budget
	}
	out.Limits = cleanRunLimits(h.Limits)
	return &out
}

// cleanRunLimits bounds a hello's limits: a value below zero, or a budget
// that is not a finite amount, reads as zero, and Set keeps only the names
// of limits. A ceiling of zero lets the app set nothing for that limit.
func cleanRunLimits(l *api.RunLimits) *api.RunLimits {
	if l == nil {
		return nil
	}
	clean := func(v api.AgentLimits) api.AgentLimits {
		if !(v.BudgetUSD > 0) || math.IsInf(v.BudgetUSD, 0) {
			v.BudgetUSD = 0
		}
		v.BudgetUSD = min(v.BudgetUSD, maxBudgetUSD)
		v.TimeoutSeconds = max(v.TimeoutSeconds, 0)
		v.RunsPerTask = max(v.RunsPerTask, 0)
		v.Concurrent = max(v.Concurrent, 0)
		return v
	}
	out := api.RunLimits{AgentLimits: clean(l.AgentLimits), Defaults: clean(l.Defaults), Max: clean(l.Max)}
	for _, n := range api.LimitNames {
		if slices.Contains(l.Set, n) {
			out.Set = append(out.Set, n)
		}
	}
	return &out
}

// encodeHarness is the harness column for a hello's harness, empty for
// none.
func encodeHarness(h *api.HarnessInfo) (string, error) {
	h = cleanHarness(h)
	if h == nil {
		return "", nil
	}
	b, err := json.Marshal(h)
	return string(b), err
}

func decodeHarness(s string) (*api.HarnessInfo, error) {
	if s == "" {
		return nil, nil
	}
	var h api.HarnessInfo
	if err := json.Unmarshal([]byte(s), &h); err != nil {
		return nil, err
	}
	return cleanHarness(&h), nil
}
