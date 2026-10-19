package store

import (
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestHarnessRoundTrip(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	codex := api.HarnessInfo{Name: "codex", Title: "Codex", Version: "0.50.0", InstructionFile: "AGENTS.md",
		Budget: api.BudgetNone, TimeoutSeconds: 1800}
	must(t, s.Hello("laptop", api.Hello{Version: "1.0.0", Harness: &codex}))
	st, err := s.State()
	must(t, err)
	if len(st.Bridges) != 1 || st.Bridges[0].Harness == nil || *st.Bridges[0].Harness != codex {
		t.Fatalf("bridges = %+v, want harness %+v", st.Bridges, codex)
	}

	// A hello without a harness clears it: an older bridge runs Claude Code.
	must(t, s.Hello("laptop", api.Hello{Version: "1.0.0"}))
	bs, err := s.Bridges()
	must(t, err)
	if bs[0].Harness != nil {
		t.Errorf("harness after a plain hello = %+v", bs[0].Harness)
	}

	// Pairing the name again clears it too.
	must(t, s.Hello("laptop", api.Hello{Version: "1.0.0", Harness: &codex}))
	must(t, s.RevokeBridge("laptop"))
	if _, err := s.CreateBridge("laptop"); err != nil {
		t.Fatal(err)
	}
	bs, err = s.Bridges()
	must(t, err)
	if bs[0].Harness != nil {
		t.Errorf("harness after pairing again = %+v", bs[0].Harness)
	}
}

func TestHarnessIsBounded(t *testing.T) {
	long := strings.Repeat("x", 500)
	for _, tc := range []struct {
		name string
		in   *api.HarnessInfo
		want *api.HarnessInfo
	}{
		{"nil", nil, nil},
		{"no name", &api.HarnessInfo{Name: "  ", Title: "Codex"}, nil},
		{"trimmed", &api.HarnessInfo{Name: " pi ", Title: " Pi ", Version: " 1.2.0 ", InstructionFile: " AGENTS.md ",
			Budget: api.BudgetMetered, BudgetUSD: 2, TimeoutSeconds: 600, Steer: true},
			&api.HarnessInfo{Name: "pi", Title: "Pi", Version: "1.2.0", InstructionFile: "AGENTS.md",
				Budget: api.BudgetMetered, BudgetUSD: 2, TimeoutSeconds: 600, Steer: true}},
		{"long", &api.HarnessInfo{Name: long, Title: long, Version: long, InstructionFile: long},
			&api.HarnessInfo{Name: api.TruncRunes(long, 64), Title: api.TruncRunes(long, 64),
				Version: api.TruncRunes(long, 64), InstructionFile: api.TruncRunes(long, 128)}},
		{"unknown budget", &api.HarnessInfo{Name: "pi", Budget: "lots", BudgetUSD: 5}, &api.HarnessInfo{Name: "pi"}},
		{"none drops a cap", &api.HarnessInfo{Name: "pi", Budget: api.BudgetNone, BudgetUSD: 5},
			&api.HarnessInfo{Name: "pi", Budget: api.BudgetNone}},
		{"below zero", &api.HarnessInfo{Name: "pi", Budget: api.BudgetNative, BudgetUSD: -1, TimeoutSeconds: -5},
			&api.HarnessInfo{Name: "pi", Budget: api.BudgetNative}},
		{"huge cap", &api.HarnessInfo{Name: "pi", Budget: api.BudgetNative, BudgetUSD: 1e12},
			&api.HarnessInfo{Name: "pi", Budget: api.BudgetNative, BudgetUSD: maxBudgetUSD}},
	} {
		got := cleanHarness(tc.in)
		if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
			t.Errorf("%s: cleanHarness = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestMigrationTwelveAddsHarness(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	must(t, err)
	seed(t, s)
	_, err = s.DB().Exec(`ALTER TABLE bridges DROP COLUMN harness; PRAGMA user_version = 11`)
	must(t, err)
	must(t, s.Close())

	s, err = Open(dir)
	must(t, err)
	defer s.Close()
	st, err := s.State()
	must(t, err)
	if st.Bridges[0].Harness != nil {
		t.Errorf("harness after the migration = %+v", st.Bridges[0].Harness)
	}
}
