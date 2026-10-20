package api

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// The names of the four limits on agent runs, as LimitsChange.Reset and
// RunLimits.Set name them.
const (
	LimitBudget      = "budgetUsd"
	LimitTimeout     = "timeoutSeconds"
	LimitRunsPerTask = "runsPerTask"
	LimitConcurrent  = "concurrent"
)

// LimitNames lists the limits in the order the app shows them.
var LimitNames = []string{LimitBudget, LimitTimeout, LimitRunsPerTask, LimitConcurrent}

// The floors of a limit the app sets. A budget must also be above zero.
const (
	MinTimeoutSeconds = 60
	MinCount          = 1
	MaxLimitBudgetUSD = 1e6
)

// The variables on the agent machine that set each limit's ceiling.
const (
	EnvMaxBudget      = "MEAT_MODE_AGENT_MAX_BUDGET_USD"
	EnvMaxTimeout     = "MEAT_MODE_AGENT_MAX_TIMEOUT"
	EnvMaxRunsPerTask = "MEAT_MODE_AGENT_MAX_RUNS_PER_TASK"
	EnvMaxConcurrent  = "MEAT_MODE_AGENT_MAX_CONCURRENT"
)

// AgentLimits are the four limits on agent runs on one machine.
type AgentLimits struct {
	BudgetUSD      float64 `json:"budgetUsd"`      // the spending cap for one run, in US dollars
	TimeoutSeconds int     `json:"timeoutSeconds"` // the time limit for one run
	RunsPerTask    int     `json:"runsPerTask"`    // runs one task may have in its lifetime
	Concurrent     int     `json:"concurrent"`     // runs live at once on the machine
}

// RunLimits are a machine's limits on agent runs as its bridge reports them:
// those in effect for a run that starts now, the machine's own defaults, and
// the ceilings the app can set them up to.
type RunLimits struct {
	AgentLimits
	Defaults AgentLimits `json:"defaults"`
	Max      AgentLimits `json:"max"`
	// Set names the limits the app has set; the others are the defaults.
	Set []string `json:"set,omitempty"`
}

// LimitsChange is the payload of a limits action. A field that is set
// changes that limit; Reset names limits that go back to the machine's
// default. A limit named in neither stays as it is.
type LimitsChange struct {
	BudgetUSD      *float64 `json:"budgetUsd,omitempty"`
	TimeoutSeconds *int     `json:"timeoutSeconds,omitempty"`
	RunsPerTask    *int     `json:"runsPerTask,omitempty"`
	Concurrent     *int     `json:"concurrent,omitempty"`
	Reset          []string `json:"reset,omitempty"`
}

// has reports whether the change sets the named limit.
func (c LimitsChange) has(name string) bool {
	switch name {
	case LimitBudget:
		return c.BudgetUSD != nil
	case LimitTimeout:
		return c.TimeoutSeconds != nil
	case LimitRunsPerTask:
		return c.RunsPerTask != nil
	case LimitConcurrent:
		return c.Concurrent != nil
	}
	return false
}

// CleanLimitsChange checks a change's shape: it changes something, resets
// only known limits, each once, and never sets and resets the same one.
func CleanLimitsChange(c LimitsChange) (LimitsChange, error) {
	var reset []string
	for _, n := range c.Reset {
		if !slices.Contains(LimitNames, n) {
			return c, fmt.Errorf("unknown limit %q; the limits are %s, %s, %s and %s", n, LimitBudget, LimitTimeout, LimitRunsPerTask, LimitConcurrent)
		}
		if c.has(n) {
			return c, fmt.Errorf("limit %s is both set and reset", n)
		}
		if !slices.Contains(reset, n) {
			reset = append(reset, n)
		}
	}
	c.Reset = reset
	if len(reset) == 0 && c.BudgetUSD == nil && c.TimeoutSeconds == nil && c.RunsPerTask == nil && c.Concurrent == nil {
		return c, errors.New("limits needs a limit to set or reset")
	}
	return c, nil
}

// CheckLimits refuses a value below its floor or above the machine's
// ceiling in max. machine names the agent machine in the message, which
// says how to raise the ceiling there.
func CheckLimits(c LimitsChange, max AgentLimits, machine string) error {
	over := func(what, ceiling, env string) error {
		return fmt.Errorf("%s can be at most %s on %s. To allow more, set %s on %s and restart its bridge", what, ceiling, machine, env, machine)
	}
	if v := c.BudgetUSD; v != nil {
		switch {
		case !(*v > 0) || *v > MaxLimitBudgetUSD:
			return errors.New("the spending cap must be more than $0")
		case *v > max.BudgetUSD:
			return over("the spending cap", DollarText(max.BudgetUSD)+" a run", EnvMaxBudget)
		}
	}
	if v := c.TimeoutSeconds; v != nil {
		switch {
		case *v < MinTimeoutSeconds:
			return errors.New("the time limit must be at least 1 minute")
		case *v > max.TimeoutSeconds:
			return over("the time limit", DurationText(max.TimeoutSeconds)+" a run", EnvMaxTimeout)
		}
	}
	for _, n := range []struct {
		v        *int
		what     string
		max      int
		env      string
		floorMsg string
	}{
		{c.RunsPerTask, "runs per task", max.RunsPerTask, EnvMaxRunsPerTask, "runs per task must be a whole number of 1 or more"},
		{c.Concurrent, "runs at once", max.Concurrent, EnvMaxConcurrent, "runs at once must be a whole number of 1 or more"},
	} {
		if n.v == nil {
			continue
		}
		switch {
		case *n.v < MinCount:
			return errors.New(n.floorMsg)
		case *n.v > n.max:
			return over(n.what, strconv.Itoa(n.max), n.env)
		}
	}
	return nil
}

// DollarText is a cap in dollars: "$2", "$2.50".
func DollarText(usd float64) string {
	if usd == float64(int64(usd)) {
		return "$" + strconv.FormatInt(int64(usd), 10)
	}
	return "$" + strconv.FormatFloat(usd, 'f', 2, 64)
}

// DurationText is a time limit in words: "45 seconds", "30 minutes",
// "1 hour 30 minutes", as the app writes it.
func DurationText(seconds int) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.Itoa(n) + " " + unit + "s"
	}
	if seconds < 60 {
		return plural(seconds, "second")
	}
	mins := (seconds + 30) / 60
	if mins < 60 {
		return plural(mins, "minute")
	}
	h := mins / 60
	if mins%60 != 0 {
		return plural(h, "hour") + " " + plural(mins%60, "minute")
	}
	return plural(h, "hour")
}
