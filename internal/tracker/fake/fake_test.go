package fake_test

import (
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
