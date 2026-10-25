package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
)

// A machine lists, reads and extends a list another machine started.
func TestListShowAndExtendAnotherMachinesList(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us, "token-sam": "laptop"})
	defer hub.Close()
	hub.SetTimeZone("UTC")
	ctx := context.Background()
	other, _, _ := testEnv(t, "")
	if err := (Config{HubURL: hub.URL, Token: "token-sam"}).Save(other.Paths.Config); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"Shopping list", "--tag", "errands", "--due", "2026-03-02 10:00"},
		{"Water the plants"},
		{"Plant the beans", "--project", "projects/garden"},
	} {
		if err := Add(ctx, args, other); err != nil {
			t.Fatal(err)
		}
	}
	env, _, out := testEnv(t, "")
	errOut := &bytes.Buffer{}
	env.Stderr = errOut
	if err := (Config{HubURL: hub.URL, Token: token}).Save(env.Paths.Config); err != nil {
		t.Fatal(err)
	}

	if err := Add(ctx, []string{"Milk", "--parent", "mm-1"}, env); err != nil {
		t.Fatalf("sub-task under another machine's to-do: %v", err)
	}
	out.Reset()
	if err := List(ctx, nil, env); err != nil {
		t.Fatal(err)
	}
	want := "mm-1     Shopping list  due 2026-03-02 10:00  #errands\n  mm-4     Milk\nmm-2     Water the plants\n"
	if got := out.String(); got != want {
		t.Errorf("list printed\n%s\nwant\n%s", got, want)
	}

	out.Reset()
	if err := List(ctx, []string{"--search", "WATER"}, env); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "mm-2     Water the plants\n" {
		t.Errorf("search printed %q", got)
	}
	out.Reset()
	if err := List(ctx, []string{"--tag", "errands", "--json"}, env); err != nil {
		t.Fatal(err)
	}
	var items []api.Item
	if err := json.Unmarshal(out.Bytes(), &items); err != nil || len(items) != 1 || items[0].ID != "mm-1" {
		t.Fatalf("json %s: %v", out, err)
	}
	out.Reset()
	if err := List(ctx, []string{"--project", "projects/garden"}, env); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "mm-3     Plant the beans\n" {
		t.Errorf("project list printed %q", got)
	}
	out.Reset()
	errOut.Reset()
	if err := List(ctx, []string{"--closed"}, env); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "No to-dos") {
		t.Errorf("closed list printed %q, %q", out, errOut)
	}
	for _, args := range [][]string{{"--closed", "--all"}, {"plants"}, {"--project", "../x"}} {
		if err := List(ctx, args, env); err == nil {
			t.Errorf("list %v was taken", args)
		}
	}

	out.Reset()
	if err := Show(ctx, []string{"mm-1"}, env); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mm-1  Shopping list", "Status:    open", "Due:       2026-03-02 10:00 UTC", "Tags:      errands", "Added by:  laptop"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	if err := Show(ctx, []string{"mm-9"}, env); err == nil || !strings.Contains(err.Error(), `no to-do "mm-9" on this list`) {
		t.Errorf("show of an unknown id: %v", err)
	}

	// Done on the other machine's to-do moves it to the closed list.
	if err := Done(ctx, []string{"mm-2"}, env); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := List(ctx, []string{"--closed"}, env); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "mm-2     Water the plants  [done]\n" {
		t.Errorf("closed list printed %q", got)
	}

	out.Reset()
	if err := Projects(ctx, nil, env); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(strings.Fields(out.String()), " "); got != "projects/garden 1 open" {
		t.Errorf("projects printed %q", out)
	}
}
