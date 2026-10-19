package store

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

func TestTagsOnHubItems(t *testing.T) {
	s, _ := open(t)
	a, _, err := s.AppendAction(api.NewAction{ID: id1, Kind: api.KindAdd, Bridge: HubBridge, Repo: PersonalRepo,
		Payload: api.Payload{Title: "Water the plants", Labels: []string{" garden ", "Errands", "errands", "human"}}}, "alex-app")
	must(t, err)
	key := hubKey(a.BeadID)
	if got := item(t, mustState(t, s), key).Labels; !slices.Equal(got, []string{"garden", "Errands"}) {
		t.Fatalf("tags after add = %q", got)
	}

	// An edit replaces the list and records the old one; its Undo restores it.
	e := edit(t, s, key, api.Edit{Labels: &[]string{"porch"}})
	if got := item(t, mustState(t, s), key).Labels; !slices.Equal(got, []string{"porch"}) {
		t.Fatalf("tags after edit = %q", got)
	}
	if w := e.Payload.Was; w == nil || w.Labels == nil || !slices.Equal(*w.Labels, []string{"garden", "Errands"}) {
		t.Fatalf("was = %+v", w)
	}
	clear := edit(t, s, key, *e.Payload.Was)
	if got := item(t, mustState(t, s), key).Labels; !slices.Equal(got, []string{"garden", "Errands"}) {
		t.Fatalf("tags after undo = %q", got)
	}
	if w := clear.Payload.Was; !slices.Equal(*w.Labels, []string{"porch"}) {
		t.Fatalf("undo's was = %+v", w)
	}

	// An empty list clears them, and its was holds the old list; clearing
	// an item with none records [] on the wire.
	cleared := edit(t, s, key, api.Edit{Labels: &[]string{}})
	if got := item(t, mustState(t, s), key).Labels; len(got) != 0 {
		t.Fatalf("tags after clearing = %q", got)
	}
	again := edit(t, s, key, api.Edit{Labels: &[]string{" "}})
	if raw := mustJSON(t, again.Payload.Was); !strings.Contains(raw, `"labels":[]`) {
		t.Errorf("was for no tags on the wire %s", raw)
	}
	_ = cleared

	// A tag that breaks a rule is refused.
	for _, bad := range []string{"two words", "a,b", strings.Repeat("a", api.MaxTagLen+1)} {
		_, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindEdit, Key: key,
			Payload: api.Payload{Set: &api.Edit{Labels: &[]string{bad}}}}, "alex-app")
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("edit with tag %q: %v, want invalid", bad, err)
		}
	}
}

func TestAddWithTags(t *testing.T) {
	s, _ := open(t)
	seed(t, s)

	// A project takes tags (through an agent's add); a tracker's repo is refused in plain words.
	pk, _, err := s.AddPersonal(api.PersonalTodo{ID: id1, Title: "Paint the fence", Labels: []string{"outdoors"}}, "laptop", "home")
	must(t, err)
	if got := item(t, mustState(t, s), pk).Labels; !slices.Equal(got, []string{"outdoors"}) {
		t.Fatalf("project add tags = %q", got)
	}
	_, _, err = s.AppendAction(api.NewAction{ID: id2, Kind: api.KindAdd, Bridge: "laptop", Repo: "garden",
		Payload: api.Payload{Title: "Water the plants", Labels: []string{"garden"}}}, "alex-app")
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "tags") {
		t.Fatalf("add with tags to a tracker's repo: %v", err)
	}

	// An agent's add sets them by the same rules.
	key, _, err := s.AddPersonal(api.PersonalTodo{ID: id3, Title: "Renew the library card", Labels: []string{"Admin", "admin", "later"}}, "laptop", "")
	must(t, err)
	if got := item(t, mustState(t, s), key).Labels; !slices.Equal(got, []string{"Admin", "later"}) {
		t.Fatalf("agent's add tags = %q", got)
	}
	_, _, err = s.AddPersonal(api.PersonalTodo{Title: "Renew the library card", Labels: []string{"two words"}}, "laptop", "")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("agent's add with a bad tag: %v", err)
	}

	// The agent's edit takes labels like any field.
	_, id := func() (string, string) { _, _, id, _ := SplitKey(key); return "", id }()
	_, _, err = s.ChangePersonal("laptop", "", id, api.PersonalChange{ID: uuid.NewString(), Kind: api.KindEdit,
		Payload: api.Payload{Set: &api.Edit{Labels: &[]string{"done-soon"}}}})
	must(t, err)
	if got := item(t, mustState(t, s), key).Labels; !slices.Equal(got, []string{"done-soon"}) {
		t.Fatalf("agent's edit tags = %q", got)
	}
}
