package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/yourfriendaaron/meat-mode/internal/api"
)

// hasRepo reports whether State lists the hub's repo name.
func hasRepo(st api.State, name string) bool {
	return slices.ContainsFunc(st.Repos, func(r api.Repo) bool { return r.Bridge == HubBridge && r.Name == name })
}

// listsProject reports whether Projects names name.
func listsProject(t *testing.T, s *Store, name string) bool {
	t.Helper()
	ps, err := s.Projects()
	must(t, err)
	return slices.ContainsFunc(ps, func(p api.Project) bool { return p.Name == name })
}

func deleteProject(t *testing.T, s *Store, repo string) api.Action {
	t.Helper()
	a, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindProjectDelete, Bridge: HubBridge, Repo: repo}, "alex-app")
	must(t, err)
	return a
}

// Deleting a project closes its open items in one action and hides it; the
// Undo reopens exactly those and shows it again.
func TestDeleteProjectAndUndo(t *testing.T) {
	s, c := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	water, _, err := s.AddPersonal(api.PersonalTodo{Title: "Water the plants"}, "laptop", garden)
	must(t, err)
	_, _, waterID, _ := SplitKey(water)
	soil, _, err := s.AddPersonal(api.PersonalTodo{Title: "Buy soil", Parent: waterID}, "laptop", garden)
	must(t, err)
	done, _, err := s.AddPersonal(api.PersonalTodo{Title: "Order seeds"}, "laptop", garden)
	must(t, err)
	_, _, doneID, _ := SplitKey(done)
	_, _, err = s.ChangePersonal("laptop", garden, doneID, api.PersonalChange{Kind: api.KindDone})
	must(t, err)
	c.tick()

	del := deleteProject(t, s, garden)
	_, _, soilID, _ := SplitKey(soil)
	if del.Status != api.StatusApplied || !slices.Equal(del.Payload.Items, []string{waterID, soilID}) {
		t.Fatalf("delete = %+v", del)
	}
	st := mustState(t, s)
	if hasRepo(st, garden) || listsProject(t, s, garden) {
		t.Fatal("a deleted project still shows")
	}
	// Its deleted items stay for the Deleted view; the done one goes with the list.
	for _, key := range []string{water, soil} {
		if it := item(t, st, key); it.Status != "closed" || it.CloseReason != ReasonDismissed {
			t.Errorf("%s after the delete: %+v", key, it)
		}
	}
	if hasItem(st, done) {
		t.Error("a done item of a deleted project still shows")
	}
	h, err := s.ItemHistory(water, 10)
	must(t, err)
	if len(h) == 0 || h[0].ID != del.ID {
		t.Errorf("history of %s = %+v", water, h)
	}

	c.tick()
	undo, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindProjectRestore, Bridge: HubBridge, Repo: garden,
		Payload: api.Payload{Items: del.Payload.Items}}, "alex-app")
	must(t, err)
	if !slices.Equal(undo.Payload.Items, del.Payload.Items) {
		t.Errorf("restore = %+v", undo)
	}
	st = mustState(t, s)
	if !hasRepo(st, garden) || !listsProject(t, s, garden) {
		t.Fatal("the Undo did not bring the project back")
	}
	for _, key := range []string{water, soil} {
		if it := item(t, st, key); it.Status != "open" {
			t.Errorf("%s after the Undo: %+v", key, it)
		}
	}
	if it := item(t, st, done); it.CloseReason != ReasonDone {
		t.Errorf("the done item after the Undo: %+v", it)
	}
	h, err = s.ItemHistory(soil, 10)
	must(t, err)
	if len(h) < 2 || h[0].ID != undo.ID || h[1].ID != del.ID {
		t.Errorf("history of %s = %+v", soil, h)
	}
}

// A deleted project shows again with an agent's next add to it, or when
// one of its items is restored from Deleted.
func TestDeletedProjectComesBack(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	water, _, err := s.AddPersonal(api.PersonalTodo{Title: "Water the plants"}, "laptop", garden)
	must(t, err)
	deleteProject(t, s, garden)
	added, _, err := s.AddPersonal(api.PersonalTodo{Title: "Pick a colour"}, "laptop", garden)
	must(t, err)
	st := mustState(t, s)
	if !hasRepo(st, garden) || !listsProject(t, s, garden) {
		t.Fatal("an add did not bring the project back")
	}
	if item(t, st, added).Status != "open" || item(t, st, water).Status != "closed" {
		t.Error("an add changed the deleted items")
	}

	deleteProject(t, s, garden)
	_, _, err = s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindReopen, Key: water}, "alex-app")
	must(t, err)
	st = mustState(t, s)
	if !hasRepo(st, garden) || item(t, st, water).Status != "open" || item(t, st, added).Status != "closed" {
		t.Error("restoring one item did not bring the project back with just that item")
	}
}

// Only a project can be deleted.
func TestDeleteProjectRefusesOtherLists(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	for name, na := range map[string]api.NewAction{
		"personal list": {Kind: api.KindProjectDelete, Bridge: HubBridge, Repo: PersonalRepo},
		"machine repo":  {Kind: api.KindProjectDelete, Bridge: "laptop", Repo: "garden"},
		"restore there": {Kind: api.KindProjectRestore, Bridge: "laptop", Repo: "garden"},
		"bad item id":   {Kind: api.KindProjectRestore, Bridge: HubBridge, Repo: garden, Payload: api.Payload{Items: []string{"-x"}}},
	} {
		na.ID = uuid.NewString()
		if _, _, err := s.AppendAction(na, "alex-app"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err := s.AppendAction(api.NewAction{ID: uuid.NewString(), Kind: api.KindProjectDelete, Bridge: HubBridge, Repo: "projects/none"}, "alex-app"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
}

// A project whose items were all done deletes with no items in its payload
// (applyProject records only the ids it closed), and the history of one of
// those items still reads through it.
func TestItemHistoryOfAProjectDeleteWithNoOpenItems(t *testing.T) {
	s, c := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	done, _, err := s.AddPersonal(api.PersonalTodo{Title: "Order seeds"}, "laptop", garden)
	must(t, err)
	_, _, doneID, _ := SplitKey(done)
	_, _, err = s.ChangePersonal("laptop", garden, doneID, api.PersonalChange{Kind: api.KindDone})
	must(t, err)
	c.tick()

	del := deleteProject(t, s, garden)
	if slices.Contains(del.Payload.Items, doneID) {
		t.Fatalf("the done item was recorded as deleted: %+v", del.Payload.Items)
	}
	h, err := s.ItemHistory(done, 10)
	if err != nil {
		t.Fatalf("ItemHistory of a done item of a deleted project: %v", err)
	}
	if len(h) == 0 {
		t.Fatal("the item's history is empty")
	}
}

// A restored hub keeps a deleted project deleted.
func TestRestoreKeepsAProjectDeleted(t *testing.T) {
	s, _ := open(t)
	_, err := s.CreateBridge("laptop")
	must(t, err)
	_, _, err = s.AddPersonal(api.PersonalTodo{Title: "Water the plants"}, "laptop", garden)
	must(t, err)
	deleteProject(t, s, garden)
	ex, err := s.Export()
	must(t, err)
	file, err := json.Marshal(ex)
	must(t, err)

	fresh, _ := open(t)
	_, err = fresh.Restore(bytes.NewReader(file))
	must(t, err)
	if hasRepo(mustState(t, fresh), garden) || listsProject(t, fresh, garden) {
		t.Error("a deleted project shows after a restore")
	}
}
