package store

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// laptopLimits is a bridge that reports its limits and ceilings.
var laptopLimits = api.RunLimits{
	AgentLimits: api.AgentLimits{BudgetUSD: 2, TimeoutSeconds: 1800, RunsPerTask: 10, Concurrent: 2},
	Defaults:    api.AgentLimits{BudgetUSD: 2, TimeoutSeconds: 1800, RunsPerTask: 10, Concurrent: 2},
	Max:         api.AgentLimits{BudgetUSD: 20, TimeoutSeconds: 14400, RunsPerTask: 50, Concurrent: 5},
}

func helloWithLimits(t *testing.T, s *Store, l *api.RunLimits) {
	t.Helper()
	must(t, s.Hello("laptop", api.Hello{Version: "1.0.0", Harness: &api.HarnessInfo{
		Name: "claude", Title: "Claude Code", Budget: api.BudgetNative, BudgetUSD: 2, TimeoutSeconds: 1800, Limits: l,
	}}))
}

func TestLimitsActionShowsPending(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	l := laptopLimits
	l.Set = []string{api.LimitConcurrent}
	l.Concurrent = 4
	helloWithLimits(t, s, &l)
	budget, runs := 7.5, 20
	a, created, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop",
		Payload: api.Payload{Limits: &api.LimitsChange{BudgetUSD: &budget, RunsPerTask: &runs, Reset: []string{api.LimitConcurrent}}}}, "alex-app")
	must(t, err)
	if !created || a.Status != api.StatusPending || a.Bridge != "laptop" || a.Repo != "" {
		t.Fatalf("action = %+v", a)
	}
	pending, err := s.PendingActions("laptop")
	must(t, err)
	if len(pending) != 1 || pending[0].Kind != api.KindLimits || pending[0].Payload.Limits == nil || *pending[0].Payload.Limits.BudgetUSD != 7.5 {
		t.Fatalf("pending = %+v", pending)
	}
	st, err := s.State()
	must(t, err)
	b := st.Bridges[0]
	if !b.LimitsPending || b.Harness.BudgetUSD != 7.5 || b.Harness.Limits.RunsPerTask != 20 || b.Harness.Limits.Concurrent != 2 ||
		!slices.Equal(b.Harness.Limits.Set, []string{api.LimitBudget, api.LimitRunsPerTask}) {
		t.Errorf("bridge = %+v, limits %+v", b, b.Harness.Limits)
	}
	// The stored harness is what the bridge said, not the pending view.
	bs, err := s.Bridges()
	must(t, err)
	if !reflect.DeepEqual(*bs[0].Harness.Limits, l) || bs[0].LimitsPending {
		t.Errorf("stored limits = %+v", bs[0].Harness.Limits)
	}
	must(t, s.ReportResult("laptop", id1, api.ActionResult{OK: true}))
	st, err = s.State()
	must(t, err)
	if st.Bridges[0].LimitsPending {
		t.Error("still pending after the result")
	}
}

func TestLimitsActionRefusals(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	f := func(v float64) *float64 { return &v }
	i := func(v int) *int { return &v }
	// The bridge has said hello, but without limits: it is too old.
	must(t, s.Hello("laptop", api.Hello{Version: "1.0.0", Harness: &api.HarnessInfo{Name: "claude"}}))
	_, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop",
		Payload: api.Payload{Limits: &api.LimitsChange{Concurrent: i(3)}}}, "alex-app")
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "too old") {
		t.Errorf("an older bridge: err = %v", err)
	}
	helloWithLimits(t, s, &laptopLimits)
	for name, tc := range map[string]struct {
		na   api.NewAction
		want string
	}{
		"no payload":         {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop"}, "payload.limits"},
		"nothing to change":  {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{}}}, "a limit to set"},
		"unknown reset":      {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{Reset: []string{"tokens"}}}}, "unknown limit"},
		"set and reset":      {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{Concurrent: i(3), Reset: []string{api.LimitConcurrent}}}}, "both"},
		"the hub":            {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: HubBridge, Payload: api.Payload{Limits: &api.LimitsChange{Concurrent: i(3)}}}, "no agents"},
		"a repo":             {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Repo: "garden", Payload: api.Payload{Limits: &api.LimitsChange{Concurrent: i(3)}}}, "machine"},
		"budget over":        {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{BudgetUSD: f(20.01)}}}, api.EnvMaxBudget},
		"budget zero":        {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{BudgetUSD: f(0)}}}, "more than $0"},
		"timeout over":       {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{TimeoutSeconds: i(14401)}}}, "4 hours"},
		"timeout under":      {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{TimeoutSeconds: i(59)}}}, "1 minute"},
		"runs per task over": {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{RunsPerTask: i(51)}}}, api.EnvMaxRunsPerTask},
		"concurrent zero":    {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{Concurrent: i(0)}}}, "1 or more"},
		"concurrent over":    {api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop", Payload: api.Payload{Limits: &api.LimitsChange{Concurrent: i(6)}}}, "at most 5 on laptop"},
	} {
		_, _, err := s.AppendAction(tc.na, "alex-app")
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want 400 naming %q", name, err, tc.want)
		}
	}
	if _, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "shed",
		Payload: api.Payload{Limits: &api.LimitsChange{Concurrent: i(3)}}}, "alex-app"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown machine: err = %v", err)
	}
	// At the ceiling is fine.
	if _, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindLimits, Bridge: "laptop",
		Payload: api.Payload{Limits: &api.LimitsChange{BudgetUSD: f(20), TimeoutSeconds: i(14400), RunsPerTask: i(50), Concurrent: i(5)}}}, "alex-app"); err != nil {
		t.Errorf("at the ceilings: %v", err)
	}
}

func TestHarnessLimitsAreBounded(t *testing.T) {
	in := &api.HarnessInfo{Name: "pi", Limits: &api.RunLimits{
		AgentLimits: api.AgentLimits{BudgetUSD: -1, TimeoutSeconds: -5, RunsPerTask: -1, Concurrent: 2},
		Max:         api.AgentLimits{BudgetUSD: 1e12, TimeoutSeconds: 60, RunsPerTask: 3, Concurrent: 2},
		Set:         []string{"concurrent", "tokens", api.LimitBudget},
	}}
	got := cleanHarness(in).Limits
	want := &api.RunLimits{
		AgentLimits: api.AgentLimits{Concurrent: 2},
		Max:         api.AgentLimits{BudgetUSD: maxBudgetUSD, TimeoutSeconds: 60, RunsPerTask: 3, Concurrent: 2},
		Set:         []string{api.LimitBudget, api.LimitConcurrent},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("limits = %+v, want %+v", got, want)
	}
}
