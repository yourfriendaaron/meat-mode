package backlog

import "testing"

// TestSweepDescs: a read keeps only its own repo's current cards cached.
func TestSweepDescs(t *testing.T) {
	tr := &Tracker{descs: map[string]descCache{
		descKey("/garden", "task-1"):  {desc: "kept"},
		descKey("/garden", "task-2"):  {desc: "no longer a card"},
		descKey("/kitchen", "task-2"): {desc: "another repo"},
	}}
	tr.sweepDescs("/garden", map[string]bool{descKey("/garden", "task-1"): true})
	if _, ok := tr.descs[descKey("/garden", "task-2")]; ok {
		t.Error("a task that is no longer a card stayed cached")
	}
	if len(tr.descs) != 2 {
		t.Errorf("cache holds %d entries, want 2", len(tr.descs))
	}
}
