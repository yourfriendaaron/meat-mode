package bridge

import (
	"context"
	"slices"
	"testing"

	"github.com/yourfriendaaron/meat-mode/internal/bridge/fakehub"
)

func TestAddAndEditTags(t *testing.T) {
	hub := fakehub.New(map[string]string{token: us})
	defer hub.Close()
	env, out := pairedEnv(t, hub) // mm-1 has no tags
	ctx := context.Background()

	if err := Add(ctx, []string{"Water the plants", "--tag", "garden", "--tag", " Errands "}, env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ps := hub.Personals()
	if got := ps[len(ps)-1].Labels; !slices.Equal(got, []string{"garden", "Errands"}) {
		t.Fatalf("added tags %q", got)
	}

	// mm-2 has two tags: --tag adds after them and skips a repeat without
	// case; --untag matches without case.
	if err := Edit(ctx, []string{"mm-2", "--tag", "porch", "--tag", "GARDEN", "--untag", "errands"}, env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cs := hub.Changes()
	if set := cs[0].Payload.Set; set == nil || set.Labels == nil || !slices.Equal(*set.Labels, []string{"garden", "porch"}) || set.Title != nil {
		t.Fatalf("edit set %+v", set)
	}
	// Removing the last tags sends an empty list, not nothing.
	if err := Edit(ctx, []string{"mm-2", "--untag", "Garden", "--untag", "porch"}, env); err != nil {
		t.Fatal(err)
	}
	if set := hub.Changes()[1].Payload.Set; set.Labels == nil || len(*set.Labels) != 0 {
		t.Fatalf("clearing set %+v", set)
	}
	// Tags go with other fields in one edit.
	if err := Edit(ctx, []string{"mm-1", "--title", "Renew the card", "--tag", "admin"}, env); err != nil {
		t.Fatal(err)
	}
	if set := hub.Changes()[2].Payload.Set; *set.Title != "Renew the card" || !slices.Equal(*set.Labels, []string{"admin"}) {
		t.Fatalf("combined set %+v", set)
	}

	for name, args := range map[string][]string{
		"same tag both ways": {"mm-1", "--tag", "Admin", "--untag", "admin"},
		"empty tag":          {"mm-1", "--tag", " "},
		"not ours":           {"mm-9", "--tag", "admin"},
	} {
		if err := Edit(ctx, args, env); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if n := len(hub.Changes()); n != 3 {
		t.Errorf("%d changes, a refused edit sent one", n)
	}
	if err := Add(ctx, []string{"Call Sam", "--tag", ""}, env); err == nil {
		t.Error("add with an empty tag: no error")
	}
}
