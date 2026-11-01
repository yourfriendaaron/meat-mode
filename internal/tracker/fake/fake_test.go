package fake_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/tracker"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/fake"
	"github.com/yourfriendaaron/meat-mode/internal/tracker/trackertest"
)

func repo(t *testing.T) string {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, fake.Store), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConformance(t *testing.T) {
	trackertest.Run(t, "fake", func(t *testing.T) (tracker.Tracker, string) {
		return fake.New().Tracker(), repo(t)
	})
}

// TestConformanceWithout runs the suite against a fake that lacks every
// optional capability, so it must answer each with ErrUnsupported.
func TestConformanceWithout(t *testing.T) {
	trackertest.Run(t, "plain", func(t *testing.T) (tracker.Tracker, string) {
		f := fake.New()
		f.Inf = tracker.Info{Name: "plain", Title: "Plain", Actions: []string{api.KindAdd}}
		return f.Tracker(), repo(t)
	})
}

func TestTrackerIsAgentOnlyWithAsk(t *testing.T) {
	f := fake.New()
	if _, ok := f.Tracker().(tracker.Agent); !ok {
		t.Fatal("a fake with ask is not an Agent")
	}
	f.Inf.Actions = slices.DeleteFunc(slices.Clone(f.Inf.Actions), func(c string) bool { return c == api.KindAsk })
	if _, ok := f.Tracker().(tracker.Agent); ok {
		t.Fatal("a fake without ask is an Agent")
	}
	if err := tracker.Validate("fake", f.Tracker()); err != nil {
		t.Fatal(err)
	}
}

func TestEpicRows(t *testing.T) {
	f := fake.New()
	if _, ok := f.Tracker().(tracker.Epics); ok {
		t.Fatal("a fake without Epics implements tracker.Epics")
	}
	f.Epics = true
	f.SetRows([]api.EpicRow{{ID: "fk-1", Title: "Plan the garden", Type: "epic", Status: "open"}}, nil)
	for _, ask := range []bool{true, false} {
		if !ask {
			f.Inf.Actions = slices.DeleteFunc(slices.Clone(f.Inf.Actions), func(k string) bool { return k == api.KindAsk })
		}
		e, ok := f.Tracker().(tracker.Epics)
		if !ok {
			t.Fatalf("ask=%v: the fake does not implement tracker.Epics", ask)
		}
		rows, err := e.EpicRows(t.Context(), "dir")
		if err != nil || len(rows) != 1 || rows[0].ID != "fk-1" {
			t.Fatalf("ask=%v: EpicRows = %+v, %v", ask, rows, err)
		}
		if _, isAgent := f.Tracker().(tracker.Agent); isAgent != ask {
			t.Fatalf("ask=%v but Agent = %v", ask, isAgent)
		}
	}
	f.SetRows(nil, errors.New("tool failed"))
	if _, err := f.Tracker().(tracker.Epics).EpicRows(t.Context(), "dir"); err == nil {
		t.Fatal("EpicRows did not return the error set")
	}
}
