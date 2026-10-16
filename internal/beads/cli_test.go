package beads_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/beads"
	"github.com/yourfriendaaron/meat-mode/internal/beads/beadstest"
)

func TestCheckVersion(t *testing.T) {
	for v, ok := range map[string]bool{
		"1.3.0": true, "1.3.1": true, "v1.4": true, "2.0.0": true, "1.3.1 (Homebrew)": true,
		"1.2.9": false, "1.3.0-rc.1": false, "0.9": false, "": false, "abc": false,
	} {
		if err := beads.CheckVersion(v); (err == nil) != ok {
			t.Errorf("CheckVersion(%q) = %v, want ok=%v", v, err, ok)
		}
	}
}

func TestKeepHuman(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	cases := []struct {
		name     string
		i        beads.Issue
		personal bool
		want     bool
	}{
		{"labelled open", beads.Issue{Status: "open", Labels: []string{"human"}}, false, true},
		{"unlabelled open", beads.Issue{Status: "open"}, false, false},
		{"unlabelled personal", beads.Issue{Status: "deferred"}, true, true},
		{"recently closed", beads.Issue{Status: "closed", Labels: []string{"human"}, ClosedAt: &recent}, false, true},
		{"closed long ago", beads.Issue{Status: "closed", Labels: []string{"human"}, ClosedAt: &old}, false, false},
	}
	for _, c := range cases {
		if got := beads.KeepHuman(c.i, c.personal, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestFingerprintChanges(t *testing.T) {
	a := beads.Issue{ID: "x-1", Title: "Water the plants", Status: "open", Labels: []string{"b", "a"}}
	b := a
	b.Labels = []string{"a", "b"}
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("label order changed the fingerprint")
	}
	b.CommentCount = 1
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("a new comment did not change the fingerprint")
	}
}

// TestCLI runs every method against a real bd in a temp repo.
func TestCLI(t *testing.T) {
	dir := beadstest.NewRepo(t, "", "plants")
	ctx := context.Background()
	c := &beads.CLI{}

	v, err := c.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := beads.CheckVersion(v); err != nil {
		t.Fatal(err)
	}

	due := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	id, err := c.Create(ctx, dir, beads.NewIssue{Title: "-Water the plants", Due: &due, ActionID: "act-1", Actor: "alex-app"})
	if err != nil {
		t.Fatal(err)
	}
	beadstest.Run(t, dir, "bd", "create", "Agent chore", "--json")

	found, err := c.FindByActionID(ctx, dir, "act-1")
	if err != nil || found != id {
		t.Fatalf("FindByActionID = %q, %v; want %q", found, err, id)
	}
	if found, _ := c.FindByActionID(ctx, dir, "act-2"); found != "" {
		t.Fatalf("FindByActionID for unknown action = %q", found)
	}

	list, err := c.ListHuman(ctx, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("ListHuman = %+v", list)
	}
	if all, _ := c.ListHuman(ctx, dir, true); len(all) != 2 {
		t.Fatalf("personal ListHuman returned %d items, want 2", len(all))
	}
	fp := list[0].Fingerprint()

	if err := c.AddComment(ctx, dir, id, "alex-app", "--Bought soil"); err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	if err := c.Defer(ctx, dir, id, &until, "alex-app"); err != nil {
		t.Fatal(err)
	}
	list, _ = c.ListHuman(ctx, dir, false)
	if len(list) != 1 || list[0].Status != "deferred" || list[0].Fingerprint() == fp || list[0].CommentCount != 1 {
		t.Fatalf("after comment and defer: %+v", list)
	}

	got, err := c.Show(ctx, dir, id)
	if err != nil {
		t.Fatal(err)
	}
	it := got.Item()
	if it.Title != "-Water the plants" || !it.DueAt.Equal(due) || !it.DeferUntil.Equal(until) ||
		!slices.Contains(it.Labels, "human") || it.CreatedBy != "alex-app" {
		t.Fatalf("Item = %+v", it)
	}
	if len(it.Comments) != 1 || it.Comments[0].Author != "alex-app" || it.Comments[0].Text != "--Bought soil" {
		t.Fatalf("comments = %+v", it.Comments)
	}

	if err := c.Close(ctx, dir, id, "Done in Meat Mode", "alex-app"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx, dir, id, "Done in Meat Mode", "alex-app"); err != nil {
		t.Fatalf("second close: %v", err)
	}
	got, _ = c.Show(ctx, dir, id)
	if it := got.Item(); it.Status != "closed" || it.CloseReason != "Done in Meat Mode" || it.ClosedAt == nil {
		t.Fatalf("after close: %+v", it)
	}
	if list, _ := c.ListHuman(ctx, dir, false); len(list) != 1 {
		t.Fatalf("recently closed item missing from list: %+v", list)
	}

	if err := c.Reopen(ctx, dir, id, "alex-app"); err != nil {
		t.Fatal(err)
	}
	if err := c.Reopen(ctx, dir, id, "alex-app"); err != nil {
		t.Fatalf("second reopen: %v", err)
	}
	if err := c.Dismiss(ctx, dir, id, "alex-app"); err != nil {
		t.Fatal(err)
	}
	got, _ = c.Show(ctx, dir, id)
	if got.Status != "closed" || got.CloseReason == nil || *got.CloseReason != "Dismissed" {
		t.Fatalf("after dismiss: %+v", got)
	}
	if err := c.Dismiss(ctx, dir, id, "alex-app"); err == nil {
		t.Fatal("dismissing a closed bead should fail like bd does")
	}

	if _, err := c.Show(ctx, dir, "tst-nope"); !errors.Is(err, beads.ErrNotFound) {
		t.Fatalf("Show unknown id: %v", err)
	}
}

func TestCheckID(t *testing.T) {
	for id, ok := range map[string]bool{
		"tst-1": true, "tst-abc.1": true, "Water_2": true, "a": true,
		"": false, "-f/etc/hostname": false, "--mem-profile=x": false, ".hidden": false,
		"a b": false, "a/b": false, "a\nb": false, strings.Repeat("a", 129): false,
	} {
		if err := beads.CheckID(id); (err == nil) != ok {
			t.Errorf("CheckID(%q) = %v, want ok=%v", id, err, ok)
		}
	}
}

// TestCLIRefusesFlagIDs passes bead ids that bd would read as flags. None may
// reach bd as one: no file is read into a comment and none is written.
func TestCLIRefusesFlagIDs(t *testing.T) {
	dir := beadstest.NewRepo(t, "", "plants")
	ctx := context.Background()
	c := &beads.CLI{}
	id, err := c.Create(ctx, dir, beads.NewIssue{Title: "Water the plants", ActionID: "act-1", Actor: "alex-app"})
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	secret := filepath.Join(tmp, "secret")
	if err := os.WriteFile(secret, []byte("not for comments\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(tmp, "profile")
	bad := []string{"-f" + secret, "--mem-profile=" + profile, "-C" + tmp, "--db=" + profile}

	for _, b := range bad {
		// The note text is the real id, as a hostile hub could send.
		if err := c.AddComment(ctx, dir, b, "alex-app", id); err == nil {
			t.Errorf("AddComment(%q) succeeded", b)
		}
		if _, err := c.Show(ctx, dir, b); err == nil {
			t.Errorf("Show(%q) succeeded", b)
		}
		if err := c.Close(ctx, dir, b, "Done", "alex-app"); err == nil {
			t.Errorf("Close(%q) succeeded", b)
		}
		if err := c.Reopen(ctx, dir, b, "alex-app"); err == nil {
			t.Errorf("Reopen(%q) succeeded", b)
		}
		if err := c.Defer(ctx, dir, b, nil, "alex-app"); err == nil {
			t.Errorf("Defer(%q) succeeded", b)
		}
		if err := c.Dismiss(ctx, dir, b, "alex-app"); err == nil {
			t.Errorf("Dismiss(%q) succeeded", b)
		}
	}
	got, err := c.Show(ctx, dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Comments) != 0 || got.Status != "open" {
		t.Errorf("bead changed through a flag id: status %s, comments %+v", got.Status, got.Comments)
	}
	if _, err := os.Stat(profile); err == nil {
		t.Error("bd wrote a file named by a flag id")
	}

	// Flag-shaped text and values stay text.
	if err := c.AddComment(ctx, dir, id, "-falex", "--mem-profile="+profile); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx, dir, id, "--db="+profile, "--actor"); err != nil {
		t.Fatal(err)
	}
	got, _ = c.Show(ctx, dir, id)
	if len(got.Comments) != 1 || got.Comments[0].Text != "--mem-profile="+profile || got.Comments[0].Author != "-falex" ||
		got.CloseReason == nil || *got.CloseReason != "--db="+profile {
		t.Errorf("flag-shaped values: %+v", got)
	}
	if _, err := os.Stat(profile); err == nil {
		t.Error("bd wrote a file named in a value")
	}
}
