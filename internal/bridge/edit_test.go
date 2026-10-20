package bridge

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
)

const changeID = "7b2d3c9f-4e5a-4f60-8b1c-2d3e4f5a6b7c"

// pairedEnv is a machine paired with hub that has added one to-do, mm-1.
func pairedEnv(t *testing.T, hub *fakehub.Hub) (Env, *bytes.Buffer) {
	t.Helper()
	env, _, out := testEnv(t, "")
	if err := (Config{HubURL: hub.URL, Token: token}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}
	if err := Add(context.Background(), []string{"Renew the library card"}, env); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	return env, out
}

func TestEditChangesOnlyTheGivenFields(t *testing.T) {
	saved := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = saved })
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	hub.SetTimeZone("America/New_York")
	env, out := pairedEnv(t, hub)
	ctx := context.Background()

	args := []string{"mm-1", "--title", "Renew the card", "--description", "", "--due", "2026-03-02",
		"--priority", "0", "--author", "planner", "--id", changeID}
	if err := Edit(ctx, args, env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := strings.TrimSpace(out.String()); got != "mm-1" {
		t.Errorf("printed %q", got)
	}
	// Flags first, and the item's key in place of its id.
	if err := Edit(ctx, []string{"--clear-due", "hub/personal/mm-1"}, env); err != nil {
		t.Fatal(err)
	}
	cs := hub.Changes()
	if len(cs) != 2 {
		t.Fatalf("changes %+v", cs)
	}
	c := cs[0]
	set := c.Payload.Set
	// 9:00 in New York on 2 March 2026 (UTC-5) is 14:00 UTC.
	due := time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)
	if c.Item != "mm-1" || c.Bridge != us || c.Kind != api.KindEdit || c.ID != changeID || c.Author != "planner" || set == nil ||
		*set.Title != "Renew the card" || set.Description == nil || *set.Description != "" ||
		set.Due == nil || !set.Due.Equal(due) || set.ClearDue || *set.Priority != 0 {
		t.Fatalf("change %+v, set %+v", c, set)
	}
	set = cs[1].Payload.Set
	if cs[1].Item != "mm-1" || !set.ClearDue || set.Due != nil || set.Title != nil || set.Description != nil || set.Priority != nil {
		t.Fatalf("clear-due set %+v", set)
	}
}

func TestEditParent(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	env, out := pairedEnv(t, hub)
	ctx := context.Background()
	if err := Add(ctx, []string{"Find the old card"}, env); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Edit(ctx, []string{"mm-2", "--parent", "mm-1"}, env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if err := Edit(ctx, []string{"mm-2", "--no-parent"}, env); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "mm-2\nmm-2\n" {
		t.Errorf("printed %q", got)
	}
	cs := hub.Changes()
	if len(cs) != 2 {
		t.Fatalf("changes %+v", cs)
	}
	if set := cs[0].Payload.Set; set.Parent == nil || *set.Parent != "mm-1" || set.Title != nil || set.Due != nil {
		t.Fatalf("--parent set %+v", set)
	}
	if set := cs[1].Payload.Set; set.Parent == nil || *set.Parent != "" {
		t.Fatalf("--no-parent set %+v", set)
	}
	// A not found names both to-dos, since it may be for either.
	if err := Edit(ctx, []string{"mm-9", "--parent", "mm-1"}, env); err == nil || !strings.Contains(err.Error(), `"mm-9" or "mm-1"`) {
		t.Errorf("unknown item: %v", err)
	}
}

func TestDoneAndReopen(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	env, out := pairedEnv(t, hub)
	ctx := context.Background()
	if err := Done(ctx, []string{"mm-1", "--author", "planner"}, env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if err := Reopen(ctx, []string{"mm-1"}, env); err != nil {
		t.Fatal(err)
	}
	cs := hub.Changes()
	if len(cs) != 2 || cs[0].Kind != api.KindDone || cs[0].Author != "planner" || cs[1].Kind != api.KindReopen || cs[1].Item != "mm-1" {
		t.Fatalf("changes %+v", cs)
	}
	if got := out.String(); got != "mm-1\nmm-1\n" {
		t.Errorf("printed %q", got)
	}
}

func TestEditRefuses(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us, "token-sam": "laptop"})
	defer hub.Close()
	env, _ := pairedEnv(t, hub)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no id":          {[]string{"--title", "Renew"}, "id"},
		"two ids":        {[]string{"mm-1", "mm-2", "--title", "Renew"}, "id"},
		"no change":      {[]string{"mm-1"}, "at least one"},
		"due and clear":  {[]string{"mm-1", "--due", "2026-03-02", "--clear-due"}, "not both"},
		"bad due":        {[]string{"mm-1", "--due", "soon"}, "--due"},
		"empty title":    {[]string{"mm-1", "--title", " "}, "--title"},
		"priority 5":     {[]string{"mm-1", "--priority", "5"}, "--priority"},
		"parent and no":  {[]string{"mm-1", "--parent", "mm-2", "--no-parent"}, "not both"},
		"empty parent":   {[]string{"mm-1", "--parent", ""}, "--no-parent"},
		"someone else's": {[]string{"mm-2", "--title", "Renew"}, "this machine added"},
		"not a to-do id": {[]string{"gd-1", "--title", "Renew"}, "this machine added"},
	} {
		if err := Edit(ctx, tc.args, env); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	// mm-2 was added by another machine.
	other, _, _ := testEnv(t, "")
	if err := (Config{HubURL: hub.URL, Token: "token-sam"}).Save(other.Paths.Config); err != nil {
		t.Fatal(err)
	}
	if err := Add(ctx, []string{"Call Sam"}, other); err != nil {
		t.Fatal(err)
	}
	if err := Done(ctx, []string{"mm-2"}, env); err == nil || !strings.Contains(err.Error(), "this machine added") {
		t.Errorf("done on another machine's to-do: %v", err)
	}
	if err := Done(ctx, []string{}, env); err == nil {
		t.Error("done with no id")
	}
	if len(hub.Changes()) != 0 {
		t.Fatalf("a refused change reached the hub: %+v", hub.Changes())
	}
	unpaired, _, _ := testEnv(t, "")
	if err := Reopen(ctx, []string{"mm-1"}, unpaired); err == nil || !strings.Contains(err.Error(), "not paired") {
		t.Errorf("unpaired: %v", err)
	}
}
