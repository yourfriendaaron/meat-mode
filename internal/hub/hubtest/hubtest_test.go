package hubtest

import (
	"testing"
)

func TestSeed(t *testing.T) {
	h := New(t, Options{})
	h.Seed(t)
	st, err := h.Store.State()
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, it := range st.Items {
		keys[it.Key] = true
	}
	for _, k := range []string{HumanKey, PlainKey, h.PersonalKey} {
		if !keys[k] {
			t.Errorf("state lacks %s: %v", k, keys)
		}
	}
	e, err := h.Store.Epics()
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Repos) != 1 || e.Repos[0].Repo != Repo || len(e.Repos[0].Rows) != len(EpicRows()) {
		t.Errorf("epics %+v", e)
	}
}
